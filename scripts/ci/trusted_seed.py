#!/usr/bin/env python3
"""Build stable distribution inputs from the frozen Darwin ARM64 GF4 seed."""
import argparse
import os
from pathlib import Path
import shutil
import sys

from native_platforms import (ROOT, CLI_BUILD_TIMEOUT, Report, brief, cli_build,
                              cli_chain, run, sha256, verify_host)
from seed_evidence import GF4_SEED_SHA256, TARGETS, stable_version


def verify(output, seed_path):
    report = Report('darwin-arm64', output)
    report.data['suite'] = 'trusted-gf4-distribution'
    report.data['seed'] = dict(status='NOT_RUN', sha256=GF4_SEED_SHA256)
    inputs = report.data.get('distribution_input', {})
    if (not stable_version(inputs.get('version', '')) or inputs.get('embedded_bunko') is not False
            or inputs['source_sha'] != report.data['source_sha']):
        report.check('distribution-input', 'FAIL', 'host', 'stable disposable checkout required')
        return 1
    if not verify_host(report):
        return 1
    if not seed_path or not Path(seed_path).is_file():
        report.check('seed-pin', 'NOT_RUN', 'host', 'provide the frozen GF4 executable in MIO_NATIVE_SEED')
        return 2
    work = report.output / 'work'
    work.mkdir()
    seed = work / 'gf4'
    shutil.copyfile(seed_path, seed)
    if not report.check('seed-pin', 'PASS' if sha256(seed) == GF4_SEED_SHA256 else 'FAIL', 'host'):
        report.data['seed']['status'] = 'FAIL'
        report.save()
        return 1
    seed.chmod(0o500)
    report.data['seed']['status'] = 'FAIL'
    # The existing production sandbox denies network, source writes and
    # execution outside the seed and generated output directories.
    report.exec_prefix = [
        '/usr/bin/sandbox-exec', '-D', 'SEED=' + str(seed),
        '-D', 'SOURCE=' + str(ROOT), '-D', 'OUT=' + str(work),
        '-f', ROOT / 'tools/native-ci/production.sb',
    ]
    first = work / 'mio-A'
    code, out, err, _ = run(report, 'seed-build-A',
                            [seed, 'build', 'selfhost/cli', '--bunko-root', ROOT / 'bunko', '-o', first],
                            timeout=CLI_BUILD_TIMEOUT, env={'PATH': ''})
    if not report.check('seed-build-A', 'PASS' if code == 0 and first.is_file() else 'FAIL', 'native',
                        '' if code == 0 and first.is_file() else f'exit {code}: {brief(err or out)}'):
        return 1
    first.chmod(0o700)
    report.hashes['cli-A'] = sha256(first)
    compiler = cli_chain(report, first, report.target, work)
    if compiler is None:
        return 1
    for target in TARGETS:
        if target == report.target:
            continue
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
    report.data['seed']['status'] = 'PASS'
    report.save()
    return 0


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    result = verify(args.output, os.environ.get('MIO_NATIVE_SEED'))
    (args.output / 'report.json').rename(args.output / 'trusted-gf4-report.json')
    sys.exit(result)
