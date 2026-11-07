"""GF4 origin evidence and its relationship to a distribution compiler."""
import re

GF4_SEED_SHA256 = '4c3f7dcbea536d756c3c4a93c18a999412625184bd5c90123f1c16a0cd9e36da'
TARGETS = ('darwin-arm64', 'linux-amd64', 'windows-amd64')
STABLE_VERSION = r'2\.[1-9]\d*\.(?:0|[1-9]\d*)'


def stable_version(version):
    return re.fullmatch(STABLE_VERSION, version) is not None


def seed_binding(seed, report, inputs, target, compiler_sha, evidence_sha):
    """Bind a separately authenticated seed report to the tested native bytes."""
    if (seed['schema_version'] != 1 or seed['suite'] != 'trusted-gf4-distribution'
            or seed['status'] != 'PASS' or seed['seed'] != dict(status='PASS', sha256=GF4_SEED_SHA256)
            or seed['target'] != 'darwin-arm64' or seed['source_sha'] != inputs['source_sha']
            or seed['distribution_input'] != inputs or report['distribution_input'] != inputs
            or seed['run'] != report['run']):
        raise ValueError('Trusted GF4 report must PASS for these exact inputs and native run')

    hashes = seed['hashes']
    checks = {c['name']: c for c in seed['checks']}
    required = {'runner-os-arch', 'seed-pin', 'seed-build-A', 'cli-fixed-point'}
    required.update('cli-stage-' + stage for stage in 'BCD')
    required.update('cross-stage-a ' + name for name in TARGETS if name != 'darwin-arm64')
    if (any(c['required'] and c['status'] != 'PASS' for c in seed['checks'])
            or any(checks.get(name, {}).get('status') != 'PASS' for name in required)
            or any(not re.fullmatch(r'[0-9a-f]{64}', hashes.get('cli-' + stage, '')) for stage in 'ABCD')
            or not hashes['cli-B'] == hashes['cli-C'] == hashes['cli-D']):
        raise ValueError('Trusted GF4 chain is incomplete or differs at the fixed point')

    trusted_sha = hashes['cli-B'] if target == 'darwin-arm64' else hashes['stage-a-' + target]
    if target != 'darwin-arm64':
        cross = checks['cross-stage-a ' + target]
        if cross.get('B') != trusted_sha or cross.get('C') != trusted_sha:
            raise ValueError('Trusted GF4 cross builds must agree')
    if trusted_sha != compiler_sha:
        raise ValueError('GF4-rooted compiler differs from the target-native B/C/D')
    return dict(status='PASS', sha256=GF4_SEED_SHA256, origin='darwin-arm64',
                report_sha256=evidence_sha, compiler_sha256=trusted_sha)
