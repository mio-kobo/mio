#!/usr/bin/env python3
"""Build a local darwin-arm64 release candidate; refuse existing destinations.
No commit, tag, push, upload, or publishing operation is performed.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import tarfile
import time

from release_automation import metadata, m18_evidence

ROOT = Path(__file__).resolve().parent.parent


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--m18-evidence', type=Path, help='fresh completed formal M18 evidence; otherwise rerun it')
    parser.add_argument('--version', default='2.0.0', help='canonical version without v, optionally -rc.N')
    args = parser.parse_args()
    metadata('v' + args.version)
    if (platform.system(), platform.machine()) != ('Darwin', 'arm64'):
        parser.error('release candidate requires a macOS ARM64 host')
    out = args.output.resolve()
    out.mkdir(parents=True, exist_ok=False)
    records = []

    def run(label, argv, cwd=ROOT):
        start = time.time()
        proc = subprocess.run([str(x) for x in argv], cwd=cwd, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        (out / (label + '.log')).write_bytes(proc.stdout)
        records.append(dict(label=label, argv=[str(x) for x in argv], cwd=str(cwd),
                            exit_code=proc.returncode, seconds=time.time() - start))
        (out / 'commands.json').write_text(json.dumps(records, indent=2) + '\n')
        if proc.returncode:
            raise RuntimeError(f'{label} failed; see {out / (label + ".log")}')
        return proc.stdout.decode()

    evidence = args.m18_evidence.resolve() if args.m18_evidence else out / 'm18'
    if not args.m18_evidence:
        run('m18', ['python3', ROOT / 'scripts/check-native-aot-fixed-point.py', '--evidence', evidence, '--gc-witness'])
    m18_evidence(evidence)

    package = out / f'mio-{args.version}-darwin-arm64'
    package.mkdir()
    for tool in ('mio', 'mochi-aot', 'mochi-aot-audit'):
        run('build-' + tool, ['env', 'CGO_ENABLED=0', 'GOOS=darwin', 'GOARCH=arm64', 'go', 'build',
                            '-buildvcs=false', '-trimpath', f'-ldflags=-s -w -buildid= -X main.version={args.version}',
                            '-o', package / tool, './cmd/' + tool])
    if run('version', [package / 'mio', 'version']) != f'Mio {args.version}\n':
        raise RuntimeError('release version mismatch')
    shutil.copy2(evidence / 'stage4.native', package / 'native-aot')
    (package / 'native-aot').chmod(0o755)
    shutil.copy2(evidence / 'native-aot.mochi', package / 'native-aot.mochi')
    for name in ('README.md', 'CHANGELOG.md', 'LICENSE'):
        shutil.copy2(ROOT / name, package / name)
    for name in ('bunko', 'examples', 'docs'):
        shutil.copytree(ROOT / name, package / name, ignore=shutil.ignore_patterns('v2.0.0-readiness.md'))
    (package / 'THIRD_PARTY_NOTICES.txt').write_text(
        'Mio source and Bunko: MIT, see LICENSE.\n'
        'The three Go-built tools include the Go standard library/runtime.\n'
        'Go BSD license and additional notices: see GO-LICENSE and GO-PATENTS.\n'
        'No external Go modules (go.mod: module Mio only).\n'
        'Mascot artwork is excluded; its separate rights are not granted by MIT.\n')
    goroot = Path(run('goroot', ['go', 'env', 'GOROOT']).strip())
    license_path = goroot / 'LICENSE'
    if not license_path.is_file():
        license_path = goroot.parent / 'LICENSE'  # Homebrew moves LICENSE above libexec.
    shutil.copy2(license_path, package / 'GO-LICENSE')
    shutil.copy2(goroot / 'PATENTS', package / 'GO-PATENTS')
    # Include vendored standard library notices where present in this Go distribution.
    for path in sorted((goroot / 'src/vendor').rglob('*')):
        if path.is_file() and path.name in ('LICENSE', 'LICENSE.txt', 'COPYING', 'NOTICE', 'PATENTS'):
            target = package / 'go-third-party' / path.relative_to(goroot / 'src/vendor')
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(path, target)
    head = run('head', ['git', 'rev-parse', 'HEAD']).strip()
    branch = run('branch', ['git', 'branch', '--show-current']).strip()
    status = run('status', ['git', 'status', '--porcelain=v1'])
    run('ci-ancestor', ['git', 'merge-base', '--is-ancestor', '8b53a25', 'HEAD'])
    # Capture tracked edits and hashes of ALL nonignored source files, including untracked M18 inputs.
    diff = subprocess.check_output(['git', 'diff', '--binary', 'HEAD'], cwd=ROOT)
    (out / 'source.diff').write_bytes(diff)
    names = subprocess.check_output(['git', 'ls-files', '-z', '--cached', '--others', '--exclude-standard'], cwd=ROOT).split(b'\0')
    sources = {os.fsdecode(n): sha(ROOT / os.fsdecode(n)) for n in names if n and (ROOT / os.fsdecode(n)).is_file()}
    (out / 'source-manifest.json').write_text(json.dumps(sources, indent=2, sort_keys=True) + '\n')
    build = dict(version=args.version, release_state='local candidate; not published', target='darwin-arm64',
                 head=head, branch=branch, dirty=bool(status), git_status=status,
                 source_diff_sha256=sha(out / 'source.diff'), source_manifest_sha256=sha(out / 'source-manifest.json'),
                 go_version=run('go-version', ['go', 'version']).strip(),
                 host=platform.platform(), m18_evidence=str(evidence),
                 m18_provenance_sha256=sha(evidence / 'provenance.json'),
                 native_aot_origin='M18 Stage 4, pure Mio generated',
                 native_aot_unsigned_sha256=sha(evidence / 'stage4.unsigned'),
                 signing='ad-hoc; no Developer ID; no notarization',
                 heap_protocol_mib=1024)
    (package / 'BUILD-INFO.json').write_text(json.dumps(build, indent=2) + '\n')
    for name in ('mio', 'mochi-aot', 'mochi-aot-audit', 'native-aot'):
        binary = package / name
        if name != 'native-aot':
            run(name + '-sign', ['/usr/bin/codesign', '--force', '--sign', '-', binary])
        run(name + '-verify', ['/usr/bin/codesign', '--verify', '--strict', binary])
        run(name + '-signature', ['/usr/bin/codesign', '-dvv', binary])
        description = run(name + '-file', ['/usr/bin/file', binary])
        if 'Mach-O 64-bit executable arm64' not in description:
            raise RuntimeError(f'wrong architecture: {name}')
        run(name + '-headers', ['/usr/bin/otool', '-hv', '-l', binary])
        run(name + '-libraries', ['/usr/bin/otool', '-L', binary])
    files = {str(p.relative_to(package)): dict(bytes=p.stat().st_size, sha256=sha(p))
             for p in sorted(package.rglob('*')) if p.is_file()}
    (package / 'MANIFEST.json').write_text(json.dumps(files, indent=2, sort_keys=True) + '\n')
    archive = out / (package.name + '.tar.gz')
    with tarfile.open(archive, 'w:gz') as tar:
        tar.add(package, arcname=package.name)
    (out / 'SHA256SUMS').write_text(f'{sha(archive)}  {archive.name}\n')
    run('install-smoke', ['python3', ROOT / 'scripts/smoke-release.py', archive, '--version', args.version, '--report', out / 'install-smoke.json'])
    print(json.dumps(dict(archive=str(archive), bytes=archive.stat().st_size, sha256=sha(archive)), indent=2))


if __name__ == '__main__':
    main()
