# Security

This page describes what the API protects, who it protects it from and why the measures in place are enough for that. To report a problem, follow [SECURITY.md](../SECURITY.md).

## What is protected

- The accounts and passwords of teachers and parents.
- Information about children, which is their names, class, moods and points.
- Avatars and trash photos that people upload, and the class videos that teachers upload straight to the bucket.
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
| Guessing a join code | A join code has six characters from an alphabet of 32, about a billion codes, and a wrong code is answered like any unknown class. A teacher can replace the code of a class with `POST /guru/classes/{classId}/join-code`, and the old code stops working at once. | `internal/classcode` |
| Locking an owner out on purpose | A device that signed in before has its own login budget through the device token | [Trusted devices](design.md#trusted-devices) |
| Flooding the public routes | Every signed in user and every account has a limit. A flood with a new email each time is not stopped by the API, so the proxy or CDN has to limit it | [A flood limit in the proxy](design.md#a-flood-limit-in-the-proxy) |
| Stolen access token | The token expires after `JWT_ACCESS_EXPIRED_MINUTES`, 60 by default, and every limit and role check still applies to it | [Architecture](architecture.md#sessions) |
| Stolen or replayed refresh token | Refresh tokens are stored only as SHA-256 hashes and are used by one atomic update, so a token works exactly once, even for parallel requests. The row of a used token stays for a week, and when it is shown again a minute or more after its use, the whole session of the sign in ends and the event is logged with the owner. A session also ends `JWT_SESSION_MAX_DAYS` days after the sign in, 90 by default, so a stolen token cannot keep it alive for ever. A revived Redis cannot make a used token valid again | [Redis is optional](design.md#redis-is-optional) |
| Reading another class or child | Teachers can only read and record the classes they teach and parents can only read their own children. Other access answers `403` or `404`, and the end to end tests cover it | [Business rules](design.md#business-rules) and `internal/e2e` |
| Injection | Every body is validated for required fields, lengths and formats before a rule runs. Queries go through GORM and the few raw statements pass their values as bound arguments, so no value is joined into SQL text | `internal/infra/validation` and the repositories |
| Hostile uploads | An upload is limited to 2 MiB for avatars and 4 MiB for photos and the whole request body to `BODY_LIMIT_MB`, 8 by default. The type is detected from the content and not from the file name, and the image is stored without its metadata, so the location and camera details of a photo are not kept. The EXIF, XMP, IPTC and comment metadata is cut out of the file without decoding it. Only what the picture needs to show correctly stays, which is the orientation and, for a JPEG, the JFIF header, the ICC color profile and the Adobe color segment | `internal/imageutil` |
| Hostile video uploads | Only a teacher of the class gets a signed address, and it allows one PUT of the announced type and length until it expires. A video cannot be larger than `VIDEO_MAX_MB`, and adding it checks that the file arrived, sits under the class folder and still has the accepted type and size. A new file waits under a `pending/` prefix that the bucket expires after a day, and it only gets a permanent key when a teacher adds it as a video. The content of a video is not decoded because it never passes through the API, and it is served as a video type from the media host | `internal/app/classroom` |
| Broken or leaked credentials | Settings come from environment variables and the signing key must be at least 32 characters. Secret scanning with push protection and gitleaks look for secrets in the repository | `internal/infra/env` and [CI/CD](ci-cd.md) |
| Browser attacks on responses | Every response carries `nosniff`, `X-Frame-Options: DENY`, a `default-src 'none'` content security policy and `no-referrer`. HSTS is set by the proxy | [README](../README.md) |
| Vulnerable or tampered dependencies | Dependabot opens update and security PRs, `govulncheck` and the Trivy filesystem scan inspect the code, and the Docker workflow scans the image of each platform whenever it can push the image, which excludes fork and Dependabot pull requests. The results of the filesystem scan are uploaded for Dependabot pull requests too, so the required code scanning check named Trivy is always present for them. Base images are pinned by digest, and the published image is signed with cosign | [CI/CD](ci-cd.md) |
| Bugs found late | CodeQL, gosec, golangci-lint, fuzz tests of the untrusted input code, the ZAP baseline and active scans and an end to end suite that checks every response against `openapi.yaml` | [Testing](testing.md) |
| A compromised container | The image is a static binary on a distroless base that runs as a non-root user | [Architecture](architecture.md#build-and-run) |

## Why this is enough

The argument has three parts.

1. **Every request is checked before a rule runs.** A signed in route checks the access token first and then applies the limit for that user. A public auth route applies the limit for the account first, because there is no token yet. Both validate the body before the feature use case runs, so a hostile request is rejected by the cheapest check that can reject it. The checks sit in the same places for every feature because the layers in [Architecture](architecture.md#layers) are the same for every feature.
2. **Data and sessions have one owner.** PostgreSQL alone decides who owns which data and whether a refresh token is valid. An access token is checked locally against the signing key and its expiry. A refresh token needs PostgreSQL to accept it, and Redis can only turn down the replay of a token that was already used, before PostgreSQL is asked, so it can never make PostgreSQL accept a token it would reject. Redis and the storage bucket can fail or be wrong without giving anyone access.
3. **The checks keep running.** The tests, the linters, CodeQL, `govulncheck`, gitleaks and the Trivy filesystem scan run on every pull request into `main` and on every push to `main`, and the security workflow also runs every day. The Trivy image scans run whenever the Docker workflow can push the image, so fork and Dependabot pull requests do not get them. The ZAP baseline scan runs on pull requests into `main` and every week, and the ZAP active scan runs every week or on demand. Dependency review only looks at pull requests, and Scorecard only looks at `main`, on every push and every day, and it can also be started by hand. A required check blocks a merge when it fails, and the documented API is tested against the real behavior. A change that weakens a measure has to get past those checks.

## What is not covered

- A flood with new emails is only limited by the proxy or CDN in front of the API.
- Anyone can send `AUTH_LIMITER_MAX` failed logins for someone else's email to block that person's new devices for one window. Devices that signed in before keep working.
- TLS and `Strict-Transport-Security` belong to the proxy. The API never terminates TLS.
- Permanent deletion of a removed account is not automated. A deleted account is hidden from every query and login.
- The project has one maintainer, so there is no second reviewer for changes.
