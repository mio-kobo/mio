from base64 import b64encode
from contextlib import redirect_stderr, redirect_stdout
from email.message import Message
from hashlib import sha256, sha512
import io
import json
from pathlib import Path
import tempfile
import subprocess
import sys
import traceback
import unittest
from unittest.mock import Mock, patch
import xml.etree.ElementTree as ET
from urllib.error import HTTPError
import zipfile

from chocolatey import build, package_files
from chocolatey_publish import existing, moderation_record, publish


class ChocolateyTests(unittest.TestCase):
    def record(self, flavor='full'):
        root = f'mio-2.1.0-windows-amd64-{flavor}'
        return dict(root=root, url=f'https://github.com/mio-kobo/mio/releases/download/v2.1.0/{root}.zip', sha='a' * 64)

    def publish_packages(self, folder):
        return {
            name: sha512(build('2.1.0', flavor, self.record(flavor), folder).read_bytes()).digest()
            for name, flavor in (('mio-core', 'core'), ('mio', 'full'))
        }

    def test_packages_and_inventory(self):
        with tempfile.TemporaryDirectory() as temp:
            for flavor in ('core', 'full'):
                package = build('2.1.0', flavor, self.record(flavor), Path(temp))
                original = package.read_bytes()
                rebuild = build('2.1.0', flavor, self.record(flavor), Path(temp) / 'rebuild')
                self.assertEqual(sha256(rebuild.read_bytes()).hexdigest(), sha256(original).hexdigest())
                with zipfile.ZipFile(package) as z:
                    name = 'mio' if flavor == 'full' else 'mio-core'
                    self.assertIsNone(z.testzip())
                    core_path = 'package/services/metadata/core-properties/mio.psmdcp'
                    self.assertCountEqual(z.namelist(), [
                        '[Content_Types].xml', '_rels/.rels', core_path, name + '.nuspec',
                        'tools/release.json', 'tools/chocolateyInstall.ps1', 'tools/chocolateyUninstall.ps1',
                    ])
                    opc = 'http://schemas.openxmlformats.org/package/2006/'
                    types = ET.fromstring(z.read('[Content_Types].xml'))
                    self.assertEqual(types.tag, '{' + opc + 'content-types}Types')
                    self.assertEqual({item.attrib['Extension']: item.attrib['ContentType'] for item in types}, {
                        'rels': 'application/vnd.openxmlformats-package.relationships+xml',
                        'psmdcp': 'application/vnd.openxmlformats-package.core-properties+xml',
                        'nuspec': 'application/octet', 'json': 'application/octet', 'ps1': 'application/octet',
                    })
                    rels = ET.fromstring(z.read('_rels/.rels'))
                    self.assertEqual(rels.tag, '{' + opc + 'relationships}Relationships')
                    self.assertEqual(len({rel.attrib['Id'] for rel in rels}), 2)
                    self.assertCountEqual([(rel.attrib['Type'], rel.attrib['Target']) for rel in rels], [
                        ('http://schemas.microsoft.com/packaging/2010/07/manifest', '/' + name + '.nuspec'),
                        (opc + 'relationships/metadata/core-properties', '/' + core_path),
                    ])
                    core = ET.fromstring(z.read(core_path))
                    self.assertEqual(core.find('{http://purl.org/dc/elements/1.1/}identifier').text, name)
                    self.assertEqual(core.find('{' + opc + 'metadata/core-properties}version').text, '2.1.0')
                    ns = {'n': 'http://schemas.microsoft.com/packaging/2011/08/nuspec.xsd'}
                    meta = ET.fromstring(z.read(name + '.nuspec')).find('n:metadata', ns)
                    self.assertEqual(meta.find('n:id', ns).text, name)
                    self.assertEqual(meta.find('n:version', ns).text, '2.1.0')
                    self.assertIsNone(meta.find('n:dependencies', ns))
                    for field, url in {
                        'iconUrl': 'https://cdn.jsdelivr.net/gh/mio-kobo/mio-docs@07dcf2520b848ee84285039f058c74983728b6e3/app/icon.png',
                        'packageSourceUrl': 'https://github.com/mio-kobo/mio/tree/Mio-2.1/scripts/package_managers',
                        'releaseNotes': 'https://github.com/mio-kobo/mio/releases/tag/v2.1.0',
                        'docsUrl': 'https://mio.kaelexvp.cc/docs/',
                        'bugTrackerUrl': 'https://github.com/mio-kobo/mio/issues',
                        'projectSourceUrl': 'https://github.com/mio-kobo/mio',
                    }.items():
                        self.assertEqual(meta.find('n:' + field, ns).text, url)
                    record = json.loads(z.read('tools/release.json'))
                    self.assertEqual(record, dict(self.record(flavor), command=name, version='2.1.0', flavor=flavor))
                    self.assertEqual(record['sha'], 'a' * 64)
                    self.assertEqual(record['version'], '2.1.0')
                    install = z.read('tools/chocolateyInstall.ps1').decode()
                    self.assertEqual(install.count('Get-ChocolateyWebFile'), 1)
                    self.assertIn(f"-Url '{record['url']}' -Checksum '{record['sha']}' -ChecksumType 'sha256'", install)
                    self.assertIn(f"(Get-MioSha256 $archive) -ne '{record['sha']}'", install)
                    self.assertNotIn('@MIO_ZIP_', install)
                    self.assertIn('[System.Security.Cryptography.SHA256]::Create()', install)
                    self.assertIn('Install-BinFile -Name $record.command -Path $exe', install)
                    self.assertIn("'.ignore'", install)
                    self.assertNotIn('LOCALAPPDATA', install)
                    self.assertIn('Uninstall-BinFile', z.read('tools/chocolateyUninstall.ps1').decode())
                with self.assertRaises(ValueError):
                    build('2.1.0', flavor, dict(self.record(flavor), sha='b' * 64), Path(temp))

    def test_reject_unstable_or_untrusted_input(self):
        for version in ('2.1.0-pr.abc123456789', '0.0.1', '2.1.0-rc.1'):
            with self.assertRaises(ValueError):
                package_files(version, 'full', self.record())
        for delta in (dict(url='https://example.com/mio.zip'), dict(sha='placeholder'), dict(root='../mio'), dict(url=self.record()['url'].replace('v2.1.0', 'latest'))):
            with self.assertRaises(ValueError):
                package_files('2.1.0', 'full', dict(self.record(), **delta))
        with self.assertRaises(ValueError):
            package_files('0.0.1', 'full', self.record(), fixture=True)

    def test_stable_entry_uses_existing_release_gate(self):
        import chocolatey
        with patch('sys.argv', ['chocolatey.py', '--tag', 'v2.1.0', '--assets', '/missing', '--release', '/release.json', '--source-sha', 'a' * 40, '--output', '/out']), patch.object(Path, 'read_text', return_value='{}'), patch('chocolatey.validate', side_effect=ValueError('seed NOT_RUN')), patch('chocolatey.build') as generate:
            with self.assertRaisesRegex(ValueError, 'NOT_RUN'):
                chocolatey.main()
            generate.assert_not_called()

    def test_publish_dry_run_duplicate_collision_and_no_secret_output(self):
        with tempfile.TemporaryDirectory() as temp:
            folder = Path(temp)
            digests = self.publish_packages(folder)
            with patch('chocolatey_publish.subprocess.run') as run:
                with patch('chocolatey_publish.urlopen') as urlopen:
                    publish(folder, '2.1.0')
                    urlopen.assert_not_called()
                run.assert_not_called()
                with patch('chocolatey_publish.moderation_record', side_effect=lambda n, v: (digests[n], 'Approved')):
                    publish(folder, '2.1.0', push=True)
                run.assert_not_called()
                with patch('chocolatey_publish.moderation_record', return_value=(sha512(b'old').digest(), 'Waiting')):
                    with self.assertRaisesRegex(ValueError, 'repair authorization required'):
                        publish(folder, '2.1.0', push=True)
                run.assert_not_called()
            with patch.dict('os.environ', CHOCOLATEY_API_KEY='secret-test-value'), \
                 patch('chocolatey_publish.moderation_record', return_value=None), \
                 patch('chocolatey_publish.existing', return_value=False), \
                 patch('chocolatey_publish.subprocess.run') as run:
                run.side_effect = subprocess.TimeoutExpired(['choco', '--api-key=secret-test-value'], 300)
                with self.assertRaises(RuntimeError) as error:
                    publish(folder, '2.1.0', push=True)
                self.assertNotIn('secret-test-value', str(error.exception))
                self.assertTrue(error.exception.__suppress_context__)

    def test_preflight_fails_closed_on_feed_error(self):
        for code in (403, 503):
            unavailable = HTTPError('private-url', code, 'private-body', None, None)
            with patch('chocolatey_publish.urlopen', side_effect=unavailable):
                with self.assertRaisesRegex(RuntimeError, f'HTTP status={code}') as error:
                    existing('mio-core', '2.1.0')
                self.assertTrue(error.exception.__suppress_context__)
                self.assertNotIn('private', str(error.exception))

    def test_moderation_record_reads_exact_version_and_review_state(self):
        digest = sha512(b'old').digest()
        template = '''<entry xmlns="http://www.w3.org/2005/Atom"
          xmlns:m="http://schemas.microsoft.com/ado/2007/08/dataservices/metadata"
          xmlns:d="http://schemas.microsoft.com/ado/2007/08/dataservices">
          <title>mio-core</title>
          <m:properties>
            <d:Version>2.1.0</d:Version>
            <d:PackageHashAlgorithm>SHA512</d:PackageHashAlgorithm>
            <d:PackageHash>{digest}</d:PackageHash>
            <d:IsApproved>{approved}</d:IsApproved>
            <d:PackageStatus>{status}</d:PackageStatus>
            <d:PackageSubmittedStatus>Waiting</d:PackageSubmittedStatus>
          </m:properties>
        </entry>'''
        with patch('chocolatey_publish.urlopen') as urlopen:
            response = urlopen.return_value.__enter__.return_value
            for approved, status, expected in (
                ('false', 'Submitted', 'Waiting'),
                ('true', 'Submitted', 'Approved'),
                ('false', 'Approved', 'Approved'),
                ('false', 'Rejected', None),
                ('', 'Submitted', None),
            ):
                with self.subTest(approved=approved, status=status):
                    response.read.return_value = template.format(
                        digest=b64encode(digest).decode(), approved=approved, status=status,
                    ).encode()
                    self.assertEqual(moderation_record('mio-core', '2.1.0'), (digest, expected))
            request = urlopen.call_args.args[0]
            self.assertTrue(request.full_url.endswith("Packages(Id='mio-core',Version='2.1.0')"))
            self.assertEqual(request.get_header('Cache-control'), 'no-cache')
            for body in (b'<private-malformed', response.read.return_value.replace(b'2.1.0', b'2.1.1')):
                response.read.return_value = body
                with self.assertRaisesRegex(RuntimeError, 'CCR metadata preflight failed') as error:
                    moderation_record('mio-core', '2.1.0')
                self.assertTrue(error.exception.__suppress_context__)
            urlopen.reset_mock()
            urlopen.side_effect = HTTPError('private-url', 403, 'private-body', None, None)
            with self.assertRaisesRegex(RuntimeError, 'HTTP status=403') as error:
                moderation_record('mio-core', '2.1.0')
            self.assertNotIn('private', str(error.exception))
            urlopen.assert_called_once()
            urlopen.reset_mock()
            urlopen.side_effect = HTTPError('private-url', 404, 'private-body', None, None)
            self.assertIsNone(moderation_record('mio-core', '2.1.0'))
            urlopen.assert_called_once()

            headers = Message()
            headers['Retry-After'] = '7'
            response.read.return_value = template.format(
                digest=b64encode(digest).decode(), approved='false', status='Submitted',
            ).encode()
            urlopen.reset_mock()
            urlopen.side_effect = [HTTPError('private-url', 503, 'private-body', headers, None),
                                   urlopen.return_value]
            with patch('chocolatey_publish.sleep') as sleep, redirect_stdout(io.StringIO()) as log:
                self.assertEqual(moderation_record('mio-core', '2.1.0'), (digest, 'Waiting'))
            sleep.assert_called_once_with(7)
            self.assertEqual(urlopen.call_count, 2)
            self.assertNotIn('private', log.getvalue())

    def test_metadata_outage_never_uploads(self):
        for retry_after, delays, message in (
            ('', [2, 5, 15], 'after 4 attempts'),
            ('120', [], 'longer wait than the retry limit'),
        ):
            with self.subTest(retry_after=retry_after), tempfile.TemporaryDirectory() as temp, \
                 patch('chocolatey_publish.urlopen') as urlopen, \
                 patch('chocolatey_publish.sleep') as sleep, \
                 patch('chocolatey_publish.existing') as download, \
                 patch('chocolatey_publish.subprocess.run') as run, \
                 redirect_stdout(io.StringIO()) as log:
                folder = Path(temp)
                self.publish_packages(folder)
                headers = Message()
                if retry_after:
                    headers['Retry-After'] = retry_after
                urlopen.side_effect = [
                    HTTPError('private-url', 503, 'private-body', headers, None)
                    for _ in range(len(delays) + 1)
                ]
                with self.assertRaisesRegex(RuntimeError, f'HTTP status=503.*{message}') as error:
                    publish(folder, '2.1.0', push=True, resubmit=True)
                self.assertEqual(urlopen.call_count, len(delays) + 1)
                self.assertEqual([call.args[0] for call in sleep.call_args_list], delays)
                traceback.print_exception(error.exception, file=log)
                self.assertNotIn('private', log.getvalue())
                download.assert_not_called()
                run.assert_not_called()

    def test_first_upload_and_scoped_moderation_repair(self):
        with tempfile.TemporaryDirectory() as temp, \
             patch.dict('os.environ', CHOCOLATEY_API_KEY='secret-test-value'), \
             patch('chocolatey_publish.moderation_record') as lookup, \
             patch('chocolatey_publish.existing', return_value=False) as download, \
             patch('chocolatey_publish.subprocess.run', return_value=Mock(returncode=0)) as run:
            folder = Path(temp)
            self.publish_packages(folder)
            for status in (None, 'Waiting', 'Pending', 'Responded', 'Updated'):
                with self.subTest(status=status):
                    run.reset_mock()
                    download.reset_mock()
                    lookup.return_value = (sha512(b'old').digest(), status) if status else None
                    publish(folder, '2.1.0', push=True, resubmit=status is not None)
                    self.assertEqual([Path(call.args[0][2]).name for call in run.call_args_list],
                                     ['mio-core.2.1.0.nupkg', 'mio.2.1.0.nupkg'])
                    self.assertEqual(download.call_count, 0 if status else 2)

    def test_blocked_second_package_prevents_any_upload(self):
        old = sha512(b'old').digest()
        with tempfile.TemporaryDirectory() as temp, \
             patch('chocolatey_publish.moderation_record') as lookup, \
             patch('chocolatey_publish.existing', return_value=True), \
             patch('chocolatey_publish.subprocess.run') as run:
            folder = Path(temp)
            self.publish_packages(folder)
            for record, message in (
                ((old, 'Approved'), 'approved and cannot be overwritten'),
                ((old, 'Rejected'), 'does not permit a repair'),
                ((old, None), 'does not permit a repair'),
                (None, 'could not be confirmed'),
            ):
                with self.subTest(record=record):
                    lookup.side_effect = [(old, 'Waiting'), record]
                    with self.assertRaisesRegex(ValueError, message):
                        publish(folder, '2.1.0', push=True, resubmit=True)
                    run.assert_not_called()
            lookup.side_effect = [None, None]
            with self.assertRaisesRegex(ValueError, 'could not be confirmed'):
                publish(folder, '2.1.0', push=True)
            with self.assertRaisesRegex(ValueError, 'requires a push for v2.1.0'):
                publish(folder, '2.1.1', push=True, resubmit=True)
            run.assert_not_called()

    def test_repair_retry_skips_successful_upload_after_partial_failure(self):
        old = sha512(b'old').digest()
        with tempfile.TemporaryDirectory() as temp, \
             patch.dict('os.environ', CHOCOLATEY_API_KEY='secret-test-value'), \
             patch('chocolatey_publish.moderation_record') as lookup, \
             patch('chocolatey_publish.subprocess.run') as run:
            folder = Path(temp)
            digests = self.publish_packages(folder)
            lookup.side_effect = [(old, 'Waiting'), (old, 'Waiting'),
                                  (digests['mio-core'], 'Updated'), (old, 'Waiting')]
            ok = subprocess.CompletedProcess([], 0)
            failed = subprocess.CompletedProcess([], 1, stderr=b'HTTP 503')
            run.side_effect = [ok, failed, ok]
            with self.assertRaisesRegex(RuntimeError, 'failed for mio.2.1.0.nupkg.*503'):
                publish(folder, '2.1.0', push=True, resubmit=True)
            self.assertEqual(run.call_count, 2)
            publish(folder, '2.1.0', push=True, resubmit=True)
            self.assertEqual([Path(call.args[0][2]).name for call in run.call_args_list],
                             ['mio-core.2.1.0.nupkg', 'mio.2.1.0.nupkg', 'mio.2.1.0.nupkg'])

    def test_repair_rejects_mismatched_nuspec_identity(self):
        with tempfile.TemporaryDirectory() as temp, \
             patch('chocolatey_publish.moderation_record', return_value=(sha512(b'old').digest(), 'Waiting')), \
             patch('chocolatey_publish.subprocess.run') as run:
            folder = Path(temp)
            self.publish_packages(folder)
            for name, version in (('other-package', '2.1.0'), ('mio', '2.1.1')):
                with self.subTest(name=name, version=version):
                    with zipfile.ZipFile(folder / 'mio.2.1.0.nupkg', 'w') as archive:
                        archive.writestr('mio.nuspec', f'<package><metadata><id>{name}</id>'
                                         f'<version>{version}</version></metadata></package>')
                    with self.assertRaisesRegex(ValueError, 'invalid Chocolatey package identity'):
                        publish(folder, '2.1.0', push=True, resubmit=True)
                    run.assert_not_called()

    def test_push_requires_moderation_confirmation(self):
        script = Path(__file__).with_name('chocolatey_publish.py')
        result = subprocess.run([sys.executable, str(script), '--packages', 'missing',
                                 '--version', '2.1.0', '--push'], capture_output=True, text=True, timeout=10)
        self.assertEqual(result.returncode, 2)
        self.assertIn('--push requires --moderation-checked', result.stderr)

    def test_push_failure_reports_only_safe_diagnostics(self):
        key = 'secret-test-value'
        cases = (
            (b'Response status code does not indicate success: 400 (raw body)', b'',
             '400 (bad request (check package metadata and submission requirements))'),
            (b'', b'HTTP 401', '401 (authentication failed (check the configured API key))'),
            (b'HTTP/1.1 403', b'HTTP 403', '403 (forbidden (check account and package permissions))'),
            (b'', b'The remote server returned an error: (409) Conflict.',
             '409 (conflict (check whether the package version already exists))'),
            (b'HTTP status code: 413', b'', '413 (package exceeds the upload size limit)'),
            (b'', b'HTTP error 429', '429 (rate limit exceeded)'),
            (b'', b'Status code: 500', '500 (internal server error (check package format and CCR moderation state))'),
            (b'', b'HTTP 503', '503 (service unavailable)'),
            (b'HTTP 400', b'HTTP 500', 'unknown'),
            (b'This package version already exists on the repository and cannot be modified.', b'',
             'unknown; Chocolatey reported that this package version already exists'),
            (b'Chocolatey v2.4.0, port 403', b'HTTP 5000\xff', 'unknown'),
        )
        with tempfile.TemporaryDirectory() as temp:
            folder = Path(temp)
            self.publish_packages(folder)

            for stdout, stderr, status in cases:
                with self.subTest(status=status):
                    command = f'choco push private-package --source=private-source --api-key={key}'
                    result = subprocess.CompletedProcess(
                        command.split(), 17,
                        stdout + b'\r\nraw stdout ' + command.encode(),
                        stderr + b'\r\nraw stderr ' + key.encode(),
                    )
                    log = io.StringIO()
                    with patch.dict('os.environ', CHOCOLATEY_API_KEY=key), \
                         patch('chocolatey_publish.moderation_record', return_value=None), \
                         patch('chocolatey_publish.existing', return_value=False), \
                         patch('chocolatey_publish.subprocess.run', return_value=result) as run, \
                         redirect_stdout(log), redirect_stderr(log), self.assertRaises(RuntimeError) as error:
                        publish(folder, '2.1.0', push=True)

                    run.assert_called_once()
                    self.assertEqual(log.getvalue(), '')
                    self.assertEqual(str(error.exception),
                                     f'CCR submission failed for mio-core.2.1.0.nupkg: returncode=17, HTTP status={status}; check the signed-in moderation queue before any retry')
                    traceback.print_exception(error.exception, file=log)
                    for private in (key, 'private-source', '--api-key=', 'raw stdout', 'raw stderr', 'raw body'):
                        self.assertNotIn(private, log.getvalue())

    def test_fixture_matching_cannot_reuse_changed_native_inputs(self):
        from fixture import compatible, wrapper, select
        self.assertTrue(wrapper('scripts/package_managers/chocolatey/tools/chocolateyInstall.ps1'))
        for path in ('selfhost/cli/main.mio', 'scripts/distribution/package.py', 'scripts/ci/native_platforms.py', 'VERSION', '.github/workflows/native.yml', 'scripts/package_managers/windows.py', 'scripts/package_managers/release.py', 'scripts/package_managers/test_install.py'):
            self.assertFalse(wrapper(path), path)
            with patch('fixture.subprocess.check_output', return_value=path + '\n'):
                self.assertFalse(compatible('fixture-sha'))
        with patch('fixture.subprocess.check_output', return_value='scripts/package_managers/chocolatey.py\n'):
            self.assertTrue(compatible('fixture-sha'))
        valid = dict(id=1, run_attempt=2, head_sha='a' * 40, conclusion='success', path='.github/workflows/distribution-pr.yml', repository=dict(full_name='mio-kobo/mio'))
        for delta in (dict(conclusion='failure'), dict(path='.github/workflows/ci-pr.yml'), dict(repository=dict(full_name='other/fork'))):
            with patch('fixture.api', return_value=dict(valid, **delta)), self.assertRaises(RuntimeError):
                select('1')
        for expired in (True, False):
            artifacts = dict(artifacts=[dict(name='mio-candidates-windows-amd64-1-2', expired=expired)])
            with patch('fixture.api', side_effect=[valid, artifacts]), patch('fixture.subprocess.run'), patch('fixture.compatible', return_value=True):
                if expired:
                    with self.assertRaises(RuntimeError):
                        select('1')
                else:
                    self.assertEqual(select('1')['run_id'], 1)

    def test_actual_yaml_gate_compiles_and_requires_executed_checks(self):
        import yaml
        workflow = Path(__file__).resolve().parents[2] / '.github/workflows/distribution-pr.yml'
        step = yaml.safe_load(workflow.read_text())['jobs']['required']['steps'][0]['run']
        script = step.split("python - <<'PY'\n", 1)[1].rsplit('\nPY', 1)[0]
        code = compile(script, 'distribution-required', 'exec')
        results = {name: dict(result='skipped') for name in ('summary', 'candidates', 'docker-full-offline', 'candidate-linux', 'apt-distributions')}
        results.update({name: dict(result='success') for name in ('scope', 'packaging-tests', 'wrapper-regression')})
        def gate(data):
            with patch.dict('os.environ', RESULTS=json.dumps(data)):
                exec(code, {})
        gate(results)
        for status in ('failure', 'skipped', 'cancelled', 'NOT_RUN'):
            with self.assertRaises(AssertionError):
                gate(dict(results, **{'wrapper-regression': dict(result=status)}))
        native = {name: dict(result='success') for name in results}
        native['wrapper-regression'] = dict(result='skipped')
        gate(native)
        for status in ('failure', 'skipped', 'NOT_RUN'):
            with self.assertRaises(AssertionError):
                gate(dict(native, summary=dict(result=status)))


if __name__ == '__main__':
    unittest.main()
