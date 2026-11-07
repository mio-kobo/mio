"""GF4 origin evidence and its relationship to a distribution compiler."""
import re

GF4_SEED_SHA256 = '4c3f7dcbea536d756c3c4a93c18a999412625184bd5c90123f1c16a0cd9e36da'
REPOSITORY = 'mio-kobo/mio'
WORKFLOW = '.github/workflows/distribution-pr.yml'
# Set only after the original executable is available as a public immutable
# release asset. Pin its tag, release ID and asset ID in this reviewed source.
GF4_SOURCE = None
GF4_ASSET_NAME = 'mio-gf4-darwin-arm64'
TARGETS = ('darwin-arm64', 'linux-amd64', 'windows-amd64')
STABLE_VERSION = r'2\.[1-9]\d*\.(?:0|[1-9]\d*)'
CROSS_TARGETS = TARGETS[1:]
GF4_LOGS = ('seed-release-attestation.log', 'seed-signature.log', 'seed-build-A.log', 'cli-B.log', 'cli-C.log', 'cli-D.log',
            *(f'cross-{stage}-{target}.log' for target in CROSS_TARGETS for stage in 'BC'))


def stable_version(version):
    return re.fullmatch(STABLE_VERSION, version) is not None


def release_context(version):
    return dict(repository=REPOSITORY, workflow=WORKFLOW, event='workflow_dispatch',
                ref=f'refs/tags/v{version}', runner_environment='github-hosted')


def valid_run(run):
    return all(re.fullmatch(r'[1-9][0-9]*', run.get(key, '')) for key in ('id', 'attempt'))


def validate_seed(seed, source, version, run):
    """Require complete hosted GF4 evidence for the exact release and attempt."""
    if (GF4_SOURCE is None or not stable_version(version)
            or not re.fullmatch(r'[0-9a-f]{40}', source)
            or not valid_run(run)
            or seed['schema_version'] != 2 or seed['suite'] != 'trusted-gf4-distribution'
            or seed['status'] != 'PASS'
            or seed['seed'] != dict(status='PASS', sha256=GF4_SEED_SHA256, source=GF4_SOURCE)
            or seed['target'] != 'darwin-arm64' or seed['source_sha'] != source
            or seed['distribution_input']['source_sha'] != source
            or seed['distribution_input']['version'] != version
            or seed['distribution_input']['embedded_bunko'] is not False
            or any(seed['run'].get(key) != run[key] for key in ('id', 'attempt'))
            or seed.get('authorization') != release_context(version)):
        raise ValueError('Trusted GF4 report must PASS for the pinned source, exact tag and Actions attempt')

    runner = seed['runner']
    if (runner['os'] != 'macOS' or runner['arch'] != 'ARM64'
            or runner['system'] != 'Darwin' or runner['machine'].lower() not in ('arm64', 'aarch64')):
        raise ValueError('Trusted GF4 report must come from a hosted Darwin ARM64 runner')

    hashes = seed['hashes']
    checks = {c['name']: c for c in seed['checks']}
    required = dict.fromkeys(('distribution-input', 'release-context', 'runner-os-arch',
                              'seed-source', 'seed-pin', 'seed-release-attestation', 'seed-signature'), 'host')
    required.update(dict.fromkeys(('seed-build-A', 'cli-fixed-point',
                                  *('cli-stage-' + stage for stage in 'BCD'),
                                  *('cross-stage-a ' + name for name in CROSS_TARGETS)), 'native'))
    if (len(checks) != len(seed['checks'])
            or any(c['required'] and c['status'] != 'PASS' for c in seed['checks'])
            or any(checks.get(name, {}).get('status') != 'PASS'
                   or checks[name].get('required') is not True
                   or checks[name].get('engine') != engine for name, engine in required.items())
            or any(not re.fullmatch(r'[0-9a-f]{64}', hashes.get('cli-' + stage, '')) for stage in 'ABCD')
            or not hashes['cli-B'] == hashes['cli-C'] == hashes['cli-D']):
        raise ValueError('Trusted GF4 chain is incomplete or differs at the fixed point')

    for target in CROSS_TARGETS:
        cross = checks['cross-stage-a ' + target]
        sha = hashes.get('stage-a-' + target, '')
        if not re.fullmatch(r'[0-9a-f]{64}', sha) or cross.get('B') != sha or cross.get('C') != sha:
            raise ValueError('Trusted GF4 cross builds must agree')
    logs = seed['logs']
    if set(logs) != set(GF4_LOGS) or any(not re.fullmatch(r'[0-9a-f]{64}', sha) for sha in logs.values()):
        raise ValueError('Trusted GF4 execution logs are incomplete')


def seed_binding(seed, report, inputs, target, compiler_sha, evidence_sha):
    """Bind a separately authenticated seed report to the tested native bytes."""
    validate_seed(seed, inputs['source_sha'], inputs['version'], report['run'])
    if (seed['distribution_input'] != inputs or report['distribution_input'] != inputs
            or seed['run'] != report['run']):
        raise ValueError('Trusted GF4 report and native verification inputs differ')
    hashes = seed['hashes']
    trusted_sha = hashes['cli-B'] if target == 'darwin-arm64' else hashes['stage-a-' + target]
    if trusted_sha != compiler_sha:
        raise ValueError('GF4-rooted compiler differs from the target-native B/C/D')
    return dict(status='PASS', sha256=GF4_SEED_SHA256, origin='darwin-arm64',
                report_sha256=evidence_sha, compiler_sha256=trusted_sha)
