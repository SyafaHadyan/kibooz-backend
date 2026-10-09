# CI/CD

| Workflow | Trigger | What it does |
|:---|:---|:---|
| `ci.yaml` | push to main, pull requests | gofmt, tidy check, vet, golangci-lint, build, race tests with PostgreSQL and Redis services, coverage upload to Codecov |
| `security.yaml` | push, pull requests, daily at 03:00 WIB | CodeQL, govulncheck, dependency review, gitleaks secret scan, Trivy filesystem scan, OSSF Scorecard (not on pull requests) |
| `config.yaml` | push to main, pull requests | actionlint and zizmor for the workflows, hadolint for the Dockerfile, Redocly lint for `openapi.yaml` and a build of the documentation site, both in the `OpenAPI lint` job |
| `vale.yaml` | push to main, pull requests | Vale prose lint of the README, the contributing and security guides and the `docs/` folder. It fails on an em dash or a semicolon in prose, and the rest of the Google style only reports locally |
| `perf.yaml` | pull requests, daily at 04:00 WIB, manual | Builds the compose stack from the pull request with a memory cap on every container, runs k6 (a 30 second smoke test on pull requests, a steady load test nightly and on demand, and a stress, spike or soak test on demand) and fails when a container was killed or restarted |
| `dast.yaml` | pull requests, weekly on Monday, manual | Builds the compose stack from the pull request, registers a teacher and a parent, replays a dozen API requests through the ZAP baseline scan and fails on any warning. The reports are uploaded as an artifact |
| `dast-active.yaml` | weekly on Sunday, manual | Builds the compose stack, registers throwaway accounts, replays the API requests and runs the ZAP active scan with an Automation Framework plan. It only reports and is not a required check |
| `docker.yaml` | push to main, tags, pull requests | Builds the image for `linux/amd64` and `linux/arm64`, pushes it to GitHub Container Registry and to Docker Hub when configured, scans both platforms with Trivy, attaches an SBOM, signs with cosign and verifies each signature right away |
| `resign.yaml` | manual, from main only | Signs an image that is already published again in the classic format, without building anything. It checks that both registries hold the same digest and that the build attestation kept by GitHub shows `docker.yaml` built that digest from the tag of that version, and only then signs and verifies each signature right away |
| `release-please.yaml` | push to main | Keeps a release PR with the next version and `CHANGELOG.md`, merging it creates the tag and the GitHub release |

Image tags are `latest` for main, `sha-<commit>` for builds of main and pull requests, `pr-<number>` for pull requests, and `1.2.3`, `1.2` and `1` for version tags. A release commit is built twice, once for main and once for its tag, so only the main build sets `latest` and the `sha-` tag and the tag build only adds the version tags. The version that `latest` reports is therefore the commit hash, and the version tags report the release number. The image is always published to GitHub Container Registry as `ghcr.io/syafahadyan/kibooz-backend` with the built-in token, so it needs no setup. Docker Hub is optional and needs the repository variable `DOCKERHUB_USERNAME` and the secret `DOCKERHUB_TOKEN`. Pull requests from forks and Dependabot only build the image. How the jobs pull their own base images is described in [Pulling images](#pulling-images). All actions are pinned to commit SHAs and kept current by Dependabot, which waits 7 days after a new release before proposing it. Every job has a `timeout-minutes` limit, so a hung job ends after a few minutes and not after the six hours that GitHub allows.
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

- The Docker Hub images of the lint and scan jobs are pulled with `.github/actions/pull-images/pull.sh`, which tries `mirror.gcr.io/hadolint/hadolint:v2.15.1@sha256:...` first and the Docker Hub name when the mirror fails. The digests are the same, so the mirror serves the identical image. The performance test and both ZAP scans do the same for Postgres and Redis in `compose.ci.yml`, and take the Go base image of the Dockerfile from the mirror with a named build context, so `compose.yml` and the Dockerfile stay as they are for production.
- The Postgres and Redis service containers of the test job name the mirror in the image as well, as `mirror.gcr.io/library/postgres:15-alpine`. If the mirror ever stops serving one of these images, change the name back to the Docker Hub one or to `public.ecr.aws/docker/library/` with the same name.
- Each job that pulls from Docker Hub calls the local action `.github/actions/pull-images` right after the checkout. The action adds the mirror to `registry-mirrors` of the Docker daemon as a second line of defense and logs in to Docker Hub with a read-only token, so an image that falls through to Docker Hub is pulled as a known account. A login that fails does not fail the job.
- `docker.yaml` is not changed. It already logs in to Docker Hub with the push token whenever it can push, and it builds from the Docker Hub base images of the Dockerfile.
- Images from `ghcr.io` and `gcr.io`, such as zizmor, ZAP and the distroless base, are not limited this way and are pulled as they are.

To set up the login, create a read-only access token in the Docker Hub account settings and add it as the secret `DOCKERHUB_PULL_TOKEN`. The account name comes from the variable `DOCKERHUB_USERNAME` that the image push already uses.

```sh
gh secret set DOCKERHUB_PULL_TOKEN                  # for pull requests of this repository
gh secret set DOCKERHUB_PULL_TOKEN --app dependabot # for Dependabot pull requests, which cannot read the secret above
```

Without the secret everything still works through the mirror alone. Pull requests from forks never get the secret, so they rely on the mirror.

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
| Vale prose lint | GitHub Actions |
| CodeQL and Trivy code scanning results | GitHub Advanced Security |
| DeepSource Docker, Go, SQL and Secrets | DeepSource |
| codecov/patch | Codecov |
| security/snyk | Snyk, a commit status that cannot be pinned to an app |

OSSF Scorecard is not required because it only runs on `main`, and the Cloudflare docs build is not required because it can be missing on pull requests from forks. DeepSource and Snyk are GitHub apps and are not workflows in this repository.

### Prose style

Vale reads `.vale.ini`, which combines the Vale and Google styles with the two rules in `.github/vale/styles/Kibooz/`. An em dash or a semicolon in prose fails the build, so split the sentence or use a comma instead. Code blocks and inline code are skipped. The Vale version in `vale.yaml` and the Google style release in `.vale.ini` are pinned and have to be bumped by hand, because Dependabot does not read them. To see the hints that do not gate the build, run Vale locally.

```sh
docker run --rm -v "$PWD:/docs" -w /docs jdkato/vale:v3.24.0 sync
docker run --rm -v "$PWD:/docs" -w /docs jdkato/vale:v3.24.0 README.md CONTRIBUTING.md SECURITY.md docs
```

## Releases

Versions are decided by [release-please](https://github.com/googleapis/release-please) from the Conventional Commit messages that reach `main`. `fix` bumps the patch version, `feat` bumps the minor version, and `feat!` or a `BREAKING CHANGE` footer bumps the major version. While the version is below 1.0.0 a breaking change only bumps the minor version.

1. Merge pull requests into `main` with a Conventional Commit title.
2. release-please opens or updates a pull request named like `chore(main) release 0.2.0` with the new version and changelog.
3. Merge that pull request when you want to release. This creates the `v0.2.0` tag and GitHub release, and the tag makes `docker.yaml` push the `0.2.0`, `0.2` and `0` image tags.

For the release pull request to run CI and for the tag to trigger the Docker workflow, add a repository secret named `RELEASE_PLEASE_TOKEN` holding a personal access token with contents and pull requests write access. GitHub does not start workflows for events created with the default token. Also allow GitHub Actions to create pull requests in the repository settings.
