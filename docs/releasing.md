# Releasing

A release is a pull request that you merge when you want to ship. This page is the order of the steps and what to check at each one. The workflows behind them are described in [CI/CD](ci-cd.md#releases), and the steps after the merge, which put the version on the server, are in [Operations](operations.md#deploy-a-version).

## How the version is chosen

[release-please](https://github.com/googleapis/release-please) reads the Conventional Commit messages that reach `main` and keeps one open pull request named like `chore(main): release 0.12.0`. The pull request changes only the version in `.release-please-manifest.json` and `CHANGELOG.md`.

| Commit | Bumps |
|:---|:---|
| `fix` | the patch number |
| `feat` | the minor number |
| `feat!` or a `BREAKING CHANGE` footer | the minor number while the version is below 1.0.0, the major number after |

Other types, such as `docs`, `ci`, `test` and `chore`, do not start a release by themselves and are left out of the changelog. The git tag carries a `v` and the image tags do not, so version 0.11.0 is the tag `v0.11.0` and the image tag `0.11.0`.

The changelog can list an entry twice, once for the commit and once for the merge commit that carries the same title, and it can list a fix for code that never shipped in an earlier release. Both are harmless. Edit the release branch only on purpose, because the next push of release-please rewrites it.

## Before you merge the release pull request

1. **Checks.** Every required check is green, the same as for any pull request.
2. **Changelog.** Read it for a breaking change, a new setting in the [README](../README.md#configuration) and a migration. A migration means a database dump before the deploy, see [Roll back](operations.md#roll-back).
3. **Active scan.** When the changelog has `feat` entries or a route changed, run the `DAST active` workflow on `main` and read its report first. The release pull request only adds the version and the changelog, so `main` already holds the code that will ship. A new high or medium finding is a reason to hold the release. A release with only fixes, docs or CI changes and no route changes can rely on the weekly scan when its last run is recent and finished and its report was read.

   ```sh
   gh workflow run dast-active.yaml --ref main
   ```
4. **The PRD.** When routes, fields, error codes, rules or settings changed, plan the sync of the PRD in the Android app repository for right after the release.

## What the merge does

Merging the release pull request starts a chain that needs nothing from you.

1. The release commit lands on `main`. It is made by the release GitHub App through the API, so GitHub signs it, which the signed commit rule of `main` needs.
2. The App creates the tag `v<version>` and the GitHub release. Only this App may create a `v*` tag, and nobody can move, update or delete one, because the two tag rulesets `release-tags-creation` and `release-tags` say so. The release is immutable, so its tag and assets cannot change after it is published.
3. The tag starts `docker.yaml`, which builds the image for `linux/amd64` and `linux/arm64` and pushes the `0.12.0`, `0.12` and `0` tags. The workflow then scans the image, attaches an SBOM, signs it with cosign and verifies the signatures. The push to `main` builds the `latest` and `sha-` tags in a separate run.

Docker Hub gets the image only from the `Docker Hub mirror` workflow, which starts when a Docker run succeeds. So watch the two Docker runs, the two mirror runs that follow them (one for the tag and one for `main`) and the Release Please run until they finish, and deploy only after the mirror run of the tag is green, because the server pulls from Docker Hub and the version tag is not there before that. A mirror run that failed is started again from its own run page, since a new build would not help. The mirror of a release tag is the one path that has not yet been seen on a real release, so read its log the first time. A failure that only comes from a timeout or a registry limit can be re-run once. Anything else is investigated like any failing check.

## After the release

1. Deploy the version as described in [Operations](operations.md#deploy-a-version).
2. Run the manual `Smoke` workflow with `expected_version` set to the new version. It fails when the server reports another version, so it also tells you that the deploy worked.
3. Sync the PRD of the Android app repository, in Indonesian, in one pull request with the routes, fields, error codes, tickets and the version in the health example.
4. Verify the image if you want to, with the cosign command in [CI/CD](ci-cd.md).

## When something goes wrong

- **A published release is wrong.** A tag cannot be moved or deleted and the assets of an immutable release cannot change, so ship a fix as the next patch version. Only the title and the notes of the release can still be edited. This is the point of the rules.
- **The tag was not created.** Open the Release Please run. The usual cause is the App token, so check the variable `RELEASE_APP_CLIENT_ID`, the secret `RELEASE_APP_PRIVATE_KEY` of the `release` environment, that the App is installed on this repository, and that it is still the bypass actor of the `release-tags-creation` ruleset.
- **The release pull request does not run CI.** It was not created with the App token. A pull request made with the default token does not start other workflows.
- **An image has no signature in the classic format.** Run the `Resign` workflow from `main`. It reads the digest from GitHub Container Registry, and when Docker Hub is set up it checks that Docker Hub holds the same digest, before it signs again. It builds nothing.
- **The Docker run on the tag failed.** Re-run it once when the cause was the infrastructure. If it fails again, fix `main` and release the fix, because the tag cannot be rebuilt from other code.
