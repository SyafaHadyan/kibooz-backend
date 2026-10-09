# Testing

```sh
go test ./...                 # unit tests, end to end tests are skipped
```

The end to end suite drives the real HTTP stack against PostgreSQL and Redis, with an in-process mock for object storage. Point it at a disposable PostgreSQL and Redis and enable it explicitly, because the tests create data.

```sh
E2E_ENABLED=true DB_NAME=kibooz DB_USERNAME=kibooz DB_PASSWORD=... go test ./... -race -count=1
```

Every response that suite sees is also checked against `openapi.yaml`, so a field, status code or error that the code changes without the spec fails the build. An object in a response also fails the check when it carries a field that the spec does not list, so a handler cannot start returning an internal field unnoticed. `TestSpecMatchesRoutes` fails when an endpoint exists in only one of the code and the spec. When the whole end to end suite passes, a final check also fails the run if a documented operation never had a single response checked, or if a documented success response was never seen, because such an operation would be documented without anything to keep it true. That check is skipped for a filtered run with `-run` or `-skip` and when `E2E_ENABLED` is not set. All of these run inside the required `Unit and end to end tests` job, so there is no extra CI job.

Run the linter with `golangci-lint run` (configuration in `.golangci.yml`).

CI uploads the coverage of the unit and end to end tests to [Codecov](https://codecov.io/gh/SyafaHadyan/kibooz-backend), which comments on pull requests that change it. `codecov/patch` is a required check, so new code needs coverage. Codecov posts it a few minutes after CI finishes, so a merge has to wait for it. `codecov/project` is not required. Dependabot pull requests upload too, using a `CODECOV_TOKEN` stored in the Dependabot secrets. The thresholds are in `codecov.yml`.

The code that takes untrusted input has fuzz tests, namely image decoding, access token validation and request body validation. A normal `go test` runs their seed cases. To search for new failing inputs, fuzz one target at a time.

```sh
go test ./internal/imageutil/ -run '^$' -fuzz FuzzDecodeBase64 -fuzztime 30s
```

## Performance tests

The `k6/` folder holds the load scripts. `smoke.js` is a short check that runs on every pull request, and `load.js` holds a steady arrival rate below the ceiling to measure how the API behaves with a whole class claiming points at once, and `stress.js` ramps past the ceiling on demand to find where the API slows down. `spike.js` sends a sudden burst above the ceiling and checks that the API recovers afterwards, and `soak.js` holds a steady load for 30 minutes to catch leaks, so its summary also lists the memory of every container at the start and at the end. All of them create their own accounts, so they only need a running API. The default rate limits and the daily claim limit would answer with errors during a load test, so the workflow raises them in its generated `.env`.

```sh
# set USER_LIMITER_MAX, AUTH_LIMITER_MAX and TRASH_DAILY_LIMIT to large values in .env first
docker compose -f compose.yml -f compose.ci.yml up -d --build --wait
k6 run k6/smoke.js
```

The latency limits in the scripts are placeholders that were set from the first runs on a GitHub runner, so raise them when you move to slower hardware. Never point the scripts at a deployment you do not own, because they create accounts and write data.

## Security scan with ZAP

The `dast.yaml` workflow starts the compose stack and runs the ZAP baseline scan, which is a passive scan that only reads responses and never attacks. The API is plain JSON without links, so the spider finds almost nothing by itself. `.zap/seed.sh` therefore registers a teacher and a parent with throwaway credentials and `.zap/hook.py` replays a dozen requests through ZAP, including authenticated, rejected and unknown routes. Every warning fails the build. A rule that does not apply can be set to `IGNORE` with a reason in `.zap/rules.tsv`. The report is uploaded as the `zap-reports` artifact.

The scan is passive, so it checks headers and information leaks but does not try injections. The ZAP image is pinned by digest and has to be bumped by hand in the workflow, because Dependabot does not read images inside shell steps.

The `dast-active.yaml` workflow runs a ZAP active scan, which does attack the API with injection and fuzzing payloads. It runs weekly on Sunday evening UTC and on demand from the Actions tab, and never on pull requests because it is slow and noisy. It reuses `.zap/seed.sh` for the throwaway accounts and replays the same requests with an Automation Framework plan, `.zap/active.yaml`, which also caps the scan at 30 minutes. The run summary lists the alerts and how many requests the API answered by status code, and the reports are uploaded as the `zap-active-reports` artifact. For now it only reports. Medium and high alerts show up as annotations and the run stays green, and it can become a gate for high alerts once a few runs have stayed quiet. The image digest is the same one as the baseline scan, so bump both together.
