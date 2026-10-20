# 0012 Which checks gate a merge into main, and which ones do not

Status: Accepted, 2026-10-21. This record was written after the fact. The list of checks is in [CI/CD](../ci-cd.md#required-checks), and the ruleset on the repository is what enforces it.

## Context

The repository has one maintainer and no second reviewer, so the checks are the only gate between a change and production. Some tools are free for a while and then run out of quota, some post their result late, some do not run on pull requests from bots or forks, and a few only make sense on `main`. A required check that is sometimes missing blocks every merge, and a check that is not required is only advice.

## Decision

The ruleset of `main` has no bypass. It requires a pull request that is merged with a merge commit ([0003](0003-merge-commits-only.md)), resolved review threads, verified signatures on every commit that reaches the branch after the rule took effect, and the older commits stay as they are, and strict status checks, so a branch must be up to date with `main` before it merges. It also holds the code scanning and code quality rules.

Every required check is pinned to the app that reports it (GitHub Actions, GitHub Advanced Security or Codecov), so another app cannot post a check with the same name. Snyk reports a plain commit status without an app, and it is the one check that stays on any source. A pull request that adds, renames or removes a job updates the ruleset in the same change.

The gates are lint and format, unit and end-to-end tests with a coverage patch status, a Go vulnerability check, dependency review, a secret scan, a filesystem scan, CodeQL, a ZAP baseline scan, a k6 smoke test, a smoke test on a throwaway stack, the workflow and OpenAPI linters, prose lint, Conventional Commits, and Snyk.

Some tools are not required on purpose.

- OSSF Scorecard only runs on `main`, so it can never be present on a pull request.
- The active ZAP scan is slow and attacks a throwaway stack, so it is run by hand before a release with new routes, and its report decides whether the release waits.
- The production smoke test needs the deployed API and runs after a deploy.
- The Cloudflare docs build can be missing on pull requests from forks.
- DeepSource is a required gate only while the free quota lasts. Its five checks were taken out of the ruleset when the quota ran out on 2026-10-15, and they go back in when it returns.
- CodeRabbit is a review aid on a trial and never a gate. Its comments are read like those of any other reviewer, and a finding is fixed or dismissed with a reason.

Commits are signed. GitHub signs the merge commits and the commits of Renovate, and the release commits are made by a GitHub App, because an App that commits through the API gets a signature from GitHub, which a personal access token does not.

## Consequences

- A tool that runs out of quota or changes its names does not silently stop gating. The ruleset is the single place to look, and a doc must not claim a gate that the ruleset does not hold.
- With the DeepSource checks out, the remaining checks and a careful reading of the diff carry the load until the quota returns.
- A required check that cannot run on a bot pull request would block dependency updates, so such a check is not made required.
- Strict checks cost a rerun when `main` moves, which is the price for a branch that was always tested on top of the latest code.
- A cancelled duplicate of a required check keeps a pull request blocked, and the fix is to run that job again.
