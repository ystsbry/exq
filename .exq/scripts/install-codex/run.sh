#!/usr/bin/env bash
set -euo pipefail
repo="$(git rev-parse --show-toplevel)"
codex plugin marketplace add "$repo"
echo "Added marketplace 'exq'. Verify/enable with 'codex plugin', then restart codex."
