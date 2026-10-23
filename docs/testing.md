# Testing

```sh
go test ./...                 # unit tests, end to end tests are skipped
```

The end to end suite drives the real HTTP stack against PostgreSQL and Redis, with an in-process mock for object storage. Point it at a disposable PostgreSQL and Redis and enable it explicitly, because the tests create data and empty the logical Redis database in `REDIS_DATABASE`. The suite refuses to run unless `DB_NAME` ends in `_test`, and `E2E_ALLOW_ANY_DB=true` overrides that for a database that you can lose.

```sh
E2E_ENABLED=true DB_NAME=kibooz_test DB_USERNAME=kibooz DB_PASSWORD=... go test ./... -race -count=1
```

The suite sets `PASSWORD_HASH_COST` to 4, the lowest cost of bcrypt, because it signs up hundreds of accounts and bcrypt at the default cost under the race detector was nearly all of its running time. The unit tests of `internal/infra/passwords` still check that the default cost is 10. The CI step runs `go test` with `-timeout 15m` and not the default of 10 minutes, so the end to end suite has room to grow, and a hung test still ends with a dump of the goroutines before the job limit of 20 minutes cuts it off.

Every response that suite sees is also checked against `openapi.yaml`, so a field, status code or error that the code changes without the spec fails the build. An object in a response also fails the check when it carries a field that the spec does not list, so a handler cannot start returning an internal field unnoticed. `TestSpecMatchesRoutes` fails when an endpoint exists in only one of the code and the spec. When the whole end to end suite passes, a final check also fails the run if a documented operation never had a single response checked, or if a documented success response was never seen, because such an operation would be documented without anything to keep it true. That check is skipped for a filtered run with `-run` or `-skip` and when `E2E_ENABLED` is not set. All of these run inside the required `Unit and end to end tests` job, so there is no extra CI job.

Run the linter with `golangci-lint run` (configuration in `.golangci.yml`).

CI uploads the coverage of the unit and end to end tests to [Codecov](https://codecov.io/gh/SyafaHadyan/kibooz-backend), which comments on pull requests that change it. `codecov/patch` is a required check, so new code needs coverage. Codecov posts it a few minutes after CI finishes, so a merge has to wait for it. `codecov/project` is not required. The thresholds are in `codecov.yml`.

A unit test in `internal/infra/db` checks the names of the migration files. Every number must have exactly one up and one down file and the numbers must run from `000001` without a gap, so when two branches add the same next number, the one that is merged second fails its pull request and has to be renumbered. This runs in the existing unit test job, so it needs no required check of its own.

The code that takes untrusted input has fuzz tests, namely the check of uploaded images, access token validation and request body validation. A normal `go test` runs their seed cases. To search for new failing inputs, fuzz one target at a time.

```sh
go test ./internal/imageutil/ -run '^$' -fuzz FuzzDecodeBase64 -fuzztime 30s
```

The `Fuzz` workflow does the same for all four targets every Saturday, three minutes each, and can be started by hand with another `fuzztime`. When a target crashes, it uploads the input as an artifact, and that file goes into `testdata/fuzz/<FuzzTargetName>/` of its package, for example `internal/imageutil/testdata/fuzz/FuzzInspect/<file>`, so a normal `go test` replays it from then on.

## Benchmarks

The hot paths that run on every request or every message have Go benchmarks, in files named `bench_test.go` next to the code. They cover the image check, the base64 decode, the signing and checking of access tokens, the request validation, the common password check and the delivery of one event to 200 chat sockets. Run one package with `go test ./internal/infra/jwt/ -run '^$' -bench . -benchmem`. A benchmark should keep its name and its meaning, because the `Benchmarks` workflow compares the head of main with the commit of a week before it, with `benchstat`, every Wednesday and when started by hand, and shows the comparison in the summary of the run. It never fails the run, because a shared runner is too noisy for that, so a number that moved by a lot is a reason to look and not a verdict.

## Mutation testing

The `Mutation testing` workflow changes the code of a package a little at a time with [Gremlins](https://github.com/go-gremlins/gremlins), a `<` that becomes `<=` or a `+` that becomes `-`, and runs the tests of that package after each change. A change that no test notices is a mutant that lived, and it points at a line that the tests run without checking. It runs every Sunday and by hand on the packages that have thorough unit tests of their own, and the list is in the workflow. A package that is tested only through the end to end tests does not belong there, because the tool runs the tests of the package alone and would call every mutant a survivor. A manual run takes other packages in its `packages` input, for example `./internal/pagination`. The report is in the summary of each job and as a JSON artifact, and it never gates a merge. It is a prompt to add a test, and only a person decides which survivors matter.

## Query plans at scale

The `Explain plans` workflow answers how every query behaves on a database of the size that the service may grow to. It runs on a pull request that changes code that talks to the database, as the required check `Query plans`, with a seed of 500 classes and a cap of 10 seconds for a call, and a person can start the full size from the Actions tab. It seeds a throwaway PostgreSQL 18 on the runner with about 200000 accounts, 4000 classes of 49 children, a chat between each second child's parent and the teacher with about a million messages, and rows in every other table, such as moods, trash scans, forum posts, videos, the leaderboard history, sessions, push tokens, the file cleanup queue and idempotency keys. Then it calls every method of every repository that reads, the writes whose surrounding statements grow with the data, and the jobs that run on their own, which are the three sweepers and the semester reset, once in the middle of a semester and once at the start of one. It writes the plan of every statement to an artifact, first `EXPLAIN` alone and then `EXPLAIN (ANALYZE, BUFFERS)` for a select that can be run again without a side effect. A statement that changes rows or takes a lock is only explained, because it runs on a connection of its own while the transaction of the caller is still open. A call that needs more than 3 minutes is cut off, the run goes on and ends red, so a query that is too slow to run still leaves its plan, and the file is written as the plans come. The times are those of a shared runner and not of the VPS, so the plans are the point and not the milliseconds, for example a sequential scan that should be an index scan. The code is `internal/explain` with the build tag `explain`, which a normal `go test ./...` skips. Never point it at a real database, because it migrates and fills the database that `DB_NAME` names.

## Performance tests

The `k6/` folder holds the load scripts. `smoke.js` is a short check that runs on every pull request, and `load.js` holds a steady arrival rate below the ceiling to measure how the API behaves with a whole class claiming points at once, and `stress.js` ramps past the ceiling on demand to find where the API slows down. `spike.js` sends a sudden burst above the ceiling and checks that the API recovers afterwards, and `soak.js` holds a steady load for 30 minutes to catch leaks, so its summary also lists the memory of every container at the start and at the end. All of them create their own accounts, so they only need a running API. The default rate limits and the daily claim limit would answer with errors during a load test, so the workflow raises them in its generated `.env`. `chat.js` holds 15 parent sockets open for 55 seconds while the teacher writes to the class room once a second, and it measures how long a message takes to reach a socket (`chat_delivery_ms`), whether any socket closes early and whether the frames arrive at all (`chat_message_frames`). It is not part of the pull request checks, so start it from the Actions tab with the script `chat`.

```sh
# set USER_LIMITER_MAX, AUTH_LIMITER_MAX, CLASS_CODE_LIMITER_MAX and TRASH_DAILY_LIMIT to large values in .env first
# compose.ci.yml takes the Go image of the Dockerfile from the Google mirror, so it needs the name of that image
export GO_IMAGE=$(grep -oE 'golang:[0-9][^ "]*@sha256:[0-9a-f]{64}' Dockerfile | head -1)
docker compose -f compose.yml -f compose.ci.yml up -d --build --wait
k6 run k6/smoke.js
```

The latency limits in the scripts are placeholders that were set from the first runs on a GitHub runner, so raise them when you move to slower hardware. Never point the scripts at a deployment you do not own, because they create accounts and write data.

## Security scan with ZAP

The `dast.yaml` workflow starts the compose stack and runs the ZAP baseline scan, which is a passive scan that only reads responses and never attacks. The API is plain JSON without links, so the spider finds almost nothing by itself. `.zap/seed.sh` therefore registers a teacher and a parent with throwaway credentials and `.zap/hook.py` replays a dozen requests through ZAP, including authenticated, rejected and unknown routes. Every warning fails the build. A rule that does not apply can be set to `IGNORE` with a reason in `.zap/rules.tsv`. The report is uploaded as the `zap-reports` artifact.

The scan is passive, so it checks headers and information leaks but does not try injections. The ZAP image is pinned by digest in `.github/tools/Dockerfile`, where the update bot can bump it, and the workflow reads it with `.github/tools/image.sh zaproxy`.

The `dast-active.yaml` workflow runs a ZAP active scan, which does attack the API with injection and fuzzing payloads. It runs weekly on Sunday evening UTC and on demand from the Actions tab, and never on pull requests because it is slow and noisy. It reuses `.zap/seed.sh` for the throwaway accounts and replays the requests of the baseline scan plus a longer list with `ZAP_FULL=true`, which covers registration, token refresh and logout, the class pages, videos, the forum, the teacher profile, guidance, trash claims with a photo, the requests that move a child to another class and the lists around them, and an account deletion with a wrong password. The baseline scan does not get the longer list, because it fails the build on any warning. The upload of a photo file is not in the list, because a replayed request cannot carry the bytes of an image, so the photo check is only reached through the base64 photo of a trash claim. The replay uses an Automation Framework plan, `.zap/active.yaml`, which also caps the scan at 30 minutes. The run summary lists the alerts, how many requests the API answered by status code, which routes answered with a server error and the errors that the API logged. A server error other than the `503` of the video upload address raises a warning, and that `503` is expected because the scan stack has no object storage. The reports are uploaded as the `zap-active-reports` artifact. For now it only reports. Medium and high alerts show up as annotations and the run stays green, and it can become a gate for high alerts once a few runs have stayed quiet. The image digest is the same one as the baseline scan, so bump both together.

## Smoke test of production

The `Smoke` workflow runs `smoke/smoke.py` against the API that is deployed, and it is started by hand from the Actions tab after a deploy. It is not part of the pull request checks, because it tests the running service and not the code of a branch. The end to end suite above covers the code, and the smoke test covers what only the real deployment has, which is the proxy, TLS, the database, Redis, the bucket and the version that is running.

Give it the version that `/healthz` has to report. The test registers throwaway teachers and parents, replays the main routes and the refusals, uploads a small file through a signed address and discards it, and then deletes its accounts. One keeper teacher stays, because deleting the last teacher of a class that has children needs a successor. The first run registers the keeper when it does not exist yet.

The keeper email is the variable `SMOKE_KEEPER_EMAIL` and its password is the secret `SMOKE_KEEPER_PASSWORD`, both in the `smoke-production` environment. That environment only accepts runs from `main`, so a workflow edited on another branch cannot read the password. Add a check to the script in the same change that adds or changes a route.

To run it from a terminal, set `BASE_URL`, `EXPECTED_VERSION`, `SMOKE_KEEPER_EMAIL` and `SMOKE_KEEPER_PASSWORD` and run `python3 -I smoke/smoke.py`. It talks to the API with curl and exits with an error when a check fails.

### The same script on a throwaway stack

The `Smoke stack` workflow runs the same script on every pull request, against a stack that it builds from the branch with `compose.yml` and `compose.ci.yml`. It checks the container of the pull request as a whole before the merge, with a real PostgreSQL and Redis and the default rate limits except the allowance of a signed in user, which is 1000 a minute there so the speed of the runner cannot decide the result, while the `Smoke` workflow checks the live deployment after the deploy. The stack has a throwaway S3 compatible server of its own, Versity Gateway from `compose.storage.yml`, with a bucket for the public files and one for the private chat media. Both are private on the server, so the script does not prove that a public address can be read anonymously, which only the `Smoke` workflow shows on the real bucket, and the server does not model the expiry rule on `pending/` either. It checks the signature of every request like Cloudflare R2 does, so the avatar and video uploads, the signed upload and read addresses and the chat photos and videos run for real. The API only accepts `https` addresses for them, so the job makes a certificate for one day for the name `storage`, tells curl on the runner where that name is through a `.curlrc` (an entry in `/etc/hosts` would also change the answer that Docker gives to the containers), and the API and the script trust the certificate through `SSL_CERT_FILE` and `CURL_CA_BUNDLE`. A stack without any bucket is still possible. Set `SMOKE_STORAGE=off` and the script expects `healthz` to report storage as `disabled`, and `STORAGE_UNAVAILABLE` from the upload routes. Plain `http` is accepted only for `127.0.0.1`, and the keeper teacher gets a random password that only lives in that job. It is a required check, so a failure blocks the merge.
