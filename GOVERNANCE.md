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

## Repository settings

No setting of this repository is changed without the approval of a code owner, which is an account that [.github/CODEOWNERS](.github/CODEOWNERS) names. This holds for everyone who has access, including an account with admin rights, and for every tool or agent that works for them.

A setting is a configuration of the repository that GitHub or a connected app keeps outside its files. Examples are rulesets and branch protection, required checks, Actions settings, environments, secrets and variables, webhooks, deploy keys, collaborators and teams, installed apps, security features, visibility, merge options, and a transfer or a rename. Everyday work on issues, pull requests, branches and releases is not a change of a setting.

The approval is a direct go-ahead from a code owner that names the change. Each new change needs a new approval, and an earlier approval does not cover it. Reading a setting needs no approval.

A change of [.github/CODEOWNERS](.github/CODEOWNERS) also needs the approval of a code owner, because that file names who may approve.

These are rules of the project, and GitHub does not enforce them. An account with enough access can change a setting without anyone's approval, and the ruleset needs no approval for a change of a file.

## Releases

Releases are automated. Release Please opens a release pull request from the commit messages that reach `main`, and the maintainer merges it when the release is ready. Merging creates the tag and the GitHub release through the release GitHub App, which is the only account allowed to create a `v*` tag. That rule stops a pushed tag and anyone who is not a repository admin. An admin can still edit the ruleset, so it does not stop a stolen admin login either. The release App key and the Docker Hub token are secrets of environments that accept no branch, only `main` (and `v*.*.*` tags for the Docker Hub one), so a workflow on a branch cannot read them. Someone with write access who gets a workflow change merged into `main` still could, because the ruleset needs no approval. So the guarantee holds while nobody with write access does that. Releases are immutable once published, so a wrong release gets a new version and not an edit.

## Becoming a collaborator

Anyone can contribute with a fork and a pull request. The maintainer decides who gets write access and invites that person, and the maintainer can remove the access again.

## Conduct and security

Everyone follows the [Code of Conduct](CODE_OF_CONDUCT.md). Vulnerabilities are reported privately as described in [SECURITY.md](SECURITY.md).

## Changing this page

Changes to this page and to [MAINTAINERS.md](MAINTAINERS.md) go through a pull request like any other change, and the maintainer decides. A change of a ruleset follows [Repository settings](#repository-settings).
