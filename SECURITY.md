# Security Policy

## Supported versions

Kibooz Backend is below version 1.0.0. Only the latest release and the `main` branch receive security fixes.

## Reporting a vulnerability

Please do not open a public issue for a security problem.

Use GitHub private vulnerability reporting instead. Open the [Security tab](https://github.com/SyafaHadyan/kibooz-backend/security) and choose **Report a vulnerability**, or go straight to the [advisory form](https://github.com/SyafaHadyan/kibooz-backend/security/advisories/new). Only the maintainer can see the report.

A good report includes the following.

- A description of the problem and its impact, for example an authentication bypass, data exposure or injection.
- Steps to reproduce it, or a proof of concept.
- The version, image tag or commit you tested.
- Relevant requests, responses or logs with any secrets removed.

You can expect a first reply within a few business days. The report is then triaged, fixed in private when it is valid, and released with a published advisory. Reporters who want credit are named once the fix has shipped.

## Scope

In scope are the API code in this repository, its database migrations, the Docker image, and the GitHub Actions workflows.

Out of scope are other people's deployments of this project, findings that need a compromised host, and reports that only describe missing hardening without a way to exploit it.

Do not run automated scanning, load testing or fuzzing against a live deployment you do not own. Use a local `docker compose up` stack instead.

## How this repository is checked

Findings from the automated tooling in this repository, which is CodeQL, govulncheck, dependency review, gitleaks, Trivy, Snyk, DeepSource and OSSF Scorecard, are triaged the same way as external reports.
