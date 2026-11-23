#!/usr/bin/env python3
"""Development-only source/packaging audit. Never imported by the compiler."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import struct
import tarfile

ROOT = Path(__file__).resolve().parents[2]
ALLOWED = {"mio", "LICENSE", "README.txt"}


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def source_audit():
    production = (ROOT / "tools/native-ci/main.mio").read_text(encoding="utf-8")
    bundle = production.split("waza bundle_files(", 1)[1].split("waza package_image(", 1)[0]
    # 兼容 mio fmt 标准空格格式化后的结构体字面量
    require(set(re.findall(r'Payload\s*\{\s*name:\s*"([^"]+)"', bundle)) == ALLOWED, "release payload allowlist changed")
    require("verify_no_foreign_runtime(image)" in production, "release no-foreign-runtime audit missing")
    policy = (ROOT / "tools/native-ci/production.sb").read_text()
    require('(deny process-exec)' in policy and '(param "SEED")' in policy, "production execution isolation missing")
    workflow = (ROOT / ".github/workflows/release.yml").read_text()
    require("PATH: ''" in workflow and "production.sb" in workflow and "MIO_NATIVE_SEED" in workflow, "native release isolation contract missing")
    require(not re.search(r'(?i)setup-(go|python|node)|pip install|scripts/ci', workflow), "release now depends on development testing tools")
    # Audit explicit imports and embedded resource paths, without matching documentation strings.
    sources = list((ROOT / "selfhost").rglob("*.mio")) + list((ROOT / "bunko").rglob("*.mio"))
    for path in sources:
        for imported in re.findall(r'^\s*yobu\s+"([^"]+)"', path.read_text(encoding="utf-8"), re.MULTILINE):
            require(not imported.startswith(("scripts/ci", "tools/native-ci", "tests/")), "Core imports development code: " + str(path))
    resources = (ROOT / "selfhost/resources/bunko_data.mio").read_text(encoding="utf-8")
    for name in re.findall(r'\bpath:\s*"([^"]+)"', resources):
        require(not name.endswith((".py", ".sh", ".go", ".json")) and not name.startswith(("scripts/", "tests/")), "test tool embedded in release resources: " + name)
    return dict(source_policy="PASS", package_allowlist=sorted(ALLOWED),
                release_dependencies="existing trusted native seed and macOS system facilities only",
                actual_native_release_build="NOT_RUN", actual_archive="NOT_RUN")


def archive_audit(path):
    with tarfile.open(path, "r:gz") as archive:
        members = archive.getmembers()
        require(len(members) == 3 and {m.name for m in members} == ALLOWED, "unexpected or duplicate release payload; external tools must not ship")
        require(all(m.isfile() for m in members), "release must contain regular files only")
        image = archive.extractfile("mio").read()
        license_data = archive.extractfile("LICENSE").read()
    require(license_data == (ROOT / "LICENSE").read_bytes(), "release license mismatch")
    require(len(image) >= 768, "native image missing or truncated")
    magic, cpu, _, kind = struct.unpack_from("<4I", image)
    require((magic, cpu, kind) == (0xFEEDFACF, 0x100000C, 2), "release is not native ARM64 Mach-O")
    for marker in (b"\xff Go buildinf:", b"__go_buildid", b"__gopclntab", b"scripts/ci/runner.py", b"libpython", b"Python.framework"):
        require(marker not in image, "foreign runtime or development driver in release: " + repr(marker))
    return dict(actual_archive="PASS", archive_path=str(Path(path).resolve()), archive_sha256=hashlib.sha256(Path(path).read_bytes()).hexdigest(), native_sha256=hashlib.sha256(image).hexdigest())


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--archive", type=Path)
    args = parser.parse_args()
    result = source_audit()
    if args.archive:
        result.update(archive_audit(args.archive))
    print(json.dumps(result, indent=2))
