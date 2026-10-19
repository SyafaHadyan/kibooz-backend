# Maintainers

This file lists who looks after Kibooz Backend and what each person can do. [GOVERNANCE.md](GOVERNANCE.md) explains how decisions are made.

| GitHub account | Role | Access | Looks after |
| --- | --- | --- | --- |
| [@SyafaHadyan](https://github.com/SyafaHadyan) | Owner and maintainer | Admin | Merging, releases, deployment, repository settings, secrets and security reports |
| [@ArmJour](https://github.com/ArmJour) | Collaborator | Write | Features and fixes through pull requests, such as the batch mood log |

## What the roles mean

The maintainer decides what is merged and released. Only the maintainer changes repository settings, rulesets, secrets and the production deployment, and only the maintainer merges a release pull request.

A collaborator opens pull requests like anyone else and may review other pull requests. The write access lets a collaborator push branches to this repository. It does not allow creating a release tag by hand, because only the release GitHub App can create one. A collaborator can still edit a workflow on a branch, and a workflow can read the repository secrets, including the release App key, so this rule depends on collaborators not doing that and the secrets are the open risk in [ADR 0004](docs/adr/0004-release-app-creates-tags.md). Write access also does not allow pushing to `main`, because the ruleset on `main` requires a pull request for everyone.

## Reaching a maintainer

Use an issue for bugs and ideas. Report a vulnerability through GitHub private vulnerability reporting, as described in [SECURITY.md](SECURITY.md). Reports about behavior in the community go to the maintainer the same way, and the [Code of Conduct](CODE_OF_CONDUCT.md) explains what happens next.

## Known limit

The project has one administrator. If that person is unavailable, nobody else can change the repository settings, rotate a secret or deploy. Adding a second administrator is not planned yet, and this file changes the day it happens.
