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
# exists, and only then sets IMAGE_TAG in the env file. It pulls through compose once for all copies, and when that fails it puts the env file back as it was
# and stops before any copy is touched, and so does an interrupt before the first copy. Then for each copy in order it recreates the one
# copy with no dependencies, and polls /healthz of that copy until the answer is status ok, every check ok or disabled, and the
# version that was asked for, twice in a row. It goes on to the next copy only when the one before is healthy. When a copy does not
# become healthy it stops, says which copies are already new and which are still old, and prints the command that goes back.
# Going back to an older version is allowed, with a warning, see below. Nothing is rolled back by itself. The new version may already have applied a migration, and an old image cannot start on a newer
# schema, so read the changelog before going back. A rolling update is only safe when the migrations of the new version leave the
# old version working, because both versions run on the same database for a short while.
#
# Extra services. Services that run the same image but are not part of the rolling update, such as the bucket size exporter, are
# named in EXTRAS. They are pulled with the copies, and recreated one after another only when every copy is healthy, with the same
# wait, so a failed extra never stops or undoes the update of the API. When an extra does not become healthy the script says so
# at the end and exits with 1, and the copies stay on the new version. Name only services of the compose file that use the image
# of IMAGE_TAG, and whose profile is in COMPOSE_PROFILES of the env file, or the script stops before anything is changed.
#
# Going back. When the version is older than the one that runs now, the script warns that this is only safe while no migration ran
# since that version, and lists the migration files of both versions on GitHub. When the newer one has files that the older one lacks, or GitHub cannot
# answer, it stops, because the old image cannot start on the newer schema. Restore the database or apply the down files first, see docs/operations.md, and then run
# it again with MIGRATIONS_HANDLED=1. When there are none, or GitHub cannot be asked, it asks you to type the version to go on, or
# to set ROLLBACK_OK to it when there is no terminal. GITHUB_TOKEN, when set, is sent with the GitHub requests to lift the limit.
#
# Settings, all optional, given as environment variables
#   COMPOSE_DIR   the folder with the compose file and the env file, default the folder of this script
#   ENV_FILE      the env file that holds IMAGE_TAG, default .env, and it is passed to every compose command
#   COPIES        the services and the host port of each, in update order, default "api-1:8000 api-2:8001"
#   EXTRAS        services that run the same image and are recreated after all copies are healthy, default none, for example "bucket-sizes"
#   IMAGE         the image that holds the version, default syafa/kibooz-backend
#   TIMEOUT       seconds to wait for one copy to become healthy, default 120
#   REPO          the GitHub repository that the migration check asks, default SyafaHadyan/kibooz-backend
#   GITHUB_TOKEN  optional token for the migration check, the anonymous limit is 60 requests an hour for each address
#   ROLLBACK_OK   the version that you confirm to go back to when there is no terminal
#   MIGRATIONS_HANDLED  1 when the database is ready for the older version, after a restore, the down files or a check by hand
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
repo="${REPO:-SyafaHadyan/kibooz-backend}"

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

extras_text="${EXTRAS:-}"
extras=()
for extra in $extras_text; do
  [[ "$extra" =~ ^[A-Za-z0-9_.-]+$ ]] || die "Every entry of EXTRAS must be a service name, got $extra"
  [[ " ${services[*]} " != *" $extra "* ]] || die "The service $extra is in COPIES and in EXTRAS"
  [[ " ${extras[*]:-} " != *" $extra "* ]] || die "The service $extra is listed twice in EXTRAS"
  extras+=("$extra")
done

cd "$compose_dir"
[ -f "$env_file" ] || die "$env_file was not found in $compose_dir"

# only one update runs at a time, because two of them would fight over the env file and the same copy
exec 9>"$compose_dir/.rolling-update.lock"
flock -n 9 || die "Another update is running on this host"

# IMAGE_TAG is set for every compose command, because a value that is exported in the shell of the operator would otherwise win
# over the env file and the copies would start on another tag than the one that was pulled and checked
compose() {
  IMAGE_TAG="$version" docker compose --env-file "$env_file" "$@"
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
# 1 from the moment the first copy is recreated, because from then on a copy may run the new version
copy_started=0
# the env file as it is now, byte for byte. The x keeps the newlines at the end, and it is cut off again
env_original="$(
  cat "$env_file"
  printf x
)"
env_original="${env_original%x}"

# restore_tag puts the env file back as it was, for a step that fails before any copy is touched. Without it the env file would
# name a version that no copy runs, and a later docker compose up -d would recreate every copy on it at once. The whole file
# is written back and not edited, so no tag with a special character can break it and nothing is left behind
restore_tag() {
  [ "$changed" = "1" ] || return 0

  if printf '%s' "$env_original" >"$env_file"; then
    changed=0
    return 0
  fi

  echo "IMAGE_TAG in $env_file could not be put back, set it to ${previous:-not set} by hand before anything runs docker compose up -d" >&2
  return 1
}

on_signal() {
  echo >&2
  if [ "$copy_started" = "0" ]; then
    restore_tag || true
  fi
  echo "Interrupted. IMAGE_TAG in $env_file is $([ "$changed" = "1" ] && echo "$version" || echo "${previous:-not set}"), copies updated so far ${updated[*]:-none}." >&2
  echo "Check the copies with docker compose ps and run the update again when they are fine." >&2
  exit 130
}
trap on_signal INT TERM HUP

echo "Checking that the services exist"
known="$(compose config --services)"
for service in "${services[@]}" ${extras[@]+"${extras[@]}"}; do
  grep -qx "$service" <<<"$known" || die "The service $service is not in the compose file or its profile is not in COMPOSE_PROFILES of $env_file, nothing was changed. The services are $(echo "$known" | tr '\n' ' ')"
done

echo "Pulling $image:$version, which also proves that the tag exists"
if [ "$dry_run" = "1" ]; then
  echo "DRY RUN docker pull $image:$version"
else
  docker pull "$image:$version" || die "$image:$version could not be pulled, so nothing was changed. The tag may not exist or Docker Hub refused the pull"
fi

# migration_names prints the up files of the migrations folder at one tag, and fails when GitHub does not give a list. Listing the
# folder of both tags, and not the compare answer, avoids the cut at 300 files and renamed files
migration_names() {
  local auth=() body
  [ -z "${GITHUB_TOKEN:-}" ] || auth=(-H "Authorization: Bearer ${GITHUB_TOKEN}")
  body="$(curl -fsSL --max-time 15 ${auth[@]+"${auth[@]}"} "https://api.github.com/repos/${repo}/contents/internal/infra/db/migrations?ref=v$1")" || return 1
  jq -er 'if type == "array" then [.[] | select(.name | test("[.]up[.]sql$")) | .name] | sort | .[] else error("not a list") end' <<<"$body"
}

# the version that runs now is the newest one that any copy reports, because after an update that stopped half way the copies run
# different versions. When no copy answers it is the tag of the env file
semver='^[0-9]+\.[0-9]+\.[0-9]+$'
running=""
for port in "${ports[@]}"; do
  seen="$(curl -sS --max-time 5 "http://localhost:${port}/healthz" 2>/dev/null | jq -r '.version // empty' 2>/dev/null || true)"
  [[ "$seen" =~ $semver ]] || continue
  if [ -z "$running" ] || [ "$(printf '%s\n%s\n' "$seen" "$running" | sort -V | tail -n 1)" = "$seen" ]; then
    running="$seen"
  fi
done
[[ "$running" =~ $semver ]] || running="$previous"
[[ "$running" =~ $semver ]] || running=""

if [ -z "$running" ]; then
  echo "The version that runs now is unknown (IMAGE_TAG is ${previous:-not set}), so this script cannot tell whether $version is a step back." >&2
  echo "If it is, read Roll back in docs/operations.md first, because an older image cannot start on a newer schema." >&2
elif [ "$running" != "$version" ] &&
  [ "$(printf '%s\n%s\n' "$version" "$running" | sort -V | head -n 1)" = "$version" ]; then
  echo
  echo "WARNING. This goes back from $running to $version." >&2
  echo "It is only safe while no migration ran since $version, because an older image cannot start on a newer schema." >&2
  echo "Take a dump of the database first, see Roll back in docs/operations.md." >&2

  if old_list="$(migration_names "$version")" && new_list="$(migration_names "$running")"; then
    added="$(comm -13 <(echo "$old_list") <(echo "$new_list"))"
    if [ -n "$added" ]; then
      echo "These migrations were added after $version and may have run on this database" >&2
      while IFS= read -r name; do
        echo "  $name" >&2
      done <<<"$added"
      blocked="the migrations above may have run"
    else
      echo "The migrations folder of $version and of $running holds the same files, so the older image should start" >&2
      blocked=""
    fi
  else
    echo "GitHub could not list the migrations of $version and $running, so one may exist" >&2
    blocked="the migrations could not be checked"
  fi

  if [ -n "$blocked" ]; then
    echo "To check by hand, compare the number in schema_migrations with the newest file of $version, for example" >&2
    echo "  docker compose exec db sh -c 'psql -U \"\$POSTGRES_USER\" \"\$POSTGRES_DB\" -c \"select version, dirty from schema_migrations\"'" >&2
    if [ "$dry_run" = "1" ]; then
      echo "DRY RUN a real run stops here until MIGRATIONS_HANDLED=1 is set"
    elif [ "${MIGRATIONS_HANDLED:-0}" != "1" ]; then
      die "Stopped because $blocked. Restore the database from a backup, apply the down files, or confirm that the database is not past the newest migration of $version, then run this again with MIGRATIONS_HANDLED=1. Nothing was changed"
    fi
  fi

  if [ "$dry_run" = "1" ]; then
    echo "DRY RUN a real run asks you to type $version to confirm"
  elif confirmed="${ROLLBACK_OK:-}" && [ "${confirmed#v}" != "$version" ]; then
    [ -t 0 ] || die "Set ROLLBACK_OK=$version to go back without a terminal. Nothing was changed"
    read -r -p "Type $version to go back to it, anything else cancels: " answer || die "Cancelled, nothing was changed"
    [ "$answer" = "$version" ] || die "Cancelled, nothing was changed"
  fi
elif [ "$running" = "$version" ]; then
  echo "$version already runs, the copies are recreated one at a time anyway"
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
if ! run compose pull "${services[@]}" ${extras[@]+"${extras[@]}"}; then
  restore_tag || die "The pull through compose failed and IMAGE_TAG in $env_file could not be put back, see above. No copy was touched"
  die "The pull through compose failed, for example because Docker Hub limited it. IMAGE_TAG in $env_file is ${previous:-not set} again and no copy was touched, so run the update again later"
fi

# go_back prints how to return to the version that ran before, when that version is known
go_back() {
  if [[ "$previous" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "To go back to $previous, run ROLLBACK_OK=$previous COMPOSE_DIR=$compose_dir ENV_FILE=$env_file COPIES=\"$copies\" ${extras_text:+EXTRAS=\"$extras_text\" }$script $previous, which warns and checks the migrations first. It stops when a migration may have run, and then MIGRATIONS_HANDLED=1 is needed after you restored the database or checked it" >&2
  else
    echo "The tag before was ${previous:-not set}, so set IMAGE_TAG back by hand and run docker compose up -d --no-deps for each updated copy" >&2
  fi
}

for index in "${!services[@]}"; do
  service="${services[$index]}"
  port="${ports[$index]}"
  echo
  echo "Updating $service on port $port"
  copy_started=1
  if ! run compose up -d --no-deps "$service"; then
    echo "docker compose up failed for $service, so it may be stopped, and it was not checked" >&2
    echo "Copies already on $version are ${updated[*]:-none} and the ones still on the old version are ${services[*]:$((index + 1))}" >&2
    echo "IMAGE_TAG in $env_file names $version, so do not run docker compose up -d before the copies are checked, because it would recreate every copy on $version" >&2
    go_back
    exit 1
  fi

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
        echo "IMAGE_TAG in $env_file names $version, so do not run docker compose up -d before the copies are checked, because it would recreate every copy on $version" >&2
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

# extra_state prints healthy when the container of a service reports healthy, running when it has no health check and is up, and the
# state of the health check or missing otherwise
extra_state() {
  local id
  id="$(compose ps -q "$1" 2>/dev/null | head -n 1)"
  [ -n "$id" ] || {
    echo missing
    return 0
  }
  docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$id" 2>/dev/null || echo missing
}

# the extras come after every copy is healthy, and a failure of one is reported at the end and never undoes the copies
extra_failed=()
for extra in ${extras[@]+"${extras[@]}"}; do
  echo
  echo "Updating $extra, which runs the same image and is not part of the rolling update"
  if ! run compose up -d --no-deps "$extra"; then
    echo "docker compose up failed for $extra" >&2
    extra_failed+=("$extra")
    continue
  fi

  if [ "$dry_run" = "1" ]; then
    continue
  fi

  start="$SECONDS"
  state=""
  while :; do
    state="$(extra_state "$extra")"
    case "$state" in
      healthy | running) break ;;
    esac
    [ $((SECONDS - start)) -lt "$timeout" ] || break
    sleep 3
  done

  case "$state" in
    healthy | running) echo "$extra is $state after $((SECONDS - start)) seconds" ;;
    *)
      echo "$extra did not become healthy within ${timeout} seconds, its state is $state" >&2
      extra_failed+=("$extra")
      ;;
  esac
done

echo
if [ "$dry_run" != "1" ]; then
  compose ps
fi
if [ "${#extra_failed[@]}" -gt 0 ]; then
  echo "The copies ${updated[*]:-none} are on $version and healthy, but ${extra_failed[*]} did not come up. Nothing was rolled back, the API is not affected." >&2
  echo "Read the logs with docker compose logs --tail 100 ${extra_failed[0]}, and bring it up again with docker compose up -d ${extra_failed[0]} when it is fixed." >&2
  exit 1
fi
echo "Done. Run the Smoke workflow with expected_version ${version} to check the routes."
