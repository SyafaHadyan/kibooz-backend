# 0004 Only the release GitHub App creates version tags

Status: Accepted, 2026-10-18

## Context

A tag that matches `v*` starts the Docker workflow and publishes an image, and a published tag cannot be removed. Anyone with write access can push a tag, so one wrong or unreviewed tag would publish an image that nobody can take back. Release Please also needs a way to create the tag and the release commit that starts the right workflows, which the default workflow token does not do.

## Decision

The release workflow gets its token from a GitHub App that is installed only on this repository. Two tag rulesets protect `v*`. `release-tags` forbids deleting, moving and updating a tag for everyone. `release-tags-creation` forbids creating one, and its only bypass actor is the App. The repository admin role is not a bypass on purpose. Releases are immutable, so the tag and the assets of a published release cannot change. Commits that an App makes through the API are signed by GitHub, which is also what lets the ruleset of `main` require signed commits.

## Consequences

- Nobody creates a version tag by hand, including the owner. A release only happens when the release pull request is merged.
- A wrong release is never edited. The next patch version replaces it.
- If the App breaks, releases stop until it is fixed. The owner can edit the ruleset in the repository settings, and that is the deliberate way around the rule, not a bypass that is open all the time.
- The App key and the Docker Hub token are secrets of environments and not of the repository. The App key is a secret of the `release` environment, which only accepts `main`. The Docker Hub token is a secret of the `docker-hub` environment, which only accepts `main` and `v*.*.*` tags, and the Docker Hub mirror and `resign.yaml` read it, see [CI/CD](../ci-cd.md). A workflow that runs on a branch cannot read either one, so a person with write access cannot mint an App token or push to Docker Hub by editing a workflow on a branch. This was first set aside, the owner decided to do it on 2026-10-21, and the repository copies of both secrets were deleted on 2026-10-22 after a release, the mirror and `resign.yaml` had all run on the environment copies.
- The rule still depends on `main`. The ruleset of `main` needs a pull request but no approval, so a person with write access can merge a pull request that changes a workflow, and that workflow then runs on `main` and can read both environments. The rule therefore holds while nobody with write access merges such a change, so the maintainer has to read every workflow change before it is merged. A required reviewer on the environments would close this, but it would mean approving every run of Release Please and of the mirror, so it was not chosen. The repository secrets that are left are `CODECOV_TOKEN`, which can only upload coverage, and `DOCKERHUB_PULL_TOKEN`, which can only read, and a branch can read both.
- A pull request of this repository can still put an image under its own `pr-<number>` tag on Docker Hub through the mirror, because the signature check only proves that the image came from the `docker.yaml` of that pull request, so such an image is never to be run as a release.

The steps are in [Releasing](../releasing.md), and the rulesets are listed in [CI/CD](../ci-cd.md#releases).
