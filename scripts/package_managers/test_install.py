#!/usr/bin/env python3
"""Real manager lifecycle tests on verified PR candidates, never publication.

Synthetic package metadata versions exercise upgrades without claiming that the
candidate compiler is a stable release. Archives themselves are never modified.
"""
import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import threading
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
from functools import partial
from release import deb, formula, winget, manifest_yaml
from smoke import smoke
from package import digest, extract, TARGETS

HERE = Path(__file__).resolve().parent


def run(*argv, **kw):
    print('+', *map(str, argv), flush=True)
    return subprocess.run(list(map(str, argv)), check=True, **kw)


def install(assets, target):
    import yaml
    results = dict(target=target, status='FAIL', checks=[])
    output = assets / 'package-manager-verification.json'
    output.write_text(json.dumps(results, indent=2)+'\n')
    def passed(name):
        results['checks'].append(dict(name=name, status='PASS'))
        output.write_text(json.dumps(results, indent=2)+'\n')
    with tempfile.TemporaryDirectory(prefix='mio-manager-') as temp:
        base = Path(temp)
        archives = {}
        for flavor in ('core', 'full'):
            found = list(assets.glob(f'mio-*-{target}-{flavor}.*'))
            assert len(found) == 1, found
            archive = found[0]
            root = extract(archive, base / ('extract-' + flavor))
            m = json.loads((root / 'manifest.json').read_text())
            assert m['flavor'] == flavor and m['target'] == target
            archives[flavor] = (archive, root, m['version'])
        # Use the candidate's own checksum validation and all original native tests
        # before entering here. Relocated installed payloads are tested again below.
        handler = partial(SimpleHTTPRequestHandler, directory=str(assets))
        server = ThreadingHTTPServer(('127.0.0.1', 0), handler)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        try:
            for flavor, (archive, root, version) in archives.items():
                name = 'mio' if flavor == 'full' else 'mio-core'
                r = dict(root=root.name, sha=digest(archive), url=f'http://127.0.0.1:{server.server_port}/{archive.name}')
                if target == 'darwin-arm64':
                    # Homebrew requires tapped formulae; this disposable tap has no
                    # network remote and cannot be mistaken for the official tap.
                    taproot = Path(subprocess.check_output(['brew', '--repository'], text=True).strip()) / 'Library/Taps/mio-ci/homebrew-candidate'
                    if subprocess.run(['brew', 'list', '--versions', name], capture_output=True, text=True).stdout.strip():
                        raise RuntimeError('refusing to replace an existing Homebrew installation: ' + name)
                    taproot.mkdir(parents=True, exist_ok=False)
                    brew_env = dict(os.environ, XDG_CONFIG_HOME=str(base / 'brew-config'), HOMEBREW_NO_AUTO_UPDATE='1', HOMEBREW_NO_AUTOREMOVE='1', HOMEBREW_NO_INSTALL_CLEANUP='1')
                    def brew(*args):
                        return run('brew', *args, env=brew_env)
                    try:
                        run('git', 'init', taproot)
                        f = taproot / 'Formula' / (name + '.rb')
                        f.parent.mkdir()
                        for other_flavor, (other_archive, other_root, _) in archives.items():
                            other_name = 'mio' if other_flavor == 'full' else 'mio-core'
                            other_record = dict(root=other_root.name, sha=digest(other_archive), url=f'http://127.0.0.1:{server.server_port}/{other_archive.name}')
                            (f.parent / (other_name + '.rb')).write_text(formula('0.0.1', other_flavor, other_record))
                        for v in ('0.0.1', '0.0.2'):
                            f.write_text(formula(v, flavor, r).replace(f'assert_match "{v}"', f'assert_match "{version}"'))
                            if subprocess.run(['brew', 'help', 'trust'], capture_output=True).returncode == 0:
                                brew('trust', '--tap', 'mio-ci/candidate')
                            brew('audit', '--formula', 'mio-ci/candidate/' + name)
                            brew('install' if v == '0.0.1' else 'upgrade', 'mio-ci/candidate/' + name)
                            installed = Path(subprocess.check_output(['brew', '--prefix', name], text=True).strip()) / 'libexec'
                            smoke(installed, target, flavor, metadata_root=installed.parent)
                            brew('test', 'mio-ci/candidate/' + name)
                        relocated = base / ('relocated-' + flavor)
                        shutil.copytree(installed, relocated)
                        for filename in ('LICENSE', 'README.md'):
                            if not (relocated / filename).exists():
                                shutil.copyfile(installed.parent / filename, relocated / filename)
                        smoke(relocated, target, flavor)
                        brew('uninstall', '--force', name)
                        assert not installed.exists()
                    finally:
                        shutil.rmtree(taproot)
                elif target == 'linux-amd64':
                    repo = base / ('apt-' + flavor)
                    keyhome = base / ('gpg-' + flavor)
                    keyhome.mkdir(mode=0o700)
                    env = dict(os.environ, GNUPGHOME=str(keyhome))
                    run('gpg', '--batch', '--passphrase', '', '--quick-generate-key', 'Mio CI <ci@example.invalid>', 'rsa2048', 'sign', '1d', env=env)
                    keys = subprocess.check_output(['gpg', '--with-colons', '--list-secret-keys'], env=env, text=True)
                    fingerprint = next(l.split(':')[9] for l in keys.splitlines() if l.startswith('fpr:'))
                    env['MIO_APT_SIGNING_FINGERPRINT'] = fingerprint
                    key = Path('/etc/apt/keyrings/mio-ci.gpg')
                    source = Path('/etc/apt/sources.list.d/mio-ci.list')
                    try:
                        for v in ('0.0.1', '0.0.2'):
                            deb(archive, repo / 'pool/main/m/mio', version, flavor, test_version=v)
                            run('bash', HERE / 'apt-index.sh', repo, env=env)
                            run('sudo', 'mkdir', '-p', key.parent)
                            run('sudo', 'cp', repo / 'mio-archive-keyring.gpg', key)
                            # apt's unprivileged acquisition process needs traversal.
                            run('chmod', '-R', 'a+rX', base)
                            listing = base / 'source.list'
                            listing.write_text(f'deb [arch=amd64 signed-by={key}] file:{repo} stable main\n')
                            run('sudo', 'cp', listing, source)
                            run('sudo', 'apt-get', 'update')
                            run('sudo', 'apt-get', 'install', '-y', name + '=' + v)
                            installed = Path('/usr/lib') / name
                            run('sudo', 'unshare', '--net', sys.executable, HERE / 'smoke.py', installed, target, flavor)
                            run('mio', 'version')
                        relocated = base / ('relocated-' + flavor)
                        shutil.copytree(installed, relocated)
                        smoke(relocated, target, flavor)
                        run('sudo', 'apt-get', 'remove', '-y', name)
                        assert not (installed / 'bin/mio').exists()
                    finally:
                        run('sudo', 'rm', '-f', source, key)
                else:
                    run('winget', '--version')
                    run('winget', 'settings', '--enable', 'LocalManifestFiles')
                    location = base / ('installed-' + flavor)
                    from windows import build
                    release_installer = assets / f'mio-{version}-windows-amd64-{flavor}-setup.exe'
                    if not release_installer.exists():
                        release_installer = build(archive, assets)
                    for v in ('0.0.1', '0.0.2', version):
                        from windows import build
                        setup = release_installer if v == version else build(archive, assets / 'inno-test', package_version=v)
                        relative = setup.relative_to(assets).as_posix()
                        installer_record = dict(url=f'http://127.0.0.1:{server.server_port}/{relative}', sha=digest(setup))
                        ident, files = winget(v, flavor, installer_record)
                        manifests = base / (flavor + v)
                        manifests.mkdir()
                        for suffix, data in files.items():
                            (manifests / (ident + '.' + suffix)).write_bytes(manifest_yaml(data).encode('utf-8'))
                        run('winget', 'validate', '--manifest', manifests)
                        command = ['winget', 'install' if v == '0.0.1' else 'upgrade', '--manifest', manifests, '--accept-package-agreements', '--accept-source-agreements', '--disable-interactivity']
                        if v == '0.0.1':
                            command += ['--location', location]
                        run(*command)
                        installed = location
                        import winreg
                        with winreg.OpenKey(winreg.HKEY_CURRENT_USER, 'Environment') as key:
                            user_path = winreg.QueryValueEx(key, 'Path')[0]
                        alias_command = Path(shutil.which(name, path=user_path) or '')
                        assert alias_command.resolve() == (installed / 'bin' / (name + '.exe')).resolve(), 'real bin must be on user PATH'
                        smoke(installed, target, flavor)
                        smoke(installed, target, flavor, command=alias_command)
                    relocated = base / ('relocated-' + flavor)
                    shutil.copytree(installed, relocated)
                    smoke(relocated, target, flavor)
                    # A local-only manifest has no community source ID to correlate.
                    run('winget', 'uninstall', '--manifest', manifests, '--silent', '--accept-source-agreements', '--disable-interactivity')
                    assert not (installed / 'bin' / (name + '.exe')).exists()
                    assert not alias_command.exists()
                    assert not location.exists() or not list(location.rglob('mio.exe'))
                passed(flavor + ' install, upgrade, offline payload, uninstall')
        finally:
            server.shutdown()
    results['status'] = 'PASS'
    if target == 'windows-amd64':
        results['limitations'] = dict(status='NOT_APPLICABLE', reason='kiso/net/tcp: no Windows Winsock runtime layer')
    output.write_text(json.dumps(results, indent=2)+'\n')

if __name__ == '__main__':
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--assets', type=Path, required=True)
    p.add_argument('--target', choices=TARGETS, required=True)
    a = p.parse_args()
    try:
        install(a.assets.resolve(), a.target)
    except Exception as error:
        output = a.assets / 'package-manager-verification.json'
        result = json.loads(output.read_text()) if output.exists() else dict(target=a.target, checks=[])
        result.update(status='FAIL', error=str(error))
        output.write_text(json.dumps(result, indent=2)+'\n')
        raise
