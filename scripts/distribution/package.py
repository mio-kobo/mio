#!/usr/bin/env python3
"""Package a native fixed-point compiler; no build tool is shipped to users."""
import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import stat
import tarfile
import zipfile

from seed_evidence import GF4, SEED_KINDS, TARGETS, seed_binding, stable_version
# Integrated tools are the only validated end-user tools today. No standalone
# binaries, Go VM, historical snapshots or test harnesses are implicitly copied.
TOOLS = ('check', 'build', 'run', 'exec', 'test', 'lsp', 'disasm')
EXAMPLES = {'hello.mio': 'chant("hello from Mio");\n',
            'math.mio': 'yobu "kiso/math"; chant(math.abs(-9));\n'}


def digest(path):
    sha = hashlib.sha256()
    with Path(path).open('rb') as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b''):
            sha.update(chunk)
    return sha.hexdigest()


def write_json(path, value):
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2, sort_keys=True)+'\n', encoding='utf-8')


def inventory(root):
    return {p.relative_to(root).as_posix(): dict(sha256=digest(p), size=p.stat().st_size)
            for p in sorted(root.rglob('*')) if p.is_file() and p != root / 'manifest.json'}


def safe_name(name):
    p = PurePosixPath(name)
    if not name or '\\' in name or ':' in name or p.is_absolute() or any(x in ('', '.', '..') for x in name.split('/')):
        raise ValueError('unsafe archive member: ' + name)
    return p


def extract(archive, destination):
    """Only fresh destinations and regular files/directories; reject duplicates,
    case collisions, links, traversal and overwriting before writing any bytes."""
    if destination.exists():
        raise ValueError('extraction destination already exists')
    entries = []
    if archive.suffix == '.zip':
        with zipfile.ZipFile(archive) as z:
            for item in z.infolist():
                if item.is_dir() or stat.S_IFMT(item.external_attr >> 16) not in (0, stat.S_IFREG):
                    raise ValueError('non-regular ZIP member')
                entries.append((item.filename, z.read(item), item.external_attr >> 16))
    else:
        with tarfile.open(archive) as t:
            for item in t.getmembers():
                if not item.isfile():
                    raise ValueError('non-regular tar member')
                entries.append((item.name, t.extractfile(item).read(), item.mode))
    seen = set()
    for name, _, _ in entries:
        safe_name(name)
        key = name.casefold()
        if key in seen:
            raise ValueError('duplicate archive member')
        seen.add(key)
    for name, _, _ in entries:
        if any(p.as_posix().casefold() in seen for p in PurePosixPath(name).parents if p.as_posix() != '.'):
            raise ValueError('file/directory collision')
    destination.mkdir(parents=True)
    for name, data, mode in entries:
        target = destination.joinpath(*PurePosixPath(name).parts)
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(data)
        target.chmod(mode & 0o777)
    roots = {PurePosixPath(n).parts[0] for n, _, _ in entries}
    if len(roots) != 1:
        raise ValueError('expected single archive root')
    return destination / roots.pop()


def gate(report, compiler, inputs, target):
    if report['target'] != target or report['status'] != 'PASS' or report['source_sha'] != inputs['source_sha']:
        raise ValueError('native report target/status/source mismatch')
    if report['run']['id'] != os.environ.get('GITHUB_RUN_ID', '') or report['run']['attempt'] != os.environ.get('GITHUB_RUN_ATTEMPT', ''):
        raise ValueError('native report belongs to another Actions run')
    checks = {c['name']: c for c in report['checks']}
    fixed = checks.get('cli-fixed-point', {})
    if fixed.get('status') != 'PASS' or fixed.get('engine') != 'native':
        raise ValueError('native CLI fixed point required')
    if any(c['required'] and c['status'] != 'PASS' for c in report['checks']):
        raise ValueError('required native checks failed')
    hashes = report['hashes']
    if not (digest(compiler) == hashes['cli-B'] == hashes['cli-C'] == hashes['cli-D']):
        raise ValueError('compiler does not match B/C/D')
    if inputs['embedded_bunko'] is not False:
        raise ValueError('source-free build input required')


def package(source, compiler, report_path, output, target, seed_report=None, seed_kind=GF4):
    inputs = json.loads((source / 'distribution-input.json').read_text(encoding='utf-8'))
    report = json.loads(report_path.read_text(encoding='utf-8'))
    gate(report, compiler, inputs, target)
    trusted_seed = report['seed']
    if stable_version(inputs['version']):
        if seed_report is None:
            raise ValueError('stable distribution requires a same-run trusted seed report')
        trusted_seed = seed_binding(json.loads(seed_report.read_text(encoding='utf-8')), report, inputs,
                                    target, digest(compiler), digest(seed_report), seed_kind)
    from prepare import EMPTY
    if (source / 'selfhost/resources/bunko_data.mio').read_text(encoding='utf-8') != EMPTY:
        raise ValueError('embedded resource table was not disabled')
    output.mkdir(parents=True, exist_ok=True)
    archives = []
    for flavor in ('core', 'full'):
        name = f"mio-{inputs['version']}-{target}-{flavor}"
        root = output / name
        root.mkdir()
        binary = root / 'bin' / ('mio.exe' if target == 'windows-amd64' else 'mio')
        binary.parent.mkdir()
        shutil.copyfile(compiler, binary)
        binary.chmod(0o755)
        shutil.copyfile(source / 'LICENSE', root / 'LICENSE')
        (root / 'README.md').write_text(
            f"# Mio {inputs['version']} {flavor.title()}\n\n"
            f"Target: {target}\nCommit: {inputs['source_sha']}\n\n" +
            ("PR validation artifact; not a stable release. " if "-pr." in inputs["version"] else
             "Release candidate; not a stable release. " if "-rc." in inputs["version"] else "Release artifact. ") + "Put bin/ on PATH.\n" +
            "Bunko order: --bunko-root, MIO_BUNKO, executable-relative ../bunko, no library.\n"
            "Core requires an external Bunko installation for kiso/ imports.\n"
            "Integrated tools: " + ', '.join(TOOLS) + '.\n', encoding='utf-8')
        if flavor == 'full':
            # Explicit file whitelist: tracked production Mio sources in kiso/,
            # excluding tests and historical patch snapshots at any depth.
            for path in sorted((source / 'bunko/kiso').rglob('*.mio')):
                rel = path.relative_to(source / 'bunko')
                if path.name.endswith('_test.mio') or any('.' in part for part in rel.parts[:-1]):
                    continue
                if path.is_symlink():
                    raise ValueError('symlink in Bunko')
                dest = root / 'bunko' / rel
                dest.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(path, dest)
            for filename, content in EXAMPLES.items():
                dest = root / 'examples' / filename
                dest.parent.mkdir(exist_ok=True)
                dest.write_text(content, encoding='utf-8')
        provenance = dict(inputs, target=target, flavor=flavor, compiler_sha256=digest(compiler),
                          native_report_sha256=digest(report_path), native_run=report['run'],
                          trusted_seed=trusted_seed, runtime_abi='RA-1', tools=list(TOOLS),
                          standalone_tools=[], build_engine='native B/C/D fixed point (Go-hosted root)')
        write_json(root / 'provenance.json', provenance)
        write_json(root / 'manifest.json', dict(schema_version=1, target=target, flavor=flavor,
                                               version=inputs['version'], source_sha=inputs['source_sha'], files=inventory(root)))
        files = sorted(p for p in root.rglob('*') if p.is_file())
        archive = output / (name + ('.zip' if target == 'windows-amd64' else '.tar.gz'))
        if target == 'windows-amd64':
            with zipfile.ZipFile(archive, 'w', compression=zipfile.ZIP_DEFLATED) as z:
                for p in files:
                    z.write(p, p.relative_to(output).as_posix())
        else:
            with tarfile.open(archive, 'w:gz') as t:
                for p in files:
                    t.add(p, arcname=p.relative_to(output).as_posix(), recursive=False)
        archives.append(archive)
    (output / 'SHA256SUMS').write_text(''.join(f'{digest(p)}  {p.name}\n' for p in archives), encoding='utf-8')
    shutil.copyfile(report_path, output / 'native-report.json')
    return archives


if __name__ == '__main__':
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--source', type=Path, required=True)
    p.add_argument('--compiler', type=Path, required=True)
    p.add_argument('--report', type=Path, required=True)
    p.add_argument('--seed', choices=SEED_KINDS, default=GF4)
    p.add_argument('--seed-report', type=Path)
    p.add_argument('--output', type=Path, required=True)
    p.add_argument('--target', choices=TARGETS, required=True)
    a = p.parse_args()
    package(a.source.resolve(), a.compiler.resolve(), a.report.resolve(), a.output.resolve(), a.target, a.seed_report, a.seed)
