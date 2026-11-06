#!/usr/bin/env python3
"""Select existing native evidence only when all non-wrapper inputs match.

No fixture can satisfy stable release gates. A missing/expired/mismatching
fixture fails with instructions to run the existing native distribution job.
"""
import argparse
import json
from pathlib import Path
import subprocess


def wrapper(path):
    return (path.startswith('scripts/package_managers/') or path.startswith('docs/installation/') or
            path in ('.github/workflows/package-manager-tests.yml', '.github/workflows/package-managers.yml',
                     '.github/workflows/distribution-pr.yml'))


def compatible(ref):
    paths = subprocess.check_output(['git', 'diff', '--name-only', ref, 'HEAD'], text=True).splitlines()
    return all(wrapper(path) for path in paths)


def api(path):
    return json.loads(subprocess.check_output(['gh', 'api', 'repos/mio-kobo/mio/' + path], text=True))


def select(run_id=None):
    runs = [api('actions/runs/' + run_id)] if run_id else api('actions/workflows/distribution-pr.yml/runs?status=success&per_page=30')['workflow_runs']
    for run in runs:
        if run['conclusion'] != 'success' or run['path'] != '.github/workflows/distribution-pr.yml' or run['repository']['full_name'] != 'mio-kobo/mio':
            continue
        sha = run['head_sha']
        subprocess.run(['git', 'fetch', 'origin', sha], check=True)
        if not compatible(sha):
            continue
        artifacts = api(f"actions/runs/{run['id']}/artifacts?per_page=100")['artifacts']
        name = f"mio-candidates-windows-amd64-{run['id']}-{run['run_attempt']}"
        if not any(a['name'] == name and not a['expired'] for a in artifacts):
            continue
        return dict(run_id=run['id'], attempt=run['run_attempt'], head_sha=sha, artifact=name, status='PASS', purpose='wrapper regression only; never stable release evidence')
    raise RuntimeError('No successful unexpired native fixture with matching source. Run distribution-pr.yml manually without stable_version, then retry.')


if __name__ == '__main__':
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--run')
    p.add_argument('--output', type=Path, required=True)
    a = p.parse_args()
    a.output.write_text(json.dumps(select(a.run), indent=2) + '\n')
