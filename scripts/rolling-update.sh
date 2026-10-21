#!/usr/bin/env bash
# Updates the API copies of a host one at a time, so the other copy keeps answering while one restarts.
#
# usage  ./rolling-update.sh <version>        for example ./rolling-update.sh 0.17.1
#
# For every copy, in order, it does what a person does by hand. It sets IMAGE_TAG in the env file, checks that the tag exists on
# Docker Hub, pulls the image, recreates one copy with no dependencies, and polls /healthz of that copy until it answers
# with status ok, every check ok and the version that was asked for. It only goes on to the next copy when the one before is
# healthy. When a copy does not become healthy it stops, leaves the other copies on the old version, and prints the command that
# goes back to the previous version. Nothing is rolled back by itself, because a new version may already have applied a migration.
#
# Settings, all optional, given as environment variables
#   COMPOSE_DIR   the folder with compose.yml and the env file, default the folder of this script
#   ENV_FILE      the env file that holds IMAGE_TAG, default .env
#   COPIES        the services and the host port of each, in update order, default "api-1:8000 api-2:8001"
#   IMAGE         the image on Docker Hub, default syafa/kibooz-backend
#   TIMEOUT       seconds to wait for one copy to become healthy, default 120
#   DRY_RUN       set to 1 to print the steps and change nothing
set -euo pipefail

version="${1:?usage rolling-update.sh <version>, for example 0.17.1}"
version="${version#v}"
compose_dir="${COMPOSE_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)}"
env_file="${ENV_FILE:-.env}"
copies="${COPIES:-api-1:8000 api-2:8001}"
image="${IMAGE:-syafa/kibooz-backend}"
timeout="${TIMEOUT:-120}"
dry_run="${DRY_RUN:-0}"

for tool in docker curl jq; do
  command -v "$tool" > /dev/null || { echo "$tool is not installed" >&2; exit 1; }
done

if ! [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "The version must look like 1.2.3, got $version" >&2
  exit 1
fi

cd "$compose_dir"
[ -f "$env_file" ] || { echo "$env_file was not found in $compose_dir" >&2; exit 1; }

run() {
  if [ "$dry_run" = "1" ]; then
    echo "DRY RUN $*"
  else
    "$@"
  fi
}

# healthy prints the answer of one copy and succeeds only when it is the right version and everything works
healthy() {
  local port="$1" body
  body="$(curl -sS --max-time 5 "http://localhost:${port}/healthz" 2> /dev/null)" || return 1
  echo "$body" | jq -c . 2> /dev/null || return 1
  echo "$body" | jq -e --arg v "$version" \
    '.success == true and .status == "ok" and .version == $v and (.checks | all(.[]; . == "ok"))' > /dev/null
}

echo "Checking that $image:$version exists on Docker Hub"
curl -fsS --max-time 15 -o /dev/null "https://hub.docker.com/v2/repositories/${image}/tags/${version}" \
  || { echo "The tag $version does not exist for $image, so nothing was changed" >&2; exit 1; }

previous="$(sed -n 's/^IMAGE_TAG=//p' "$env_file" | tail -1)"
echo "IMAGE_TAG was ${previous:-not set} and becomes $version"

if [ "$dry_run" != "1" ]; then
  if grep -q '^IMAGE_TAG=' "$env_file"; then
    sed -i "s/^IMAGE_TAG=.*/IMAGE_TAG=${version}/" "$env_file"
  else
    printf '\nIMAGE_TAG=%s\n' "$version" >> "$env_file"
  fi
fi

services=""
for copy in $copies; do
  services="$services ${copy%%:*}"
done

echo "Pulling the image"
# shellcheck disable=SC2086
run docker compose pull $services

for copy in $copies; do
  service="${copy%%:*}"
  port="${copy##*:}"
  echo
  echo "Updating $service on port $port"
  run docker compose up -d --no-deps "$service"

  if [ "$dry_run" = "1" ]; then
    continue
  fi

  waited=0
  until answer="$(healthy "$port")"; do
    if [ "$waited" -ge "$timeout" ]; then
      echo "$service did not become healthy within ${timeout} seconds, the last answer was ${answer:-none}" >&2
      echo "The other copies are still on the old version. To go back, run ${0} ${previous:-<previous version>}" >&2
      echo "Read the logs with docker compose logs --tail 100 $service" >&2
      exit 1
    fi
    sleep 3
    waited=$((waited + 3))
  done

  echo "$service is healthy after ${waited} seconds"
  echo "$answer"
done

echo
if [ "$dry_run" != "1" ]; then
  docker compose ps
fi
echo "Done. Run the Smoke workflow with expected_version ${version} to check the routes."
