"""Pinned bootstrap identities and evidence binding for distribution compilers."""
from dataclasses import dataclass
import re

GF4_SEED_SHA256 = '4c3f7dcbea536d756c3c4a93c18a999412625184bd5c90123f1c16a0cd9e36da'
REPOSITORY = 'mio-kobo/mio'
WORKFLOW = '.github/workflows/distribution-pr.yml'
# Set only after the original executable is available as a public immutable
# release asset. Pin its tag, release ID and asset ID in this reviewed source.
GF4_SOURCE = {
    'tag': 'gf4-seed-v1-immutable',
    'release_id': 437559652,
    'asset_id': 720633092,
}
GF4_ASSET_NAME = 'mio-gf4-darwin-arm64'
TARGETS = ('darwin-arm64', 'linux-amd64', 'windows-amd64')
STABLE_VERSION = r'2\.[1-9]\d*\.(?:0|[1-9]\d*)'
CROSS_TARGETS = TARGETS[1:]
SEED_LOGS = ('seed-release-attestation.log', 'seed-signature.log', 'seed-build-A.log', 'cli-B.log', 'cli-C.log', 'cli-D.log',
            *(f'cross-{stage}-{target}.log' for target in CROSS_TARGETS for stage in 'BC'))


# Mio v2.1 native bootstrap seed
NATIVE_V21_SEED_SHA256 = (
    'be555a5e51380a0151c3e4b17d57dd79120123ab170dae3103146d32ab5a0c79'
)

NATIVE_V21_SOURCE = {
    'tag': 'bootstrap-v2.1.0-seed.1',
    'release_id': 437632871,
    'asset_id': 720957113,
}

NATIVE_V21_ASSET_NAME = 'mio-v2.1.0-darwin-arm64'

GF4 = 'gf4'
NATIVE_V21 = 'native-v21'
SEED_KINDS = (GF4, NATIVE_V21)
GF4_LOGS = SEED_LOGS  # Historical evidence uses the same set of execution logs.


@dataclass(frozen=True)
class SeedIdentity:
    artifact: str
    tag: str
    release_id: int
    asset_id: int
    asset_name: str
    sha256: str

    @property
    def source(self):
        return dict(tag=self.tag, release_id=self.release_id, asset_id=self.asset_id)

    @property
    def suite(self):
        return self.artifact + '-distribution'

    @property
    def report_name(self):
        return self.artifact + '-report.json'

    @property
    def log_dir(self):
        return self.artifact + '-logs'


def seed_identity(kind):
    if kind == GF4:
        if GF4_SOURCE is None:
            return None
        return SeedIdentity('trusted-gf4', **GF4_SOURCE, asset_name=GF4_ASSET_NAME, sha256=GF4_SEED_SHA256)
    if kind == NATIVE_V21:
        return SeedIdentity('trusted-native-v21', **NATIVE_V21_SOURCE,
                            asset_name=NATIVE_V21_ASSET_NAME, sha256=NATIVE_V21_SEED_SHA256)
    raise ValueError(f'unknown bootstrap seed: {kind}')


def seed_record(identity, kind):
    record = dict(status='PASS', sha256=identity.sha256, source=identity.source)
    if kind == NATIVE_V21:
        record['asset_name'] = identity.asset_name
    return record


def stable_version(version):
    return re.fullmatch(STABLE_VERSION, version) is not None


def release_context(version):
    return dict(repository=REPOSITORY, workflow=WORKFLOW, event='workflow_dispatch',
                ref=f'refs/tags/v{version}', runner_environment='github-hosted')


def valid_run(run):
    return all(re.fullmatch(r'[1-9][0-9]*', run.get(key, '')) for key in ('id', 'attempt'))


def validate_seed(seed, source, version, run, kind=GF4):
    """Require complete hosted evidence for the selected seed, release and attempt."""
    identity = seed_identity(kind)
    if (identity is None or not stable_version(version)
            or not re.fullmatch(r'[0-9a-f]{40}', source)
            or not valid_run(run)
            or seed['schema_version'] != 2 or seed['suite'] != identity.suite
            or seed['status'] != 'PASS'
            or seed['seed'] != seed_record(identity, kind)
            or seed['target'] != 'darwin-arm64' or seed['source_sha'] != source
            or seed['distribution_input']['source_sha'] != source
            or seed['distribution_input']['version'] != version
            or seed['distribution_input']['embedded_bunko'] is not False
            or any(seed['run'].get(key) != run[key] for key in ('id', 'attempt'))
            or seed.get('authorization') != release_context(version)):
        raise ValueError('Trusted seed report must PASS for the pinned source, exact tag and Actions attempt')

    runner = seed['runner']
    if (runner['os'] != 'macOS' or runner['arch'] != 'ARM64'
            or runner['system'] != 'Darwin' or runner['machine'].lower() not in ('arm64', 'aarch64')):
        raise ValueError('Trusted seed report must come from a hosted Darwin ARM64 runner')

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
        raise ValueError('Trusted seed chain is incomplete or differs at the fixed point')

    for target in CROSS_TARGETS:
        cross = checks['cross-stage-a ' + target]
        sha = hashes.get('stage-a-' + target, '')
        if not re.fullmatch(r'[0-9a-f]{64}', sha) or cross.get('B') != sha or cross.get('C') != sha:
            raise ValueError('Trusted seed cross builds must agree')
    logs = seed['logs']
    if set(logs) != set(SEED_LOGS) or any(not re.fullmatch(r'[0-9a-f]{64}', sha) for sha in logs.values()):
        raise ValueError('Trusted seed execution logs are incomplete')


def seed_binding(seed, report, inputs, target, compiler_sha, evidence_sha, kind=GF4):
    """Bind a separately authenticated seed report to the tested native bytes."""
    validate_seed(seed, inputs['source_sha'], inputs['version'], report['run'], kind)
    if (seed['distribution_input'] != inputs or report['distribution_input'] != inputs
            or seed['run'] != report['run']):
        raise ValueError('Trusted seed report and native verification inputs differ')
    hashes = seed['hashes']
    trusted_sha = hashes['cli-B'] if target == 'darwin-arm64' else hashes['stage-a-' + target]
    if trusted_sha != compiler_sha:
        raise ValueError('Seed-rooted compiler differs from the target-native B/C/D')
    identity = seed_identity(kind)
    binding = dict(status='PASS', sha256=identity.sha256, origin='darwin-arm64',
                   report_sha256=evidence_sha, compiler_sha256=trusted_sha)
    if kind == NATIVE_V21:
        binding.update(kind=kind, source=identity.source, asset_name=identity.asset_name)
    return binding
