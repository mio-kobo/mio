#!/usr/bin/env python3
"""Build only from a verified Linux Full archive, exercise real offline containers,
then save/remove/load and repeat. Never compiles Mio or pushes an image."""
import argparse
import json
import os
from pathlib import Path
import shutil
import struct
import subprocess
import sys
import tempfile

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts/distribution'))
from package import digest, extract, inventory, write_json

ROOT = Path(__file__).resolve().parents[1]


def require_static_amd64(compiler):
    data = compiler.read_bytes()
    if data[:6] != b'\x7fELF\x02\x01' or struct.unpack_from('<H', data, 18)[0] != 62:
        raise ValueError('expected ELF64 little-endian AMD64 compiler')
    offset, = struct.unpack_from('<Q', data, 32)
    size, count = struct.unpack_from('<HH', data, 54)
    # PT_INTERP or PT_DYNAMIC implies dynamic runtime dependencies. The native
    # syscall-only backend currently emits neither; fail if that changes.
    types = [struct.unpack_from('<I', data, offset + size*i)[0] for i in range(count)]
    if not types or any(t in (2, 3) for t in types):
        raise ValueError('unexpected dynamic linker/dependencies; review base image')
    return dict(format='ELF64', machine='AMD64', program_header_types=types,
                dynamic_interpreter=None, dynamic_dependencies=[])


def build(candidates, output, image=None):
    output.mkdir(parents=True, exist_ok=True)
    report = dict(status='FAIL', image=image, platform='linux/amd64', checks=[], commands=[])
    def run(argv, **kwargs):
        proc = subprocess.run([str(x) for x in argv], capture_output=True, text=True, timeout=1800, **kwargs)
        report['commands'].append(dict(argv=[str(x) for x in argv], code=proc.returncode,
                                       stdout=proc.stdout, stderr=proc.stderr))
        write_json(output / 'docker-verification.json', report)
        if proc.returncode:
            raise RuntimeError('command failed: ' + str(argv))
        return proc.stdout.strip()
    def check(name, condition):
        report['checks'].append(dict(name=name, status='PASS' if condition else 'FAIL'))
        write_json(output / 'docker-verification.json', report)
        if not condition: raise RuntimeError(name)
    try:
        verification = json.loads((candidates / 'verification.json').read_text())
        check('Linux candidate verification', verification['status'] == 'PASS' and verification['target'] == 'linux-amd64')
        sums = dict(line.split('  ', 1)[::-1] for line in (candidates / 'SHA256SUMS').read_text().splitlines())
        archives = list(candidates.glob('*-linux-amd64-full.tar.gz'))
        check('single Linux Full archive', len(archives) == 1)
        archive = archives[0]
        check('Full archive SHA256', digest(archive) == sums[archive.name] == verification['archives'][archive.name])
        with tempfile.TemporaryDirectory(prefix='mio docker build ') as temp:
            context = Path(temp)
            extracted = extract(archive, context / 'unpack')
            manifest = json.loads((extracted / 'manifest.json').read_text())
            provenance = json.loads((extracted / 'provenance.json').read_text())
            check('Full identity and exact manifest', manifest['target'] == 'linux-amd64' and manifest['flavor'] == 'full'
                  and manifest['files'] == inventory(extracted) and provenance['embedded_bunko'] is True
                  and not (extracted / 'bunko').exists())
            compiler = extracted / 'bin/mio'
            sha = digest(compiler)
            check('Full compiler provenance', sha == provenance['compiler_sha256'])
            # The image tag follows the verified archive version, so a candidate
            # or RC never inherits another release's hardcoded image name.
            image = image or 'mio:%s-full' % manifest['version']
            report['image'] = image
            report.update(compiler_sha256=sha, source_sha=manifest['source_sha'], full_archive_sha256=digest(archive),
                          linkage=require_static_amd64(compiler))
            # Independent system-tool evidence, retained with the image archive.
            run(['readelf', '-h', '-l', '-d', compiler])
            shutil.move(str(extracted), context / 'full')
            shutil.copy2(ROOT / 'docker/Dockerfile', context / 'Dockerfile')
            run(['docker', 'build', '--platform=linux/amd64', '--network=none', '--build-arg', 'MIO_COMPILER_SHA256='+sha,
                 '--build-arg', 'MIO_SOURCE_SHA='+manifest['source_sha'], '-t', image, context])
        inspect = json.loads(run(['docker', 'image', 'inspect', image]))[0]
        check('non-root Linux AMD64 image', inspect['Os'] == 'linux' and inspect['Architecture'] == 'amd64'
              and inspect['Config']['User'] == '1000:1000')
        report['image_id'] = inspect['Id']
        def offline(round_name):
            # Outside the checkout, contains spaces and Unicode; host UID/GID is
            # passed explicitly so bind-mounted outputs retain host ownership.
            with tempfile.TemporaryDirectory(prefix='Mio 离线 workspace ') as workspace:
                work = Path(workspace)
                shutil.copyfile(ROOT / 'docker/offline-test.sh', work / 'offline-test.sh')
                requests = [dict(jsonrpc='2.0', id=1, method='initialize', params={}),
                            dict(jsonrpc='2.0', id=2, method='shutdown'), dict(jsonrpc='2.0', method='exit')]
                wire = b''
                for request in requests:
                    body = json.dumps(request).encode()
                    wire += f'Content-Length: {len(body)}\r\n\r\n'.encode()+body
                (work / 'lsp-input').write_bytes(wire)
                args = ['docker', 'run', '--rm', '--pull=never', '--platform=linux/amd64', '--network=none',
                        '--user', f'{os.getuid()}:{os.getgid()}', '--env', 'HOME=/tmp',
                        '--mount', f'type=bind,src={work},dst=/workspace', image]
                inside = run(args + ['sha256sum', '/opt/mio/bin/mio']).split()[0]
                check(round_name+' compiler SHA256 equals Linux Full', inside == sha)
                run(args + ['/bin/sh', '/workspace/offline-test.sh'])
                check(round_name+' host persistence and permissions',
                      (work / 'persisted.txt').read_text() == 'persisted\n' and
                      (work / 'hello').stat().st_uid == os.getuid() and (work / 'math').is_file())
                # Host runs the generated native artifacts too.
                check(round_name+' host runs generated image', run([work / 'hello']) == 'offline hello')
        offline('built')
        saved = output / 'mio-linux-amd64-full-candidate.docker.tar'
        run(['docker', 'save', '--output', saved, image])
        report['archive_sha256'] = digest(saved)
        run(['docker', 'image', 'rm', image])
        run(['docker', 'load', '--input', saved])
        loaded = json.loads(run(['docker', 'image', 'inspect', image]))[0]
        check('save/load preserves image ID', loaded['Id'] == inspect['Id'])
        offline('reloaded')
        (output / 'SHA256SUMS').write_text(f"{digest(saved)}  {saved.name}\n")
        report['status'] = 'PASS'
    finally:
        write_json(output / 'docker-verification.json', report)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--candidates', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--image', help='Image tag; defaults to mio:<verified archive version>-full')
    args = parser.parse_args()
    build(args.candidates.resolve(), args.output.resolve(), args.image)
