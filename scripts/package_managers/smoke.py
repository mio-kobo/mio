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
    # Inno's generated uninstall support is owned by the installer, not the archive.
    for name in ('unins000.exe', 'unins000.dat', 'unins000.msg'):
        files.pop(name, None)
    alias = root / 'bin/mio-core.exe'
    if flavor == 'core' and target == 'windows-amd64' and alias.exists() and (root / 'bin/mio.exe').exists():
        assert digest(alias) == digest(root / 'bin/mio.exe'), 'alias differs from compiler'
        files.pop('bin/mio-core.exe')
    if metadata_root is not None:
        for name in ('LICENSE', 'README.md'):
            if name not in files:
                path = metadata_root / name
                files[name] = dict(sha256=digest(path), size=path.stat().st_size)
    assert files == m['files'], 'installed inventory mismatch'
    cli = root / 'bin' / ('mio.exe' if target == 'windows-amd64' else 'mio')
    if target == 'windows-amd64' and flavor == 'core' and (root / 'bin/mio-core.exe').exists() and not cli.exists():
        cli = root / 'bin/mio-core.exe'
    assert digest(cli) == json.loads((root / 'provenance.json').read_text())['compiler_sha256']
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
        if flavor == 'full':
            assert run([cli, 'run', hello]).strip() == 'hello from Mio'
            mochi = cwd / 'hello.mochi'
            run([cli, 'build', hello, '--emit=mochi', '-o', mochi])
            assert run([cli, 'exec', mochi]).strip() == 'hello from Mio'
            assert len(run([cli, 'disasm', mochi])) > 10
            tests = cwd / 'sample'
            tests.mkdir()
            (tests / 'sample_test.mio').write_text('yobu "kiso/testing"; waza test_math() -> unit { testing.expect(hai,"ok"); }\n')
            assert '1 passed' in run([cli, 'test', tests])
            requests = [dict(jsonrpc='2.0', id=1, method='initialize', params={}), dict(jsonrpc='2.0', id=2, method='shutdown'), dict(jsonrpc='2.0', method='exit')]
            wire = b''
            for request in requests:
                body = json.dumps(request).encode()
                wire += f'Content-Length: {len(body)}\r\n\r\n'.encode() + body
            proc = subprocess.run([str(cli), 'lsp', '--stdio'], input=wire, cwd=cwd, env=env, capture_output=True, timeout=30)
            assert proc.returncode == 0 and b'capabilities' in proc.stdout, (proc.returncode, proc.stdout, proc.stderr)
        if target == 'windows-amd64':
            print('NOT_APPLICABLE: kiso/net/tcp (no Windows Winsock runtime layer)')
    print(f'PASS: {target} {flavor} inventory, version, offline compile/execute, Bunko contract')

if __name__ == '__main__':
    p = argparse.ArgumentParser()
    p.add_argument('root', type=Path)
    p.add_argument('target')
    p.add_argument('flavor', choices=['core', 'full'])
    a = p.parse_args()
    smoke(a.root, a.target, a.flavor)
