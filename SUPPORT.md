# Support

Kibooz Backend is an open source project that is maintained by its owners, and there is no paid support or response time promise. This page says where to look first and where each kind of question belongs.

## Look here first

| You want to know | Read |
|:---|:---|
| What a route takes and returns, with every field, limit and status | The [API reference](https://docs.kibooz.syafahadyan.com) |
| Whether the API or the documentation site is down right now | The [status page](https://status.syafahadyan.com/) |
| What an `errorCode` means and what an app does about it | [Error codes](docs/errors.md) |
| How an app handles tokens, the device token and rate limits | The [client guide](docs/client-guide.md) |
| How to run the stack and what each setting does | The [README](README.md) and [Operations](docs/operations.md) |
| Why something works the way it does | The [design notes](docs/design.md) and the [PRD differences](docs/prd-differences.md) |
| How to build, test and send a change | [CONTRIBUTING.md](CONTRIBUTING.md) |

## Where each question goes

- **A bug.** Open an issue with the [bug report form](https://github.com/SyafaHadyan/kibooz-backend/issues/new?template=bug_report.yml). Say what happened, the steps to reproduce it, the version from `/healthz` and the logs or responses, with secrets and personal data removed.
- **An idea or a missing feature.** Open an issue with the [feature request form](https://github.com/SyafaHadyan/kibooz-backend/issues/new?template=feature_request.yml).
- **A security problem.** Never open a public issue. Use the private report described in [SECURITY.md](SECURITY.md).
- **A barrier in the documentation or the API for a person with a disability.** Follow [ACCESSIBILITY.md](ACCESSIBILITY.md).
- **A question about the Android app.** Ask in the repository of the app, [ValentinusMarvel/kibooz-app](https://github.com/ValentinusMarvel/kibooz-app). The PRD there is the source of the contract that this API follows.
- **A change you want to make.** Read [CONTRIBUTING.md](CONTRIBUTING.md) first. A commit message or a pull request title that does not follow Conventional Commits blocks the merge.

## What is not supported

Running and securing your own deployment is your responsibility, and this repository cannot debug it. The [operations page](docs/operations.md) lists what to check, and an issue is welcome when it turns out to be a bug in the code. Only the latest release and `main` are supported. [SECURITY.md](SECURITY.md) states that only the latest release and `main` receive security fixes.
