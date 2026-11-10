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
OPC = 'http://schemas.openxmlformats.org/package/2006/'
MANIFEST_REL = 'http://schemas.microsoft.com/packaging/2010/07/manifest'
CORE_REL = OPC + 'relationships/metadata/core-properties'
CORE_PROPERTIES = 'package/services/metadata/core-properties/mio.psmdcp'
CORE_CONTENT_TYPE = 'application/vnd.openxmlformats-package.core-properties+xml'
DUBLIN_CORE = 'http://purl.org/dc/elements/1.1/'


def opc_parts(manifest, fields):
    # NuGet 2 locates the nuspec via this relationship. NuGet 3 also
    # writes it for compatibility, although its reader can find a bare nuspec.
    rels = ET.Element('Relationships', xmlns=OPC + 'relationships')
    for relation, target, identifier in (
        (MANIFEST_REL, manifest, 'RManifest'),
        (CORE_REL, CORE_PROPERTIES, 'RCoreProperties'),
    ):
        ET.SubElement(rels, 'Relationship', Type=relation, Target='/' + target, Id=identifier)

    core = ET.Element('coreProperties', xmlns=OPC + 'metadata/core-properties',
                      attrib={'xmlns:dc': DUBLIN_CORE})
    for element, field in (('dc:creator', 'authors'), ('dc:description', 'description'),
                           ('dc:identifier', 'id'), ('version', 'version'), ('keywords', 'tags')):
        ET.SubElement(core, element).text = fields[field]

    types = ET.Element('Types', xmlns=OPC + 'content-types')
    for extension, content_type in (
        ('rels', 'application/vnd.openxmlformats-package.relationships+xml'),
        ('psmdcp', CORE_CONTENT_TYPE),
        ('nuspec', 'application/octet'),
        ('ps1', 'application/octet'),
        ('json', 'application/octet'),
    ):
        ET.SubElement(types, 'Default', Extension=extension, ContentType=content_type)

    return {
        path: ET.tostring(element, encoding='utf-8', xml_declaration=True)
        for path, element in (('_rels/.rels', rels), (CORE_PROPERTIES, core), ('[Content_Types].xml', types))
    }


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
    pkg = ET.Element('package', xmlns='http://schemas.microsoft.com/packaging/2011/08/nuspec.xsd')
    meta = ET.SubElement(pkg, 'metadata')
    description = ('Native Mio compiler, supported Bunko standard library and integrated check/build/run/exec/test/lsp/disasm tools. Supports offline development.' if flavor == 'full' else 'The identical native Mio compiler without importable Bunko or standalone tools. Compiles library-free programs. Command: mio-core.')
    fields = dict(id=name, version=version, title='Mio' + (' Core' if flavor == 'core' else ''), authors='Mio Kobo',
                  projectUrl='https://github.com/mio-kobo/mio', licenseUrl='https://github.com/mio-kobo/mio/blob/Mio-2.1/LICENSE',
                  iconUrl='https://cdn.jsdelivr.net/gh/mio-kobo/mio-docs@07dcf2520b848ee84285039f058c74983728b6e3/app/icon.png',
                  packageSourceUrl='https://github.com/mio-kobo/mio/tree/Mio-2.1/scripts/package_managers',
                  releaseNotes=f'https://github.com/mio-kobo/mio/releases/tag/v{version}' if not fixture else 'Chocolatey lifecycle fixture.',
                  docsUrl='https://mio.kaelexvp.cc/docs/',
                  bugTrackerUrl='https://github.com/mio-kobo/mio/issues',
                  projectSourceUrl='https://github.com/mio-kobo/mio',
                  requireLicenseAcceptance='false', copyright='Copyright (c) 2026 ExMikuPro', tags='mio programming language compiler native bunko',
                  summary='Mio programming language (' + flavor + ')', description=description + '\nWindows AMD64 only. Keep projects outside the Chocolatey package directory. Full and Core coexist as mio and mio-core. Requires Chocolatey; no extra runtime dependencies.')
    for key, value in fields.items():
        ET.SubElement(meta, key).text = value
    data = dict(record, command=name, flavor=flavor, version=record['root'].removeprefix('mio-').removesuffix('-windows-amd64-' + flavor))
    files = {name + '.nuspec': ET.tostring(pkg, encoding='utf-8', xml_declaration=True),
             'tools/release.json': (json.dumps(data, sort_keys=True, indent=2) + '\n').encode()}
    for script in ('chocolateyInstall.ps1', 'chocolateyUninstall.ps1'):
        content = (HERE / 'chocolatey/tools' / script).read_bytes().replace(b'\r\n', b'\n')
        if script == 'chocolateyInstall.ps1':
            # Keep the URL and its checksum visible to CCR's script analysis.
            content = content.replace(b'@MIO_ZIP_URL@', record['url'].encode('ascii'))
            content = content.replace(b'@MIO_ZIP_SHA256@', record['sha'].encode('ascii'))
        files['tools/' + script] = content
    files.update(opc_parts(name + '.nuspec', fields))
    return files


def build(version, flavor, record, output, fixture=False):
    files = package_files(version, flavor, record, fixture)
    stream = io.BytesIO()
    # These small metadata-only packages use stored entries to keep bytes
    # independent of the host zlib version.
    with zipfile.ZipFile(stream, 'w') as archive:
        for name, data in sorted(files.items()):
            info = zipfile.ZipInfo(name, date_time=(2020, 1, 1, 0, 0, 0))
            info.create_system = 3  # Use the same Unix ZIP attributes on Windows.
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
    p.add_argument('--source-sha', required=True)
    p.add_argument('--output', type=Path, required=True)
    a = p.parse_args()
    version, records = validate(a.assets, a.tag, json.loads(a.release.read_text()), a.source_sha)
    for flavor in ('core', 'full'):
        print(build(version, flavor, records['windows-amd64', flavor], a.output))


if __name__ == '__main__':
    main()
