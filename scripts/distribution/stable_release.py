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
from seed_evidence import (GF4, NATIVE_V21, REPOSITORY, SEED_KINDS, SEED_LOGS, TARGETS, WORKFLOW,
                           seed_identity, stable_version, validate_seed)

REPORT = 'release-verification.json'
BUNDLE = 'release-verification.sigstore.json'
EVIDENCE_SCHEMAS = {GF4: 1, NATIVE_V21: 2}


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


def evidence_files(version, kind=GF4):
    identity = seed_identity(kind)
    return set(payloads(version) + reports() + [
        identity.report_name, *(f'{identity.log_dir}/{name}' for name in SEED_LOGS),
        'docker-verification.json', 'chocolatey-verification.json', 'SHA256SUMS',
        *[f'apt-container-{i}-verification.json' for i in range(3)],
    ])


def check_seed_artifact(report_path, logs, source, version, run, kind=GF4):
    seed = json.loads(report_path.read_text())
    validate_seed(seed, source, version, run, kind)
    for name in SEED_LOGS:
        if digest(logs / name) != seed['logs'][name]:
            raise ValueError('Trusted seed log does not match its report: ' + name)


def assemble(downloads, output, version, kind=GF4):
    if (not stable_version(version) or os.environ['GITHUB_REF'] != f'refs/tags/v{version}'
            or os.environ['GITHUB_EVENT_NAME'] != 'workflow_dispatch' or os.environ['GITHUB_REPOSITORY'] != REPOSITORY):
        raise ValueError('release evidence requires the official workflow dispatched on the exact stable tag')
    run_id, attempt = os.environ['GITHUB_RUN_ID'], os.environ['GITHUB_RUN_ATTEMPT']
    suffix = f'{run_id}-{attempt}'
    source = os.environ['GITHUB_SHA']
    run = dict(id=run_id, attempt=attempt)
    identity = seed_identity(kind)
    seed = downloads / f'{identity.artifact}-{suffix}'
    check_seed_artifact(seed / identity.report_name, seed / 'logs', source, version, run, kind)
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
    shutil.copyfile(seed / identity.report_name, output / identity.report_name)
    logs = output / identity.log_dir
    logs.mkdir()
    for name in SEED_LOGS:
        shutil.copyfile(seed / 'logs' / name, logs / name)
    for i in range(3):
        shutil.copyfile(downloads / f'apt-container-{i}-{suffix}' / 'package-manager-verification.json',
                        output / f'apt-container-{i}-verification.json')
    (output / 'SHA256SUMS').write_text(''.join(f'{digest(output / name)}  {name}\n' for name in payloads(version)))
    write_json(output / REPORT, dict(schema_version=EVIDENCE_SCHEMAS[kind], status='PASS', version=version, source_sha=source,
                                    run=run,
                                    files={name: digest(output / name) for name in evidence_files(version, kind)}))


def authenticate_report(assets, tag, source):
    """Authenticate the signed inventory before looking for the listed files."""
    if not tag.startswith('v') or not stable_version(tag[1:]) or not re.fullmatch(r'[0-9a-f]{40}', source):
        raise ValueError('expected the exact commit resolved from the stable release tag')
    try:
        subprocess.run(['gh', 'attestation', 'verify', str(assets / REPORT), '--bundle', str(assets / BUNDLE),
                        '--repo', REPOSITORY, '--signer-workflow', f'{REPOSITORY}/{WORKFLOW}',
                        '--signer-digest', source, '--source-digest', source, '--source-ref', f'refs/tags/{tag}',
                        '--deny-self-hosted-runners'], check=True)
    except (OSError, subprocess.CalledProcessError) as error:
        raise ValueError('stable release verification attestation is missing or invalid') from error
    evidence = json.loads((assets / REPORT).read_text())
    kind = next((kind for kind, schema in EVIDENCE_SCHEMAS.items() if evidence['schema_version'] == schema), None)
    if (kind is None or evidence['status'] != 'PASS' or evidence['version'] != tag[1:]
            or evidence['source_sha'] != source or set(evidence['files']) != evidence_files(tag[1:], kind)):
        raise ValueError('stable release attestation describes different inputs')
    return evidence, kind


def authenticate(assets, tag, source, *, restore=None):
    evidence, kind = authenticate_report(assets, tag, source)
    if restore is not None:
        restore(assets, evidence)
    for name, sha in evidence['files'].items():
        path = assets / name
        if path.is_symlink() or not path.is_file():
            raise ValueError('signed release evidence asset is missing: ' + name)
        if digest(path) != sha:
            raise ValueError('asset does not match signed release evidence: ' + name)
    identity = seed_identity(kind)
    check_seed_artifact(assets / identity.report_name, assets / identity.log_dir, source, tag[1:], evidence['run'], kind)
    return evidence['run'], kind


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--downloads', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--version', required=True)
    parser.add_argument('--seed', choices=SEED_KINDS, default=GF4)
    args = parser.parse_args()
    assemble(args.downloads, args.output, args.version, args.seed)
