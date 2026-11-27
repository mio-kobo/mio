#!/usr/bin/env python3
"""Verify that a checked-out job matches the immutable source revision requested."""
from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys


def verify_provenance(requested_sha: str, repo: str = ".", workflow_ref: str = "", run_id: str = "", attempt_id: str = "") -> dict:
    actual_sha = subprocess.check_output(
        ["git", "-C", repo, "rev-parse", "--verify", "HEAD"],
        text=True,
    ).strip()
    record = {
        "requested_sha": requested_sha,
        "actual_sha": actual_sha,
        "workflow_ref": workflow_ref,
        "run_id": run_id,
        "attempt_id": attempt_id,
    }
    if requested_sha and actual_sha != requested_sha:
        raise ValueError(f"source provenance mismatch: requested {requested_sha}, checked out {actual_sha}")
    return record


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--requested-sha", default=os.environ.get("REQUESTED_SOURCE_SHA", ""))
    parser.add_argument("--require-sha", action="store_true")
    parser.add_argument("--repo", default=".")
    args = parser.parse_args(argv)
    requested_sha = args.requested_sha.strip()
    if args.require_sha and not requested_sha:
        parser.error("missing requested source SHA")
    record = verify_provenance(
        requested_sha=requested_sha or os.environ.get("GITHUB_SHA", ""),
        repo=args.repo,
        workflow_ref=os.environ.get("GITHUB_WORKFLOW_REF", ""),
        run_id=os.environ.get("GITHUB_RUN_ID", ""),
        attempt_id=os.environ.get("GITHUB_RUN_ATTEMPT", ""),
    )
    print(json.dumps(record), flush=True)
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (ValueError, subprocess.CalledProcessError) as exc:
        print(f"ERROR: {exc}", file=sys.stderr, flush=True)
        raise SystemExit(1)
