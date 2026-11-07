#!/usr/bin/env python3
"""Assemble and independently verify the evidence for a tagged stable build."""
import argparse
import json
import os
from pathlib import Path
import re
import shutil
import subprocess

from package import digest, write_json
from seed_evidence import TARGETS, stable_version

REPOSITORY = 'mio-kobo/mio'
WORKFLOW = '.github/workflows/distribution-pr.yml'
REPORT = 'release-verification.json'
BUNDLE = 'release-verification.sigstore.json'
SEED_REPORT = 'trusted-gf4-report.json'


def archives(version, target):
    extension = 'zip' if target == 'windows-amd64' else 'tar.gz'
    return [f'mio-{version}-{target}-{flavor}.{extension}' for flavor in ('core', 'full')]


def installers(version):
    return [f'mio-{version}-windows-amd64-{flavor}-setup.exe' for flavor in ('core', 'full')]


def docker_archive(version):
    return f'mio-{version}-linux-amd64-full.docker.tar'


def payloads(version):
    return [name for target in TARGETS for name in archives(version, target)] + installers(version) + [docker_archive(version)]


def reports():
    return [f'{target}-{report}' for target in TARGETS for report in
            ('native-report.json', 'verification.json', 'package-manager-verification.json')]


def evidence_files(version):
    return set(payloads(version) + reports() + [
        SEED_REPORT, 'docker-verification.json', 'chocolatey-verification.json', 'SHA256SUMS',
        *[f'apt-container-{i}-verification.json' for i in range(3)],
    ])


def assemble(downloads, output, version):
    if not stable_version(version) or os.environ['GITHUB_REF'] != f'refs/tags/v{version}':
        raise ValueError('release evidence requires the exact stable tag')
    run_id, attempt = os.environ['GITHUB_RUN_ID'], os.environ['GITHUB_RUN_ATTEMPT']
    suffix = f'{run_id}-{attempt}'
    source = os.environ['GITHUB_SHA']
    output.mkdir(parents=True)
    for target in TARGETS:
        artifact = downloads / f'mio-candidates-{target}-{suffix}'
        names = archives(version, target)
        if target == 'windows-amd64':
            names += installers(version)
        for name in names:
            shutil.copyfile(artifact / name, output / name)
        for name in ('native-report.json', 'verification.json', 'package-manager-verification.json'):
            shutil.copyfile(artifact / name, output / f'{target}-{name}')

    docker = downloads / f'mio-docker-linux-amd64-candidate-{suffix}'
    docker_report = json.loads((docker / 'docker-verification.json').read_text())
    image = docker / 'mio-linux-amd64-full-candidate.docker.tar'
    if docker_report['archive_sha256'] != digest(image) or docker_report['source_sha'] != source:
        raise ValueError('offline Docker evidence does not match the image or source')
    shutil.copyfile(image, output / docker_archive(version))
    shutil.copyfile(docker / 'docker-verification.json', output / 'docker-verification.json')
    shutil.copyfile(downloads / f'chocolatey-tests-{suffix}' / 'chocolatey-verification.json',
                    output / 'chocolatey-verification.json')
    shutil.copyfile(downloads / f'trusted-gf4-{suffix}' / SEED_REPORT, output / SEED_REPORT)
    for i in range(3):
        shutil.copyfile(downloads / f'apt-container-{i}-{suffix}' / 'package-manager-verification.json',
                        output / f'apt-container-{i}-verification.json')
    (output / 'SHA256SUMS').write_text(''.join(f'{digest(output / name)}  {name}\n' for name in payloads(version)))
    write_json(output / REPORT, dict(schema_version=1, status='PASS', version=version, source_sha=source,
                                    run=dict(id=run_id, attempt=attempt),
                                    files={name: digest(output / name) for name in evidence_files(version)}))


def authenticate(assets, tag, source):
    if not re.fullmatch(r'[0-9a-f]{40}', source):
        raise ValueError('expected the exact commit resolved from the release tag')
    try:
        subprocess.run(['gh', 'attestation', 'verify', str(assets / REPORT), '--bundle', str(assets / BUNDLE),
                        '--repo', REPOSITORY, '--signer-workflow', f'{REPOSITORY}/{WORKFLOW}',
                        '--signer-digest', source, '--source-digest', source, '--source-ref', f'refs/tags/{tag}',
                        '--deny-self-hosted-runners'], check=True)
    except (OSError, subprocess.CalledProcessError) as error:
        raise ValueError('stable release verification attestation is missing or invalid') from error
    evidence = json.loads((assets / REPORT).read_text())
    if (evidence['schema_version'] != 1 or evidence['status'] != 'PASS' or evidence['version'] != tag[1:]
            or evidence['source_sha'] != source or set(evidence['files']) != evidence_files(tag[1:])):
        raise ValueError('stable release attestation describes different inputs')
    for name, sha in evidence['files'].items():
        if digest(assets / name) != sha:
            raise ValueError('asset does not match signed release evidence: ' + name)
    return evidence['run']


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--downloads', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--version', required=True)
    args = parser.parse_args()
    assemble(args.downloads, args.output, args.version)
