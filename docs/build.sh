#!/bin/sh
# Builds the API documentation site into dist/ for Cloudflare Workers Builds, which runs this as its build command.
# The Redocly version is pinned here, so a new release changes the page only when someone bumps it.
set -eu

rm -rf dist
mkdir -p dist

npx --yes @redocly/cli@2.59.0 build-docs openapi.yaml -o dist/index.html
cp docs/_headers dist/_headers
