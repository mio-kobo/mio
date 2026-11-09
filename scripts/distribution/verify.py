#!/usr/bin/env python3
"""Execute extracted candidates outside the repository and verify their contract."""
import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

from package import TARGETS, digest, extract, inventory, write_json, safe_name
from versions import is_build_version


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
            safe_name(name)
            check('archive checksum ' + name, digest(output / name) == sha)
            if name.endswith('-setup.exe'):
                check('Windows installer target', target == 'windows-amd64' and '-windows-amd64-' in name)
                continue
            check('native archive format', name.endswith(('.zip', '.tar.gz')))
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
                overrides = {key: str(value) for key, value in (extra or {}).items()}
                proc = subprocess.run([str(a) for a in argv], cwd=cwd, env=dict(env, **overrides),
                                      input=stdin, capture_output=True, timeout=timeout)
                text = (proc.stdout + proc.stderr).decode('utf-8', errors='replace')
                log.append(dict(argv=[str(x) for x in argv], code=proc.returncode, output=text))
                write_json(output / 'execution-log.json', log)
                return proc.returncode, text
            binaries, roots = {}, {}
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
                      is_build_version(manifest['version']) and
                      provenance['embedded_bunko'] is (flavor == 'full'))
                check(flavor + ' payload', {p.parts[0] for p in map(Path, manifest['files'])} ==
                      {'bin', 'LICENSE', 'README.md', 'provenance.json'})
                cli = root / 'bin' / ('mio.exe' if target == 'windows-amd64' else 'mio')
                binaries[flavor] = cli
                roots[flavor] = root
                check(flavor + ' compiler provenance', digest(cli) == provenance['compiler_sha256'])
                code, text = run([cli, 'version'])
                version_line = f"mio v{manifest['version']} "
                source_stamp = f"commit {manifest['source_sha']};"
                check(flavor + ' version', code == 0 and text.startswith(version_line) and source_stamp in text)
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
                else:
                    check('Full automatic Bunko check', code == 0)
                    path_env = dict(PATH=str(cli.parent) + os.pathsep + env.get('PATH', ''))
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
                    # Invalid CLI override must beat valid env and embedded paths.
                    code, text = run([cli, 'check', math, '--bunko-root', cwd / 'missing'], dict(MIO_BUNKO=str(roots['core'] / 'bunko')))
                    check('explicit invalid root does not fall back', code != 0 and 'E_BUNKO_ROOT' in text)
                    for command in ([cli, 'check', math], [cli, 'build', hello], [cli, 'test', 'sample'], [cli, 'lsp', '--stdio']):
                        code, text = run(command, dict(MIO_BUNKO=str(cwd / 'missing')))
                        check('invalid env root ' + str(command[1]), code != 0 and 'E_BUNKO_ROOT' in text)
                    missing = cwd / 'missing.mio'
                    missing.write_text('yobu "kiso/not/a/package";\n', encoding='utf-8')
                    code, text = run([cli, 'check', missing])
                    check('Full missing module diagnostic', code != 0 and 'kiso/not/a/package' in text)
            check('Core and Full compiler SHA256 differ', digest(binaries['core']) != digest(binaries['full']))
            # A minimal external Bunko overrides embedded resolution for both flavors.
            override = cwd / 'external-bunko'
            (override / 'kiso/override').mkdir(parents=True)
            (override / 'kiso/override/override.mio').write_text(
                'koukai waza answer() -> int64 { kaesu 42; }\n', encoding='utf-8')
            external = cwd / 'external.mio'
            external.write_text('yobu "kiso/override"; chant(override.answer());\n', encoding='utf-8')
            for flavor in ('core', 'full'):
                cli = binaries[flavor]
                image = cwd / (flavor + '-external')
                code, text = run([cli, 'build', external, '--target=' + target, '--bunko-root', override, '-o', image])
                check(flavor + ' explicit external Bunko build', code == 0)
                code, text = run([image])
                check(flavor + ' explicit external Bunko execute', code == 0 and text.strip() == '42')
                code, text = run([cli, 'build', external, '--target=' + target, '-o', image], dict(MIO_BUNKO=str(override)))
                check(flavor + ' environment external Bunko build', code == 0)
                code, text = run([image])
                check(flavor + ' environment external Bunko execute', code == 0 and text.strip() == '42')
            # Core adjacent executable-relative lookup still works; embedded Full prefers its own table.
            shutil.copytree(override, roots['core'] / 'bunko')
            code, text = run([binaries['core'], 'check', external])
            check('Core adjacent Bunko check', code == 0)
            (roots['full'] / 'bunko/kiso').mkdir(parents=True)
            code, text = run([binaries['full'], 'check', math])
            check('Full embedded Bunko precedence over adjacent tree', code == 0)
            # Full works as a lone executable and imports every supported kiso module.
            lonely = base / 'lonely full'
            lonely.mkdir()
            lone = lonely / ('mio.exe' if target == 'windows-amd64' else 'mio')
            shutil.copyfile(binaries['full'], lone)
            if target != 'windows-amd64':
                lone.chmod(0o755)
            code, text = run([lone, 'check', math])
            check('Full lone executable check', code == 0)
            all_source = cwd / 'all.mio'
            all_source.write_text("""yobu "kiso/math";
yobu "kiso/string";
yobu "kiso/array";
yobu "kiso/bytes";
yobu "kiso/io";
yobu "kiso/fs";
yobu "kiso/encoding/json";
yobu "kiso/regexp";
yobu "kiso/crypto/sha256";
yobu "kiso/os";
yobu "kiso/net/tcp";
chant(math.abs(-9));
chant(string.concat("a","b"));
kaku xs: []int64=[1,2]; array.push(xs,3); chant(len(xs));
kaku data: []uint8=[uint8(104),uint8(105)]; chant(bytes.equal(data,data));
kaku reader=io.reader_from_bytes(data); kaku dst: []uint8=[uint8(0),uint8(0)]; chant(io.read_full(reader,dst,0,2).count);
chant(fs.exists("all.mio"));
chant(json.parse(string.to_bytes("{}")).error);
chant(regexp.matches(regexp.compile("a+"),"aaa"));
chant(len(sha256.sum(data)));
chant(os.monotonic_ms()>=0);
chant(len(tcp.ipv4(uint8(127),uint8(0),uint8(0),uint8(1),0).octets));
""", encoding='utf-8')
            image = cwd / ('all.exe' if target == 'windows-amd64' else 'all')
            code, text = run([lone, 'build', all_source, '--target=' + target, '-o', image])
            check('Full lone executable all-module build', code == 0)
            code, text = run([image])
            expected = ["9", "ab", "3", "hai", "2", "hai", "", "hai", "32", "hai", "4"]
            check('Full lone executable all-module run', code == 0 and [line.strip() for line in text.splitlines()] == expected)
            code, text = run([lone, 'check', math], dict(MIO_BUNKO=str(cwd / 'missing')))
            check('Full lone executable invalid env root', code != 0 and 'E_BUNKO_ROOT' in text)
            lonely_core = lonely / ('mio-core.exe' if target == 'windows-amd64' else 'mio-core')
            shutil.copyfile(binaries['core'], lonely_core)
            if target != 'windows-amd64':
                lonely_core.chmod(0o755)
            code, text = run([lonely_core, 'check', math])
            check('Core lone executable missing Bunko diagnostic', code != 0 and 'E_BUNKO_MISSING' in text)
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
