#!/usr/bin/env python3
"""Six-platform parity matrix (docs/parity/matrix.json): validate it and render a readable table.

  parity_matrix.py check            validate the schema and the honesty rules; exit 1 on any violation
  parity_matrix.py backlog          list every (platform, feature) that is not VALIDATED_BY_CI: the parity backlog (exit 0 always)
  parity_matrix.py render [--write] print (or write docs/parity/README.md) the matrix as Markdown

Honesty rules enforced by `check`: every platform x feature pair has exactly one entry; the status is one of the allowed states; a
VALIDATED_BY_CI entry cites a run and at least one passing check; UNSUPPORTED, BLOCKED, NOT_RUN and IN_PROGRESS entries state their limitation and the
next action; nothing but VALIDATED_BY_CI counts toward parity (SKIP, NOT_RUN, UNSUPPORTED, cross-compilation without execution never do).
Parity is complete only when the backlog is empty.
"""
import json
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parents[2]
MATRIX = ROOT / "docs/parity/matrix.json"
STATES = ("IMPLEMENTED", "VALIDATED_BY_CI", "UNSUPPORTED", "IN_PROGRESS", "BLOCKED", "NOT_RUN")
REQUIRED = ("platform", "feature", "status", "implementation", "verification", "ci_evidence", "pr", "limitations", "next_action")


def load(path=MATRIX):
    return json.loads(Path(path).read_text(encoding="utf-8"))


def problems(doc):
    found = []
    platforms = doc.get("platforms", [])
    features = [f["id"] for f in doc.get("features", [])]
    if len(platforms) != 6:
        found.append("the matrix must name exactly the six platforms")
    seen = {}
    for entry in doc.get("entries", []):
        for field in REQUIRED:
            if field not in entry:
                found.append("%s/%s: missing field %s" % (entry.get("platform"), entry.get("feature"), field))
        key = (entry.get("platform"), entry.get("feature"))
        if key in seen:
            found.append("%s/%s: duplicate entry" % key)
        seen[key] = entry
        if entry.get("status") not in STATES:
            found.append("%s/%s: unknown status %r" % (*key, entry.get("status")))
        evidence = entry.get("ci_evidence") or {}
        if entry.get("status") == "VALIDATED_BY_CI" and not (evidence.get("run") and evidence.get("checks")):
            found.append("%s/%s: VALIDATED_BY_CI needs a CI run and at least one passing check" % key)
        if entry.get("status") in ("UNSUPPORTED", "BLOCKED", "NOT_RUN", "IN_PROGRESS") and not (entry.get("limitations") and entry.get("next_action")):
            found.append("%s/%s: %s needs a limitation and a next action" % (*key, entry.get("status")))
    for platform in platforms:
        for feature in features:
            if (platform, feature) not in seen:
                found.append("%s/%s: no entry" % (platform, feature))
    return found


def backlog(doc):
    return [e for e in doc["entries"] if e["status"] != "VALIDATED_BY_CI"]


def render(doc):
    platforms = doc["platforms"]
    order = {p: i for i, p in enumerate(platforms)}
    lines = ["# Six-platform parity matrix", "",
             "Generated from `docs/parity/matrix.json` by `scripts/ci/parity_matrix.py render`; do not edit by hand.", "",
             "Evidence: %s, head `%s`. %s" % (doc["evidence"]["url"], doc["evidence"]["head_sha"], doc["evidence"]["note"]), "",
             "Only **VALIDATED_BY_CI** counts toward parity (native execution on that platform). UNSUPPORTED, NOT_RUN, IN_PROGRESS and BLOCKED never do.", "",
             "| feature | " + " | ".join(platforms) + " |", "|---|" + "---|" * len(platforms)]
    short = {"VALIDATED_BY_CI": "VALIDATED", "UNSUPPORTED": "UNSUPPORTED", "IN_PROGRESS": "IN_PROGRESS", "BLOCKED": "BLOCKED", "NOT_RUN": "NOT_RUN", "IMPLEMENTED": "IMPLEMENTED"}
    for feature in doc["features"]:
        row = {e["platform"]: e for e in doc["entries"] if e["feature"] == feature["id"]}
        lines.append("| %s %s | " % (feature["id"], feature["name"]) + " | ".join(short[row[p]["status"]] for p in platforms) + " |")
    open_items = sorted(backlog(doc), key=lambda e: (order.get(e["platform"], 99), e["feature"]))
    lines += ["", "## Parity backlog (%d entries)" % len(open_items), ""]
    for e in open_items:
        lines.append("* **%s / %s %s** (%s): %s. Next: %s" % (e["platform"], e["feature"], e["feature_name"], e["status"], e["limitations"], e["next_action"]))
    return "\n".join(lines) + "\n"


def main(argv):
    command = argv[1] if len(argv) > 1 else "check"
    doc = load()
    if command == "check":
        found = problems(doc)
        for line in found:
            print("FAIL " + line, file=sys.stderr)
        print("parity matrix: %d entries, %d in the backlog" % (len(doc["entries"]), len(backlog(doc))))
        return 1 if found else 0
    if command == "backlog":
        for e in backlog(doc):
            print("%s/%s %s: %s" % (e["platform"], e["feature"], e["status"], e["feature_name"]))
        print("backlog: %d" % len(backlog(doc)))
        return 0
    if command == "render":
        text = render(doc)
        if "--write" in argv:
            (ROOT / "docs/parity/README.md").write_text(text, encoding="utf-8")
        else:
            print(text)
        return 0
    print(__doc__)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv))
