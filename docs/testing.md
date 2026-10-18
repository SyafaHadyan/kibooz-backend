# Testing

```sh
go test ./...                 # unit tests, end to end tests are skipped
```

The end to end suite drives the real HTTP stack against PostgreSQL and Redis, with an in-process mock for object storage. Point it at a disposable PostgreSQL and Redis and enable it explicitly, because the tests create data and empty the logical Redis database in `REDIS_DATABASE`. The suite refuses to run unless `DB_NAME` ends in `_test`, and `E2E_ALLOW_ANY_DB=true` overrides that for a database that you can lose.

```sh
E2E_ENABLED=true DB_NAME=kibooz_test DB_USERNAME=kibooz DB_PASSWORD=... go test ./... -race -count=1
```

Every response that suite sees is also checked against `openapi.yaml`, so a field, status code or error that the code changes without the spec fails the build. An object in a response also fails the check when it carries a field that the spec does not list, so a handler cannot start returning an internal field unnoticed. `TestSpecMatchesRoutes` fails when an endpoint exists in only one of the code and the spec. When the whole end to end suite passes, a final check also fails the run if a documented operation never had a single response checked, or if a documented success response was never seen, because such an operation would be documented without anything to keep it true. That check is skipped for a filtered run with `-run` or `-skip` and when `E2E_ENABLED` is not set. All of these run inside the required `Unit and end to end tests` job, so there is no extra CI job.

Run the linter with `golangci-lint run` (configuration in `.golangci.yml`).

CI uploads the coverage of the unit and end to end tests to [Codecov](https://codecov.io/gh/SyafaHadyan/kibooz-backend), which comments on pull requests that change it. `codecov/patch` is a required check, so new code needs coverage. Codecov posts it a few minutes after CI finishes, so a merge has to wait for it. `codecov/project` is not required. Dependabot pull requests upload too, using a `CODECOV_TOKEN` stored in the Dependabot secrets. The thresholds are in `codecov.yml`.

A unit test in `internal/infra/db` checks the names of the migration files. Every number must have exactly one up and one down file and the numbers must run from `000001` without a gap, so when two branches add the same next number, the one that is merged second fails its pull request and has to be renumbered. This runs in the existing unit test job, so it needs no required check of its own.

The code that takes untrusted input has fuzz tests, namely the check of uploaded images, access token validation and request body validation. A normal `go test` runs their seed cases. To search for new failing inputs, fuzz one target at a time.

```sh
go test ./internal/imageutil/ -run '^$' -fuzz FuzzDecodeBase64 -fuzztime 30s
```

## Performance tests

The `k6/` folder holds the load scripts. `smoke.js` is a short check that runs on every pull request, and `load.js` holds a steady arrival rate below the ceiling to measure how the API behaves with a whole class claiming points at once, and `stress.js` ramps past the ceiling on demand to find where the API slows down. `spike.js` sends a sudden burst above the ceiling and checks that the API recovers afterwards, and `soak.js` holds a steady load for 30 minutes to catch leaks, so its summary also lists the memory of every container at the start and at the end. All of them create their own accounts, so they only need a running API. The default rate limits and the daily claim limit would answer with errors during a load test, so the workflow raises them in its generated `.env`.

```sh
# set USER_LIMITER_MAX, AUTH_LIMITER_MAX, CLASS_CODE_LIMITER_MAX and TRASH_DAILY_LIMIT to large values in .env first
docker compose -f compose.yml -f compose.ci.yml up -d --build --wait
k6 run k6/smoke.js
```

The latency limits in the scripts are placeholders that were set from the first runs on a GitHub runner, so raise them when you move to slower hardware. Never point the scripts at a deployment you do not own, because they create accounts and write data.

## Security scan with ZAP

The `dast.yaml` workflow starts the compose stack and runs the ZAP baseline scan, which is a passive scan that only reads responses and never attacks. The API is plain JSON without links, so the spider finds almost nothing by itself. `.zap/seed.sh` therefore registers a teacher and a parent with throwaway credentials and `.zap/hook.py` replays a dozen requests through ZAP, including authenticated, rejected and unknown routes. Every warning fails the build. A rule that does not apply can be set to `IGNORE` with a reason in `.zap/rules.tsv`. The report is uploaded as the `zap-reports` artifact.

The scan is passive, so it checks headers and information leaks but does not try injections. The ZAP image is pinned by digest and has to be bumped by hand in the workflow, because Dependabot does not read images inside shell steps.

The `dast-active.yaml` workflow runs a ZAP active scan, which does attack the API with injection and fuzzing payloads. It runs weekly on Sunday evening UTC and on demand from the Actions tab, and never on pull requests because it is slow and noisy. It reuses `.zap/seed.sh` for the throwaway accounts and replays the requests of the baseline scan plus a longer list with `ZAP_FULL=true`, which covers registration, token refresh and logout, the class pages, videos, the forum, the teacher profile, guidance, trash claims with a photo and an account deletion with a wrong password. The baseline scan does not get the longer list, because it fails the build on any warning. The upload of a photo file is not in the list, because a replayed request cannot carry the bytes of an image, so the photo check is only reached through the base64 photo of a trash claim. The replay uses an Automation Framework plan, `.zap/active.yaml`, which also caps the scan at 30 minutes. The run summary lists the alerts, how many requests the API answered by status code, which routes answered with a server error and the errors that the API logged. A server error other than the `503` of the video upload address raises a warning, and that `503` is expected because the scan stack has no object storage. The reports are uploaded as the `zap-active-reports` artifact. For now it only reports. Medium and high alerts show up as annotations and the run stays green, and it can become a gate for high alerts once a few runs have stayed quiet. The image digest is the same one as the baseline scan, so bump both together.

## Smoke test of production

The `Smoke` workflow runs `smoke/smoke.py` against the API that is deployed, and it is started by hand from the Actions tab after a deploy. It is not part of the pull request checks, because it tests the running service and not the code of a branch. The end to end suite above covers the code, and the smoke test covers what only the real deployment has, which is the proxy, TLS, the database, Redis, the bucket and the version that is running.

Give it the version that `/healthz` has to report. The test registers throwaway teachers and parents, replays the main routes and the refusals, uploads a small file through a signed address and discards it, and then deletes its accounts. One keeper teacher stays, because deleting the last teacher of a class that has children needs a successor. The first run registers the keeper when it does not exist yet.

The keeper email is the variable `SMOKE_KEEPER_EMAIL` and its password is the secret `SMOKE_KEEPER_PASSWORD`, both in the `smoke-production` environment. That environment only accepts runs from `main`, so a workflow edited on another branch cannot read the password. Add a check to the script in the same change that adds or changes a route.

To run it from a terminal, set `BASE_URL`, `EXPECTED_VERSION`, `SMOKE_KEEPER_EMAIL` and `SMOKE_KEEPER_PASSWORD` and run `python3 -I smoke/smoke.py`. It talks to the API with curl and exits with an error when a check fails.

### The same script on a throwaway stack

The `Smoke stack` workflow runs the same script on every pull request, against a stack that it builds from the branch with `compose.yml` and `compose.ci.yml`. It checks the container of the pull request as a whole before the merge, with a real PostgreSQL and Redis and the default rate limits, while the `Smoke` workflow checks the live deployment after the deploy. The stack has no bucket, so the workflow sets `SMOKE_STORAGE=off`. The script then expects `healthz` to report storage as `disabled`, and `STORAGE_UNAVAILABLE` from the avatar and the video upload routes, and it skips the checks that need a real file in a bucket. Plain `http` is accepted only for `127.0.0.1`, and the keeper teacher gets a random password that only lives in that job. It is not a required check yet, so a failure is a signal to read and not a block.
