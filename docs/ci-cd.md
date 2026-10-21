# CI/CD

| Workflow | Trigger | What it does |
|:---|:---|:---|
| `ci.yaml` | push to main, pull requests | gofmt, tidy check, vet, golangci-lint, build, race tests with PostgreSQL and Redis services, coverage upload to Codecov |
| `security.yaml` | push, pull requests, daily at 03:00 WIB | CodeQL, govulncheck, dependency review (which also refuses a new dependency under SSPL, the one license here that does not fit the GPL-3.0 of the repository), gitleaks secret scan, Trivy filesystem scan, OSSF Scorecard (not on pull requests) |
| `config.yaml` | push to main, pull requests | actionlint and zizmor for the workflows, hadolint for the Dockerfile, Redocly lint for `openapi.yaml` and a build of the documentation site, both in the `OpenAPI lint` job, and the Renovate validator in strict mode for `renovate.json` in the `Renovate config` job |
| `commits.yaml` | pull requests | Lints the commits of the pull request with commitlint and its Conventional Commits rules, which are set in `commitlint.config.mjs`, and checks the pull request title against the same types. Merge commits are skipped, and the line lengths of the body and footer are not limited because Renovate and Release Please write long ones |
| `vale.yaml` | push to main, pull requests | Vale prose lint of the README, the contributing, security and accessibility guides and the `docs/` folder. It fails on an em dash or a semicolon in prose, and the rest of the Google style only reports locally |
| `perf.yaml` | pull requests, daily at 04:00 WIB, manual | Builds the compose stack from the pull request with a memory cap on every container, runs k6 (a 30 second smoke test on pull requests, a steady load test nightly and on demand, and a stress, spike, soak or chat socket test on demand) and fails when a container was killed or restarted |
| `dast.yaml` | pull requests, weekly on Monday, manual | Builds the compose stack from the pull request, registers a teacher and a parent, replays a dozen API requests through the ZAP baseline scan and fails on any warning. The reports are uploaded as an artifact |
| `dast-active.yaml` | weekly on Sunday, manual | Builds the compose stack, registers throwaway accounts, replays the API requests and runs the ZAP active scan with an Automation Framework plan. It only reports and is not a required check |
| `docker.yaml` | push to main, tags, pull requests | Builds the image for `linux/amd64` and `linux/arm64`, pushes it to GitHub Container Registry only, scans both platforms with Trivy, attaches an SBOM, signs with cosign and verifies the signature right away. It never logs in to Docker Hub, because a pull request can change this workflow, and `docker-hub-mirror.yaml` copies the image there |
| `docker-hub-mirror.yaml` | after `docker.yaml` finishes | Copies the image that `docker.yaml` built from GitHub Container Registry to Docker Hub by digest. The tags are `pr-<number>` for a pull request, `sha-<commit>` and `latest` for main, and the version tags for a release, where `1.2` and `1` only move when the release is the highest of its line. It runs from `main` with the `docker-hub` environment and never checks out the repository. It copies an image only when the cosign signature on it was made by `docker.yaml` at the ref and with the trigger of the run that started the mirror, which is `main`, the release tag or the merge of that pull request, and a push or a manual run must also name the same commit. It copies that signature, verifies it again on Docker Hub and moves `latest` or a floating version tag last. For a pull request it skips the copy when the pull request already has a newer commit, and for main when main has one. A `pr-<number>` image is only as trustworthy as its pull request, because the author can change `docker.yaml` there, so never run one as a release. The environment rule that only `main` and tags may use the environment does not restrict this workflow, because `workflow_run` always runs on the default branch, so the signature check is what protects the tags. A prerelease tag such as `v1.2.3-rc.1` is not copied. It is not a required check. The Docker workflow no longer holds the Docker Hub token, and the repository copy of the secret is deleted by the owner after this workflow has run for a release. Docker Hub gets the tags of a pull request, of main and of a release only through this workflow, and `resign.yaml` also writes a signature there with the same token, so a release tag is copied for the first time at the next release and the run of the mirror for it is the one to read first. |
| `resign.yaml` | manual, from main only | Signs an image that is already published again in the classic format, without building anything. It checks that both registries hold the same digest and that the build attestation kept by GitHub shows `docker.yaml` built that digest from the tag of that version, and only then signs and verifies each signature right away. The Docker Hub token comes from the `docker-hub` environment |
| `fuzz.yaml` | weekly on Saturday, manual | Runs each of the four fuzz targets (`FuzzInspect`, `FuzzDecodeBase64`, `FuzzValidateToken` and `FuzzStruct`) for three minutes by default (at most 30 minutes by hand, because a job stops after 45), in a job of its own for each target. The input of a crash is uploaded as an artifact, and the file is added to `testdata/fuzz/<FuzzTargetName>/` of its package as a regression test. It never runs on pull requests and is not a required check |
| `links.yaml` | weekly on Monday, manual | Checks every link in the Markdown files of the root and `docs/` with lychee and fails when one is dead. A site that answers 429 is not counted as dead. It never runs on pull requests and is not a required check |
| `openapi-diff.yaml` | pull requests | Compares `openapi.yaml` of the merge of the pull request with the same file on its base with oasdiff, runs it without network and fails on a breaking change or a warning, such as a removed route, a removed field or parameter, a field that changes type or a new required request field. A pull request that has a commit marked as breaking (`type(scope)!: summary` or a `BREAKING CHANGE:` footer) only prints the changes, which is the mark that Release Please reads, because the title of a pull request never reaches `main` with a merge commit. It lists every change that touches a client in the log either way. It compares the two copies of the file only, so a behavior change that the file does not describe passes, and the workflow comes from the pull request like every other check, so its diff has to be read |
| `smoke.yaml` | manual only | Runs `smoke/smoke.py` against the deployed API after a deploy. It checks the version, registers throwaway accounts, replays the main routes and a real upload to the bucket, and removes the accounts again. The input `chat_media` says whether the private bucket of the chat photos and videos is set up, and the checks of those expect `STORAGE_UNAVAILABLE` when it is turned off. It never runs on pull requests and is not a required check. The keeper login comes from the `smoke-production` environment, which only runs from `main` can read |
| `smoke-stack.yaml` | pull requests, manual | Builds the compose stack from the pull request without a bucket and runs the same `smoke/smoke.py` against it on `127.0.0.1` with `SMOKE_STORAGE=off`, so a branch is exercised end to end before the merge. It is a required check |
| `release-please.yaml` | push to main | Keeps a release PR with the next version and `CHANGELOG.md`, merging it creates the tag and the GitHub release. The private key of the release App is a secret of the `release` environment, which only `main` can use |

Image tags are `latest` for main, `sha-<commit>` for builds of main and pull requests, `pr-<number>` for pull requests, and `1.2.3`, `1.2` and `1` for version tags. A release commit is built twice, once for main and once for its tag, so only the main build sets `latest` and the `sha-` tag and the tag build only adds the version tags. The version that `latest` reports is therefore the commit hash, and the version tags report the release number. The image is always published to GitHub Container Registry as `ghcr.io/syafahadyan/kibooz-backend` with the built-in token, so it needs no setup. Docker Hub is optional and gets the image from `docker-hub-mirror.yaml`, which needs the repository variable `DOCKERHUB_USERNAME` and the secret `DOCKERHUB_TOKEN` of the `docker-hub` environment. Pull requests from forks and from Dependabot only build the image. How the jobs pull their own base images is described in [Pulling images](#pulling-images). All actions are pinned to commit SHAs and kept current by Renovate, which waits 7 days after a new release before proposing it, except for a security fix, which it proposes at once. Every job has a `timeout-minutes` limit, so a hung job ends after a few minutes and not after the six hours that GitHub allows.
Verify an image with cosign by checking the identity of the workflow that signed it. A normal build is signed by `docker.yaml` and an image signed again by hand is signed by `resign.yaml`, so the identity pattern accepts both.

```sh
cosign verify \
  --new-bundle-format=false \
  --certificate-identity-regexp '^https://github\.com/SyafaHadyan/kibooz-backend/\.github/workflows/(docker\.yaml@refs/(heads/main|tags/v.+)|resign\.yaml@refs/heads/main)$' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  ghcr.io/syafahadyan/kibooz-backend:0.4.0
```

The pattern leaves out the identity of pull request builds on purpose, so an image tagged `pr-<number>` does not pass this check and only builds from `main` or from a version tag do. The check inside `docker.yaml` accepts pull request identities because it only verifies the image that the same run just built.

Every tag is one image for `linux/amd64` and `linux/arm64`, and Docker pulls the one that matches the machine. The Go binary is cross-compiled on the build machine, so the arm64 image needs no emulation. The signature covers the index of both platforms. Trivy scans each platform and reports it in its own code scanning category, but the SBOM is made for the amd64 image only, which has the same Go modules and the same base image packages as the arm64 one.

## Pulling images

Docker Hub limits anonymous pulls per IP address, and every GitHub runner shares its addresses with many other users. Jobs that pulled `postgres`, `redis` or a lint image therefore failed at random, either with `toomanyrequests` or with a timeout on `auth.docker.io`. The jobs avoid Docker Hub where they can.

- The Docker Hub images of the lint and scan jobs are pulled with `.github/actions/pull-images/pull.sh`, which tries `mirror.gcr.io/` plus the image name first and the Docker Hub name when the mirror fails. The digests are the same, so the mirror serves the identical image. The performance test and both ZAP scans do the same for Postgres and Redis in `compose.ci.yml`, and take the Go base image of the Dockerfile from the mirror with a named build context, so `compose.yml` and the Dockerfile stay as they are for production. The name of that image comes from `GO_IMAGE`, which each of those workflows reads from the Dockerfile into its `.env`, so a bump of the Go image only changes the Dockerfile. A test in `internal/repocheck` fails when a workflow forgets the line or the compose file carries a digest of its own.
- The Postgres and Redis service containers of the test job name the mirror in the image as well, as `mirror.gcr.io/library/postgres:18-alpine`. If the mirror ever stops serving one of these images, change the name back to the Docker Hub one or to `public.ecr.aws/docker/library/` with the same name.
- Each job that pulls from Docker Hub calls the local action `.github/actions/pull-images` right after the checkout. The action adds the mirror to `registry-mirrors` of the Docker daemon as a second line of defense and logs in to Docker Hub with a read-only token, so an image that falls through to Docker Hub is pulled as a known account. A login that fails does not fail the job.
- `docker.yaml` builds from the Docker Hub base images of the Dockerfile and does not log in to Docker Hub to push.
- Images from `ghcr.io` and `gcr.io`, such as zizmor, ZAP and the distroless base, are not limited this way and are pulled as they are.

To set up the login, create a read-only access token in the Docker Hub account settings and add it as the secret `DOCKERHUB_PULL_TOKEN`. The account name comes from the variable `DOCKERHUB_USERNAME`, which the Docker Hub mirror also uses.

```sh
gh secret set DOCKERHUB_PULL_TOKEN
```

Pull requests of Renovate read this secret like any other pull request of the repository. Dependabot pull requests could not, so an older setup also kept a copy for them with `--app dependabot`. Dependabot no longer opens version updates, so that copy is gone.

Without the secret everything still works through the mirror alone. Pull requests from forks never get the secret, so they rely on the mirror.

## Dependency updates

Renovate opens the update pull requests, and `renovate.json` holds its rules. It runs on Mondays before 07:00 WIB, proposes a release only after it is 7 days old, and groups minor and patch updates of Go modules (`go-minor-patch`), Actions (`actions`) and the documentation tooling (`docs-tooling`). A major update gets its own pull request. After a Go update it runs `go mod tidy`, because a plain `go get` leaves `tools/go.sum` without the entries that govulncheck needs. The commit type is `chore` for Go modules, `ci` for Actions and the tools of the workflows, and `build` for npm, Dockerfiles and compose files, so none of them starts a release. The bot keeps an issue called Dependency Dashboard with every update it knows about. Security fixes ignore the schedule and the 7 day wait. Renovate learns about them from the Dependabot alerts of the repository, so those alerts stay on.

The Go release is held on purpose. The `go` line of `go.mod` names the patch release, and the workflows read it with `go-version-file`, so `renovate.json` turns off updates of the Go version itself and a new patch release is raised by hand in one place. Snyk used to fail on a `go` line with a patch newer than 1.27.1, and it reads 1.27.2 now. The Go image in the Dockerfile is still updated.

## Pinned tools

The bot cannot read a tool that is named inside a `run` step, so the tools of the jobs are listed where it can.

- `.github/tools/Dockerfile` has one stage for each image that a job starts with `docker run`, which are actionlint, zizmor, hadolint, gitleaks and ZAP. Nothing builds it. The workflows get the image from `.github/tools/image.sh <stage>`, so the version of a bump is the version the jobs run. A test in `internal/repocheck` fails when a workflow writes an image digest by hand or names a stage that the file lacks.
- `tools/go.mod` is a separate Go module with govulncheck as a `tool`. The security workflow installs it with `go -C tools install tool`, and the root `go.mod` keeps only what the service needs.
- The golangci-lint, k6 and Vale versions sit in action inputs. A comment such as `# renovate: datasource=github-releases depName=grafana/k6` right above the line tells Renovate where to look, so keep the comment when you edit the line.

The Google style release in `.vale.ini` and the Vale image in the commands of this page are read by two more patterns in `renovate.json`, so only the `go` line of `go.mod` is still bumped by hand.

## Required checks

A pull request can only be merged into `main` when these checks pass. The repository ruleset pins every check to the app that reports it, so another app cannot post a check with the same name. Add a new check to the ruleset in the same change that introduces it.

| Check | Reported by |
|:---|:---|
| Lint | GitHub Actions |
| Unit and end to end tests | GitHub Actions |
| Build, scan and push image | GitHub Actions |
| Go vulnerability check | GitHub Actions |
| Dependency review | GitHub Actions |
| Secret scan (gitleaks) | GitHub Actions |
| ZAP baseline scan | GitHub Actions |
| k6 performance test | GitHub Actions |
| Trivy filesystem scan | GitHub Actions |
| Actionlint, Zizmor, Hadolint and OpenAPI lint | GitHub Actions |
| OpenAPI breaking changes | GitHub Actions |
| Renovate config | GitHub Actions |
| Vale prose lint | GitHub Actions |
| Conventional commits | GitHub Actions |
| Smoke test on a throwaway stack | GitHub Actions |
| CodeQL and Trivy code scanning results | GitHub Advanced Security |
| codecov/patch | Codecov |
| security/snyk | Snyk, a commit status that cannot be pinned to an app |

OSSF Scorecard is not required because it only runs on `main`, and the Cloudflare docs build is not required because it can be missing on pull requests from forks. DeepSource and Snyk are GitHub apps and are not workflows in this repository.

The five DeepSource checks (Docker, Go, SQL, Secrets and JavaScript) are not required at the moment, because the free quota of the account ran out on 2026-10-15. They go back into the ruleset, pinned to DeepSource, when the quota returns, about November 1, 2026. Until then the other checks and the review of the diff carry the load.

CodeRabbit is a review app on a trial of the account, and it is not a required check, so nothing in the ruleset waits for it. Its trial is expected to end around October 21, 2026, and from then on pull requests get no automatic review comments and no `CodeRabbit` status. Nothing has to change in the ruleset when that happens. The review of the diff, the tests, CodeQL and the other checks carry the load, and a pull request that used to be reviewed by it needs a careful read by a person. If the plan is renewed, the app starts reviewing again with no change here.

### Signed commits

The ruleset of `main` also requires verified signatures on every commit that reaches the branch. GitHub signs the merge commits, the commits made in its web editor, the "Update branch" merges and the commits of Renovate, which makes its commits through the GitHub API. The release commits are made by a GitHub App, because commits that an App makes through the API are signed by GitHub, while the old personal access token made unsigned ones. That is one more reason the release workflow uses an App token. Commits that landed on `main` before the rule stay as they are.

### Prose style

Vale reads `.vale.ini`, which combines the Vale and Google styles with the two rules in `.github/vale/styles/Kibooz/`. An em dash or a semicolon in prose fails the build, so split the sentence or use a comma instead. Code blocks and inline code are skipped. `CODE_OF_CONDUCT.md` is exempt in `.vale.ini` and is not in the file list of `vale.yaml`, because it is the Contributor Covenant text as published and is not rewritten. The Vale version in `vale.yaml`, the Google style release in `.vale.ini` and the Vale image in the commands above are pinned, and Renovate raises all three. Keep the `# renovate:` comment in `vale.yaml` and the shape of the other two lines, because the patterns in `renovate.json` read them. To see the hints that do not gate the build, run Vale locally.

```sh
docker run --rm -v "$PWD:/docs" -w /docs jdkato/vale:v3.24.0 sync
docker run --rm -v "$PWD:/docs" -w /docs jdkato/vale:v3.24.0 README.md CONTRIBUTING.md SECURITY.md ACCESSIBILITY.md docs
```

## Releases

Versions are decided by [release-please](https://github.com/googleapis/release-please) from the Conventional Commit messages that reach `main`. `fix` bumps the patch version, `feat` bumps the minor version, and `feat!` or a `BREAKING CHANGE` footer bumps the major version. While the version is below 1.0.0 a breaking change only bumps the minor version.

1. Merge pull requests into `main` with a Conventional Commit title.
2. release-please opens or updates a pull request named like `chore(main): release 0.2.0` with the new version and changelog.
3. When the release has `feat` entries or changed routes, run the `DAST active` workflow on `main` from the Actions tab or with `gh workflow run dast-active.yaml`, and read its report. The release pull request only adds the version and the changelog, so `main` holds the code that ships. The scan only reports and is not a required check, so a new high or medium finding is a reason to hold the release. A release with only fixes, docs or CI changes and no route changes can rely on the weekly scan when its last run is recent and finished and its report was read with the same rule, because a green run can still hold alerts.
4. Merge the release pull request when you want to release. This creates the `v0.2.0` tag and GitHub release, and the tag makes `docker.yaml` push the `0.2.0`, `0.2` and `0` image tags.

For the release pull request to run CI and for the tag to trigger the Docker workflow, the workflow gets its token from a GitHub App with contents, pull requests and issues write access, installed only on this repository. Set the repository variable `RELEASE_APP_CLIENT_ID` and the secret `RELEASE_APP_PRIVATE_KEY` from that App. GitHub does not start workflows for events created with the default token, and the release commits are made by the App instead of a personal account. The App is also the bypass actor of the `release-tags-creation` ruleset, so only this App can create a `v*` tag.
