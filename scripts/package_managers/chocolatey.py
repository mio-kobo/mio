#!/usr/bin/env python3
"""Generate small, deterministic NuGet packages around verified official ZIPs.

Stable entry point always uses the existing six-archive release/seed gate.
The loopback fixture entry point is explicitly separate and cannot publish.
"""
import argparse
import io
import json
from pathlib import Path
import re
import xml.etree.ElementTree as ET
import zipfile
from release import immutable, stable, validate

HERE = Path(__file__).resolve().parent
SOURCE = 'https://push.chocolatey.org/'
FEED = 'https://community.chocolatey.org/api/v2/'


def package_files(version, flavor, record, fixture=False):
    if flavor not in ('core', 'full'):
        raise ValueError('unknown flavor')
    name = 'mio' if flavor == 'full' else 'mio-core'
    if fixture:
        if version not in ('0.0.1', '0.0.2') or not re.fullmatch(r'http://127\.0\.0\.1:[0-9]+/mio-[\w.-]+\.zip', record['url']):
            raise ValueError('fixture must use synthetic versions and loopback ZIP')
    else:
        stable('v' + version)
        filename = f'mio-{version}-windows-amd64-{flavor}.zip'
        if record['url'] != f'https://github.com/mio-kobo/mio/releases/download/v{version}/{filename}' or record['root'] != filename[:-4]:
            raise ValueError('fixed official stable ZIP URL/root required')
    if not re.fullmatch('[0-9a-f]{64}', record['sha']) or not re.fullmatch(r'mio-[\w.-]+-windows-amd64-' + flavor, record['root']):
        raise ValueError('invalid checksum/root')
    pkg = ET.Element('package', xmlns='http://schemas.microsoft.com/packaging/2015/06/nuspec.xsd')
    meta = ET.SubElement(pkg, 'metadata')
    description = ('Native Mio compiler, supported Bunko standard library and integrated check/build/run/exec/test/lsp/disasm tools. Supports offline development.' if flavor == 'full' else 'The identical native Mio compiler without importable Bunko or standalone tools. Compiles library-free programs. Command: mio-core.')
    fields = dict(id=name, version=version, title='Mio' + (' Core' if flavor == 'core' else ''), authors='Mio Kobo',
                  projectUrl='https://github.com/mio-kobo/mio', licenseUrl='https://github.com/mio-kobo/mio/blob/Mio-2.1/LICENSE',
                  requireLicenseAcceptance='false', projectSourceUrl='https://github.com/mio-kobo/mio',
                  packageSourceUrl='https://github.com/mio-kobo/mio/tree/Mio-2.1/scripts/package_managers/chocolatey',
                  bugTrackerUrl='https://github.com/mio-kobo/mio/issues', tags='mio programming language compiler native bunko',
                  summary='Mio programming language (' + flavor + ')', description=description + '\nWindows AMD64 only. kiso/net/tcp is unsupported (no Winsock layer). Keep projects outside the Chocolatey package directory. Full and Core coexist as mio and mio-core. Requires Chocolatey; no extra runtime dependencies.')
    for key, value in fields.items():
        ET.SubElement(meta, key).text = value
    data = dict(record, command=name, flavor=flavor, version=record['root'].removeprefix('mio-').removesuffix('-windows-amd64-' + flavor))
    files = {name + '.nuspec': ET.tostring(pkg, encoding='utf-8', xml_declaration=True),
             'tools/release.json': (json.dumps(data, sort_keys=True, indent=2) + '\n').encode()}
    for script in ('chocolateyInstall.ps1', 'chocolateyUninstall.ps1'):
        files['tools/' + script] = (HERE / 'chocolatey/tools' / script).read_bytes()
    # OPC content types used by NuGet/Chocolatey; no binary payload is embedded.
    files['[Content_Types].xml'] = b'<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="nuspec" ContentType="application/octet"/><Default Extension="ps1" ContentType="application/octet"/><Default Extension="json" ContentType="application/octet"/></Types>'
    return files


def build(version, flavor, record, output, fixture=False):
    files = package_files(version, flavor, record, fixture)
    stream = io.BytesIO()
    with zipfile.ZipFile(stream, 'w', compression=zipfile.ZIP_DEFLATED) as archive:
        for name, data in sorted(files.items()):
            info = zipfile.ZipInfo(name, date_time=(2020, 1, 1, 0, 0, 0))
            info.compress_type = zipfile.ZIP_DEFLATED
            info.external_attr = 0o100644 << 16
            archive.writestr(info, data)
    name = 'mio' if flavor == 'full' else 'mio-core'
    path = output / f'{name}.{version}.nupkg'
    immutable(path, stream.getvalue())
    return path


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--assets', type=Path, required=True)
    p.add_argument('--tag', required=True)
    p.add_argument('--release', type=Path, required=True)
    p.add_argument('--output', type=Path, required=True)
    a = p.parse_args()
    version, records = validate(a.assets, a.tag, json.loads(a.release.read_text()))
    for flavor in ('core', 'full'):
        print(build(version, flavor, records['windows-amd64', flavor], a.output))


if __name__ == '__main__':
    main()
