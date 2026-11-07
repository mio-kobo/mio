#!/usr/bin/env python3
"""Immutable CCR submission. No publishing unless --push is explicit."""
import argparse
import os
from pathlib import Path
import re
import subprocess
from urllib.error import HTTPError
from urllib.request import urlopen

from chocolatey import FEED, SOURCE
from release import stable


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
    409: 'conflict (check whether the package version already exists)',
    413: 'package exceeds the upload size limit',
    429: 'rate limit exceeded',
    500: 'internal server error',
}


def push_failure(result):
    # Only emit an unambiguous numeric status and a fixed description.
    codes = {
        int(code)
        for output in (result.stdout, result.stderr)
        for code in HTTP_ERROR.findall(output)
    }
    status = 'unknown'
    if len(codes) == 1:
        code = codes.pop()
        status = f'{code} ({HTTP_ERROR_DETAILS.get(code, "unknown")})'
    return f'returncode={result.returncode}, HTTP status={status}'


def existing(name, version):
    try:
        with urlopen(FEED + f'package/{name}/{version}', timeout=60) as response:
            return response.read()
    except HTTPError as error:
        if error.code == 404:
            return None
        raise


def publish(folder, version, push=False, fetch=existing):
    stable('v' + version)
    pending = []
    # Preflight BOTH packages before sending either; never overwrite/re-push.
    for name in ('mio-core', 'mio'):
        package = folder / f'{name}.{version}.nupkg'
        content = package.read_bytes()
        remote = fetch(name, version)
        if remote is not None:
            if remote != content:
                raise ValueError(f'CCR already contains different content: {name} {version}')
            print(f'UNCHANGED: {name} {version}; no submission')
        else:
            pending.append(package)
    if not push:
        for path in pending:
            print('DRY_RUN: would submit ' + path.name)
        return
    if not pending:
        return
    key = os.environ.get('CHOCOLATEY_API_KEY')
    if not key:
        raise ValueError('protected CHOCOLATEY_API_KEY is required')
    for path in pending:
        # Never print argv, process output, or CalledProcessError (contains key).
        try:
            result = subprocess.run(['choco', 'push', str(path), '--source=' + SOURCE, '--api-key=' + key], capture_output=True, timeout=300)
        except (subprocess.TimeoutExpired, OSError):
            # TimeoutExpired includes argv: suppress its exception chain as well.
            raise RuntimeError('CCR submission could not finish for ' + path.name + '; retry after checking remote state') from None
        if result.returncode:
            raise RuntimeError(f'CCR submission failed for {path.name}: {push_failure(result)}')
        print('SUBMITTED for community review: ' + path.name)


if __name__ == '__main__':
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--packages', type=Path, required=True)
    p.add_argument('--version', required=True)
    p.add_argument('--push', action='store_true')
    a = p.parse_args()
    publish(a.packages, a.version, a.push)
