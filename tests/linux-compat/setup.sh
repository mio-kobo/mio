#!/bin/sh
# Installs what tests/linux-compat/run.py needs inside a distribution container: Python 3, a C compiler and binutils.
set -eu
if command -v apt-get >/dev/null 2>&1; then
    sh "$(dirname "$0")/../../scripts/ci/apt_retry.sh" apt-get update -qq
    sh "$(dirname "$0")/../../scripts/ci/apt_retry.sh" env DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends python3 gcc libc6-dev binutils >/dev/null
elif command -v apk >/dev/null 2>&1; then
    apk add --no-cache -q python3 build-base binutils
else
    echo "unsupported package manager" >&2
    exit 2
fi
