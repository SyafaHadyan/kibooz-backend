#!/usr/bin/env bash
# Updates the API copies of a host one at a time, so the other copy keeps answering while one restarts.
#
# usage  ./rolling-update.sh <version>        for example ./rolling-update.sh 0.17.1
#
# What it needs on the host. Docker with the compose plugin (v2), curl, jq and flock. A compose file that defines one service
# for each copy, api-1 on port 8000 and api-2 on port 8001 by default. A reverse proxy that stops sending requests to a copy
# that is down, because that is what keeps the site up while a copy restarts. Run it in tmux or screen, so a dropped ssh
# connection does not stop it half way.
#
# What it does, in this order. It checks that the services exist, pulls the image of the version, which also proves that the tag
# exists, and only then sets IMAGE_TAG in the env file. Then for each copy in order it pulls through compose, recreates the one
# copy with no dependencies, and polls /healthz of that copy until the answer is status ok, every check ok or disabled, and the
# version that was asked for, twice in a row. It goes on to the next copy only when the one before is healthy. When a copy does not
# become healthy it stops, says which copies are already new and which are still old, and prints the command that goes back.
# Nothing is rolled back by itself. The new version may already have applied a migration, and an old image cannot start on a newer
# schema, so read the changelog before going back. A rolling update is only safe when the migrations of the new version leave the
# old version working, because both versions run on the same database for a short while.
#
# Settings, all optional, given as environment variables
#   COMPOSE_DIR   the folder with the compose file and the env file, default the folder of this script
#   ENV_FILE      the env file that holds IMAGE_TAG, default .env, and it is passed to every compose command
#   COPIES        the services and the host port of each, in update order, default "api-1:8000 api-2:8001"
#   IMAGE         the image that holds the version, default syafa/kibooz-backend
#   TIMEOUT       seconds to wait for one copy to become healthy, default 120
#   DRY_RUN       1, true or yes prints the steps and changes nothing, but it still checks the services
set -euo pipefail

die() {
  echo "$*" >&2
  exit 1
}

version="${1:?usage rolling-update.sh <version>, for example 0.17.1}"
version="${version#v}"
script="$(realpath "${BASH_SOURCE[0]}")"
compose_dir="${COMPOSE_DIR:-$(dirname "$script")}"
env_file="${ENV_FILE:-.env}"
copies="${COPIES:-api-1:8000 api-2:8001}"
image="${IMAGE:-syafa/kibooz-backend}"
timeout="${TIMEOUT:-120}"

case "${DRY_RUN:-0}" in
  1 | true | yes) dry_run=1 ;;
  0 | false | no | "") dry_run=0 ;;
  *) die "DRY_RUN must be 1, true, yes, 0, false or no, got ${DRY_RUN}" ;;
esac

for tool in docker curl jq flock; do
  command -v "$tool" >/dev/null || die "$tool is not installed"
done

[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "The version must look like 1.2.3, got $version"
[[ "$timeout" =~ ^[0-9]+$ ]] || die "TIMEOUT must be a number of seconds, got $timeout"

services=()
ports=()
for copy in $copies; do
  [[ "$copy" =~ ^([A-Za-z0-9_.-]+):([0-9]+)$ ]] || die "Every entry of COPIES must look like service:port, got $copy"
  [[ " ${services[*]:-} " != *" ${BASH_REMATCH[1]} "* ]] || die "The service ${BASH_REMATCH[1]} is listed twice in COPIES"
  [[ " ${ports[*]:-} " != *" ${BASH_REMATCH[2]} "* ]] || die "The port ${BASH_REMATCH[2]} is listed twice in COPIES"
  services+=("${BASH_REMATCH[1]}")
  ports+=("${BASH_REMATCH[2]}")
done
[ "${#services[@]}" -gt 0 ] || die "COPIES lists no service"

cd "$compose_dir"
[ -f "$env_file" ] || die "$env_file was not found in $compose_dir"

# only one update runs at a time, because two of them would fight over the env file and the same copy
exec 9>"$compose_dir/.rolling-update.lock"
flock -n 9 || die "Another update is running on this host"

compose() {
  docker compose --env-file "$env_file" "$@"
}

run() {
  if [ "$dry_run" = "1" ]; then
    echo "DRY RUN $*"
  else
    "$@"
  fi
}

# healthy prints the answer of one copy and succeeds only when it is the right version and nothing is wrong
healthy() {
  local port="$1" body
  body="$(curl -sS --max-time 5 "http://localhost:${port}/healthz" 2>/dev/null)" || return 1
  echo "$body" | jq -c . 2>/dev/null || return 1
  echo "$body" | jq -e --arg v "$version" \
    '.success == true and .status == "ok" and .version == $v and (.checks | type == "object") and (.checks | all(.[]; . == "ok" or . == "disabled"))' >/dev/null
}

# the tag of the env file as it is now, without quotes or a carriage return
previous="$(sed -n 's/^IMAGE_TAG=//p' "$env_file" | tail -1 | tr -d "\r\"'")"
changed=0
updated=()

on_signal() {
  echo >&2
  echo "Interrupted. IMAGE_TAG in $env_file is $([ "$changed" = "1" ] && echo "$version" || echo "${previous:-not set}"), copies updated so far ${updated[*]:-none}." >&2
  echo "Check the copies with docker compose ps and run the update again when they are fine." >&2
  exit 130
}
trap on_signal INT TERM HUP

echo "Checking that the services exist"
known="$(compose config --services)"
for service in "${services[@]}"; do
  grep -qx "$service" <<<"$known" || die "The service $service is not in the compose file, nothing was changed. The services are $(echo "$known" | tr '\n' ' ')"
done

echo "Pulling $image:$version, which also proves that the tag exists"
if [ "$dry_run" = "1" ]; then
  echo "DRY RUN docker pull $image:$version"
else
  docker pull "$image:$version" || die "$image:$version could not be pulled, so nothing was changed. The tag may not exist or Docker Hub refused the pull"
fi

echo "IMAGE_TAG was ${previous:-not set} and becomes $version"
if [ "$dry_run" = "1" ]; then
  echo "DRY RUN set IMAGE_TAG=$version in $env_file"
else
  if grep -q '^IMAGE_TAG=' "$env_file"; then
    sed -i --follow-symlinks "s/^IMAGE_TAG=.*/IMAGE_TAG=${version}/" "$env_file"
  else
    printf '\nIMAGE_TAG=%s\n' "$version" >>"$env_file"
  fi
  changed=1
fi

echo "Pulling through compose"
run compose pull "${services[@]}"

# go_back prints how to return to the version that ran before, when that version is known
go_back() {
  if [[ "$previous" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "To go back to $previous, run COMPOSE_DIR=$compose_dir ENV_FILE=$env_file COPIES=\"$copies\" $script $previous" >&2
  else
    echo "The tag before was ${previous:-not set}, so set IMAGE_TAG back by hand and run docker compose up -d --no-deps for each updated copy" >&2
  fi
}

for index in "${!services[@]}"; do
  service="${services[$index]}"
  port="${ports[$index]}"
  echo
  echo "Updating $service on port $port"
  run compose up -d --no-deps "$service"

  if [ "$dry_run" = "1" ]; then
    continue
  fi

  start="$SECONDS"
  good=0
  answer=""
  while [ "$good" -lt 2 ]; do
    if answer="$(healthy "$port")"; then
      good=$((good + 1))
    else
      good=0
    fi

    if [ "$good" -lt 2 ]; then
      if [ $((SECONDS - start)) -ge "$timeout" ]; then
        echo "$service did not become healthy within ${timeout} seconds, the last answer was ${answer:-none}" >&2
        rest="${services[*]:$((index + 1))}"
        echo "Copies already on $version are ${updated[*]:-none} and the ones still on the old version are ${rest:-none}. $service itself is on $version and not healthy" >&2
        echo "Read the logs with docker compose logs --tail 100 $service. A migration of $version may already have run, so read the changelog before going back" >&2
        go_back
        exit 1
      fi
      sleep 3
    fi
  done

  updated+=("$service")
  echo "$service is healthy after $((SECONDS - start)) seconds"
  echo "$answer"
done

echo
if [ "$dry_run" != "1" ]; then
  compose ps
fi
echo "Done. Run the Smoke workflow with expected_version ${version} to check the routes."
