#!/usr/bin/env python3
"""Open review PRs in the two official distribution repositories, never merge."""
import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
from release import stable, digest, immutable


def run(*args, **kw):
    print('+', *map(str, args), flush=True)
    return subprocess.check_output([str(a) for a in args], text=True, **kw).strip()


def propose(generated, assets, tag):
    stable(tag)
    receipt = {p.name: digest(p) for p in sorted(assets.iterdir()) if p.is_file()}
    receipt_bytes = (json.dumps(receipt, sort_keys=True, indent=2)+'\n').encode()
    for repo, directory in [('homebrew-mio', 'homebrew'), ('mio-apt', 'apt')]:
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp) / repo
            run('gh', 'repo', 'clone', 'mio-kobo/' + repo, root)
            run('git', '-C', root, 'config', 'credential.helper', '!gh auth git-credential')
            branch = 'release/' + tag
            existing = run('git', '-C', root, 'ls-remote', '--heads', 'origin', branch)
            if existing:
                run('git', '-C', root, 'fetch', 'origin', branch)
                run('git', '-C', root, 'checkout', 'FETCH_HEAD')
            record = root / 'releases' / (tag + '.json')
            if record.exists():
                if record.read_bytes() != receipt_bytes:
                    raise ValueError('published version differs: ' + repo)
                if existing:
                    prs = json.loads(run('gh', 'pr', 'list', '--repo', 'mio-kobo/' + repo, '--head', branch, '--state', 'all', '--json', 'number'))
                    if not prs:
                        body = Path(temp) / 'body.md'
                        body.write_text(f'Prepare {tag} from verified stable Release assets. Review required before merge.\n')
                        run('gh', 'pr', 'create', '--repo', 'mio-kobo/' + repo, '--head', branch, '--title', 'Prepare Mio ' + tag, '--body-file', body)
                print('PASS: identical version already proposed/published in ' + repo)
                continue
            if existing:
                raise ValueError('existing release branch has no matching receipt')
            run('git', '-C', root, 'switch', '-c', branch)
            src = generated / directory
            for p in src.rglob('*'):
                if p.is_file():
                    dest = root / p.relative_to(src)
                    dest.parent.mkdir(parents=True, exist_ok=True)
                    if directory == 'apt':
                        immutable(dest, p.read_bytes())
                    else:
                        dest.write_bytes(p.read_bytes())
            if directory == 'apt':
                run('bash', Path(__file__).with_name('apt-index.sh'), root)
                (root / '.nojekyll').touch()
            immutable(record, receipt_bytes)
            run('git', '-C', root, 'config', 'user.name', 'Mio distribution bot')
            run('git', '-C', root, 'config', 'user.email', 'distribution@mio-lang.org')
            run('git', '-C', root, 'add', '.')
            run('git', '-C', root, 'commit', '-m', 'Prepare Mio ' + tag)
            run('git', '-C', root, 'push', 'origin', branch)
            body = Path(temp) / 'body.md'
            body.write_text(f'Prepare {tag} from the verified stable GitHub Release.\n\nReview required before merge and Pages activation. WinGet manifests remain a separate manual submission artifact.\n')
            run('gh', 'pr', 'create', '--repo', 'mio-kobo/' + repo, '--head', branch, '--title', 'Prepare Mio ' + tag, '--body-file', body)

if __name__ == '__main__':
    p = argparse.ArgumentParser()
    p.add_argument('--generated', type=Path, required=True)
    p.add_argument('--assets', type=Path, required=True)
    p.add_argument('--tag', required=True)
    a = p.parse_args()
    propose(a.generated.resolve(), a.assets.resolve(), a.tag)
