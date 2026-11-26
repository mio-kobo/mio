#!/usr/bin/env python3
"""Bounded exponential-backoff retry wrapper for transient APT mirror failures.

Security and repository-verification failures (invalid signatures, NO_PUBKEY,
EXPKEYSIG, missing/invalid Release metadata, authentication errors, checksum
mismatches) and unrelated package errors are never retried and never bypassed.
"""
import argparse
import re
import subprocess
import sys
import time

PERMANENT_SECURITY_PATTERNS = (
    re.compile(r"\bNO_PUBKEY\b", re.IGNORECASE),
    re.compile(r"\bEXPKEYSIG\b", re.IGNORECASE),
    re.compile(r"\bBADSIG\b", re.IGNORECASE),
    re.compile(r"signatures?\s+(?:were|was)\s+invalid", re.IGNORECASE),
    re.compile(r"signatures?\s+couldn'?t\s+be\s+verified", re.IGNORECASE),
    re.compile(r"is\s+not\s+signed", re.IGNORECASE),
    re.compile(r"does\s+not\s+have\s+a\s+Release\s+file", re.IGNORECASE),
    re.compile(r"Invalid\s+Release", re.IGNORECASE),
    re.compile(r"Hash\s+Sum\s+mismatch", re.IGNORECASE),
    re.compile(r"Checksum\s+mismatch", re.IGNORECASE),
    re.compile(r"Authentication\s+failed", re.IGNORECASE),
    re.compile(r"\b401\s+Unauthorized\b", re.IGNORECASE),
    re.compile(r"\b403\s+Forbidden\b", re.IGNORECASE),
    re.compile(r"Insecure\s+repository", re.IGNORECASE),
)

TRANSIENT_PATTERNS = (
    re.compile(r"File\s+has\s+unexpected\s+size", re.IGNORECASE),
    re.compile(r"Mirror\s+sync\s+in\s+progress", re.IGNORECASE),
    re.compile(r"Writing\s+more\s+data\s+than\s+expected", re.IGNORECASE),
    re.compile(r"Temporary\s+failure\s+resolving", re.IGNORECASE),
    re.compile(r"Could\s+not\s+resolve", re.IGNORECASE),
    re.compile(r"Connection\s+timed\s+out", re.IGNORECASE),
    re.compile(r"Connection\s+refused", re.IGNORECASE),
    re.compile(r"Connection\s+reset\s+by\s+peer", re.IGNORECASE),
    re.compile(r"Network\s+is\s+unreachable", re.IGNORECASE),
    re.compile(r"Failed\s+to\s+fetch", re.IGNORECASE),
    re.compile(r"\b(?:429|502|503)\b"),
)


def classify_apt_failure(output: str) -> str:
    """Classify APT failure output as 'permanent_security', 'transient', or 'permanent_other'."""
    for pattern in PERMANENT_SECURITY_PATTERNS:
        if pattern.search(output):
            return "permanent_security"
    for pattern in TRANSIENT_PATTERNS:
        if pattern.search(output):
            return "transient"
    return "permanent_other"


def run_with_retry(
    cmd: list[str],
    max_attempts: int = 3,
    base_delay: float = 2.0,
    sleep_fn=time.sleep,
) -> int:
    if max_attempts < 1:
        raise ValueError("max_attempts must be >= 1")
    if not cmd:
        raise ValueError("command must not be empty")

    for attempt in range(1, max_attempts + 1):
        print(f"[apt-retry] attempt {attempt}/{max_attempts}: {' '.join(cmd)}", file=sys.stderr, flush=True)
        proc = subprocess.run(cmd, capture_output=True, text=True)
        if proc.stdout:
            sys.stdout.write(proc.stdout)
            sys.stdout.flush()
        if proc.stderr:
            sys.stderr.write(proc.stderr)
            sys.stderr.flush()

        if proc.returncode == 0:
            return 0

        combined = f"{proc.stdout}\n{proc.stderr}"
        kind = classify_apt_failure(combined)
        if kind != "transient":
            print(
                f"[apt-retry] non-retryable failure ({kind}), exiting with code {proc.returncode}",
                file=sys.stderr,
                flush=True,
            )
            return proc.returncode

        if attempt >= max_attempts:
            print(
                f"[apt-retry] transient failure persisted after {attempt} attempts, exiting with code {proc.returncode}",
                file=sys.stderr,
                flush=True,
            )
            return proc.returncode

        delay = base_delay * (2 ** (attempt - 1))
        print(
            f"[apt-retry] transient mirror/network failure detected; retrying in {delay:.1f}s",
            file=sys.stderr,
            flush=True,
        )
        sleep_fn(delay)

    return 1


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Retry transient APT failures with bounded backoff.")
    parser.add_argument("--max-attempts", type=int, default=3)
    parser.add_argument("--base-delay", type=float, default=2.0)
    parser.add_argument("command", nargs=argparse.REMAINDER)
    args = parser.parse_args(argv)

    cmd = list(args.command)
    if cmd and cmd[0] == "--":
        cmd = cmd[1:]
    if not cmd:
        parser.error("missing command after --")
    return run_with_retry(cmd, max_attempts=args.max_attempts, base_delay=args.base_delay)


if __name__ == "__main__":
    raise SystemExit(main())
