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
from release import deb, formula, winget
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
                    taproot.mkdir(parents=True, exist_ok=False)
                    try:
                        run('git', 'init', taproot)
                        f = taproot / 'Formula' / (name + '.rb')
                        f.parent.mkdir()
                        for v in ('0.0.1', '0.0.2'):
                            f.write_text(formula(v, flavor, r).replace(f'assert_match "{v}"', f'assert_match "{version}"'))
                            run('brew', 'audit', '--formula', 'mio-ci/candidate/' + name)
                            run('brew', 'install' if v == '0.0.1' else 'upgrade', 'mio-ci/candidate/' + name)
                            installed = Path(subprocess.check_output(['brew', '--prefix', name], text=True).strip()) / 'libexec'
                            smoke(installed, target, flavor)
                            run('brew', 'test', 'mio-ci/candidate/' + name)
                        run('brew', 'uninstall', name)
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
                        relocated = base / 'relocated'
                        shutil.copytree(installed, relocated)
                        smoke(relocated, target, flavor)
                        run('sudo', 'apt-get', 'remove', '-y', name)
                        assert not (installed / 'bin/mio').exists()
                    finally:
                        run('sudo', 'rm', '-f', source, key)
                else:
                    run('winget', 'settings', '--enable', 'LocalManifestFiles')
                    location = base / ('installed-' + flavor)
                    for v in ('0.0.1', '0.0.2'):
                        ident, files = winget(v, flavor, r)
                        manifests = base / (flavor + v)
                        manifests.mkdir()
                        for suffix, data in files.items():
                            (manifests / (ident + '.' + suffix)).write_text(yaml.safe_dump(data, sort_keys=False))
                        run('winget', 'validate', '--manifest', manifests)
                        run('winget', 'install' if v == '0.0.1' else 'upgrade', '--manifest', manifests, '--location', location, '--accept-package-agreements', '--accept-source-agreements', '--disable-interactivity')
                        installed = location / root.name
                        # WinGet Core creates a hardlink for its distinct command
                        # alias. Validate that additional file before inventory.
                        alias = installed / 'bin/mio-core.exe'
                        if alias.exists():
                            assert digest(alias) == digest(installed / 'bin/mio.exe')
                            # Keep WinGet-owned alias intact for upgrade/uninstall.
                        smoke(installed, target, flavor)
                        alias_command = Path(os.environ['LOCALAPPDATA']) / 'Microsoft/WinGet/Links' / (name + '.exe')
                        run(alias_command, 'version')
                    relocated = base / 'relocated'
                    shutil.copytree(installed, relocated)
                    smoke(relocated, target, flavor)
                    run('winget', 'uninstall', '--id', ident, '--exact', '--silent', '--disable-interactivity')
                    assert not location.exists() or not list(location.rglob('mio.exe'))
                passed(flavor + ' install, upgrade, offline payload, uninstall')
        finally:
            server.shutdown()
    results['status'] = 'PASS'
    if target == 'windows-amd64':
        results['limitations'] = dict(status='NOT_APPLICABLE', reason='Mio run/exec/test spawning and LSP stdin polling')
    output.write_text(json.dumps(results, indent=2)+'\n')

if __name__ == '__main__':
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--assets', type=Path, required=True)
    p.add_argument('--target', choices=TARGETS, required=True)
    a = p.parse_args()
    install(a.assets.resolve(), a.target)
