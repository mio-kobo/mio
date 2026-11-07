"""Retrieve the pinned native bootstrap executable from an immutable Mio release."""
from contextlib import closing
import hashlib
import json
from urllib.error import HTTPError
from urllib.parse import quote, urlsplit
from urllib.request import HTTPRedirectHandler, Request, build_opener

from seed_evidence import REPOSITORY

API = f'https://api.github.com/repos/{REPOSITORY}/releases'
CDN = 'release-assets.githubusercontent.com'
MAX_SEED_SIZE = 64 * 1024 * 1024


class SeedUnavailable(Exception):
    """The approved public asset is missing or temporarily inaccessible."""


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def request(url, accept, allow_cdn=False):
    # Do not forward API headers to the CDN or log its signed URL.
    opener = build_opener(NoRedirect())
    try:
        return opener.open(Request(url, headers={'Accept': accept}), timeout=30)
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
            raise ValueError(f'seed source returned HTTP {error.code}') from None


def download_seed(identity, destination):
    tag = quote(identity.tag, safe='')
    with request(f'{API}/tags/{tag}', 'application/vnd.github+json') as response:
        release = json.load(response)
    if (release.get('immutable') is not True or release.get('draft') is not False
            or release.get('id') != identity.release_id or release.get('tag_name') != identity.tag):
        raise ValueError('seed release identity or immutability did not match the pin')

    url = f"{API}/assets/{identity.asset_id}"
    download_url = f'https://github.com/{REPOSITORY}/releases/download/{tag}/{identity.asset_name}'
    assets = [asset for asset in release['assets'] if asset.get('id') == identity.asset_id]
    if not assets:
        raise SeedUnavailable('approved seed asset is missing from its immutable release')
    asset, = assets
    size = asset.get('size')
    if (asset.get('name') != identity.asset_name or asset.get('state') != 'uploaded'
            or asset.get('url') != url or asset.get('browser_download_url') != download_url
            or asset.get('digest') not in (None, 'sha256:' + identity.sha256)
            or type(size) is not int or not 0 < size <= MAX_SEED_SIZE):
        raise ValueError('seed asset metadata did not match the approved source')

    sha = hashlib.sha256()
    with request(url, 'application/octet-stream', allow_cdn=True) as response, destination.open('xb') as stream:
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
