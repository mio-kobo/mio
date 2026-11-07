"""Restore the original native bootstrap logs omitted from the v2.1.0 Release."""
import gzip
import hashlib
import json
from pathlib import Path, PurePosixPath
import re
import subprocess
import tarfile
import tempfile
import zlib

from package import digest
from seed_evidence import NATIVE_V21, REPOSITORY, SEED_LOGS, seed_identity
from stable_release import authenticate_report

STABLE_TAG = 'v2.1.0'
EVIDENCE_TAG = 'v2.1.0-evidence.1'
LOG_DIR = seed_identity(NATIVE_V21).log_dir
ARCHIVE = f'{LOG_DIR}.tar.gz'
MAX_ARCHIVE_SIZE = 1024 * 1024
RELEASE_PURL = f'pkg:github/{REPOSITORY}@{EVIDENCE_TAG}'
PAX_KEYS = {b'LIBARCHIVE.xattr.com.apple.provenance', b'SCHILY.xattr.com.apple.provenance'}


def github(*args):
    try:
        return subprocess.check_output(['gh', *args], text=True)
    except (OSError, subprocess.CalledProcessError) as error:
        raise ValueError(f'could not verify or retrieve supplemental evidence for {EVIDENCE_TAG}') from error


def download_archive(directory, source):
    release = json.loads(github('api', '--hostname', 'github.com', f'repos/{REPOSITORY}/releases/tags/{EVIDENCE_TAG}'))
    if (release.get('tag_name') != EVIDENCE_TAG or release.get('immutable') is not True
            or release.get('draft') is not False or release.get('prerelease') is not True
            or not release.get('published_at') or type(release.get('id')) is not int):
        raise ValueError('expected the published immutable supplemental prerelease')
    assets = release['assets']
    url = f'https://github.com/{REPOSITORY}/releases/download/{EVIDENCE_TAG}/{ARCHIVE}'
    if len(assets) != 1 or assets[0].get('name') != ARCHIVE:
        raise ValueError('expected the supplemental evidence archive as the only release asset')
    asset = assets[0]
    size = asset.get('size')
    if (asset.get('state') != 'uploaded' or asset.get('browser_download_url') != url
            or type(size) is not int or not 0 < size <= MAX_ARCHIVE_SIZE):
        raise ValueError('invalid supplemental evidence asset metadata')
    commit = json.loads(github('api', '--hostname', 'github.com', f'repos/{REPOSITORY}/commits/refs/tags/{EVIDENCE_TAG}'))
    if commit.get('sha') != source:
        raise ValueError('supplemental tag does not resolve to the stable source commit')

    repo = f'github.com/{REPOSITORY}'
    github('release', 'download', EVIDENCE_TAG, '--repo', repo, '--pattern', ARCHIVE, '--dir', str(directory))
    archive = directory / ARCHIVE
    if archive.stat().st_size != size:
        raise ValueError('supplemental evidence asset size mismatch')
    sha = digest(archive)
    if asset.get('digest') != 'sha256:' + sha:
        raise ValueError('supplemental evidence asset digest mismatch')
    verified = json.loads(github('release', 'verify-asset', EVIDENCE_TAG, str(archive), '--repo', repo, '--format', 'json'))
    statement = verified['verificationResult']['statement']
    predicate = statement['predicate']
    if (statement.get('predicateType') != 'https://in-toto.io/attestation/release/v0.2'
            or predicate.get('repository') != REPOSITORY or predicate.get('tag') != EVIDENCE_TAG
            or predicate.get('purl') != RELEASE_PURL or predicate.get('databaseId') != str(release['id'])):
        raise ValueError('supplemental attestation identifies a different release')
    subjects = statement['subject']
    if {'uri': RELEASE_PURL, 'digest': {'sha1': source}} not in subjects:
        raise ValueError('supplemental attestation does not bind the stable source commit')
    if {'name': ARCHIVE, 'digest': {'sha256': sha}} not in subjects:
        raise ValueError('supplemental attestation does not bind the evidence archive')
    return archive


def check_pax(name, data, member_name):
    # The published archive has local macOS provenance xattrs. Ignore their
    # values and disallow PAX fields that could alter the actual tar entry.
    path = PurePosixPath(member_name)
    if name != str(path.parent / 'PaxHeader' / path.name):
        raise ValueError('unexpected supplemental archive PAX header')
    seen = set()
    while data:
        match = re.match(rb'([1-9][0-9]{0,6}) ([^=\n]+)=', data)
        if not match:
            raise ValueError('malformed supplemental archive PAX record')
        size = int(match[1])
        key = match[2]
        if (not match.end() < size <= len(data) or data[size - 1] != 10
                or key not in PAX_KEYS or key in seen):
            raise ValueError('invalid supplemental archive PAX record')
        seen.add(key)
        data = data[size:]
    if not seen:
        raise ValueError('empty supplemental archive PAX header')


def read_logs(archive, hashes):
    # Read the gzip trailer and bound expansion before interpreting tar headers.
    try:
        with gzip.open(archive, 'rb') as stream:
            data = stream.read(MAX_ARCHIVE_SIZE + 1)
    except (OSError, EOFError, zlib.error) as error:
        raise ValueError('malformed supplemental gzip archive') from error
    block = tarfile.BLOCKSIZE
    zero_block = bytes(block)
    if len(data) > MAX_ARCHIVE_SIZE or len(data) % block:
        raise ValueError('invalid supplemental tar archive size')

    logs = {}
    seen = set()
    offset = 0
    pax = None
    while data[offset:offset + block] != zero_block:
        try:
            member = tarfile.TarInfo.frombuf(data[offset:offset + block], 'utf-8', 'surrogateescape')
        except tarfile.HeaderError as error:
            raise ValueError('malformed supplemental tar header') from error
        start = offset + block
        end = start + member.size
        offset = end + (-member.size % block)
        if member.size < 0 or offset > len(data) or any(data[end:offset]):
            raise ValueError('malformed supplemental tar member')
        content = data[start:end]
        if member.type == tarfile.XHDTYPE and pax is None:
            pax = (member.name, content)
            continue
        if member.name in seen:
            raise ValueError('duplicate supplemental archive member: ' + member.name)
        seen.add(member.name)
        if member.type == tarfile.REGTYPE and member.name in hashes:
            if hashlib.sha256(content).hexdigest() != hashes[member.name]:
                raise ValueError('log does not match signed release evidence: ' + member.name)
            logs[member.name] = content
        elif member.type != tarfile.DIRTYPE or member.name != LOG_DIR or member.size != 0:
            raise ValueError('unexpected supplemental archive member: ' + member.name)
        if pax is not None:
            name, metadata = pax
            check_pax(name, metadata, member.name)
            pax = None
    if pax is not None or len(data) - offset < 2 * block or any(data[offset:]):
        raise ValueError('malformed supplemental tar archive ending')
    if set(logs) != set(hashes):
        raise ValueError('supplemental archive is missing expected logs')
    return logs


def restore_logs(assets, tag, source):
    if tag != STABLE_TAG:
        return
    destination = assets / LOG_DIR
    if destination.is_symlink():
        raise ValueError('supplemental log destination is a symlink')
    if destination.is_dir():
        # The caller still authenticates every log. Never repair or replace an
        # existing directory, including one that is incomplete or modified.
        return
    evidence, kind = authenticate_report(assets, tag, source)
    if kind != NATIVE_V21:
        raise ValueError('supplemental logs require the original native-v21 release evidence')
    hashes = {f'{LOG_DIR}/{name}': evidence['files'][f'{LOG_DIR}/{name}'] for name in SEED_LOGS}
    with tempfile.TemporaryDirectory(dir=assets) as temp:
        directory = Path(temp)
        logs = read_logs(download_archive(directory, source), hashes)
        staged = directory / LOG_DIR
        staged.mkdir()
        for name in SEED_LOGS:
            (staged / name).write_bytes(logs[f'{LOG_DIR}/{name}'])
        staged.rename(destination)
