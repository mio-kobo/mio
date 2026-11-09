#!/usr/bin/env python3
"""Submit to CCR after maintainer confirmation, with a scoped v2.1.0 repair option."""
import argparse
from base64 import b64decode
from email.utils import parsedate_to_datetime
from hashlib import sha512
from http.client import HTTPException
from io import BytesIO
import os
from pathlib import Path
import re
import subprocess
from time import sleep, time
from urllib.error import HTTPError
from urllib.request import Request, urlopen
import xml.etree.ElementTree as ET
from zipfile import BadZipFile, ZipFile

from chocolatey import FEED, SOURCE
from release import stable


PACKAGE_NAMES = ('mio-core', 'mio')
REPAIR_VERSION = '2.1.0'
REPAIR_STATUSES = {'Waiting', 'Pending', 'Responded', 'Updated'}
METADATA_BACKOFF = (2, 5, 15, None)
METADATA_RETRY_STATUS = {502, 503, 504}
METADATA_MAX_RETRY_DELAY = 30
ODATA = {
    'a': 'http://www.w3.org/2005/Atom',
    'm': 'http://schemas.microsoft.com/ado/2007/08/dataservices/metadata',
    'd': 'http://schemas.microsoft.com/ado/2007/08/dataservices',
}


HTTP_ERROR = re.compile(
    rb'\b(?:HTTP(?:/\d+(?:\.\d+)?)?(?:[ \t]+(?:status(?:[ \t]+code)?|error))?'
    rb'|(?:response[ \t]+)?status[ \t]+code(?:[ \t]+does not indicate success)?'
    rb'|the remote server returned an error)'
    rb'(?:[ \t]*:[ \t]*|[ \t]+)\(?([45][0-9]{2})\b',
    re.IGNORECASE,
)
HTTP_ERROR_DETAILS = {
    400: 'bad request (check package metadata and submission requirements)',
    401: 'authentication failed (check the configured API key)',
    403: 'forbidden (check account and package permissions)',
    406: 'not acceptable (check the package version and CCR moderation queue)',
    409: 'conflict (check whether the package version already exists)',
    413: 'package exceeds the upload size limit',
    429: 'rate limit exceeded',
    500: 'internal server error (check package format and CCR moderation state)',
    502: 'bad gateway',
    503: 'service unavailable',
    504: 'gateway timeout',
}


# Chocolatey may print this conflict message without an HTTP status.
ALREADY_EXISTS = b'already exists on the repository and cannot be modified'


def push_failure(result):
    # Only emit an unambiguous numeric status and a fixed description.
    codes = {
        int(code)
        for output in (result.stdout, result.stderr)
        for code in HTTP_ERROR.findall(output or b'')
    }
    status = 'unknown'
    if len(codes) == 1:
        code = codes.pop()
        status = f'{code} ({HTTP_ERROR_DETAILS.get(code, "unknown")})'
    details = ''
    if any(ALREADY_EXISTS in (output or b'').lower() for output in (result.stdout, result.stderr)):
        details = '; Chocolatey reported that this package version already exists'
    return f'returncode={result.returncode}, HTTP status={status}{details}'


def existing(name, version):
    try:
        with urlopen(FEED + f'package/{name}/{version}', timeout=60):
            return True
    except HTTPError as error:
        if error.code == 404:
            return False
        raise RuntimeError(f'CCR download preflight failed for {name} {version}: HTTP status={error.code}') from None
    except (OSError, HTTPException):
        raise RuntimeError(f'CCR download preflight failed for {name} {version}') from None


def metadata_body(name, version):
    url = FEED + f"Packages(Id='{name}',Version='{version}')"
    request = Request(url, headers={'Accept': 'application/atom+xml', 'Cache-Control': 'no-cache'})
    failure = f'CCR metadata preflight failed for {name} {version}'
    for attempt, delay in enumerate(METADATA_BACKOFF, 1):
        try:
            with urlopen(request, timeout=20) as response:
                return response.read()
        except HTTPError as error:
            retry_after = error.headers.get('Retry-After', '') if error.headers else ''
            error.close()
            if error.code == 404:
                return None
            status = f'HTTP status={error.code}'
            if error.code not in METADATA_RETRY_STATUS:
                raise RuntimeError(f'{failure}: {status}') from None
        except (OSError, HTTPException):
            retry_after = ''
            status = 'network error'

        if delay is None:
            raise RuntimeError(f'{failure}: {status} after {attempt} attempts') from None
        if retry_after:
            try:
                seconds = int(retry_after) if retry_after.isdecimal() else parsedate_to_datetime(retry_after).timestamp() - time()
            except (ValueError, TypeError, OverflowError, OSError):
                seconds = 0
            delay = max(delay, seconds)
        if delay > METADATA_MAX_RETRY_DELAY:
            raise RuntimeError(f'{failure}: {status}; CCR requested a longer wait than the retry limit') from None
        print(f'CCR metadata preflight for {name} {version}: {status}; retrying in {delay:g}s', flush=True)
        sleep(delay)


def moderation_record(name, version):
    body = metadata_body(name, version)
    if body is None:
        return None
    try:
        entry = ET.fromstring(body)
        if (entry.tag != '{' + ODATA['a'] + '}entry'
                or entry.findtext('a:title', namespaces=ODATA) != name
                or entry.findtext('m:properties/d:Version', namespaces=ODATA) != version
                or entry.findtext('m:properties/d:PackageHashAlgorithm', namespaces=ODATA) != 'SHA512'):
            raise ValueError
        digest = b64decode(entry.findtext('m:properties/d:PackageHash', '', ODATA), validate=True)
        if len(digest) != sha512().digest_size:
            raise ValueError
        approved = entry.findtext('m:properties/d:IsApproved', namespaces=ODATA)
        status = entry.findtext('m:properties/d:PackageStatus', namespaces=ODATA)
        if approved == 'true' or status == 'Approved':
            return digest, 'Approved'
        if approved == 'false' and status == 'Submitted':
            return digest, entry.findtext('m:properties/d:PackageSubmittedStatus', namespaces=ODATA)
        return digest, None
    except (ET.ParseError, ValueError, LookupError):
        raise RuntimeError(f'CCR metadata preflight failed for {name} {version}') from None


def package_digest(package, name, version):
    content = package.read_bytes()
    try:
        with ZipFile(BytesIO(content)) as archive:
            manifests = [part for part in archive.namelist() if part.lower().endswith('.nuspec')]
            if manifests != [name + '.nuspec']:
                raise ValueError
            manifest = ET.fromstring(archive.read(manifests[0]))
        if (manifest.findtext('{*}metadata/{*}id') != name
                or manifest.findtext('{*}metadata/{*}version') != version):
            raise ValueError
    except (BadZipFile, ET.ParseError, ValueError, LookupError, RuntimeError):
        raise ValueError(f'invalid Chocolatey package identity: {package.name}') from None
    return sha512(content).digest()


def publish(folder, version, push=False, *, resubmit=False):
    stable('v' + version)
    if resubmit and (not push or version != REPAIR_VERSION):
        raise ValueError(f'CCR repair authorization requires a push for v{REPAIR_VERSION}')
    packages = [folder / f'{name}.{version}.nupkg' for name in PACKAGE_NAMES]
    for package in packages:
        if not package.is_file() or not package.stat().st_size:
            raise ValueError(f'missing or empty Chocolatey package: {package}')
    if not push:
        for package in packages:
            print('DRY_RUN: built ' + package.name)
        return

    pending = []
    # Check both packages before sending either. The feed digest and moderation
    # status come from the same record, avoiding stale CDN downloads on retry.
    for name, package in zip(PACKAGE_NAMES, packages):
        local_digest = package_digest(package, name, version)
        record = moderation_record(name, version)
        if record is None:
            if resubmit or existing(name, version):
                raise ValueError(f'CCR moderation status could not be confirmed for {name} {version}')
            pending.append((package, 'SUBMITTED'))
            continue
        digest, status = record
        if digest == local_digest:
            print(f'UNCHANGED: {name} {version}; no submission')
            continue
        if status == 'Approved':
            raise ValueError(f'CCR version is approved and cannot be overwritten: {name} {version}')
        if status not in REPAIR_STATUSES:
            raise ValueError(f'CCR moderation status does not permit a repair for {name} {version}')
        if not resubmit:
            raise ValueError(f'CCR already contains different content: {name} {version}; repair authorization required')
        pending.append((package, 'RESUBMITTED'))
    if not pending:
        return
    key = os.environ.get('CHOCOLATEY_API_KEY')
    if not key:
        raise ValueError('protected CHOCOLATEY_API_KEY is required')
    for path, action in pending:
        # Never print argv, process output, or CalledProcessError (contains key).
        try:
            result = subprocess.run(['choco', 'push', str(path), '--source=' + SOURCE, '--api-key=' + key], capture_output=True, timeout=300)
        except (subprocess.TimeoutExpired, OSError):
            # TimeoutExpired includes argv: suppress its exception chain as well.
            raise RuntimeError('CCR submission could not finish for ' + path.name + '; check the signed-in moderation queue before any retry') from None
        if result.returncode:
            raise RuntimeError(f'CCR submission failed for {path.name}: {push_failure(result)}; check the signed-in moderation queue before any retry')
        print(f'{action} for community review: {path.name}')


if __name__ == '__main__':
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--packages', type=Path, required=True)
    p.add_argument('--version', required=True)
    p.add_argument('--push', action='store_true')
    p.add_argument('--moderation-checked', action='store_true',
                   help='confirm both exact versions were checked in the signed-in CCR moderation queue')
    p.add_argument('--resubmit-v2-1-0', action='store_true',
                   help='authorize the mio and mio-core v2.1.0 moderation repair')
    a = p.parse_args()
    if a.push and not a.moderation_checked:
        p.error('--push requires --moderation-checked')
    if a.resubmit_v2_1_0 and (not a.push or a.version != REPAIR_VERSION):
        p.error(f'--resubmit-v2-1-0 requires --push --version {REPAIR_VERSION}')
    publish(a.packages, a.version, a.push, resubmit=a.resubmit_v2_1_0)
