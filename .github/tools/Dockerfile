# The update bot reads this file for the images that the workflows run with docker run, because it does not read run steps.
# Nothing builds it. The workflows ask .github/tools/image.sh for the image of a stage, so a bump here is the bump the jobs use.
FROM rhysd/actionlint:1.7.12@sha256:b1934ee5f1c509618f2508e6eb47ee0d3520686341fec936f3b79331f9315667 AS actionlint
FROM ghcr.io/zizmorcore/zizmor:1.33.1@sha256:ecd3ffe6158de1322e05a044ed43749bee12b1dccb7e1a7859bc6ac4b2a1c516 AS zizmor
FROM hadolint/hadolint:v2.15.1@sha256:32dac94127fd60b7b7e3fbfc65e1383b9b5e25c9bfd7b8536de7a539fe68a12d AS hadolint
FROM zricethezav/gitleaks:v8.30.1@sha256:c00b6bd0aeb3071cbcb79009cb16a60dd9e0a7c60e2be9ab65d25e6bc8abbb7f AS gitleaks
FROM ghcr.io/zaproxy/zaproxy:stable@sha256:9624669db72f54d86c4b46a7064518bd9bf79d8c61a4c31befa9923489f328af AS zaproxy
FROM tufin/oasdiff:v1.39.0@sha256:004bef22058ace818b61f0eb2f05efb89fc6a3882f68bf4db0b6a86f40723fe0 AS oasdiff
FROM koalaman/shellcheck:v0.11.0@sha256:61862eba1fcf09a484ebcc6feea46f1782532571a34ed51fedf90dd25f925a8d AS shellcheck
FROM mvdan/shfmt:v3.14.1@sha256:8c06884a35683d8763fba6c0d9484d11a61a96da65f8d497d6f625825a6043f2 AS shfmt
FROM dotenvlinter/dotenv-linter:4.0.0@sha256:49a3c89203aeabb814e7fc028c4bcaf569c6ead29e58dbad5e348324d042d120 AS dotenv-linter
FROM ghcr.io/astral-sh/ruff:0.17.6@sha256:2671e9bf3e50c24a88e731f209f9bf01c0360da6857ae36bca680b222efc3528 AS ruff
FROM ghcr.io/sbdchd/squawk:2.74.0@sha256:54319ad1c14de54abc3471029c62d7eccd485dddff31dad96238384f7a89d1d4 AS squawk
FROM timberio/vector:0.62.0-alpine@sha256:149c77c09aabf16a3aa543abf9cea2e2152c1d9a7d4a45669fe7ad05f4ed8b27 AS vector
