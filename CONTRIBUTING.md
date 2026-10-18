# Contributing

Thanks for helping with Kibooz Backend. This guide keeps changes easy to review and release.

Everyone who takes part in this project follows the [Code of Conduct](CODE_OF_CONDUCT.md).

## Set up

You need the Go version in `go.mod`, Docker, and a PostgreSQL database. Redis is optional.

```sh
cp .env.example .env              # fill in DB_* and JWT_SECRET_KEY
go run ./cmd/api
```

The README has the quick start and the full list of settings. The `docs/` folder has the design notes, the testing guide and the CI/CD guide, and the API reference is at <https://docs.kibooz.syafahadyan.com>.

## Making a change

1. Branch from `main`. Name the branch `type/short-description`, for example `fix/refresh-retry`, using the same type as your commits.
2. Keep one pull request to one concern. Unrelated changes go into separate branches and pull requests.
3. Write commits in the [Conventional Commits](https://www.conventionalcommits.org/) style, as `type(scope): summary`. The summary is the whole message, so do not add a body. The types are `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `chore` and `revert`. A pull request cannot be merged while a commit message or its title does not follow this, because the required check "Conventional commits" fails. Fix the message with `git commit --amend` for the last commit, or an interactive rebase for an older one, and push again.
4. Sign your commits. The `main` branch only accepts verified signatures, so a pull request with an unsigned commit cannot be merged. GitHub signs its own merge commits, the commits of its web editor and the ones that Dependabot and the release app make. To sign a commit you already made, run `git commit --amend --no-edit -S` for the last one. For older ones, start an interactive rebase with `git rebase -i BASE`, where `BASE` is the commit before them, mark each of them as `edit`, and run `git commit --amend --no-edit -S` on each stopped commit followed by `git rebase --continue`. An interactive rebase alone does not sign the commits it recreates. Then push the branch again.
5. Write code, comments, log lines, documentation and API messages in English. Match the style of the code around your change. Prose is checked by Vale, so write without em dashes and semicolons, as described in [docs/ci-cd.md](docs/ci-cd.md).

Releases are automated by release-please from the commit messages that reach `main`. Do not edit `CHANGELOG.md`, `.release-please-manifest.json` or version numbers by hand.

The API contract includes JSON field names and enum values such as `SENANG` or `GURU`. Changing them breaks clients, so treat that as a breaking change.

## Checks to run before you push

```sh
gofmt -l .
go vet ./...
go test ./... -race -count=1
docker run --rm -v "$PWD:/app" -w /app golangci/golangci-lint:v2.14.0 golangci-lint run
```

The end to end tests are skipped unless you enable them. They create data in PostgreSQL and some tests empty the Redis database that `REDIS_DATABASE` names with `FLUSHDB`, so only point them at a disposable PostgreSQL and Redis, never at a database or cache you care about. The tests refuse a `DB_NAME` that does not end in `_test`, unless `E2E_ALLOW_ANY_DB=true` is set for a database that you can lose.

```sh
E2E_ENABLED=true DB_NAME=kibooz_test DB_USERNAME=kibooz DB_PASSWORD=... REDIS_PASSWORD=... go test ./internal/e2e/ -race -count=1
```

Add tests with every behaviour change. A bug fix should come with a test that fails without the fix.

## Pull requests

- The title is a Conventional Commit, such as `fix(auth): keep refresh retryable after a database error`. The "Conventional commits" check blocks the merge when the title or any commit does not follow it.
- The body is only the list of commits in the pull request, one bullet per commit.
- Add one label, `bug` for fixes, `enhancement` for features and `documentation` for documentation only.
- Pull requests are merged with a merge commit, never squashed or rebased.
- Every required check must pass. They are listed in the repository rules and described in [docs/ci-cd.md](docs/ci-cd.md).
- Read the bot comments on your pull request. DeepSource and code scanning report findings there.

## Reporting problems

Use the issue templates for bugs and feature requests. Report security problems privately as described in [SECURITY.md](SECURITY.md).
