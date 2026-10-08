# Security

This page describes what the API protects, who it protects it from and why the measures in place are enough for that. To report a problem, follow [SECURITY.md](../SECURITY.md).

## What is protected

- The accounts and passwords of teachers and parents.
- Information about children, which is their names, class, moods and points.
- Avatars and trash photos that people upload.
- The availability of the API for a class that is using it.

## Who is a threat

| Actor | What they can try |
|:---|:---|
| A stranger on the internet | Guess passwords, flood the public routes, send malformed or hostile requests and uploads |
| A signed in user | Read or change data of another class, child or account |
| Someone with a stolen token | Reuse an access token, a refresh token or a device token |
| A supply chain attacker | Slip a bad dependency, base image or workflow into the build |

The mood detection runs on the device, so the API never receives a face photo and the server does not have to defend one.

## Threats and measures

| Threat | Measure | Where to see it |
|:---|:---|:---|
| Guessing passwords | Passwords are stored as salted bcrypt hashes. Login, register, refresh and logout are limited per account, so many addresses cannot add up to more attempts. A login for an unknown email does the same bcrypt work as a real one, so timing does not reveal which emails exist | [Rate limits](design.md#rate-limits) and `internal/app/auth` |
| Locking an owner out on purpose | A device that signed in before has its own login budget through the device token | [Trusted devices](design.md#trusted-devices) |
| Flooding the public routes | Every signed in user and every account has a limit. A flood with a new email each time is not stopped by the API, so the proxy or CDN has to limit it | [A flood limit in the proxy](design.md#a-flood-limit-in-the-proxy) |
| Stolen access token | The token expires after `JWT_ACCESS_EXPIRED_MINUTES`, 60 by default, and every limit and role check still applies to it | [Architecture](architecture.md#sessions) |
| Stolen or replayed refresh token | Refresh tokens are stored only as SHA-256 hashes and are consumed by one atomic delete, so a token works exactly once, even for parallel requests. A revived Redis cannot make a used token valid again | [Redis is optional](design.md#redis-is-optional) |
| Reading another class or child | Teachers can only read and record the classes they teach and parents can only read their own children. Other access answers `403` or `404`, and the end to end tests cover it | [Business rules](design.md#business-rules) and `internal/e2e` |
| Injection | Every body is validated for required fields, lengths and formats before a rule runs. Queries go through GORM and the few raw statements pass their values as bound arguments, so no value is joined into SQL text | `internal/infra/validation` and the repositories |
| Hostile uploads | An upload is limited to 2 MB for avatars and 4 MB for photos and the whole request body to `BODY_LIMIT_MB`, 8 by default. The type is detected from the content and not from the file name, and the image is decoded before it is stored | `internal/imageutil` |
| Broken or leaked credentials | Settings come from environment variables and the signing key must be at least 32 characters. Secret scanning with push protection and gitleaks look for secrets in the repository | `internal/infra/env` and [CI/CD](ci-cd.md) |
| Browser attacks on responses | Every response carries `nosniff`, `X-Frame-Options: DENY`, a `default-src 'none'` content security policy and `no-referrer`. HSTS is set by the proxy | [README](../README.md) |
| Vulnerable or tampered dependencies | Dependabot opens update and security PRs, `govulncheck` and Trivy scan the code and the image, base images are pinned by digest, and the published image is signed with cosign | [CI/CD](ci-cd.md) |
| Bugs found late | CodeQL, gosec, golangci-lint, fuzz tests of the untrusted input code, the ZAP baseline and active scans and an end to end suite that checks every response against `openapi.yaml` | [Testing](testing.md) |
| A compromised container | The image is a static binary on a distroless base that runs as a non-root user | [Architecture](architecture.md#build-and-run) |

## Why this is enough

The argument has three parts.

1. **Every request is checked before a rule runs.** A signed in route checks the access token first and then applies the limit for that user. A public auth route applies the limit for the account first, because there is no token yet. Both validate the body before any rule runs, so a hostile request is rejected by the cheapest check that can reject it. The checks sit in the same places for every feature because the layers in [Architecture](architecture.md#layers) are the same for every feature.
2. **Data and sessions have one owner.** PostgreSQL alone decides who owns which data and whether a refresh token is valid. An access token is checked locally against the signing key and its expiry, and a refresh token only works while PostgreSQL holds it. Redis and the storage bucket can fail or be wrong without giving anyone access.
3. **The checks keep running.** The tools above run on every pull request and on a schedule, a required check blocks a merge when it fails, and the documented API is tested against the real behavior. A change that weakens a measure has to get past those checks.

## What is not covered

- A flood with new emails is only limited by the proxy or CDN in front of the API.
- Anyone can send `AUTH_LIMITER_MAX` failed logins for someone else's email to block that person's new devices for one window. Devices that signed in before keep working.
- TLS and `Strict-Transport-Security` belong to the proxy. The API never terminates TLS.
- Permanent deletion of a removed account is not automated. A deleted account is hidden from every query and login.
- The project has one maintainer, so there is no second reviewer for changes.
