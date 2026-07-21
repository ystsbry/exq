#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
rm -rf bin/
echo "Removed bin/"
