#!/usr/bin/env python3
"""Fail-closed package-manager metadata generation from verified Release assets.

No compiler building, credential handling or remote publishing lives here.
"""
import argparse
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'distribution'))
from package import TARGETS, digest, extract, inventory
GF4_SEED_SHA256 = '4c3f7dcbea536d756c3c4a93c18a999412625184bd5c90123f1c16a0cd9e36da'


def stable(tag):
    if not re.fullmatch(r'v2\.(?:[1-9]\d*)\.(?:0|[1-9]\d*)', tag):
        raise ValueError('requires a stable v2.1+ tag, never a candidate')
    return tag[1:]


def immutable(path, data):
    path.parent.mkdir(parents=True, exist_ok=True)
    if path.exists() and path.read_bytes() != data:
        raise ValueError('refusing to overwrite different published content: ' + str(path))
    path.write_bytes(data)


def validate(assets, tag, release):
    version = stable(tag)
    if release['tag_name'] != tag or release['draft'] or release['prerelease'] or not release['published_at']:
        raise ValueError('published stable GitHub Release required')
    sums = {}
    for line in (assets / 'SHA256SUMS').read_text().splitlines():
        sha, name = line.split('  ')
        if not re.fullmatch('[0-9a-f]{64}', sha) or Path(name).name != name or name in sums:
            raise ValueError('invalid/duplicate checksum')
        sums[name] = sha
    records = {}
    source = None
    for target in TARGETS:
        hashes = []
        for flavor in ('core', 'full'):
            name = f'mio-{version}-{target}-{flavor}'
            filename = name + ('.zip' if target == 'windows-amd64' else '.tar.gz')
            archive = assets / filename
            if digest(archive) != sums[filename]:
                raise ValueError('checksum mismatch: ' + filename)
            remote = [a for a in release['assets'] if a['name'] == filename]
            url = f'https://github.com/mio-kobo/mio/releases/download/{tag}/{filename}'
            if len(remote) != 1 or remote[0]['browser_download_url'] != url or remote[0].get('digest') != 'sha256:' + sums[filename]:
                raise ValueError('Release digest/URL mismatch: ' + filename)
            with tempfile.TemporaryDirectory() as temp:
                root = extract(archive, Path(temp) / 'unpacked')
                m = json.loads((root / 'manifest.json').read_text())
                p = json.loads((root / 'provenance.json').read_text())
                if m['version'] != version or m['target'] != target or m['flavor'] != flavor or inventory(root) != m['files']:
                    raise ValueError('payload manifest mismatch')
                source = source or m['source_sha']
                if m['source_sha'] != source or p['source_sha'] != source or p['embedded_bunko'] is not False or any(p[k] != m[k] for k in ('version', 'target', 'flavor')):
                    raise ValueError('source/embedded Bunko mismatch')
                binary = root / 'bin' / ('mio.exe' if target == 'windows-amd64' else 'mio')
                hashes.append(digest(binary))
                if p['compiler_sha256'] != hashes[-1]:
                    raise ValueError('compiler provenance mismatch')
                if flavor == 'core' and set(m['files']) != {'bin/' + binary.name, 'LICENSE', 'README.md', 'provenance.json'}:
                    raise ValueError('Core contains extra files')
                if flavor == 'full' and not list((root / 'bunko/kiso').rglob('*.mio')):
                    raise ValueError('Full missing Bunko')
                report_path = assets / f'{target}-native-report.json'
                report = json.loads(report_path.read_text())
                if digest(report_path) != p['native_report_sha256'] or report['status'] != 'PASS' or report['source_sha'] != source or report['target'] != target:
                    raise ValueError('native report mismatch')
                checks = {c['name']: c for c in report['checks']}
                if checks['cli-fixed-point']['status'] != 'PASS' or checks['cli-fixed-point']['engine'] != 'native':
                    raise ValueError('native fixed point required')
                if any(c['required'] and c['status'] != 'PASS' for c in report['checks']):
                    raise ValueError('required native check not PASS')
                if any(report['hashes']['cli-' + x] != hashes[-1] for x in 'BCD'):
                    raise ValueError('B/C/D mismatch')
                # Stable releases require separate frozen seed evidence. Hosted
                # candidate NOT_RUN is intentionally insufficient for publishing.
                if report['seed']['status'] != 'PASS' or report['seed'].get('sha256') != GF4_SEED_SHA256:
                    raise ValueError('stable release requires Trusted Seed PASS')
            records[target, flavor] = dict(filename=filename, sha=sums[filename], url=url, root=name)
        if hashes[0] != hashes[1]:
            raise ValueError('Core/Full compilers differ')
        evidence = json.loads((assets / f'{target}-verification.json').read_text())
        if evidence['status'] != 'PASS' or evidence['target'] != target or not evidence['checks'] or any(c['status'] != 'PASS' for c in evidence['checks']):
            raise ValueError('distribution validation required')
        if evidence['archives'] != {r['filename']: r['sha'] for (t, f), r in records.items() if t == target}:
            raise ValueError('distribution evidence not bound to archives')
    docker = json.loads((assets / 'docker-verification.json').read_text())
    if docker['status'] != 'PASS' or docker['source_sha'] != source or docker['full_archive_sha256'] != records['linux-amd64', 'full']['sha'] or not docker['checks'] or any(c['status'] != 'PASS' for c in docker['checks']):
        raise ValueError('Docker offline validation must match Linux Full')
    for flavor in ('core', 'full'):
        filename = f'mio-{version}-windows-amd64-{flavor}-setup.exe'
        sha = sums[filename]
        url = f'https://github.com/mio-kobo/mio/releases/download/{tag}/{filename}'
        remote = [a for a in release['assets'] if a['name'] == filename]
        if digest(assets / filename) != sha or len(remote) != 1 or remote[0]['browser_download_url'] != url or remote[0].get('digest') != 'sha256:' + sha:
            raise ValueError('Windows installer Release digest/URL mismatch')
        records['windows-installer', flavor] = dict(filename=filename, sha=sha, url=url)
    return version, records


def formula(version, flavor, r, local=False):
    name, klass, other = ('mio', 'Mio', 'mio-core') if flavor == 'full' else ('mio-core', 'MioCore', 'mio')
    url = r['url']
    return f'''class {klass} < Formula
  desc "Programming language with native compilation ({flavor})"
  homepage "https://github.com/mio-kobo/mio"
  url "{url}"
  version "{version}"
  sha256 "{r['sha']}"
  license "MIT"
  depends_on arch: :arm64
  depends_on :macos
  conflicts_with "{other}", because: "both provide the mio command"

  def install
    libexec.install Dir["*"]
    bin.install_symlink libexec/"bin/mio"
  end

  test do
    assert_match "{version}", shell_output("#{{bin}}/mio version")
    (testpath/"hello.mio").write 'chant("hello from Mio");'
    system bin/"mio", "build", "hello.mio", "--target=darwin-arm64", "-o", "hello"
    assert_equal "hello from Mio\\n", shell_output("./hello")
    (testpath/"math.mio").write 'yobu "kiso/math"; chant(math.abs(-9));'
    {('system bin/"mio", "check", "math.mio"' if flavor == 'full' else 'assert_match "E_BUNKO_MISSING", shell_output("#{bin}/mio check math.mio 2>&1", 1)')}
  end
end
'''


def winget(version, flavor, r):
    ident = 'MioKobo.Mio' + ('.Core' if flavor == 'core' else '')
    alias = 'mio' if flavor == 'full' else 'mio-core'
    common = dict(PackageIdentifier=ident, PackageVersion=version, ManifestVersion='1.12.0')
    return ident, {
        'yaml': dict(common, DefaultLocale='en-US', ManifestType='version'),
        'locale.en-US.yaml': dict(common, PackageLocale='en-US', Publisher='Mio Kobo', PackageName='Mio' + (' Core' if flavor == 'core' else ''), License='MIT', LicenseUrl='https://github.com/mio-kobo/mio/blob/Mio-2.1/LICENSE', PackageUrl='https://github.com/mio-kobo/mio', ShortDescription='Mio programming language', ManifestType='defaultLocale'),
        'installer.yaml': dict(common, InstallerType='inno', Scope='user', InstallerSwitches=dict(Silent='/VERYSILENT /SUPPRESSMSGBOXES /NORESTART /SP-', SilentWithProgress='/SILENT /SUPPRESSMSGBOXES /NORESTART /SP-', Log='/LOG="<LOGPATH>"', InstallLocation='/DIR="<INSTALLPATH>"'), UpgradeBehavior='install', Commands=[alias], Installers=[dict(Architecture='x64', InstallerUrl=r['url'], InstallerSha256=r['sha'].upper())], ManifestType='installer')}


def manifest_yaml(value):
    import yaml
    kind = value['ManifestType']
    schema = f'https://aka.ms/winget-manifest.{kind}.1.12.0.schema.json'
    return '# yaml-language-server: $schema=' + schema + '\n' + yaml.safe_dump(value, sort_keys=False)


def generate(version, records, output):
    for flavor in ('core', 'full'):
        name = 'mio' if flavor == 'full' else 'mio-core'
        immutable(output / 'homebrew/Formula' / (name + '.rb'), formula(version, flavor, records['darwin-arm64', flavor]).encode())
        ident, manifests = winget(version, flavor, records['windows-installer', flavor])
        folder = output / 'winget/manifests/m' / Path(*ident.split('.')) / version
        for suffix, value in manifests.items():
            immutable(folder / (ident + '.' + suffix), manifest_yaml(value).encode())


def deb(archive, output, version, flavor, test_version=None):
    if test_version is None:
        stable('v' + version)
    name, other = ('mio', 'mio-core') if flavor == 'full' else ('mio-core', 'mio')
    with tempfile.TemporaryDirectory() as temp:
        base = Path(temp)
        root = extract(archive, base / 'extracted')
        m = json.loads((root / 'manifest.json').read_text())
        if m['target'] != 'linux-amd64' or m['flavor'] != flavor or m['version'] != version or inventory(root) != m['files']:
            raise ValueError('invalid Debian payload')
        stage = base / 'stage'
        dest = stage / 'usr/lib' / name
        dest.parent.mkdir(parents=True)
        shutil.copytree(root, dest)
        (stage / 'usr/bin').mkdir(parents=True)
        (stage / 'usr/bin/mio').symlink_to('../lib/' + name + '/bin/mio')
        control = stage / 'DEBIAN'
        control.mkdir()
        (control / 'control').write_text(f'Package: {name}\nVersion: {test_version or version}\nArchitecture: amd64\nMaintainer: Mio Kobo <maintainers@mio-lang.org>\nSection: devel\nPriority: optional\nConflicts: {other}\nReplaces: {other}\nHomepage: https://github.com/mio-kobo/mio\nDescription: Mio programming language ({flavor})\n')
        output.mkdir(parents=True, exist_ok=True)
        target = output / f'{name}_{test_version or version}_amd64.deb'
        built = base / target.name
        subprocess.run(['dpkg-deb', '--root-owner-group', '--build', str(stage), str(built)], check=True, env=dict(os.environ, SOURCE_DATE_EPOCH='0'))
        immutable(target, built.read_bytes())


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--assets', type=Path, required=True)
    p.add_argument('--tag', required=True)
    p.add_argument('--release', type=Path, required=True)
    p.add_argument('--output', type=Path, required=True)
    a = p.parse_args()
    version, records = validate(a.assets, a.tag, json.loads(a.release.read_text()))
    generate(version, records, a.output)
    for flavor in ('core', 'full'):
        deb(a.assets / records['linux-amd64', flavor]['filename'], a.output / 'apt/pool/main/m/mio', version, flavor)
    print('PASS: six stable archives, native/seed/distribution evidence; metadata generated')

if __name__ == '__main__':
    main()
