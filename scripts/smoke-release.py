#!/usr/bin/env python3
"""Extract and verify a Mio candidate outside the checkout, with no Go on PATH.
Every packaged executable runs with checkout reads and child processes denied.
"""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import sys

sys.dont_write_bytecode = True

ROOT = Path(__file__).resolve().parent.parent


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('archive', type=Path)
    parser.add_argument('--report', type=Path, required=True)
    parser.add_argument('--version', default='2.0.0')
    args = parser.parse_args()
    from release_automation import parse_tag
    parse_tag('v' + args.version)
    report = dict(status='RUNNING', archive=str(args.archive.resolve()), commands=[],
                  environment='empty PATH, isolated HOME, packaged MIO_BUNKO',
                  sandbox='deny checkout reads, process-fork and all other process-exec')
    try:
        with tempfile.TemporaryDirectory(prefix='mio-install-') as temporary:
            base = Path(temporary).resolve()
            with tarfile.open(args.archive) as archive:
                for member in archive.getmembers():
                    destination = (base / member.name).resolve()
                    if not destination.is_relative_to(base) or not (member.isfile() or member.isdir()):
                        raise RuntimeError('unsafe archive member: ' + member.name)
                names = [m.name for m in archive.getmembers()]
                if len(names) != len(set(names)):
                    raise RuntimeError('duplicate archive members')
                archive.extractall(base, filter='data')
            package = base / f'mio-{args.version}-darwin-arm64'
            if {p.name for p in base.iterdir()} != {package.name}:
                raise RuntimeError('unexpected archive root')
            manifest = json.loads((package / 'MANIFEST.json').read_text())
            actual_files = {str(p.relative_to(package)) for p in package.rglob('*') if p.is_file()}
            if actual_files != set(manifest) | {'MANIFEST.json'}:
                raise RuntimeError('archive file inventory mismatch')
            for name, expected in manifest.items():
                path = package / name
                if path.stat().st_size != expected['bytes'] or sha(path) != expected['sha256']:
                    raise RuntimeError('archive content mismatch: ' + name)
            for name in ('mio', 'mochi-aot', 'mochi-aot-audit', 'native-aot'):
                binary = package / name
                subprocess.run(['/usr/bin/codesign', '--verify', '--strict', binary], check=True)
                signature = subprocess.check_output(['/usr/bin/codesign', '-dvv', binary], stderr=subprocess.STDOUT, text=True)
                if 'Signature=adhoc' not in signature:
                    raise RuntimeError('expected ad-hoc signing: ' + name)
                description = subprocess.check_output(['/usr/bin/file', binary], text=True)
                if 'Mach-O 64-bit executable arm64' not in description:
                    raise RuntimeError('wrong architecture: ' + name)
                subprocess.run(['/usr/bin/otool', '-hv', '-l', binary], check=True, capture_output=True)
                dependencies = subprocess.check_output(['/usr/bin/otool', '-L', binary], text=True)
                for dependency in dependencies.splitlines()[1:]:
                    if not dependency.strip().startswith(('/usr/lib/', '/System/Library/')):
                        raise RuntimeError('non-system dependency: ' + dependency)

            work = base / 'first-project'
            work.mkdir()
            env = {'PATH': '', 'HOME': str(base), 'TMPDIR': str(base), 'MIO_BUNKO': str(package / 'bunko')}
            report['go_available'] = shutil.which('go', path=env['PATH']) is not None
            if report['go_available']:
                raise RuntimeError('Go unexpectedly available')

            def run(label, argv, expected_code=0, expected_stdout=None, stdin=None):
                policy = base / 'smoke.sb'
                policy.write_text('(version 1)(allow default)(deny file-read* (subpath ' + json.dumps(str(ROOT)) + '))'
                                  '(deny process-fork)(deny process-exec)(allow process-exec (literal ' + json.dumps(str(argv[0])) + '))')
                proc = subprocess.run(['/usr/bin/sandbox-exec', '-f', policy, *map(str, argv)],
                                      cwd=work, env=env, input=stdin, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=180)
                stdout, stderr = proc.stdout.decode(), proc.stderr.decode()
                report['commands'].append(dict(label=label, argv=list(map(str, argv)), exit_code=proc.returncode,
                                               stdout=stdout, stderr=stderr))
                if proc.returncode != expected_code or (expected_stdout is not None and stdout != expected_stdout):
                    raise RuntimeError(f'{label}: exit={proc.returncode}, stdout={stdout!r}, stderr={stderr!r}')
                return stdout, stderr

            run('checkout-denial-probe', ['/bin/cat', ROOT / 'go.mod'], expected_code=1)
            mio = package / 'mio'
            run('version', [mio, 'version'], expected_stdout=f'Mio {args.version}\n')
            run('version-alias', [mio, '--version'], expected_stdout=f'Mio {args.version}\n')
            help_text, _ = run('help', [mio, 'help'])
            if not help_text.startswith(f'Mio {args.version}\n'):
                raise RuntimeError('help version mismatch')
            fixtures = {
                'hello': ('chant("Hello, Mio!");\n', 'Hello, Mio!\n'),
                'features': ('''yobu "kiso/array";
waza maybe(flag: bool) -> int64? { moshi (flag) { kaesu 41; } kaesu mu; }
waza counter() -> () -> int64 {
 kaku n = 40;
 kaesu waza() -> int64 { n = n + 1; kaesu n; };
}
waza run() -> unit {
 kaku a = array.make[int64?](2, mu); a[1] = maybe(hai);
 kaku v = a[1]; moshi (v != mu) { chant(v + 1); }
 kaku next = counter(); chant(next()); chant(next());
}
run();
''', '42\n41\n42\n')}
            for name, (source, expected) in fixtures.items():
                src, mochi = work / (name + '.mio'), work / (name + '.mochi')
                src.write_text(source)
                run(name + '-check', [mio, 'check', src], expected_stdout='check passed\n')
                run(name + '-vm-run', [mio, 'run', src], expected_stdout=expected)
                run(name + '-build', [mio, 'build', '-o', mochi, src])
                run(name + '-vm-exec', [mio, 'exec', mochi], expected_stdout=expected)
                # Both shipped backends must work with the same freshly compiled program.
                for backend in ('mochi-aot', 'native-aot'):
                    target = work / (name + '-' + backend)
                    if backend == 'mochi-aot':
                        run(name + '-host-aot', [package / backend, '-o', target, mochi])
                    else:
                        (work / 'input.config').write_text(str(mochi))
                        (work / 'output.config').write_text(str(target))
                        result, _ = run(name + '-pure-aot', [package / backend])
                        if not result.startswith('native-aot: ok\n') or not target.is_file():
                            raise RuntimeError('pure AOT failed without a nonzero exit')
                    # Validate complete native image structure with the existing formal checker.
                    import importlib.util
                    spec = importlib.util.spec_from_file_location('m18_checker', ROOT / 'scripts/check-native-aot-fixed-point.py')
                    checker = importlib.util.module_from_spec(spec)
                    spec.loader.exec_module(checker)
                    checker.validate_macho(target)
                    target.chmod(0o755)  # NativeFS writes data files; install executable permissions explicitly.
                    subprocess.run(['/usr/bin/codesign', '--force', '--sign', '-', target], check=True, capture_output=True)
                    subprocess.run(['/usr/bin/codesign', '--verify', '--strict', target], check=True)
                    run(name + '-' + backend + '-execute', [target], expected_stdout=expected)
            bad = work / 'bad.mio'
            bad.write_text('kaku x: int64 = "wrong";\n')
            _, diagnostic = run('error-diagnostic', [mio, 'check', '--format=json', bad], expected_code=1)
            if not diagnostic.strip():
                raise RuntimeError('missing diagnostic')
            # Check the packaged LSP initialize/version/shutdown protocol too.
            messages = [dict(jsonrpc='2.0', id=1, method='initialize', params={}),
                        dict(jsonrpc='2.0', id=2, method='shutdown'), dict(jsonrpc='2.0', method='exit')]
            wire = b''
            for message in messages:
                body = json.dumps(message).encode()
                wire += f'Content-Length: {len(body)}\r\n\r\n'.encode() + body
            output, _ = run('lsp-handshake', [mio, 'lsp', '--stdio'], stdin=wire)
            if args.version not in output or 'capabilities' not in output:
                raise RuntimeError('LSP initialize/version response missing')
            report.update(status='PASS', manifest_files=len(manifest), independent_install=True,
                          executable_bytes_preserved=True, strict_codesign=True,
                          hello_and_features_vm_and_both_aot=True)
    except BaseException as error:
        report.update(status='FAIL', error=str(error))
        raise
    finally:
        args.report.write_text(json.dumps(report, indent=2) + '\n')


if __name__ == '__main__':
    main()
