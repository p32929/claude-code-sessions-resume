#!/usr/bin/env bash
# Build then run the ccsessions TUI. Always builds first.
set -euo pipefail

cd "$(dirname "$0")"

echo "Building ccsessions…"
go build -o ccsessions .

echo "Starting…"
exec ./ccsessions "$@"
