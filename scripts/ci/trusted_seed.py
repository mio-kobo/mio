#!/usr/bin/env python3
"""Build stable distribution inputs from the frozen Darwin ARM64 GF4 seed."""
import argparse
import http.client
import os
from pathlib import Path
import re
import shutil
import sys
from urllib.error import URLError

from native_platforms import (ROOT, Report, brief, cli_build,
                              cli_chain, run, sha256, verify_host)
from gf4_source import SeedUnavailable, download_seed
from seed_evidence import (CROSS_TARGETS, GF4_LOGS, GF4_SEED_SHA256, GF4_SOURCE,
                           REPOSITORY, WORKFLOW, release_context, stable_version, valid_run)


# The frozen seed's historical self-builds took roughly 19–23 minutes on
# Darwin ARM64. Use the existing CI seed-build budget for this older binary;
# cli_chain and cross builds retain the current compiler's shorter timeout.
GF4_BUILD_TIMEOUT = 7200


def verify(output, local_seed=None):
    report = Report('darwin-arm64', output)
    report.data.update(schema_version=2, suite='trusted-gf4-distribution',
                       seed=dict(status='NOT_RUN', sha256=GF4_SEED_SHA256))
    inputs = report.data.get('distribution_input', {})
    version = inputs.get('version', '')
    source = report.data['source_sha']
    inputs_match = (stable_version(version) and inputs.get('embedded_bunko') is False
                    and re.fullmatch(r'[0-9a-f]{40}', source) and inputs.get('source_sha') == source)
    if not report.check('distribution-input', 'PASS' if inputs_match else 'FAIL', 'host',
                        '' if inputs_match else 'stable disposable checkout with the exact source commit required'):
        return 1

    actions = os.environ.get('GITHUB_ACTIONS') == 'true'
    if actions or local_seed is None:
        ref = os.environ.get('GITHUB_REF')
        workflow_ref = f'{REPOSITORY}/{WORKFLOW}@{ref}'
        context = dict(repository=os.environ.get('GITHUB_REPOSITORY'),
                       workflow=WORKFLOW if os.environ.get('GITHUB_WORKFLOW_REF') == workflow_ref else None,
                       event=os.environ.get('GITHUB_EVENT_NAME'), ref=ref,
                       runner_environment=os.environ.get('RUNNER_ENVIRONMENT'))
        authorized = (actions and local_seed is None and context == release_context(version)
                      and os.environ.get('GITHUB_SHA') == source and valid_run(report.data['run']))
        if not report.check('release-context', 'PASS' if authorized else 'FAIL', 'host',
                            '' if authorized else 'hosted seed retrieval requires dispatch on the exact stable tag'):
            return 1
        report.data['authorization'] = context

    if not verify_host(report):
        return 1
    if local_seed is None and (os.environ.get('RUNNER_OS'), os.environ.get('RUNNER_ARCH')) != ('macOS', 'ARM64'):
        report.check('hosted-runner', 'FAIL', 'host', 'expected GitHub macOS/ARM64 runner metadata')
        return 1

    work = report.output / 'work'
    work.mkdir(mode=0o700)
    seed = work / 'gf4'
    try:
        if local_seed is None:
            if GF4_SOURCE is None:
                raise SeedUnavailable('no approved immutable Mio release asset for the frozen GF4 executable')
            download_seed(GF4_SOURCE, seed)
            report.data['seed']['source'] = GF4_SOURCE
            report.check('seed-source', 'PASS', 'host')
        else:
            if not local_seed.is_file():
                raise SeedUnavailable('local replay requires the original frozen GF4 executable')
            shutil.copyfile(local_seed, seed)
            report.data['seed']['source'] = 'local-replay'
    except (SeedUnavailable, URLError, TimeoutError) as error:
        reason = str(error) if isinstance(error, SeedUnavailable) else 'GF4 source could not be reached'
        report.check('seed-source', 'NOT_RUN', 'host', reason)
        return 2
    except (OSError, ValueError, http.client.HTTPException) as error:
        report.data['seed']['status'] = 'FAIL'
        reason = str(error) if type(error) is ValueError else 'GF4 retrieval or copy failed'
        report.check('seed-source', 'FAIL', 'host', reason)
        return 1

    report.data['seed']['status'] = 'FAIL'
    if not report.check('seed-pin', 'PASS' if sha256(seed) == GF4_SEED_SHA256 else 'FAIL', 'host'):
        return 1
    if local_seed is None:
        code, out, err, _ = run(report, 'seed-release-attestation',
                               ['gh', 'release', 'verify-asset', GF4_SOURCE['tag'], seed, '--repo', REPOSITORY],
                               timeout=120)
        if not report.check('seed-release-attestation', 'PASS' if code == 0 else 'FAIL', 'host', brief(err or out)):
            return 1
    code, out, err, _ = run(report, 'seed-signature', ['/usr/bin/codesign', '--verify', '--strict', seed])
    if not report.check('seed-signature', 'PASS' if code == 0 else 'FAIL', 'host', brief(err or out)):
        return 1
    # The existing production sandbox denies network, source writes and
    # execution outside the seed and generated output directories.
    report.exec_prefix = [
        '/usr/bin/env', '-u', 'GH_TOKEN',
        '/usr/bin/sandbox-exec', '-D', 'SEED=' + str(seed),
        '-D', 'SOURCE=' + str(ROOT), '-D', 'OUT=' + str(work),
        '-f', ROOT / 'tools/native-ci/production.sb',
    ]
    seed.chmod(0o500)
    first = work / 'mio-A'
    code, out, err, seconds = run(report, 'seed-build-A',
                            [seed, 'build', 'selfhost/cli', '--bunko-root', ROOT / 'bunko', '-o', first],
                            timeout=GF4_BUILD_TIMEOUT, env={'PATH': ''})
    if not report.check('seed-build-A', 'PASS' if code == 0 and first.is_file() else 'FAIL', 'native',
                        '' if code == 0 and first.is_file() else f'exit {code}: {brief(err or out)}',
                        seconds=round(seconds, 1)):
        return 1
    first.chmod(0o700)
    report.hashes['cli-A'] = sha256(first)
    compiler = cli_chain(report, first, report.target, work)
    if compiler is None:
        return 1
    for target in CROSS_TARGETS:
        first = work / f'cross-B-{target}'
        second = work / f'cross-C-{target}'
        ok_b, reason_b, _ = cli_build(report, f'cross-B-{target}', compiler, 'selfhost/cli', target, first)
        ok_c, reason_c, _ = cli_build(report, f'cross-C-{target}', work / 'mio-C', 'selfhost/cli', target, second)
        hash_b = sha256(first) if ok_b else ''
        hash_c = sha256(second) if ok_c else ''
        match = bool(hash_b) and hash_b == hash_c
        reason = reason_b if not ok_b else reason_c if not ok_c else 'Darwin B and C produced different images'
        if not report.check('cross-stage-a ' + target, 'PASS' if match else 'FAIL', 'native',
                            '' if match else reason, B=hash_b, C=hash_c):
            return 1
        report.hashes['stage-a-' + target] = hash_b
    report.data['logs'] = {name: sha256(report.logs / name) for name in GF4_LOGS
                           if local_seed is None or name != 'seed-release-attestation.log'}
    report.data['seed']['status'] = 'PASS'
    report.save()
    return 0


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--local-seed', type=Path, help='Independent replay outside GitHub Actions only')
    args = parser.parse_args()
    result = verify(args.output, args.local_seed)
    (args.output / 'report.json').rename(args.output / 'trusted-gf4-report.json')
    sys.exit(result)
