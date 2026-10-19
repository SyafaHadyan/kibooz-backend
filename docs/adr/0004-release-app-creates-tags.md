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
- The App key is a repository secret that a workflow change on a branch could read, and so is the Docker Hub token. The rulesets stop everyone from creating a tag by hand, but a person with write access who edits a workflow can mint an App token and create a tag, or push an image to Docker Hub. The rule therefore holds only while nobody with write access does that. Moving the secrets into an environment limited to `main` was considered and the owner chose to keep them as repository secrets for now.

The steps are in [Releasing](../releasing.md), and the rulesets are listed in [CI/CD](../ci-cd.md#releases).
