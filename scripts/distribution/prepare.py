#!/usr/bin/env python3
"""Prepare ONLY a disposable checkout for a source-free candidate build.

Legacy resources and tools/native-ci stay untouched in the real source checkout.
Every native generation in this checkout consumes the same generated input.
"""
import argparse
import hashlib
import json
import re
import subprocess
import shutil
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
EMPTY = '// Candidate distribution: importable Bunko sources are external.\nwaza data_files() -> []File { kaesu []; }\n'


def prepare(root, revision, stable_version=None):
    if not re.fullmatch(r'[0-9a-f]{40}', revision):
        raise ValueError('expected full source commit SHA')
    resource = root / 'selfhost/resources/bunko_data.mio'
    original_hash = hashlib.sha256(resource.read_bytes()).hexdigest()
    resource.write_text(EMPTY, encoding='utf-8')
    cli = root / 'selfhost/cli/main.mio'
    version = '2.1.0-pr.' + revision[:12]
    if stable_version is not None:
        if not re.fullmatch(r'2\.[1-9]\d*\.(?:0|[1-9]\d*)', stable_version):
            raise ValueError('stable 2.1+ version required')
        version = stable_version
    old = 'mio v2.0.0-dev (pure Mio Native AOT; GF3 candidate)'
    text = cli.read_text(encoding='utf-8')
    if old not in text:
        raise ValueError('unexpected CLI version template')
    cli.write_text(text.replace(old, f'mio v{version} ({"release" if stable_version else "candidate"}; commit {revision}; pure Mio Native AOT)'), encoding='utf-8')
    record = dict(schema_version=1, version=version, source_sha=revision,
                  embedded_bunko=False, original_resource_sha256=original_hash,
                  generated_resource_sha256=hashlib.sha256(resource.read_bytes()).hexdigest())
    (root / 'distribution-input.json').write_text(json.dumps(record, indent=2)+'\n', encoding='utf-8')
    return record


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--stable-version', help='Release builds only; never promotes an existing candidate binary')
    parser.add_argument('--disposable-checkout', required=True, type=Path)
    args = parser.parse_args()
    destination = args.disposable_checkout.resolve()
    if destination == ROOT:
        parser.error('refusing to prepare the source checkout; copy it first')
    if destination.exists():
        parser.error('destination must be new')
    tracked = subprocess.check_output(['git', 'ls-files', '-z'], cwd=ROOT).decode().split('\0')
    for name in filter(None, tracked):
        src = ROOT / name
        if src.is_symlink():
            parser.error('symlink in source checkout: '+name)
        dest = destination / name
        dest.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(src, dest)
    revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    print(json.dumps(prepare(destination, revision, args.stable_version), indent=2))
