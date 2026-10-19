# Governance

Kibooz Backend is a small project with one maintainer and a few collaborators. This page says how changes are accepted so that nobody has to guess. The people are listed in [MAINTAINERS.md](MAINTAINERS.md).

## Decisions

The maintainer makes the final call on what is merged, what is released and which direction the project takes. Most decisions happen in a pull request or an issue, in the open, so the reasoning stays on record.

The API is shared with the Kibooz mobile app, so a change to the contract needs more care. That covers JSON field names, enum values, error codes and status codes. Such a change is described in the pull request, written into `openapi.yaml` in the same pull request, and noted in [docs/prd-differences.md](docs/prd-differences.md) when it departs from the product requirements. A change that breaks existing clients is called out in the pull request title and body. The product requirements and the roadmap live in the [app repository](https://github.com/ValentinusMarvel/kibooz-app/tree/main/docs/prd_and_roadmap), and [ROADMAP.md](ROADMAP.md) shows how the backend follows them.

## Changes

Every change reaches `main` through a pull request. The ruleset on `main` requires the following for everyone, including the maintainer.

- The required checks pass and the branch is up to date with `main`.
- Review threads are resolved.
- Commits are signed and follow Conventional Commits.
- The merge is a merge commit.

The ruleset does not require an approving review. Review from another person is welcome on any pull request and expected on a change to the API contract, but it is a habit and not a rule that GitHub enforces. [CONTRIBUTING.md](CONTRIBUTING.md) has the practical steps.

## Releases

Releases are automated. Release Please opens a release pull request from the commit messages that reach `main`, and the maintainer merges it when the release is ready. Merging creates the tag and the GitHub release through the release GitHub App, which is the only account allowed to create a `v*` tag. That rule stops a pushed tag and anyone who is not a repository admin. An admin can still edit the ruleset, so it does not stop a stolen owner login either, and it does not stop a branch workflow from reading the release App key, because the repository secrets are readable by anyone with write access who edits a workflow, so the guarantee holds while nobody with write access does that. Releases are immutable once published, so a wrong release gets a new version and not an edit.

## Becoming a collaborator

Anyone can contribute with a fork and a pull request. The maintainer decides who gets write access and invites that person, and the maintainer can remove the access again.

## Conduct and security

Everyone follows the [Code of Conduct](CODE_OF_CONDUCT.md). Vulnerabilities are reported privately as described in [SECURITY.md](SECURITY.md).

## Changing this page

Changes to this page, to [MAINTAINERS.md](MAINTAINERS.md) and to the rulesets go through a pull request like any other change, and the maintainer decides.
