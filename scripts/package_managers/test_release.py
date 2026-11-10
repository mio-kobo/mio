import json
import copy
import subprocess
from unittest.mock import patch
from pathlib import Path
import sys
import tempfile
import unittest
sys.path.insert(0, str(Path(__file__).resolve().parent))
from release import stable, immutable, formula, winget, validate, windows_product_code
from package import digest, inventory, write_json
from seed_evidence import (GF4, GF4_LOGS, GF4_SEED_SHA256, GF4_SOURCE, NATIVE_V21, SEED_KINDS,
                           TARGETS, release_context, seed_binding, seed_identity, seed_record)
from stable_release import (BUNDLE, EVIDENCE_SCHEMAS, REPORT, archives, assemble, authenticate,
                            check_seed_artifact, docker_archive, evidence_files, installers)

SOURCE = 'a' * 40
VERSION = '2.1.0'
RUN = dict(id='123', attempt='1')


def version_for(kind):
    """Historical v2.1.0 evidence uses GF4; the native v2.1 seed covers v2.1.1."""
    return VERSION if kind == GF4 else '2.1.1'


def inputs_for(kind):
    return dict(version=version_for(kind), source_sha=SOURCE, embedded_bunko=kind != GF4)


INPUTS = inputs_for(GF4)
SEED_REPORT = 'trusted-gf4-report.json'
SEED_LOG_DIR = 'trusted-gf4-logs'


class ReleaseTests(unittest.TestCase):
    def seed_fixture(self, base, compiler_sha, kind=GF4):
        identity = seed_identity(kind)
        inputs = inputs_for(kind)
        host = ['distribution-input', 'release-context', 'runner-os-arch', 'seed-source', 'seed-pin',
                'seed-release-attestation', 'seed-signature']
        native = ['seed-build-A', 'cli-fixed-point', *['cli-stage-' + stage for stage in 'BCD'],
                  *['cross-stage-a ' + target for target in TARGETS if target != 'darwin-arm64']]
        logs = base / identity.log_dir
        logs.mkdir(exist_ok=True)
        for name in GF4_LOGS:
            (logs / name).write_text('fixture execution log')
        seed = dict(schema_version=2, suite=identity.suite, status='PASS', target='darwin-arm64',
                    source_sha=SOURCE, distribution_input=inputs, run=RUN,
                    authorization=release_context(inputs['version']),
                    runner=dict(os='macOS', arch='ARM64', system='Darwin', machine='arm64'),
                    seed=seed_record(identity, kind),
                    checks=[dict(name=name, status='PASS', required=True, engine=engine)
                            for engine, names in [('host', host), ('native', native)] for name in names],
                    hashes={**{f'cli-{stage}': compiler_sha for stage in 'ABCD'},
                            **{f'stage-a-{target}': compiler_sha for target in TARGETS}},
                    logs={name: digest(logs / name) for name in GF4_LOGS})
        for check in seed['checks']:
            if check['name'].startswith('cross-stage-a '):
                check.update(B=compiler_sha, C=compiler_sha)
        write_json(base / identity.report_name, seed)
        return seed

    def test_candidates_and_unstable_tags_rejected(self):
        for tag in ['2.1.0', 'v2.1.0-pr.abc', 'v2.1.0-rc.1', 'v2.1.0-dev', 'v2.0.0', 'v2.01.0', 'v2.1.00', '../v2.1.0']:
            with self.subTest(tag=tag), self.assertRaises(ValueError):
                stable(tag)
        self.assertEqual(stable('v2.1.0'), '2.1.0')
        for tag, version in [('v2.1.1', '2.1.1'), ('v2.1.2', '2.1.2'), ('v2.2.0', '2.2.0'), ('v3.0.0', '3.0.0')]:
            with self.subTest(tag=tag):
                self.assertEqual(stable(tag), version)

    def test_immutable_idempotent_and_rejects_replacement(self):
        with tempfile.TemporaryDirectory() as temp:
            p = Path(temp) / 'version/file'
            immutable(p, b'one')
            immutable(p, b'one')
            with self.assertRaises(ValueError):
                immutable(p, b'two')
            self.assertEqual(p.read_bytes(), b'one')

    def test_draft_prerelease_unpublished_and_wrong_release_rejected(self):
        for patch in [dict(draft=True), dict(prerelease=True), dict(published_at=None), dict(tag_name='v2.1.0-rc.1')]:
            r = dict(tag_name='v2.1.0', draft=False, prerelease=False, published_at='date')
            r.update(patch)
            with self.assertRaises(ValueError):
                validate(Path('/no/assets'), 'v2.1.0', r, SOURCE)

    def fixture(self, base, kind=GF4):
        import tarfile
        import zipfile
        version = version_for(kind)
        tag = 'v' + version
        inputs = inputs_for(kind)
        embedded = kind != GF4
        release = dict(tag_name=tag, draft=False, prerelease=False, published_at='date', assets=[])
        full_bytes = b'native Full fixture'
        # Legacy v2.1.0 distributions ship one compiler with an external Bunko
        # tree; only the embedded Core/Full architecture has distinct bytes.
        core_bytes = b'native Core fixture' if embedded else full_bytes
        compiler_sha = __import__('hashlib').sha256(full_bytes).hexdigest()
        core_sha = __import__('hashlib').sha256(core_bytes).hexdigest()
        seed = self.seed_fixture(base, compiler_sha, kind)
        seed_path = base / seed_identity(kind).report_name
        sums = []
        for target in ['darwin-arm64', 'linux-amd64', 'windows-amd64']:
            checks = [dict(name='cli-fixed-point', status='PASS', engine='native', required=True),
                      dict(name='core-cli-fixed-point', status='PASS', engine='native', required=True)]
            checks += [dict(name=name, status='PASS', engine='native', required=True) for name in
                       ('full-embedded-runtime', 'full-embedded-transitive', 'core-no-embedded',
                        'core-external-root', 'explicit-root-precedence', 'invalid-module-diagnostic')]
            report = dict(target=target, status='PASS', source_sha='a'*40,
                          seed=dict(status='NOT_RUN', sha256=GF4_SEED_SHA256), distribution_input=inputs, run=RUN,
                          hashes=dict({f'cli-{s}': compiler_sha for s in 'BCD'},
                                      **{f'core-cli-{s}': core_sha for s in 'BCD'}),
                          checks=checks)
            report_path = base / (target+'-native-report.json')
            write_json(report_path, report)
            archives = {}
            for flavor in ['core', 'full']:
                name = f'mio-{version}-{target}-{flavor}'
                root = base / name
                (root / 'bin').mkdir(parents=True)
                binary = root / 'bin' / ('mio.exe' if target == 'windows-amd64' else 'mio')
                binary.write_bytes(full_bytes if flavor == 'full' else core_bytes)
                (root / 'LICENSE').write_text('MIT fixture')
                (root / 'README.md').write_text('test fixture only')
                if not embedded and flavor == 'full':
                    (root / 'bunko/kiso').mkdir(parents=True)
                    (root / 'bunko/kiso/math.mio').write_text('// fixture Bunko')
                binding = seed_binding(seed, report, inputs, target, compiler_sha, digest(seed_path), kind)
                if embedded:
                    binding = dict(binding, core_compiler_sha256=core_sha)
                provenance = dict(inputs, target=target, flavor=flavor,
                                  embedded_bunko=embedded and flavor == 'full',
                                  compiler_sha256=compiler_sha if flavor == 'full' else core_sha,
                                  native_report_sha256=digest(report_path), trusted_seed=binding)
                if embedded:
                    provenance['core_compiler_sha256'] = core_sha
                write_json(root / 'provenance.json', provenance)
                write_json(root / 'manifest.json', dict(version=version, source_sha='a'*40, target=target, flavor=flavor, files=inventory(root)))
                archive = base / (name + ('.zip' if target == 'windows-amd64' else '.tar.gz'))
                files = [p for p in root.rglob('*') if p.is_file()]
                if target == 'windows-amd64':
                    with zipfile.ZipFile(archive, 'w') as z:
                        for p in files:
                            z.write(p, p.relative_to(base))
                else:
                    with tarfile.open(archive, 'w:gz') as t:
                        for p in files:
                            t.add(p, arcname=p.relative_to(base), recursive=False)
                sha = digest(archive)
                archives[archive.name] = sha
                sums.append(sha+'  '+archive.name+'\n')
                release['assets'].append(dict(name=archive.name, digest='sha256:'+sha,
                    browser_download_url=f'https://github.com/mio-kobo/mio/releases/download/{tag}/{archive.name}'))
            write_json(base / (target+'-verification.json'), dict(target=target, status='PASS', checks=[dict(status='PASS')], archives=archives))
        for flavor in ('core', 'full'):
            filename = f'mio-{version}-windows-amd64-{flavor}-setup.exe'
            (base / filename).write_bytes(b'unit fixture only; not executable')
            sha = digest(base / filename)
            sums.append(sha+'  '+filename+'\n')
            release['assets'].append(dict(name=filename, digest='sha256:'+sha, browser_download_url=f'https://github.com/mio-kobo/mio/releases/download/{tag}/{filename}'))
        image = docker_archive(version)
        (base / image).write_bytes(b'Docker test fixture')
        sums.append(f'{digest(base / image)}  {image}\n')
        (base / 'SHA256SUMS').write_text(''.join(sums))
        linux_full = next(a['digest'][7:] for a in release['assets'] if a['name'].endswith('linux-amd64-full.tar.gz'))
        write_json(base / 'docker-verification.json', dict(status='PASS', source_sha='a'*40, full_archive_sha256=linux_full, archive_sha256=digest(base / image), checks=[dict(status='PASS')]))
        return release

    @patch('release.authenticate', return_value=(RUN, GF4))
    def test_six_asset_gate_rejects_tampering_and_missing_evidence(self, _authenticate):
        with tempfile.TemporaryDirectory() as temp:
            base = Path(temp)
            release = self.fixture(base)
            version, records = validate(base, 'v2.1.0', release, SOURCE)
            self.assertEqual(version, '2.1.0')
            self.assertEqual(len(records), 8)
            bad = copy.deepcopy(release)
            bad['assets'][0]['digest'] = 'sha256:' + '0'*64
            with self.assertRaises(ValueError):
                validate(base, 'v2.1.0', bad, SOURCE)
            original_digest = digest
            report_path = base / 'darwin-arm64-native-report.json'
            original_report = report_path.read_bytes()
            for change in ['wrong-run', 'wrong-input', 'fixed-not-run', 'hash', 'source']:
                report = json.loads(original_report)
                if change == 'wrong-run': report['run']['attempt'] = '2'
                if change == 'wrong-input': report['distribution_input']['version'] = '2.1.1'
                if change == 'fixed-not-run': report['checks'][0]['status'] = 'NOT_RUN'
                if change == 'hash': report['hashes']['cli-C'] = '0'*64
                if change == 'source': report['source_sha'] = 'b'*40
                write_json(report_path, report)
                # Rebinding only the evidence file hash isolates deeper gates.
                def rebound(path):
                    return original_digest_bytes if path == report_path else original_digest(path)
                original_digest_bytes = __import__('hashlib').sha256(original_report).hexdigest()
                with patch('release.digest', side_effect=rebound), self.assertRaises(ValueError):
                    validate(base, 'v2.1.0', release, SOURCE)
            report_path.write_bytes(original_report)
            evidence = base / 'darwin-arm64-verification.json'
            original = evidence.read_bytes()
            data = json.loads(original)
            for status in ['FAIL', 'NOT_RUN', 'SKIP', 'NOT_APPLICABLE']:
                data['checks'][0]['status'] = status
                write_json(evidence, data)
                with self.assertRaises(ValueError):
                    validate(base, 'v2.1.0', release, SOURCE)
            evidence.write_bytes(original)
            archive = base / records['darwin-arm64', 'core']['filename']
            archive.write_bytes(archive.read_bytes()+b'tamper')
            with self.assertRaises(ValueError):
                validate(base, 'v2.1.0', release, SOURCE)

    def test_seed_binding_rejects_stale_or_unrelated_origin(self):
        with tempfile.TemporaryDirectory() as temp:
            base = Path(temp)
            self.fixture(base)
            seed = json.loads((base / SEED_REPORT).read_text())
            report = json.loads((base / 'linux-amd64-native-report.json').read_text())
            compiler = report['hashes']['cli-B']
            for field, value in [
                ('source_sha', 'b' * 40),
                ('target', 'linux-amd64'),
                ('distribution_input', dict(inputs_for(GF4), version='2.1.1')),
                ('seed', dict(status='NOT_RUN', sha256=GF4_SEED_SHA256)),
                ('seed', dict(status='PASS', sha256='b' * 64)),
                ('run', dict(RUN, attempt='2')),
                ('run', dict(RUN, id='999')),
                ('seed', dict(seed['seed'], source=dict(GF4_SOURCE, asset_id=99))),
                ('runner', dict(seed['runner'], machine='x86_64')),
                ('authorization', dict(seed['authorization'], event='pull_request')),
                ('checks', seed['checks'][:-1]),
                ('logs', {}),
                ('hashes', dict(seed['hashes'], **{'cli-A': 'invalid'})),
                ('hashes', dict(seed['hashes'], **{'stage-a-linux-amd64': 'b' * 64})),
                ('hashes', dict(seed['hashes'], **{'cli-D': 'b' * 64})),
            ]:
                with self.subTest(field=field, value=value), self.assertRaises(ValueError):
                    seed_binding(dict(seed, **{field: value}), report, inputs_for(GF4), 'linux-amd64', compiler, 'c' * 64)

    def test_assembly_rejects_pr_dispatch_and_stale_seed_before_creating_output(self):
        env = dict(GITHUB_REF='refs/tags/v2.1.0', GITHUB_REPOSITORY='mio-kobo/mio',
                   GITHUB_EVENT_NAME='pull_request', GITHUB_SHA=SOURCE,
                   GITHUB_RUN_ID='123', GITHUB_RUN_ATTEMPT='2')
        with tempfile.TemporaryDirectory() as temp:
            base = Path(temp)
            output = base / 'output'
            with patch.dict('os.environ', env), self.assertRaisesRegex(ValueError, 'official workflow'):
                assemble(base, output, VERSION)
            download = base / 'trusted-gf4-123-2'
            download.mkdir()
            self.seed_fixture(download, 'c' * 64)
            with (patch.dict('os.environ', dict(env, GITHUB_EVENT_NAME='workflow_dispatch')),
                  self.assertRaisesRegex(ValueError, 'exact tag and Actions attempt')):
                assemble(base, output, VERSION)
            self.assertFalse(output.exists())

    @patch('stable_release.subprocess.run')
    def test_assembly_requires_native_artifact_and_attests_its_inventory(self, _gh):
        version = version_for(NATIVE_V21)
        env = dict(GITHUB_REF='refs/tags/v' + version, GITHUB_REPOSITORY='mio-kobo/mio',
                   GITHUB_EVENT_NAME='workflow_dispatch', GITHUB_SHA=SOURCE,
                   GITHUB_RUN_ID=RUN['id'], GITHUB_RUN_ATTEMPT=RUN['attempt'])
        suffix = f"{RUN['id']}-{RUN['attempt']}"
        with tempfile.TemporaryDirectory() as temp, patch.dict('os.environ', env):
            base = Path(temp)
            output = base / 'output'
            legacy = base / f'trusted-gf4-{suffix}'
            legacy.mkdir()
            self.seed_fixture(legacy, 'c' * 64)
            with self.assertRaises(FileNotFoundError):
                assemble(base, output, version, NATIVE_V21)
            self.assertFalse(output.exists())

            identity = seed_identity(NATIVE_V21)
            seed = base / f'{identity.artifact}-{suffix}'
            seed.mkdir()
            self.seed_fixture(seed, 'c' * 64, NATIVE_V21)
            (seed / identity.log_dir).rename(seed / 'logs')
            for target in TARGETS:
                artifact = base / f'mio-candidates-{target}-{suffix}'
                artifact.mkdir()
                for name in archives(version, target) + ['native-report.json', 'verification.json',
                                                          'package-manager-verification.json']:
                    (artifact / name).write_bytes(b'fixture')
                if target == 'windows-amd64':
                    for name in installers(version):
                        (artifact / name).write_bytes(b'fixture')
            docker = base / f'mio-docker-linux-amd64-candidate-{suffix}'
            docker.mkdir()
            image = docker / 'mio-linux-amd64-full-candidate.docker.tar'
            image.write_bytes(b'fixture')
            write_json(docker / 'docker-verification.json', dict(archive_sha256=digest(image), source_sha=SOURCE))
            for artifact, name in [('chocolatey-tests', 'chocolatey-verification.json'),
                                   *((f'apt-container-{i}', 'package-manager-verification.json') for i in range(3))]:
                path = base / f'{artifact}-{suffix}'
                path.mkdir()
                (path / name).write_bytes(b'fixture')

            assemble(base, output, version, NATIVE_V21)
            self.assertEqual(authenticate(output, 'v' + version, SOURCE), (RUN, NATIVE_V21))
            self.assertFalse((output / SEED_REPORT).exists())

    def test_assembly_checks_executed_logs_against_seed_report(self):
        with tempfile.TemporaryDirectory() as temp:
            base = Path(temp)
            self.seed_fixture(base, 'c' * 64)
            logs = base / SEED_LOG_DIR
            check_seed_artifact(base / SEED_REPORT, logs, SOURCE, VERSION, RUN)
            (logs / GF4_LOGS[0]).write_text('tampered execution log')
            with self.assertRaisesRegex(ValueError, 'log does not match'):
                check_seed_artifact(base / SEED_REPORT, logs, SOURCE, VERSION, RUN)

    def test_both_seed_bindings_reject_mixed_pins(self):
        for kind, other_kind in ((GF4, NATIVE_V21), (NATIVE_V21, GF4)):
            identity, other = seed_identity(kind), seed_identity(other_kind)
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as temp:
                compiler = 'c' * 64
                seed = self.seed_fixture(Path(temp), compiler, kind)
                inputs = inputs_for(kind)
                report = dict(run=RUN, distribution_input=inputs)
                binding = seed_binding(seed, report, inputs, 'linux-amd64', compiler, 'd' * 64, kind)
                self.assertEqual(binding['sha256'], identity.sha256)
                if kind == NATIVE_V21:
                    self.assertEqual(binding['kind'], NATIVE_V21)
                    self.assertEqual(binding['source'], identity.source)
                    self.assertEqual(binding['asset_name'], identity.asset_name)
                with self.assertRaises(ValueError):
                    seed_binding(seed, report, inputs, 'linux-amd64', compiler, 'd' * 64, other_kind)
                mixed = [dict(seed['seed'], source=dict(identity.source, **{field: value}))
                         for field, value in other.source.items()]
                mixed.append(dict(seed['seed'], sha256=other.sha256))
                if kind == NATIVE_V21:
                    mixed.append(dict(seed['seed'], asset_name=other.asset_name))
                for record in mixed:
                    with self.subTest(record=record), self.assertRaises(ValueError):
                        seed_binding(dict(seed, seed=record), report, inputs, 'linux-amd64', compiler, 'd' * 64, kind)

    @patch('stable_release.subprocess.run')
    def test_authentication_checks_signer_source_and_all_evidence_hashes(self, gh):
        for kind in SEED_KINDS:
            gh.reset_mock(side_effect=True)
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as temp:
                base = Path(temp)
                version = version_for(kind)
                tag = 'v' + version
                release = self.fixture(base, kind)
                for name in evidence_files(version, kind):
                    path = base / name
                    if not path.exists():
                        path.parent.mkdir(exist_ok=True)
                        path.write_bytes(b'test evidence')
                evidence = dict(schema_version=EVIDENCE_SCHEMAS[kind], status='PASS', source_sha=SOURCE,
                                version=version, run=RUN,
                                files={name: digest(base / name) for name in evidence_files(version, kind)})
                write_json(base / REPORT, evidence)
                self.assertEqual(authenticate(base, tag, SOURCE), (RUN, kind))
                self.assertEqual(len(validate(base, tag, release, SOURCE)[1]), 8)
                args = gh.call_args.args[0]
                self.assertIn(str(base / BUNDLE), args)
                self.assertEqual(args[args.index('--source-ref') + 1], 'refs/tags/' + tag)
                self.assertEqual(args[args.index('--signer-digest') + 1], SOURCE)
                self.assertEqual(args[args.index('--source-digest') + 1], SOURCE)
                self.assertIn('--deny-self-hosted-runners', args)
                wrong_schema = EVIDENCE_SCHEMAS[NATIVE_V21 if kind == GF4 else GF4]
                write_json(base / REPORT, dict(evidence, schema_version=wrong_schema))
                with self.assertRaisesRegex(ValueError, 'describes different inputs'):
                    authenticate(base, tag, SOURCE)
                write_json(base / REPORT, evidence)
                (base / seed_identity(kind).report_name).write_bytes(b'fabricated evidence')
                with self.assertRaisesRegex(ValueError, 'does not match signed'):
                    authenticate(base, tag, SOURCE)
                gh.side_effect = subprocess.CalledProcessError(1, 'gh')
                with self.assertRaisesRegex(ValueError, 'missing or invalid'):
                    authenticate(base, tag, SOURCE)

    def test_layout_conflicts_and_distinct_windows_aliases(self):
        r = dict(url='https://github.com/mio-kobo/mio/releases/download/v2.1.0/a.zip', sha='a'*64, root='mio-2.1.0-windows-amd64-full')
        aliases = []
        for flavor in ['core', 'full']:
            f = formula('2.1.0', flavor, r)
            self.assertIn('libexec.install', f)
            self.assertIn('bin.install_symlink libexec/"bin/mio"', f)
            self.assertIn('conflicts_with', f)
            ident, files = winget('2.1.0', flavor, r)
            manifest = files['installer.yaml']
            self.assertEqual(manifest['InstallerType'], 'inno')
            from windows import installer_script
            product_code = manifest['ProductCode']
            self.assertEqual(product_code, windows_product_code(flavor))
            self.assertIn('AppId={' + product_code.removesuffix('_is1'), installer_script('payload', 'output', '2.1.0', flavor, 'setup'))
            self.assertNotIn('NestedInstallerFiles', manifest)
            installer = manifest['Installers'][0]
            self.assertEqual(installer['InstallerSha256'], 'A'*64)
            aliases.append(manifest['Commands'][0])
        self.assertEqual(aliases, ['mio-core', 'mio'])

if __name__ == '__main__':
    unittest.main()
