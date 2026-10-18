# 0003 Pull requests are merged with merge commits

Status: Accepted

## Context

GitHub offers three ways to merge a pull request. Squashing gives a tidy history of one commit per pull request, and rebasing gives a straight line. Both rewrite the commits that were reviewed. Release Please builds the changelog from the commit messages that reach `main`, and every commit of a pull request has to follow Conventional Commits.

## Decision

The repository allows merge commits only, and the ruleset of `main` says so. The merge commit keeps the title of the pull request as its subject and has no body, so a long pull request description never ends up in the history. The commits of the pull request stay on `main` as they were written.

## Consequences

- The commits that were checked by the "Conventional commits" job are the commits that reach `main`, so what was reviewed is what is kept.
- `git log` shows each pull request as a group, and a whole change can be reverted by reverting its merge commit.
- The changelog can list an entry twice, once for the commit and once for the merge commit that has the same title. It is noise and not an error.
- A branch has to be up to date with `main` before it can merge, so a pull request that falls behind is updated with a merge. GitHub signs that merge, which keeps the signed commit rule satisfied.
