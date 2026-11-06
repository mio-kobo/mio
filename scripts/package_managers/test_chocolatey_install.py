#!/usr/bin/env python3
"""Real Windows Chocolatey lifecycle; fixtures are never publication inputs."""
import argparse
from contextlib import contextmanager
from functools import partial
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import threading

from chocolatey import build
from release import digest, extract, inventory
from smoke import smoke
from test_install import run


@contextmanager
def offline():
    # Block all outbound traffic from the installed native compiler, including
    # children using the same image. No networking is needed by supported tools.
    rule = 'Mio Chocolatey offline regression'
    program = str(Path(os.environ['ChocolateyInstall']) / 'lib/*/tools/payload/bin/mio.exe')
    # Windows firewall program rules do not accept wildcards. Use actual files.
    programs = list((Path(os.environ['ChocolateyInstall']) / 'lib').glob('mio*/tools/payload/bin/mio*.exe'))
    assert programs, program
    previous = {}
    try:
        for profile in ('domainprofile', 'privateprofile', 'publicprofile'):
            state = subprocess.check_output(['netsh', 'advfirewall', 'show', profile, 'state'], text=True)
            match = re.search(r'State\s+(ON|OFF)', state)
            assert match, state
            previous[profile] = match[1].lower()
            if match[1] == 'OFF':
                run('netsh', 'advfirewall', 'set', profile, 'state', 'on')
        for i, exe in enumerate(programs):
            run('netsh', 'advfirewall', 'firewall', 'add', 'rule', f'name={rule}{i}', 'dir=out', 'action=block', f'program={exe}', 'enable=yes', 'profile=any')
        yield
    finally:
        for i, _ in enumerate(programs):
            run('netsh', 'advfirewall', 'firewall', 'delete', 'rule', f'name={rule}{i}')
        for profile, state in previous.items():
            if state == 'off':
                run('netsh', 'advfirewall', 'set', profile, 'state', state)


def install(assets, stable_packages=None):
    if os.name != 'nt' or os.environ.get('GITHUB_ACTIONS') != 'true':
        raise RuntimeError('requires an isolated Windows Actions runner')
    choco = Path(os.environ['ChocolateyInstall'])
    result = dict(status='FAIL', target='windows-amd64', checks=[], archives={})
    report = assets / 'chocolatey-verification.json'
    def passed(name):
        result['checks'].append(dict(name=name, status='PASS'))
        report.write_text(json.dumps(result, indent=2) + '\n')
    report.write_text(json.dumps(result))
    try:
        run('choco', '--version')
        import winreg
        def paths():
            values = []
            for hive, key in ((winreg.HKEY_CURRENT_USER, 'Environment'), (winreg.HKEY_LOCAL_MACHINE, r'SYSTEM\CurrentControlSet\Control\Session Manager\Environment')):
                with winreg.OpenKey(hive, key) as handle:
                    values.append(winreg.QueryValueEx(handle, 'Path')[0])
            return values
        previous = paths()
        packages = assets / 'chocolatey-test'
        packages.mkdir(exist_ok=True)
        server = ThreadingHTTPServer(('127.0.0.1', 0), partial(SimpleHTTPRequestHandler, directory=str(assets)))
        threading.Thread(target=server.serve_forever, daemon=True).start()
        installed = {}
        with tempfile.TemporaryDirectory(prefix='Mio choco projects ') as temp:
            project = Path(temp) / 'keep.mio'
            project.write_text('chant("keep");')
            try:
                for flavor in ('core', 'full'):
                    name = 'mio' if flavor == 'full' else 'mio-core'
                    if (choco / 'lib' / name).exists() or (choco / 'bin' / (name + '.exe')).exists():
                        raise RuntimeError('refusing to replace existing package/shim: ' + name)
                    archive, = assets.glob(f'mio-*-windows-amd64-{flavor}.zip')
                    root = extract(archive, Path(temp) / flavor)
                    record = dict(url=f'http://127.0.0.1:{server.server_port}/{archive.name}', root=root.name, sha=digest(archive))
                    result['archives'][archive.name] = record['sha']
                    payload = choco / 'lib' / name / 'tools/payload'
                    shim = choco / 'bin' / (name + '.exe')
                    installed[flavor] = (payload, shim)
                    ignore = 'bin/' + name + '.exe.ignore'
                    expected_manifest = json.loads((root / 'manifest.json').read_text())
                    if flavor == 'core':
                        expected_manifest['files']['bin/mio-core.exe'] = expected_manifest['files'].pop('bin/mio.exe')
                    versions = ['0.0.1', '0.0.2']
                    if stable_packages is not None:
                        version = json.loads((root / 'manifest.json').read_text())['version']
                        stable_package = stable_packages / f'{name}.{version}.nupkg'
                        shutil.copyfile(stable_package, packages / stable_package.name)
                        versions.append(version)
                    for v in versions:
                        package = build(v, flavor, record, packages, fixture=True) if v in ('0.0.1', '0.0.2') else packages / stable_package.name
                        assert package.stat().st_size < 20000, 'metadata-only package expected'
                        run('choco', 'install' if v == '0.0.1' else 'upgrade', name, '--version=' + v, '--source=' + str(packages), '--yes', '--no-progress', '--limit-output')
                        assert json.loads((payload / 'manifest.json').read_text()) == expected_manifest
                        actual = inventory(payload)
                        assert actual.pop(ignore)['size'] == 0
                        assert actual == expected_manifest['files'], 'archive inventory with only the Core executable name changed'
                        assert digest(payload / 'bin' / (name + '.exe')) == digest(root / 'bin/mio.exe'), 'compiler bytes must not change'
                        assert shim.is_file() and not shim.is_symlink()
                        resolved = subprocess.check_output(['where.exe', name], text=True).splitlines()[0]
                        assert Path(resolved).resolve() == shim.resolve(), resolved
                        # ShimGen diagnostics prove the real executable target, not a
                        # copied executable or WinGet-style filesystem alias.
                        diagnostic = subprocess.check_output([str(shim), '--shimgen-log', 'version'], text=True)
                        target = payload / 'bin' / (name + '.exe')
                        assert (str(target).lower() in diagnostic.lower() or
                                os.path.relpath(target, shim.parent).lower() in diagnostic.lower()), diagnostic
                        assert json.loads((root / 'manifest.json').read_text())['version'] in diagnostic
                        with offline():
                            smoke(payload, 'windows-amd64', flavor, manager_files=(ignore,))
                            smoke(payload, 'windows-amd64', flavor, command=shim, manager_files=(ignore,))
                        assert paths() == previous, 'Chocolatey bin owns PATH; no per-user mutation'
                        assert project.read_text() == 'chant("keep");'
                        passed(flavor + ' ' + v + ' install/upgrade, real shim/PATH, hashes, offline native/Bunko/tools')
                        if v == '0.0.1':
                            (payload / 'obsolete-release-file.txt').write_text('must disappear on upgrade')
                    # Same-version install is a no-op; explicit force reinstalls safely.
                    run('choco', 'install', name, '--version=' + versions[-1], '--source=' + str(packages), '-y', '--no-progress')
                    run('choco', 'install', name, '--version=' + versions[-1], '--source=' + str(packages), '-y', '--force', '--no-progress')
                    with offline():
                        smoke(payload, 'windows-amd64', flavor, command=shim, manager_files=(ignore,))
                    passed(flavor + ' repeated and forced install')
                assert digest(installed['core'][0] / 'bin/mio-core.exe') == digest(installed['full'][0] / 'bin/mio.exe')
                with offline():
                    for flavor, (payload, shim) in installed.items():
                        ignore = 'bin/' + ('mio-core' if flavor == 'core' else 'mio') + '.exe.ignore'
                        smoke(payload, 'windows-amd64', flavor, command=shim, manager_files=(ignore,))
                passed('Core/Full coexist, distinct commands, identical compiler bytes')
                for flavor, (payload, shim) in installed.items():
                    name = 'mio' if flavor == 'full' else 'mio-core'
                    run('choco', 'uninstall', name, '--yes', '--no-progress')
                    assert not payload.exists() and not shim.exists()
                    assert project.read_text() == 'chant("keep");' and paths() == previous
                    if flavor == 'core':
                        other, command = installed['full']
                        with offline():
                            smoke(other, 'windows-amd64', 'full', command=command, manager_files=('bin/mio.exe.ignore',))
                        # Restore Core to exercise the opposite removal order too.
                        run('choco', 'install', 'mio-core', '--version=' + versions[-1], '--source=' + str(packages), '-y', '--no-progress')
                    else:
                        other, command = installed['core']
                        with offline():
                            smoke(other, 'windows-amd64', 'core', command=command, manager_files=('bin/mio-core.exe.ignore',))
                        run('choco', 'uninstall', 'mio-core', '--yes', '--no-progress')
                        assert not other.exists() and not command.exists()
                    passed(flavor + ' uninstall, other flavor and user project preserved, PATH unchanged')
                # Even --ignore-checksums must not bypass the package's own
                # SHA-256 verification. Exercise this through real Chocolatey.
                archive, = assets.glob('mio-*-windows-amd64-core.zip')
                negative = assets / 'checksum-negative'
                bad = dict(url=f'http://127.0.0.1:{server.server_port}/{archive.name}', root=archive.stem, sha='0' * 64)
                build('0.0.1', 'core', bad, negative, fixture=True)
                rejected = subprocess.run(['choco', 'install', 'mio-core', '--version=0.0.1', '--source=' + str(negative), '--yes', '--no-progress', '--ignore-checksums'], capture_output=True, text=True, timeout=300)
                assert rejected.returncode != 0 and 'Mio archive SHA-256 mismatch' in rejected.stdout + rejected.stderr, (rejected.returncode, rejected.stdout, rejected.stderr)
                assert not (choco / 'bin/mio-core.exe').exists()
                assert not (choco / 'lib/mio-core/tools/payload').exists()
                passed('wrong archive SHA-256 rejected with nonzero exit even with --ignore-checksums')
            finally:
                server.shutdown()
        result['status'] = 'PASS'
        result['limitations'] = {'kiso/net/tcp': 'NOT_APPLICABLE: no Windows Winsock layer'}
    except Exception as error:
        result.update(status='FAIL', error=str(error))
        raise
    finally:
        report.write_text(json.dumps(result, indent=2) + '\n')


if __name__ == '__main__':
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--assets', type=Path, required=True)
    p.add_argument('--stable-packages', type=Path)
    a = p.parse_args()
    install(a.assets.resolve(), a.stable_packages)
