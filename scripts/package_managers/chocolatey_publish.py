#!/usr/bin/env python3
"""Immutable CCR submission. No publishing unless --push is explicit."""
import argparse
import os
from pathlib import Path
import subprocess
from urllib.error import HTTPError
from urllib.request import urlopen

from chocolatey import FEED, SOURCE
from release import stable


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
        result = subprocess.run(['choco', 'push', str(path), '--source=' + SOURCE, '--api-key=' + key], capture_output=True)
        if result.returncode:
            raise RuntimeError('CCR submission failed for ' + path.name + '; inspect moderation/account status without exposing credentials')
        print('SUBMITTED for community review: ' + path.name)


if __name__ == '__main__':
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--packages', type=Path, required=True)
    p.add_argument('--version', required=True)
    p.add_argument('--push', action='store_true')
    a = p.parse_args()
    publish(a.packages, a.version, a.push)
