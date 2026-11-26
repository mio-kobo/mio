#!/bin/sh
# Installs what tests/linux-compat/run.py needs inside a distribution container: Python 3, a C compiler and binutils.
set -eu
if command -v apt-get >/dev/null 2>&1; then
    # Ubuntu and Debian mirrors are sometimes caught mid-sync ("File has unexpected size"); retry the index update.
    tries=0
    until apt-get update -qq; do
        tries=$((tries + 1))
        if [ "$tries" -ge 4 ]; then echo "apt-get update failed $tries times" >&2; exit 100; fi
        sleep $((tries * 15))
    done
    DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends python3 gcc libc6-dev binutils >/dev/null
elif command -v apk >/dev/null 2>&1; then
    apk add --no-cache -q python3 build-base binutils
else
    echo "unsupported package manager" >&2
    exit 2
fi
