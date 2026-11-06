#!/usr/bin/env python3
"""Check an installed payload offline, including every shipped file."""
import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'distribution'))
from package import inventory, digest


def smoke(root, target, flavor, metadata_root=None, command=None):
    root = root.resolve()
    m = json.loads((root / 'manifest.json').read_text())
    files = inventory(root)
    alias = root / 'bin/mio-core.exe'
    if flavor == 'core' and target == 'windows-amd64' and alias.exists():
        assert digest(alias) == digest(root / 'bin/mio.exe'), 'alias differs from compiler'
        files.pop('bin/mio-core.exe')
    if metadata_root is not None:
        for name in ('LICENSE', 'README.md'):
            if name not in files:
                path = metadata_root / name
                files[name] = dict(sha256=digest(path), size=path.stat().st_size)
    assert files == m['files'], 'installed inventory mismatch'
    cli = root / 'bin' / ('mio.exe' if target == 'windows-amd64' else 'mio')
    if command is not None:
        cli = command
    env = dict(os.environ)
    env.pop('MIO_BUNKO', None)
    def run(args, expected=0):
        p = subprocess.run([str(x) for x in args], cwd=cwd, env=env, capture_output=True, text=True, timeout=300)
        assert p.returncode == expected, (args, p.returncode, p.stdout, p.stderr)
        return p.stdout + p.stderr
    with tempfile.TemporaryDirectory(prefix='Mio offline ') as temp:
        cwd = Path(temp)
        assert m['version'] in run([cli, 'version'])
        hello = cwd / 'hello.mio'
        hello.write_text('chant("hello from Mio");\n')
        image = cwd / ('hello.exe' if target == 'windows-amd64' else 'hello')
        run([cli, 'build', hello, '--target=' + target, '-o', image])
        assert run([image]).strip() == 'hello from Mio'
        math = cwd / 'math.mio'
        math.write_text('yobu "kiso/math"; chant(math.abs(-9));\n')
        if flavor == 'full':
            run([cli, 'check', math])
            run([cli, 'build', math, '--target=' + target, '-o', image])
            assert run([image]).strip() == '9'
        else:
            assert 'E_BUNKO_MISSING' in run([cli, 'check', math], 1)
        if target == 'windows-amd64':
            print('NOT_APPLICABLE: Mio run/exec/test process spawning and LSP stdin polling')
    print(f'PASS: {target} {flavor} inventory, version, offline compile/execute, Bunko contract')

if __name__ == '__main__':
    p = argparse.ArgumentParser()
    p.add_argument('root', type=Path)
    p.add_argument('target')
    p.add_argument('flavor', choices=['core', 'full'])
    a = p.parse_args()
    smoke(a.root, a.target, a.flavor)
