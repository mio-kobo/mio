#!/usr/bin/env python3
"""Retrieve and verify the pinned native bootstrap executable from an immutable Mio release."""
import argparse
from contextlib import closing
import hashlib
import json
import os
from pathlib import Path
import platform
import struct
import subprocess
import sys
import tempfile
from urllib.error import HTTPError
from urllib.parse import quote, urlsplit
from urllib.request import HTTPRedirectHandler, Request, build_opener

ROOT = Path(__file__).resolve().parents[2]
DIST_DIR = ROOT / 'scripts' / 'distribution'
if str(DIST_DIR) not in sys.path:
    sys.path.insert(0, str(DIST_DIR))

from seed_evidence import ACTIVE_SEED_KIND, REPOSITORY, seed_identity

API = f'https://api.github.com/repos/{REPOSITORY}/releases'
CDN = 'release-assets.githubusercontent.com'
MAX_SEED_SIZE = 64 * 1024 * 1024
DEFAULT_SEED_DIR = ROOT / 'bootstrap' / 'seeds' / 'native-v21'

MH_MAGIC_64 = 0xFEEDFACF
CPU_TYPE_ARM64 = 0x0100000C
MH_EXECUTE = 2


class SeedUnavailable(Exception):
    """The approved public asset is missing or temporarily inaccessible."""


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def default_seed_path(identity=None):
    identity = identity or seed_identity(ACTIVE_SEED_KIND)
    return DEFAULT_SEED_DIR / identity.asset_name


def sha256_file(path):
    digest = hashlib.sha256()
    with Path(path).open('rb') as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b''):
            digest.update(block)
    return digest.hexdigest()


def verify_macho_arm64(path):
    """Verify the file is a 64-bit ARM64 Mach-O executable."""
    data = Path(path).read_bytes()
    if len(data) < 32:
        raise ValueError('seed binary is too small to be a Mach-O executable')
    magic, cputype, _cpusubtype, filetype = struct.unpack_from('<IIII', data, 0)
    if magic != MH_MAGIC_64 or cputype != CPU_TYPE_ARM64 or filetype != MH_EXECUTE:
        raise ValueError(
            f'seed binary is not a darwin-arm64 Mach-O executable '
            f'(magic=0x{magic:08x}, cputype=0x{cputype:08x}, filetype={filetype})'
        )


def verify_codesign(path):
    """Verify code signature on macOS."""
    if platform.system() != 'Darwin':
        return
    proc = subprocess.run(
        ['/usr/bin/codesign', '--verify', '--strict', str(path)],
        capture_output=True,
        text=True,
    )
    if proc.returncode != 0:
        detail = (proc.stderr or proc.stdout).strip()
        raise ValueError(f'seed code signature verification failed: {detail}')


def verify_seed_file(path, identity=None, check_arch=True, check_signature=True):
    """Verify an existing seed file against the pinned identity."""
    identity = identity or seed_identity(ACTIVE_SEED_KIND)
    path = Path(path)
    if not path.is_file():
        raise SeedUnavailable(f'seed file not found: {path}')
    actual_size = path.stat().st_size
    if identity.size is not None and actual_size != identity.size:
        raise ValueError(f'seed size mismatch: expected {identity.size}, got {actual_size}')
    actual_sha = sha256_file(path)
    if actual_sha != identity.sha256:
        raise ValueError(f'seed SHA-256 mismatch: expected {identity.sha256}, got {actual_sha}')
    if check_arch:
        verify_macho_arm64(path)
    if check_signature:
        verify_codesign(path)
    return dict(
        path=str(path),
        sha256=actual_sha,
        size=actual_size,
        tag=identity.tag,
        release_id=identity.release_id,
        asset_id=identity.asset_id,
        asset_name=identity.asset_name,
        platform=identity.platform,
    )


def request(url, accept, allow_cdn=False):
    # Do not forward API headers to the CDN or log its signed URL.
    headers = {'Accept': accept, 'User-Agent': 'mio-seed-source/1.0'}
    token = os.environ.get('GH_TOKEN') or os.environ.get('GITHUB_TOKEN')
    if token and url.startswith(API):
        headers['Authorization'] = f'Bearer {token}'
    opener = build_opener(NoRedirect())
    try:
        return opener.open(Request(url, headers=headers), timeout=30)
    except HTTPError as error:
        with closing(error):
            if error.code in (404, 410):
                raise SeedUnavailable('approved seed release or asset is unavailable') from None
            if 300 <= error.code < 400:
                location = error.headers.get('Location', '')
                dest = urlsplit(location)
                if (allow_cdn and error.code == 302 and dest.scheme == 'https'
                        and dest.netloc == CDN and dest.path.startswith('/github-production-release-asset')
                        and not dest.fragment):
                    return request(location, accept)
                raise ValueError('unexpected seed source redirect') from None
            detail = ''
            try:
                body = error.read(256).decode('utf-8', errors='replace').strip()
                if body:
                    detail = f': {body}'
            except OSError:
                pass
            raise ValueError(f'seed source returned HTTP {error.code}{detail}') from None


def download_seed(identity, destination, executable=False, verify_arch=False, verify_signature=False):
    destination = Path(destination)
    tag = quote(identity.tag, safe='')
    download_url = f'https://github.com/{REPOSITORY}/releases/download/{tag}/{identity.asset_name}'
    size = identity.size
    asset_url = f'{API}/assets/{identity.asset_id}'
    try:
        with request(f'{API}/tags/{tag}', 'application/vnd.github+json') as response:
            release = json.load(response)
        if (release.get('immutable') is not True or release.get('draft') is not False
                or release.get('id') != identity.release_id or release.get('tag_name') != identity.tag):
            raise ValueError('seed release identity or immutability did not match the pin')

        assets = [asset for asset in release['assets'] if asset.get('id') == identity.asset_id]
        if not assets:
            raise SeedUnavailable('approved seed asset is missing from its immutable release')
        asset, = assets
        size = asset.get('size')
        if (asset.get('name') != identity.asset_name or asset.get('state') != 'uploaded'
                or asset.get('url') != asset_url or asset.get('browser_download_url') != download_url
                or asset.get('digest') not in (None, 'sha256:' + identity.sha256)
                or type(size) is not int or not 0 < size <= MAX_SEED_SIZE
                or ((verify_arch or verify_signature) and identity.size is not None and size != identity.size)):
            raise ValueError('seed asset metadata did not match the approved source')
        stream_url = asset_url
    except ValueError as error:
        if 'HTTP 403' not in str(error) or 'rate limit' not in str(error).lower() or size is None:
            raise
        stream_url = download_url

    destination.parent.mkdir(parents=True, exist_ok=True)
    fd, tmp_name = tempfile.mkstemp(prefix='.seed-', suffix='.tmp', dir=str(destination.parent))
    tmp_path = Path(tmp_name)
    try:
        sha = hashlib.sha256()
        with request(stream_url, 'application/octet-stream', allow_cdn=True) as response, os.fdopen(fd, 'wb') as stream:
            if response.status != 200:
                raise ValueError('expected a complete seed download')
            remaining = size
            while remaining:
                chunk = response.read(min(remaining, 1024 * 1024))
                if not chunk:
                    raise ValueError('incomplete seed download')
                sha.update(chunk)
                stream.write(chunk)
                remaining -= len(chunk)
            if response.read(1):
                raise ValueError('seed download exceeds its declared size')
        if sha.hexdigest() != identity.sha256:
            raise ValueError('downloaded seed does not match the pinned SHA-256')
        if verify_arch:
            verify_macho_arm64(tmp_path)
        if verify_signature:
            verify_codesign(tmp_path)
        tmp_path.chmod(0o755 if executable else 0o644)
        os.replace(tmp_path, destination)
    except BaseException:
        try:
            tmp_path.unlink()
        except OSError:
            pass
        raise


def ensure_seed(destination=None, identity=None):
    identity = identity or seed_identity(ACTIVE_SEED_KIND)
    destination = Path(destination) if destination else default_seed_path(identity)
    if destination.is_file():
        try:
            info = verify_seed_file(destination, identity, check_arch=True, check_signature=True)
            destination.chmod(0o755)
            return info
        except ValueError:
            pass
    download_seed(identity, destination, executable=True, verify_arch=True, verify_signature=True)
    return verify_seed_file(destination, identity, check_arch=True, check_signature=True)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    group = parser.add_mutually_exclusive_group(required=True)
    group.add_argument('--check', action='store_true', help='verify pinned seed identity and local seed file')
    group.add_argument('--ensure', action='store_true', help='download (if needed) and verify the pinned seed')
    parser.add_argument('--seed', default=ACTIVE_SEED_KIND, help='seed kind (default: native-v21)')
    parser.add_argument('--path', type=Path, help='target seed file path (default: bootstrap/seeds/native-v21/mio-v2.1.0-darwin-arm64)')
    args = parser.parse_args(argv)

    identity = seed_identity(args.seed)
    if identity is None:
        print(f'error: no immutable identity for seed kind {args.seed}', file=sys.stderr)
        return 1
    target = args.path or (Path(os.environ['MIO_NATIVE_SEED']) if os.environ.get('MIO_NATIVE_SEED') else default_seed_path(identity))

    try:
        if args.check:
            info = verify_seed_file(target, identity, check_arch=True, check_signature=True)
        else:
            info = ensure_seed(target, identity)
    except (SeedUnavailable, ValueError, OSError) as error:
        print(f'error: {error}', file=sys.stderr)
        return 2 if isinstance(error, SeedUnavailable) else 1
    print(json.dumps(dict(status='PASS', kind=args.seed, **info), indent=2))
    return 0


if __name__ == '__main__':
    sys.exit(main())
