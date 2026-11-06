#!/usr/bin/env python3
"""Prepare ONLY a disposable checkout for a source-free candidate build.

Legacy resources and tools/native-ci stay untouched in the real source checkout.
Every native generation in this checkout consumes the same generated input.
"""
import argparse
import os
import hashlib
import json
import re
import subprocess
import shutil
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
EMPTY = '// Candidate distribution: importable Bunko sources are external.\nwaza data_files() -> []File { kaesu []; }\n'


def prepare(root, revision, stable_version=None, rc_version=None):
    if not re.fullmatch(r'[0-9a-f]{40}', revision):
        raise ValueError('expected full source commit SHA')
    version = '2.1.0-pr.' + revision[:12]
    if stable_version is not None and rc_version is not None:
        raise ValueError('stable and RC versions are mutually exclusive')
    if stable_version is not None:
        if not re.fullmatch(r'2\.[1-9]\d*\.(?:0|[1-9]\d*)', stable_version):
            raise ValueError('stable 2.1+ version required')
        version = stable_version
    if rc_version is not None:
        if not re.fullmatch(r'2\.[1-9]\d*\.(?:0|[1-9]\d*)-rc\.[1-9]\d*', rc_version):
            raise ValueError('2.1+ RC version required')
        version = rc_version
    resource = root / 'selfhost/resources/bunko_data.mio'
    original_hash = hashlib.sha256(resource.read_bytes()).hexdigest()
    cli = root / 'selfhost/cli/main.mio'
    old = 'mio v2.0.0-dev (pure Mio Native AOT; GF3 candidate)'
    text = cli.read_text(encoding='utf-8')
    if old not in text:
        raise ValueError('unexpected CLI version template')
    resource.write_text(EMPTY, encoding='utf-8')
    cli.write_text(text.replace(old, f'mio v{version} ({"release" if stable_version else "release candidate" if rc_version else "candidate"}; commit {revision}; pure Mio Native AOT)'), encoding='utf-8')
    record = dict(schema_version=1, version=version, source_sha=revision,
                  embedded_bunko=False, original_resource_sha256=original_hash,
                  generated_resource_sha256=hashlib.sha256(resource.read_bytes()).hexdigest())
    (root / 'distribution-input.json').write_text(json.dumps(record, indent=2)+'\n', encoding='utf-8')
    return record


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--stable-version', default=os.environ.get('MIO_DISTRIBUTION_STABLE_VERSION') or None, help='Release builds only; never promotes an existing candidate binary')
    parser.add_argument('--rc-version', default=os.environ.get('MIO_DISTRIBUTION_RC_VERSION') or None, help='Pre-release builds only; dispatch on the exact existing RC tag')
    parser.add_argument('--disposable-checkout', required=True, type=Path)
    args = parser.parse_args()
    if args.stable_version and args.rc_version:
        parser.error('stable and RC versions are mutually exclusive')
    requested_version = args.stable_version or args.rc_version
    if requested_version and os.environ.get('GITHUB_ACTIONS') == 'true':
        if os.environ.get('GITHUB_EVENT_NAME') != 'workflow_dispatch' or os.environ.get('GITHUB_REF') != 'refs/tags/v' + requested_version:
            parser.error('release native build requires dispatch on its exact pre-existing tag')
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
    print(json.dumps(prepare(destination, revision, args.stable_version, args.rc_version), indent=2))
