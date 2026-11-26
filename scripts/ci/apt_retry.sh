#!/bin/sh
# POSIX shell bounded retry wrapper for transient APT mirror/network failures.
# Used inside minimal Debian/Ubuntu containers before Python 3 is installed.
set -eu

max_attempts=3
delay=2
attempt=1

if [ "$#" -eq 0 ]; then
    echo "usage: apt_retry.sh <command> [args...]" >&2
    exit 2
fi

is_permanent_security() {
    printf '%s' "$1" | grep -qiE 'NO_PUBKEY|EXPKEYSIG|BADSIG|signatures? (were|was) invalid|signatures? couldn.?t be verified|is not signed|does not have a Release file|Invalid Release|Hash Sum mismatch|Checksum mismatch|Authentication failed|401  ?Unauthorized|403  ?Forbidden|Insecure repository'
}

is_transient() {
    printf '%s' "$1" | grep -qiE 'File has unexpected size|Mirror sync in progress|Writing more data than expected|Temporary failure resolving|Could not resolve|Connection timed out|Connection refused|Connection reset by peer|Network is unreachable|Failed to fetch|\b(429|502|503)\b'
}

tmp_out=$(mktemp)
trap 'rm -f "$tmp_out"' EXIT

while [ "$attempt" -le "$max_attempts" ]; do
    echo "[apt-retry] attempt $attempt/$max_attempts: $*" >&2
    rc=0
    "$@" >"$tmp_out" 2>&1 || rc=$?
    cat "$tmp_out" >&2
    if [ "$rc" -eq 0 ]; then
        exit 0
    fi
    output=$(cat "$tmp_out")
    if is_permanent_security "$output"; then
        echo "[apt-retry] permanent security/verification failure, exiting with code $rc" >&2
        exit "$rc"
    fi
    if ! is_transient "$output"; then
        echo "[apt-retry] non-transient failure, exiting with code $rc" >&2
        exit "$rc"
    fi
    if [ "$attempt" -ge "$max_attempts" ]; then
        echo "[apt-retry] transient failure persisted after $attempt attempts, exiting with code $rc" >&2
        exit "$rc"
    fi
    echo "[apt-retry] transient failure detected; retrying in ${delay}s" >&2
    sleep "$delay"
    delay=$((delay * 2))
    attempt=$((attempt + 1))
done
