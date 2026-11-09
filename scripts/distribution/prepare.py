#!/usr/bin/env python3
"""Prepare ONLY a disposable checkout for a source-free candidate build.

Legacy resources and tools/native-ci stay untouched in the real source checkout.
Every native generation in this checkout consumes the same generated input.

The version is resolved from the committed CLI source, so a compiler built from
one release line can never be silently labeled as another. With
``--stable-version`` or ``--rc-version`` the requested release base version must
equal the source base version, and the generated CLI version is the exact
requested version.
"""
import argparse
import hashlib
import json
import os
import re
import shutil
import subprocess
import tarfile
import tempfile
from pathlib import Path

from versions import VersionError, parse_version, tag_for

ROOT = Path(__file__).resolve().parents[2]
EMPTY = '// Core distribution: Bunko is not embedded.\nwaza data_available() -> bool { kaesu iie; }\nwaza data_files() -> []File { kaesu []; }\n'
SOURCE_VERSION = re.compile(
    r'mio v(?P<version>(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*))'
    r'(?P<suffix>-[A-Za-z0-9.]+)? \(pure Mio Native AOT; [^)\n]*\)')


def source_version(text):
    """Return the development version declared by the committed CLI source."""
    matches = list(SOURCE_VERSION.finditer(text))
    if len(matches) != 1:
        raise ValueError('unexpected CLI version template')
    try:
        version = parse_version(matches[0].group('version'))
    except VersionError as error:
        raise ValueError('unexpected CLI source version: ' + str(error)) from error
    return matches[0], version


def prepare(root, revision, stable_version=None, rc_version=None):
    if not re.fullmatch(r'[0-9a-f]{40}', revision):
        raise ValueError('expected full source commit SHA')
    if stable_version is not None and rc_version is not None:
        raise ValueError('stable and RC versions are mutually exclusive')
    requested = None
    if stable_version is not None:
        try:
            requested = parse_version(stable_version)
        except VersionError as error:
            raise ValueError('stable release version required: ' + str(error)) from error
        if requested.is_rc:
            raise ValueError('stable release version required')
    if rc_version is not None:
        try:
            requested = parse_version(rc_version)
        except VersionError as error:
            raise ValueError('release candidate version required: ' + str(error)) from error
        if not requested.is_rc:
            raise ValueError('release candidate version required')

    resource = root / 'selfhost/resources/bunko_data.mio'
    resource_data = resource.read_text(encoding='utf-8')
    if 'waza data_available() -> bool { kaesu hai; }' not in resource_data:
        raise ValueError('Full distribution requires a populated embedded Bunko resource')
    resource_hash = hashlib.sha256(resource.read_bytes()).hexdigest()

    cli = root / 'selfhost/cli/main.mio'
    text = cli.read_text(encoding='utf-8')
    match, declared = source_version(text)
    if requested is not None and declared.base != requested.base:
        raise ValueError('source declares Mio %s but release %s was requested' % (declared, requested))
    version = requested if requested is not None else '%s-pr.%s' % (declared, revision[:12])
    label = 'release' if requested is not None and not requested.is_rc else \
        'release candidate' if requested is not None else 'candidate'
    replacement = 'mio v%s (%s; commit %s; pure Mio Native AOT)' % (version, label, revision)
    updated = text.replace(match.group(0), replacement, 1)
    if replacement not in updated:
        raise ValueError('generated CLI version was not written')
    with cli.open('w', encoding='utf-8', newline='\n') as stream:
        stream.write(updated)
    record = dict(schema_version=1, version=str(version), source_sha=revision,
                  source_version=str(declared),
                  embedded_bunko=True, resource_sha256=resource_hash,
                  core_resource_sha256=hashlib.sha256(EMPTY.encode('utf-8')).hexdigest(),
                  generated_cli_sha256=hashlib.sha256(cli.read_bytes()).hexdigest())
    (root / 'distribution-input.json').write_text(json.dumps(record, indent=2) + '\n', encoding='utf-8')
    return record


def copy_committed_source(root, revision, destination):
    # git archive also applies Git's output line endings. Use LF consistently
    # so a Windows checkout and a Darwin checkout produce identical inputs.
    with tempfile.TemporaryFile() as stream:
        subprocess.run(['git', '-c', 'core.autocrlf=false', '-c', 'core.eol=lf',
                        'archive', '--format=tar', revision], cwd=root, stdout=stream, check=True)
        stream.seek(0)
        with tarfile.open(fileobj=stream) as archive:
            if any(not (member.isfile() or member.isdir()) for member in archive.getmembers()):
                raise ValueError('non-regular member in stable source archive')
            try:
                archive.extractall(destination, filter='data')
            except TypeError:  # Python < 3.11.4 has no extraction filter; members were checked above.
                archive.extractall(destination)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--stable-version', default=os.environ.get('MIO_DISTRIBUTION_STABLE_VERSION') or None,
                        help='Release builds only; never promotes an existing candidate binary')
    parser.add_argument('--rc-version', default=os.environ.get('MIO_DISTRIBUTION_RC_VERSION') or None,
                        help='Pre-release builds only; dispatch on the exact existing RC tag')
    parser.add_argument('--disposable-checkout', required=True, type=Path)
    args = parser.parse_args()
    if args.stable_version and args.rc_version:
        parser.error('stable and RC versions are mutually exclusive')
    requested_version = args.stable_version or args.rc_version
    if requested_version:
        try:
            parsed = parse_version(requested_version)
        except VersionError as error:
            parser.error(str(error))
        if args.stable_version and parsed.is_rc:
            parser.error('stable release version required')
        if args.rc_version and not parsed.is_rc:
            parser.error('release candidate version required')
        if os.environ.get('GITHUB_ACTIONS') == 'true':
            if (os.environ.get('GITHUB_EVENT_NAME') != 'workflow_dispatch'
                    or os.environ.get('GITHUB_REF') != 'refs/tags/' + tag_for(parsed)):
                parser.error('release native build requires dispatch on its exact pre-existing tag')
    destination = args.disposable_checkout.resolve()
    if destination == ROOT:
        parser.error('refusing to prepare the source checkout; copy it first')
    if destination.exists():
        parser.error('destination must be new')
    revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    if requested_version and os.environ.get('GITHUB_ACTIONS') == 'true' and revision != os.environ.get('GITHUB_SHA'):
        parser.error('release checkout does not match the Actions source commit')
    if args.stable_version:
        copy_committed_source(ROOT, revision, destination)
    else:
        tracked = subprocess.check_output(['git', 'ls-files', '-z'], cwd=ROOT).decode().split('\0')
        for name in filter(None, tracked):
            src = ROOT / name
            if src.is_symlink():
                parser.error('symlink in source checkout: ' + name)
            dest = destination / name
            dest.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(src, dest)
    print(json.dumps(prepare(destination, revision, args.stable_version, args.rc_version), indent=2))
