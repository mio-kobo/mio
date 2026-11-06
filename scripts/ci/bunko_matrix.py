#!/usr/bin/env python3
"""Bunko public-function test matrix: which native tests exercise every public function of bunko/kiso, and their
result on each native platform.

The inventory is generated from the source (every `koukai waza` of every package under bunko/kiso), so a new public
function appears automatically. A function is covered by a test unit when the unit references it:
  * a `*_test.mio` package (bunko/ or selfhost/): an unqualified call inside the function's own package, or
    `<alias>.<name>(` where <alias> is the last segment of a `yobu` import of the function's package;
  * a cross-platform case in tests/native-differential/cases or tests/runtime-abi/cases (same reference rule);
  * a runner protocol of scripts/ci/native_platforms.py listed in bunko_matrix_coverage.json (child processes, exec,
    exit, test channel), for functions that cannot be called inside a test image.

Results come from the native reports of .github/workflows/native.yml (native-report-<target>/report.json and the
differential results.json): a unit is PASS on a platform only if its check is PASS there. A function is PASS when at
least one covering unit is PASS and none is FAIL; FAIL when any covering unit failed; UNSUPPORTED when
bunko_matrix_coverage.json records a technical reason for that platform; otherwise UNVERIFIED (no report, or no
covering unit ran). NOT_RUN and SKIP are never counted as PASS.

  bunko_matrix.py --check                  fail when a library function has no covering unit and no recorded reason
  bunko_matrix.py --reports DIR --output D  write D/bunko-test-matrix.json and D/bunko-test-matrix.md
"""
import argparse
import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
PLATFORMS = ("darwin-arm64", "linux-amd64", "windows-amd64")
COVERAGE_FILE = Path(__file__).with_name("bunko_matrix_coverage.json")
# Compiler/runtime implementation packages (code generators, object formats, collector, ABI) live under bunko/ but are
# toolchain internals, not the user library; they are listed with their coverage but --check does not require it.
INTERNAL_PREFIXES = ("kiso/aot/", "kiso/runtime/")
PUBLIC = re.compile(r"^\s*koukai\s+waza\s+([A-Za-z_][A-Za-z0-9_]*)", re.M)
IMPORT = re.compile(r'^\s*yobu\s+"([^"]+)"\s*;', re.M)
FUNCTION = re.compile(r"^\s*(?:koukai\s+)?waza\s+([A-Za-z_][A-Za-z0-9_]*)", re.M)


def packages():
    """{module: {"dir": Path, "functions": [names]}} for every importable package under bunko/kiso."""
    found = {}
    for directory in sorted({p.parent for p in (ROOT / "bunko/kiso").rglob("*.mio")}):
        relative = directory.relative_to(ROOT / "bunko").as_posix()
        if any("." in part for part in relative.split("/")):
            continue  # not an importable package path (e.g. a kept pre-patch snapshot)
        names = []
        for source in sorted(directory.glob("*.mio")):
            if source.name.endswith("_test.mio"):
                continue
            for name in PUBLIC.findall(source.read_text(encoding="utf-8")):
                if name not in names:
                    names.append(name)
        if names:
            found[relative] = {"dir": directory, "functions": names}
    return found


def strip_comments(text):
    return re.sub(r"//[^\n]*", "", text)


def references(text, module, own_package):
    """Function names of `module` referenced by a source text."""
    text = strip_comments(text)
    names = set()
    aliases = {imported.split("/")[-1] for imported in IMPORT.findall(text) if imported == module}
    for alias in aliases:
        names.update(re.findall(r"(?<![A-Za-z0-9_.])%s\.([A-Za-z_][A-Za-z0-9_]*)\s*[\[(]" % re.escape(alias), text))
    if own_package:
        defined = set(FUNCTION.findall(text))
        for name in re.findall(r"(?<![A-Za-z0-9_.])([A-Za-z_][A-Za-z0-9_]*)\s*[\[(]", text):
            if name not in defined:
                names.add(name)
    return names


def test_units():
    """[(unit id, kind, source path, text)]: test packages, differential cases and contract cases."""
    units = []
    for top in ("bunko", "selfhost"):
        for package in sorted({p.parent for p in (ROOT / top).rglob("*_test.mio")}):
            text = "\n".join(p.read_text(encoding="utf-8") for p in sorted(package.glob("*_test.mio")))
            units.append(("mio-test " + package.relative_to(ROOT).as_posix(), "package", package, text))
    for suite, folder in (("native-differential", "tests/native-differential/cases"), ("runtime-abi-contract", "tests/runtime-abi/cases")):
        for case in sorted((ROOT / folder).glob("*.mio")):
            units.append(("%s %s" % (suite, case.stem), "case", case, case.read_text(encoding="utf-8")))
    return units


def coverage_config():
    data = json.loads(COVERAGE_FILE.read_text(encoding="utf-8"))
    return data.get("protocols", {}), data.get("unsupported", {}), data.get("not_testable", {})


def build_inventory():
    found = packages()
    units = test_units()
    protocols, unsupported, not_testable = coverage_config()
    rows = []
    for module, info in found.items():
        covering = {name: [] for name in info["functions"]}
        for unit, kind, path, text in units:
            own = kind == "package" and path == info["dir"]
            for name in references(text, module, own) & set(info["functions"]):
                covering[name].append(unit)
        for name in info["functions"]:
            key = "%s.%s" % (module, name)
            units_for = sorted(set(covering[name]) | {"protocol " + p for p in protocols.get(key, [])})
            rows.append({
                "module": module,
                "function": name,
                "library": not module.startswith(INTERNAL_PREFIXES),
                "tests": units_for,
                "unsupported": unsupported.get(key, unsupported.get(module + ".*", {})),
                "not_testable": not_testable.get(key, ""),
            })
    return rows


def load_reports(directory):
    """{platform: {unit id: status}} from native-report-*/report.json and the differential/contract results."""
    results = {}
    if not directory:
        return results
    for report_path in sorted(Path(directory).rglob("report.json")):
        try:
            report = json.loads(report_path.read_text(encoding="utf-8"))
        except (OSError, ValueError):
            continue
        target = report.get("target")
        if target not in PLATFORMS:
            continue
        statuses = results.setdefault(target, {})
        for check in report.get("checks", []):
            name = check.get("name", "")
            if name.startswith("mio-test bunko/") or name.startswith("mio-test selfhost/"):
                statuses[name] = check.get("status")
            elif name in ("mio-run-protocol", "os-process-protocol", "mio-test-protocol"):
                statuses["protocol " + name] = check.get("status")
        for suite, folder in (("native-differential", "native-differential"), ("runtime-abi-contract", "runtime-abi")):
            results_path = report_path.parent / folder / "results.json"
            if not results_path.is_file():
                continue
            per_case = {}
            loaded = json.loads(results_path.read_text(encoding="utf-8"))
            for item in loaded.get("results", []) if isinstance(loaded, dict) else loaded:
                if item.get("engine") != "native":
                    continue
                case = "%s %s" % (suite, item.get("case") or item.get("name"))
                status = item.get("status")
                if status == "NOT_APPLICABLE":
                    continue
                previous = per_case.get(case, "PASS")
                per_case[case] = status if status != "PASS" else previous
            statuses.update(per_case)
    return results


def verdict(row, platform, statuses):
    reason = row["unsupported"].get(platform) if isinstance(row["unsupported"], dict) else ""
    if reason:
        return "UNSUPPORTED", reason
    if not row["tests"]:
        return "UNVERIFIED", row["not_testable"] or "no covering test"
    if statuses is None:
        return "UNVERIFIED", "no native report for this platform"
    seen = [statuses.get(unit) for unit in row["tests"]]
    if "FAIL" in seen:
        return "FAIL", ", ".join(u for u, s in zip(row["tests"], seen) if s == "FAIL")
    if "PASS" in seen:
        return "PASS", ""
    return "UNVERIFIED", "covering tests did not run (%s)" % ", ".join(sorted({str(s) for s in seen}))


def render(rows, results, output, note=""):
    output.mkdir(parents=True, exist_ok=True)
    table = []
    for row in rows:
        entry = dict(row)
        entry["results"] = {}
        for platform in PLATFORMS:
            status, detail = verdict(row, platform, results.get(platform))
            entry["results"][platform] = {"status": status, "note": detail}
        table.append(entry)
    summary = {p: {} for p in PLATFORMS}
    for entry in table:
        if entry["library"]:
            for p in PLATFORMS:
                s = entry["results"][p]["status"]
                summary[p][s] = summary[p].get(s, 0) + 1
    data = {"schema_version": 1, "platforms": list(PLATFORMS), "reports": sorted(results), "note": note, "summary_library": summary,
            "functions": table}
    (output / "bunko-test-matrix.json").write_text(json.dumps(data, indent=1, ensure_ascii=False) + "\n", encoding="utf-8")
    lines = ["# Bunko public-function test matrix", "",
             "Generated by `scripts/ci/bunko_matrix.py` from the source inventory and the native reports of "
             "`.github/workflows/native.yml`. PASS = a covering native test ran and passed on that platform; "
             "UNVERIFIED = no covering test ran there (or no report); UNSUPPORTED = recorded technical reason. "
             "Cross-compilation is never counted.", "",
             "Reports: " + (", ".join(sorted(results)) or "none"), ""] + ([note, ""] if note else []) + [
             "| Library functions | " + " | ".join(PLATFORMS) + " |", "| --- |" + " --- |" * len(PLATFORMS)]
    statuses = ("PASS", "FAIL", "UNSUPPORTED", "UNVERIFIED")
    for s in statuses:
        lines.append("| %s | %s |" % (s, " | ".join(str(summary[p].get(s, 0)) for p in PLATFORMS)))
    current = None
    for entry in table:
        if entry["module"] != current:
            current = entry["module"]
            kind = "library" if entry["library"] else "toolchain internal"
            lines += ["", "## %s (%s)" % (current, kind), "",
                      "| Function | " + " | ".join(PLATFORMS) + " | Tests |", "| --- |" + " --- |" * (len(PLATFORMS) + 1)]
        cells = []
        for p in PLATFORMS:
            r = entry["results"][p]
            cells.append(r["status"] + (" (%s)" % r["note"] if r["status"] in ("UNSUPPORTED", "FAIL") else ""))
        tests = "<br>".join(entry["tests"]) if entry["tests"] else "none: " + (entry["not_testable"] or "no covering test")
        lines.append("| `%s` | %s | %s |" % (entry["function"], " | ".join(cells), tests))
    (output / "bunko-test-matrix.md").write_text("\n".join(lines) + "\n", encoding="utf-8")
    return data


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--reports", help="directory holding native-report-*/report.json")
    parser.add_argument("--output", help="directory for bunko-test-matrix.json / .md")
    parser.add_argument("--check", action="store_true", help="fail when a library function has no covering test")
    parser.add_argument("--note", default="", help="provenance line written into the matrix (where the reports come from)")
    parser.add_argument("--require-pass", action="store_true",
                        help="with --reports: fail unless every covered library function is PASS or UNSUPPORTED on every platform")
    args = parser.parse_args()
    rows = build_inventory()
    problems = []
    if args.check:
        for row in rows:
            if row["library"] and not row["tests"] and not row["not_testable"]:
                problems.append("%s.%s has no covering native test" % (row["module"], row["function"]))
    if args.output:
        data = render(rows, load_reports(args.reports), Path(args.output), args.note)
        if args.require_pass:
            for entry in data["functions"]:
                if not entry["library"] or not entry["tests"]:
                    continue
                for p in PLATFORMS:
                    if entry["results"][p]["status"] not in ("PASS", "UNSUPPORTED"):
                        problems.append("%s.%s on %s: %s %s" % (entry["module"], entry["function"], p,
                                                                entry["results"][p]["status"], entry["results"][p]["note"]))
    library = [r for r in rows if r["library"]]
    print("bunko public functions: %d (library %d, covered %d)" % (len(rows), len(library), sum(1 for r in library if r["tests"])))
    for problem in problems:
        print("FAIL: " + problem)
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
