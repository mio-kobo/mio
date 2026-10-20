#!/usr/bin/env bash
# Local release preparation only. Never creates a tag or contacts a release API.
set -euo pipefail
root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
exec python3 "$root_dir/scripts/prepare-release.py" "$@"
