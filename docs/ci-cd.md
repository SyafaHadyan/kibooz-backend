# CI/CD

| Workflow | Trigger | What it does |
|:---|:---|:---|
| `ci.yaml` | push to main, pull requests | gofmt, tidy check, vet, golangci-lint, build, race tests with PostgreSQL and Redis services, coverage upload to Codecov |
| `security.yaml` | push, pull requests, daily at 03:00 WIB | CodeQL, govulncheck, dependency review, gitleaks secret scan, Trivy filesystem scan, OSSF Scorecard (not on pull requests) |
| `config.yaml` | push to main, pull requests | actionlint and zizmor for the workflows, hadolint for the Dockerfile, Redocly lint for `openapi.yaml` and a build of the documentation site, both in the `OpenAPI lint` job |
| `perf.yaml` | pull requests, daily at 04:00 WIB, manual | Builds the compose stack from the pull request with a memory cap on every container, runs k6 (a 30 second smoke test on pull requests, a steady load test nightly and on demand, and a stress, spike or soak test on demand) and fails when a container was killed or restarted |
| `dast.yaml` | pull requests, weekly on Monday, manual | Builds the compose stack from the pull request, registers a teacher and a parent, replays a dozen API requests through the ZAP baseline scan and fails on any warning. The reports are uploaded as an artifact |
| `dast-active.yaml` | weekly on Sunday, manual | Builds the compose stack, registers throwaway accounts, replays the API requests and runs the ZAP active scan with an Automation Framework plan. It only reports and is not a required check |
| `docker.yaml` | push to main, tags, pull requests | Builds the image, pushes it to GitHub Container Registry and to Docker Hub when configured, scans with Trivy, attaches an SBOM and signs with cosign |
| `release-please.yaml` | push to main | Keeps a release PR with the next version and `CHANGELOG.md`, merging it creates the tag and the GitHub release |

Image tags are `latest` for main, `sha-<commit>` for every build, `pr-<number>` for pull requests, and `1.2.3`, `1.2` and `1` for version tags. The image is always published to GitHub Container Registry as `ghcr.io/syafahadyan/kibooz-backend` with the built-in token, so it needs no setup. Docker Hub is optional and needs the repository variable `DOCKERHUB_USERNAME` and the secret `DOCKERHUB_TOKEN`. Pull requests from forks and Dependabot only build the image. All actions are pinned to commit SHAs and kept current by Dependabot, which waits 7 days after a new release before proposing it.

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
| CodeQL and Trivy code scanning results | GitHub Advanced Security |
| DeepSource Docker, Go, SQL and Secrets | DeepSource |
| codecov/patch | Codecov |
| security/snyk | Snyk, a commit status that cannot be pinned to an app |

OSSF Scorecard is not required because it only runs on `main`, and the Cloudflare docs build is not required because it can be missing on pull requests from forks. DeepSource and Snyk are GitHub apps and are not workflows in this repository.

## Releases

Versions are decided by [release-please](https://github.com/googleapis/release-please) from the Conventional Commit messages that reach `main`. `fix` bumps the patch version, `feat` bumps the minor version, and `feat!` or a `BREAKING CHANGE` footer bumps the major version. While the version is below 1.0.0 a breaking change only bumps the minor version.

1. Merge pull requests into `main` with a Conventional Commit title.
2. release-please opens or updates a pull request named like `chore(main) release 0.2.0` with the new version and changelog.
3. Merge that pull request when you want to release. This creates the `v0.2.0` tag and GitHub release, and the tag makes `docker.yaml` push the `0.2.0`, `0.2` and `0` image tags.

For the release pull request to run CI and for the tag to trigger the Docker workflow, add a repository secret named `RELEASE_PLEASE_TOKEN` holding a personal access token with contents and pull requests write access. GitHub does not start workflows for events created with the default token. Also allow GitHub Actions to create pull requests in the repository settings.
