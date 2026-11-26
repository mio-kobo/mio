"""Select whole artifacts downloaded by pattern within a single workflow run."""

from fnmatch import fnmatchcase
import os
from pathlib import Path
import re
import shutil


def select_artifacts(downloads, destination, pattern, run_id, *, group=False):
    downloads = Path(downloads)
    destination = Path(destination)
    suffix = re.compile(rf"^(.+)-{re.escape(run_id)}-([1-9][0-9]*)$")
    latest = {}
    for entry in downloads.glob("*"):
        match = suffix.fullmatch(entry.name)
        if not entry.is_dir() or entry.is_symlink() or not match:
            continue
        name, suffix_attempt = match.groups()
        attempt = int(suffix_attempt)
        if fnmatchcase(name, pattern) and attempt > latest.get(name, (0, None))[0]:
            latest[name] = (attempt, entry)

    if not latest:
        raise ValueError(f"No artifacts matching {pattern!r} in workflow run {run_id}")
    if not group and len(latest) != 1:
        raise ValueError(f"Expected one artifact matching {pattern!r}, found {len(latest)}")
    if destination.exists():
        raise ValueError(f"Artifact destination already exists: {destination}")

    if group:
        destination.mkdir(parents=True)
    else:
        destination.parent.mkdir(parents=True, exist_ok=True)
    for _, entry in sorted(latest.values(), key=lambda item: item[1].name):
        print(f"Using {entry.name}")
        shutil.move(str(entry), str(destination / entry.name if group else destination))
    return None if group else next(iter(latest.values()))[0]


if __name__ == "__main__":
    path = os.environ["ARTIFACT_DESTINATION"]
    try:
        attempt = select_artifacts(
            path + "-attempts", path, os.environ["ARTIFACT_PATTERN"], os.environ["GITHUB_RUN_ID"],
            group=os.environ["ARTIFACT_GROUP"] == "true",
        )
        if attempt is not None and os.environ.get("GITHUB_OUTPUT"):
            with open(os.environ["GITHUB_OUTPUT"], "a", encoding="utf-8") as output:
                output.write(f"attempt={attempt}\n")
    except (OSError, ValueError) as error:
        raise SystemExit(str(error)) from error
