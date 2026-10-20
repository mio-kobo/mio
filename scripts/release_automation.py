#!/usr/bin/env python3
"""Fail-closed tag policy and release transport. No network calls on import."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import subprocess
import urllib.error
import urllib.parse
import urllib.request

ROOT = Path(__file__).resolve().parent.parent
TAG = re.compile(r'v(2\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*))(-rc\.([1-9][0-9]*))?', re.ASCII)
GOLDEN = {
    'seed': 'b92e4887ace8be54b76d62f29a708037eb8633edbb04876d0aade65e5ae0794d',
    'stage': '1fe684077b0e0aba6a9e8f73cb062b5f0b1e94e35f36da683cf0fc94d9fedb2d',
    'mochi': 'a07ee129a44938ab276806be30bc06a85a477ad74c0b4064489ed592b1b0da13',
}


def require(ok, message):
    if not ok:
        raise RuntimeError(message)


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def git(*args):
    return subprocess.check_output(['git', *args], cwd=ROOT, text=True).strip()


def parse_tag(tag):
    match = TAG.fullmatch(tag)
    require(match, 'expected v2.x.y or v2.x.y-rc.N (canonical decimal, N >= 1)')
    return dict(tag=tag, base=match[1], version=tag[1:], prerelease=bool(match[2]),
                notes=f'docs/releases/v{match[1]}.md')


def metadata(tag, root=ROOT):
    info = parse_tag(tag)
    require((root / 'VERSION').read_text().strip() == info['base'], 'VERSION differs from tag base')
    cli = (root / 'cmd/mio/main.go').read_text()
    require(re.search(r'^var version = "' + re.escape(info['base']) + '"$', cli, re.M), 'CLI source version differs')
    require((root / info['notes']).is_file() and (root / info['notes']).stat().st_size > 0, 'release notes missing')
    return info


def workflow_policy(root=ROOT):
    # PyYAML is installed explicitly in CI; BaseLoader keeps the YAML key "on" a string.
    import yaml
    workflows = {}
    for path in (root / '.github/workflows').glob('*.y*ml'):
        workflows[path.name] = yaml.load(path.read_text(), Loader=yaml.BaseLoader)
    release = workflows['release.yml']
    for name, doc in workflows.items():
        require(isinstance(doc, dict) and 'jobs' in doc, f'invalid workflow: {name}')
        if name != 'release.yml':
            for job in doc['jobs'].values():
                require(job.get('permissions', {}).get('contents') != 'write', f'competing writer: {name}')
            require(doc.get('permissions', {}).get('contents') != 'write', f'competing writer: {name}')
            text = (root / '.github/workflows' / name).read_text()
            require(not re.search(r'gh\s+release|releases\s*:\s*write|release_automation.py\s+publish|softprops/action-gh-release', text), f'competing publisher: {name}')
    require(release['permissions'] == {'contents': 'read'}, 'global permissions must be read-only')
    require(set(release['on']) == {'push', 'workflow_dispatch'}, 'unexpected release trigger')
    require(release['on']['push']['tags'] == ['v2.*'], 'tag-only push required')
    jobs = release['jobs']
    require(set(jobs) == {'preflight', 'test', 'm18', 'package', 'verify', 'publish'}, 'required gates missing')
    require(set(jobs['publish']['needs']) == {'preflight', 'test', 'm18', 'package', 'verify'}, 'publish gates missing')
    require(jobs['publish']['if'] == "github.event_name == 'push' && startsWith(github.ref, 'refs/tags/v2.')", 'dry-run publish guard changed')
    for name, dependencies in {'test': {'preflight'}, 'm18': {'preflight', 'test'},
                               'package': {'preflight', 'm18'}, 'verify': {'preflight', 'package'}}.items():
        actual = jobs[name]['needs']
        require(set([actual] if isinstance(actual, str) else actual) == dependencies, 'gate dependencies differ: ' + name)
    for name, job in jobs.items():
        require('continue-on-error' not in job, 'job may not ignore failure')
        require(job.get('permissions', {}) == ({'contents': 'write'} if name == 'publish' else {}), f'excess permissions: {name}')
        for step in job.get('steps', []):
            require('continue-on-error' not in step, 'step may not ignore failure')
            if 'uses' in step:
                require(re.fullmatch(r'[^@]+@[0-9a-f]{40}', step['uses']), 'action must be SHA pinned')


def api(path, method='GET', data=None, content_type='application/json'):
    base = os.environ.get('GITHUB_API_URL', 'https://api.github.com')
    url = path if path.startswith('https://') else base + '/repos/' + os.environ['GITHUB_REPOSITORY'] + path
    require(urllib.parse.urlparse(url).hostname in {'api.github.com', 'uploads.github.com'}, 'unexpected API host')
    body = json.dumps(data).encode() if isinstance(data, dict) else data
    request = urllib.request.Request(url, data=body, method=method, headers={
        'Authorization': 'Bearer ' + os.environ['GH_TOKEN'], 'Accept': 'application/vnd.github+json',
        'X-GitHub-Api-Version': '2022-11-28', 'Content-Type': content_type})
    try:
        with urllib.request.urlopen(request, timeout=120) as response:
            return json.load(response)
    except urllib.error.HTTPError as error:
        if method == 'GET' and error.code == 404:
            return None
        raise RuntimeError(f'GitHub API {method} failed: HTTP {error.code}') from None


def absent(tag):
    require(api('/releases/tags/' + urllib.parse.quote(tag, safe='')) is None,
            'Release already exists; refusing to replace assets')
    # The by-tag endpoint only promises published releases. List with authentication
    # as well so an interrupted upload's draft also blocks retries.
    page = 1
    while True:
        releases = api(f'/releases?per_page=100&page={page}')
        require(isinstance(releases, list), 'cannot inspect existing release drafts')
        require(not any(item['tag_name'] == tag for item in releases),
                'Release already exists (including drafts); refusing to replace assets')
        if len(releases) < 100:
            return
        page += 1


def workflow_baseline(commit):
    # GitHub's contents-only GITHUB_TOKEN cannot create a release whose target
    # introduces workflow changes relative to the default branch.
    repository = api('')
    default = api('/contents/.github/workflows?ref=' + urllib.parse.quote(repository['default_branch'], safe=''))
    target = api('/contents/.github/workflows?ref=' + urllib.parse.quote(commit, safe=''))
    require(isinstance(default, list) and isinstance(target, list), 'cannot inspect workflow baseline')
    require({item['name']: item['sha'] for item in default} == {item['name']: item['sha'] for item in target},
            'merge the reviewed workflow revision into the default branch before tagging')


def remote_commit(tag, commit):
    value = api('/git/ref/tags/' + urllib.parse.quote(tag, safe=''))
    require(value, 'remote tag missing')
    obj = value['object']
    for _ in range(8):
        if obj['type'] == 'commit':
            require(obj['sha'] == commit, 'remote tag moved or commit differs')
            return
        require(obj['type'] == 'tag', 'tag must resolve to a commit')
        obj = api('/git/tags/' + obj['sha'])['object']
    raise RuntimeError('excessive nested tags')


def m18_evidence(evidence):
    report = json.loads((evidence / 'provenance.json').read_text())
    require(report['status'] == 'BOOTSTRAP_PASS' and report['native_fixed_point'] and
            report['go_aot_invocations'] == 1 and report['stage4_executed'], 'formal M18 incomplete')
    require((evidence / 'git-head.stdout').read_text().strip() == git('rev-parse', 'HEAD'), 'M18 commit differs')
    require(report['gc_witness']['gc_count'] == 1 and report['gc_witness']['shadow_frame'] == 0 and
            report['gc_witness']['exit_argument'] == 0, 'GC witness differs')
    for name in ['seed', *[f'stage{i}' for i in range(1, 6)]]:
        expected = GOLDEN['seed' if name == 'seed' else 'stage']
        require(sha(evidence / (name + '.unsigned')) == expected == report['artifacts'][name]['sha256'], 'M18 golden mismatch: ' + name)
        require(sha(evidence / (name + '.native')) == report['artifacts'][name]['signed']['sha256'] and
                report['artifacts'][name]['codesign_strict'], 'signed M18 artifact changed')
    require(sha(evidence / 'native-aot.mochi') == GOLDEN['mochi'], 'Mochi golden mismatch')
    original = Path(report['root'])
    for item in json.loads((evidence / 'source-manifest.json').read_text()):
        source = ROOT / Path(item['path']).relative_to(original)
        require(sha(source) == item['sha256'], 'M18 source differs: ' + str(source))
    return report


def verify_assets(directory, tag, commit, run_id):
    info = parse_tag(tag)
    name = f'mio-{info["version"]}-darwin-arm64.tar.gz'
    checksum = f'{sha(directory / name)}  {name}\n'
    require((directory / 'SHA256SUMS').read_text() == checksum, 'checksum differs')
    provenance = json.loads((directory / 'release-provenance.json').read_text())
    require(provenance['tag'] == tag and provenance['commit'] == commit and
            provenance['run_id'] == run_id and provenance['run_attempt'] == os.environ.get('GITHUB_RUN_ATTEMPT', 'local') and
            provenance['version'] == info['version'], 'provenance source differs')
    require(provenance['sha256'] == sha(directory / name) and
            provenance['m18']['status'] == 'BOOTSTRAP_PASS' and provenance['smoke']['status'] == 'PASS', 'acceptance missing')
    require(provenance['assets'] == [name, 'SHA256SUMS', 'release-provenance.json'], 'asset inventory differs')
    return info, provenance


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('command', choices=['static', 'preflight', 'm18', 'provenance', 'verify', 'publish'])
    parser.add_argument('--tag', default=os.environ.get('RELEASE_TAG'))
    parser.add_argument('--directory', type=Path, default=Path('build/release'))
    parser.add_argument('--evidence', type=Path, default=Path('build/m18'))
    args = parser.parse_args()
    if args.command == 'static':
        workflow_policy()
        return
    if args.command == 'm18':
        m18_evidence(args.evidence)
        return
    info = metadata(args.tag)
    commit = git('rev-parse', 'HEAD')
    require(commit == os.environ.get('GITHUB_SHA', commit), 'checkout differs from workflow commit')
    run_id = os.environ.get('GITHUB_RUN_ID', 'local')
    if args.command == 'preflight':
        workflow_policy()
        if os.environ.get('GITHUB_EVENT_NAME') == 'push':
            require(os.environ['GITHUB_REF'] == 'refs/tags/' + args.tag, 'not the triggering tag')
            require(git('rev-parse', args.tag + '^{commit}') == commit, 'tag differs from checkout')
            remote_commit(args.tag, commit)
            workflow_baseline(commit)
            absent(args.tag)
        require(not git('status', '--porcelain', '--untracked-files=normal'), 'CI source checkout is dirty')
        with open(os.environ['GITHUB_OUTPUT'], 'a') as out:
            for key in ['tag', 'version', 'notes']:
                out.write(f'{key}={info[key]}\n')
        return
    if args.command == 'provenance':
        report = m18_evidence(args.evidence)
        smoke = json.loads((args.directory / 'install-smoke.json').read_text())
        require(smoke['status'] == 'PASS', 'install failed')
        name = f'mio-{info["version"]}-darwin-arm64.tar.gz'
        import datetime
        value = dict(version=info['version'], tag=args.tag, commit=commit,
                     runner_os=platform.system(), runner_arch=platform.machine(),
                     tools={'go': subprocess.check_output(['go', 'version'], text=True).strip(),
                            'python': platform.python_version(),
                            'clang': subprocess.check_output(['xcrun', 'clang', '--version'], text=True).strip(),
                            'lldb': subprocess.check_output(['xcrun', 'lldb', '--version'], text=True).strip()},
                     run_id=run_id, run_attempt=os.environ.get('GITHUB_RUN_ATTEMPT', 'local'),
                     workflow=os.environ.get('GITHUB_WORKFLOW_REF', 'local'),
                     built_at=datetime.datetime.now(datetime.timezone.utc).isoformat(),
                     build_entry='scripts/build-release.sh', sha256=sha(args.directory / name),
                     m18={'status': report['status'], 'golden': GOLDEN, 'gc_witness': report['gc_witness'],
                          'provenance_sha256': sha(args.evidence / 'provenance.json')},
                     smoke=smoke, signing='ad-hoc; no Developer ID; no notarization',
                     assets=[name, 'SHA256SUMS', 'release-provenance.json'])
        (args.directory / 'release-provenance.json').write_text(json.dumps(value, indent=2) + '\n')
    info, provenance = verify_assets(args.directory, args.tag, commit, run_id)
    if args.command == 'publish':
        require(os.environ.get('GITHUB_EVENT_NAME') == 'push' and os.environ.get('GITHUB_REF') == 'refs/tags/' + args.tag,
                'only a tag push can publish')
        remote_commit(args.tag, commit)
        workflow_baseline(commit)
        absent(args.tag)
        # Draft first: a failed upload leaves a visible recovery blocker, never a partial public release.
        release = api('/releases', 'POST', dict(tag_name=args.tag, target_commitish=commit,
                      name='Mio ' + args.tag, body=(ROOT / info['notes']).read_text(),
                      draft=True, prerelease=info['prerelease']))
        for name in provenance['assets']:
            payload = (args.directory / name).read_bytes()
            asset = api(release['upload_url'].split('{')[0] + '?name=' + urllib.parse.quote(name), 'POST',
                        payload, 'application/octet-stream')
            require(asset['name'] == name and asset['size'] == len(payload) and
                    asset['state'] == 'uploaded', 'uploaded asset incomplete')
            if asset.get('digest'):
                require(asset['digest'] == 'sha256:' + hashlib.sha256(payload).hexdigest(), 'uploaded digest differs')
        remote_commit(args.tag, commit)
        api('/releases/' + str(release['id']), 'PATCH', {'draft': False, 'prerelease': info['prerelease'], 'make_latest': 'false' if info['prerelease'] else 'true'})


if __name__ == '__main__':
    main()
