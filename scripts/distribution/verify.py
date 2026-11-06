#!/usr/bin/env python3
"""Execute extracted candidates outside the repository and verify their contract."""
import argparse
import json
import os
import re
from pathlib import Path
import shutil
import subprocess
import tempfile

from package import TARGETS, digest, extract, inventory, write_json


def verify(output, target):
    results = dict(target=target, status='FAIL', checks=[], archives={}, integrated_tools={})
    log = []
    def check(name, condition):
        results['checks'].append(dict(name=name, status='PASS' if condition else 'FAIL'))
        write_json(output / 'verification.json', results)
        if not condition:
            raise RuntimeError(name)
    try:
        sums = (output / 'SHA256SUMS').read_text(encoding='utf-8').splitlines()
        archives = []
        for line in sums:
            sha, name = line.split('  ', 1)
            check('archive checksum ' + name, digest(output / name) == sha)
            archives.append(output / name)
            results['archives'][name] = sha
        check('two candidates', len(archives) == 2)
        archives.sort(key=lambda p: '-full.' in p.name)
        with tempfile.TemporaryDirectory(prefix='Mio 候选 install ') as temp:
            base = Path(temp)
            cwd = base / 'unrelated 工作目录'
            cwd.mkdir()
            env = dict(os.environ)
            env.pop('MIO_BUNKO', None)
            def run(argv, extra=None, stdin=None, timeout=300):
                proc = subprocess.run([str(a) for a in argv], cwd=cwd, env=dict(env, **(extra or {})),
                                      input=stdin, capture_output=True, timeout=timeout)
                text = (proc.stdout + proc.stderr).decode('utf-8', errors='replace')
                log.append(dict(argv=[str(x) for x in argv], code=proc.returncode, output=text))
                write_json(output / 'execution-log.json', log)
                return proc.returncode, text
            binaries = []
            for archive in archives:
                unpacked = extract(archive, base / archive.stem)
                # Also relocate after unpacking: installation cannot depend on original path.
                root = base / ('移動 ' + unpacked.name)
                shutil.move(str(unpacked), root)
                manifest = json.loads((root / 'manifest.json').read_text(encoding='utf-8'))
                provenance = json.loads((root / 'provenance.json').read_text(encoding='utf-8'))
                flavor = manifest['flavor']
                check(flavor + ' exact inventory', inventory(root) == manifest['files'])
                check(flavor + ' identity', manifest['target'] == target and
                      manifest['source_sha'] == provenance['source_sha'] and
                      bool(re.fullmatch(r'2\.[1-9]\d*\.(?:0|[1-9]\d*)(?:-pr\.[0-9a-f]{12})?', manifest['version'])) and provenance['embedded_bunko'] is False)
                cli = root / 'bin' / ('mio.exe' if target == 'windows-amd64' else 'mio')
                binaries.append(cli)
                check(flavor + ' compiler provenance', digest(cli) == provenance['compiler_sha256'])
                code, text = run([cli, 'version'])
                check(flavor + ' version', code == 0 and manifest['version'] in text and manifest['source_sha'] in text)
                hello = cwd / 'hello.mio'
                hello.write_text('chant("hello from Mio");\n', encoding='utf-8')
                image = cwd / ('hello.exe' if target == 'windows-amd64' else 'hello')
                code, text = run([cli, 'build', hello, '--target=' + target, '-o', image])
                check(flavor + ' hello build', code == 0 and image.is_file())
                code, text = run([image])
                check(flavor + ' hello execute', code == 0 and text == 'hello from Mio\n')
                math = cwd / 'math.mio'
                math.write_text('yobu "kiso/math"; chant(math.abs(-9));\n', encoding='utf-8')
                code, text = run([cli, 'check', math])
                if flavor == 'core':
                    check('Core missing Bunko diagnostic', code != 0 and 'E_BUNKO_MISSING' in text and 'kiso/math' in text)
                    check('Core payload', {p.parts[0] for p in map(Path, manifest['files'])} == {'bin', 'LICENSE', 'README.md', 'provenance.json'})
                else:
                    check('Full automatic Bunko check', code == 0)
                    path_env = dict(PATH=str(cli.parent) + os.pathsep + env.get('PATH', ''))
                    # Windows CreateProcess ignores the child environment's PATH
                    # for executable search; cmd resolves PATH inside that child.
                    launch = [cli.name, 'check', math]
                    if target == 'windows-amd64':
                        launch = [env.get('COMSPEC', 'cmd.exe'), '/d', '/c', cli.name, 'check', math]
                    code, text = run(launch, path_env)
                    check('Full PATH executable lookup', code == 0)
                    code, text = run([cli, 'build', math, '--target=' + target, '-o', image])
                    check('Full math build', code == 0)
                    code, text = run([image])
                    check('Full math execute', code == 0 and text.strip() == '9')
                    code, text = run([cli, 'build', hello, '--emit=mochi', '-o', cwd / 'hello.mochi'])
                    check('Full Mochi build', code == 0)
                    code, text = run([cli, 'disasm', cwd / 'hello.mochi'])
                    check('Full disasm', code == 0 and len(text) > 10)
                    code, text = run([cli, 'run', hello, '--target=' + target])
                    check('Full run', code == 0 and text == 'hello from Mio\n')
                    code, text = run([cli, 'exec', cwd / 'hello.mochi', '--target=' + target])
                    check('Full exec', code == 0 and text == 'hello from Mio\n')
                    if target != 'windows-amd64':
                        alias = cwd / 'mio alias'
                        alias.symlink_to(cli)
                        code, text = run([alias, 'check', math])
                        check('Full symlink executable lookup', code == 0)
                    tests = cwd / 'sample'
                    tests.mkdir()
                    (tests / 'sample_test.mio').write_text('yobu "kiso/testing"; waza test_math() -> unit { testing.expect(hai,"ok"); }\n', encoding='utf-8')
                    code, text = run([cli, 'test', tests])
                    check('Full mio test', code == 0 and '1 passed' in text)
                    requests = [dict(jsonrpc='2.0', id=1, method='initialize', params={}),
                                dict(jsonrpc='2.0', id=2, method='shutdown'), dict(jsonrpc='2.0', method='exit')]
                    wire = b''
                    for request in requests:
                        body = json.dumps(request).encode()
                        wire += f'Content-Length: {len(body)}\r\n\r\n'.encode() + body
                    code, text = run([cli, 'lsp', '--stdio'], stdin=wire, timeout=30)
                    check('Full lsp handshake', code == 0 and 'capabilities' in text)
                    if target == 'windows-amd64':
                        results['integrated_tools']['kiso/net/tcp'] = dict(status='NOT_APPLICABLE', reason='Windows runtime has no Winsock layer')
                    # Invalid CLI override must beat valid env and bundled paths.
                    code, text = run([cli, 'check', math, '--bunko-root', cwd / 'missing'], dict(MIO_BUNKO=str(root / 'bunko')))
                    check('explicit invalid root does not fall back', code != 0 and 'E_BUNKO_ROOT' in text)
                    for command in ([cli, 'check', math], [cli, 'build', hello], [cli, 'test', 'sample'], [cli, 'lsp', '--stdio']):
                        code, text = run(command, dict(MIO_BUNKO=str(cwd / 'missing')))
                        check('invalid env root ' + str(command[1]), code != 0 and 'E_BUNKO_ROOT' in text)
                    # Core can use the Full library via either explicit channel.
                    core = next(p for p in binaries if '-core' in str(p)) if any('-core' in str(p) for p in binaries) else None
                    if core:
                        code, text = run([core, 'check', math, '--bunko-root', root / 'bunko'], dict(MIO_BUNKO=str(cwd / 'missing')))
                        check('CLI override beats env', code == 0)
                        code, text = run([core, 'check', math], dict(MIO_BUNKO=str(root / 'bunko')))
                        check('Core external environment root', code == 0)
            check('Core/Full identical compiler SHA256', len(binaries) == 2 and digest(binaries[0]) == digest(binaries[1]))
        results['status'] = 'PASS'
    finally:
        write_json(output / 'verification.json', results)
    return results


if __name__ == '__main__':
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--output', type=Path, required=True)
    p.add_argument('--target', choices=TARGETS, required=True)
    a = p.parse_args()
    verify(a.output.resolve(), a.target)
