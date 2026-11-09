#!/usr/bin/env python3
"""Version-independent Mio release version, tag and archive-branch resolution.

This module is the single source of truth for the permanent version archive
convention. Release code and workflows must not duplicate semantic-version
regular expressions or branch names.

    stable tag     archive branch
    v2.1.1         Mio-2.1.1
    v2.1.2         Mio-2.1.2
    v2.2.0         Mio-2.2.0
    v3.0.0         Mio-3.0.0
    RC v2.1.2-rc.1 Mio-2.1.2
    RC v3.0.0-rc.1 Mio-3.0.0

The only historical exception is the already published v2.1.0 line, which
predates per-version archive branches and stays on Mio-2.1. That exception is
deliberately narrow: it never applies to a future version.

A release tag must resolve to the exact tip of its version branch. The release
branch does not need to be merged into the repository default branch. A moved
version branch or a tag pointing at an unrelated commit is rejected.
"""
import argparse
import json
import os
import re
import subprocess
import sys
from dataclasses import dataclass
from typing import Optional

REPOSITORY = 'mio-kobo/mio'

# Mio release versioning starts with the published v2.1.0 line. Older 1.x and
# 2.0.x versions are historical and are never rebuilt through this pipeline.
MINIMUM_STABLE = (2, 1, 0)
# Per-version archive branches exist for every release after v2.1.0.
HISTORICAL_BRANCH = 'Mio-2.1'
HISTORICAL_VERSION = (2, 1, 0)

VERSION_PATTERN = r'(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)'
RC_PATTERN = VERSION_PATTERN + r'-rc\.([1-9]\d*)'
CANDIDATE_PATTERN = VERSION_PATTERN + r'-pr\.([0-9a-f]{12})'
RELEASE_PATTERN = r'v(' + RC_PATTERN + r'|' + VERSION_PATTERN + r')'

_STABLE_RE = re.compile(r'^' + VERSION_PATTERN + r'$')
_RC_RE = re.compile(r'^' + RC_PATTERN + r'$')
_CANDIDATE_RE = re.compile(r'^' + CANDIDATE_PATTERN + r'$')
_TAG_RE = re.compile(r'^v(' + RC_PATTERN + r'|' + VERSION_PATTERN + r')$')

# Kept for callers that previously imported it from seed_evidence.
STABLE_VERSION = VERSION_PATTERN


class VersionError(ValueError):
    """A version or tag does not belong to the supported Mio release scheme."""


class RefError(VersionError):
    """A Git tag, branch or source revision violates the archive convention."""


@dataclass(frozen=True)
class Version:
    """A parsed Mio version, optionally an RC."""

    major: int
    minor: int
    patch: int
    rc: Optional[int] = None

    def __post_init__(self):
        if self.rc is not None and self.rc < 1:
            raise VersionError('RC number must be a positive integer')

    @property
    def base(self):
        return (self.major, self.minor, self.patch)

    @property
    def is_rc(self):
        return self.rc is not None

    @property
    def is_stable(self):
        return self.rc is None

    def __str__(self):
        text = '%d.%d.%d' % self.base
        return text + ('-rc.%d' % self.rc if self.rc is not None else '')


def _bad(text, reason='not a valid Mio release version'):
    raise VersionError('%r is %s' % (text, reason))


def parse_version(value):
    """Parse a stable or RC version string; reject everything else."""
    if isinstance(value, Version):
        return value
    if not isinstance(value, str):
        _bad(value)
    match = _RC_RE.fullmatch(value)
    if match:
        version = Version(int(match.group(1)), int(match.group(2)), int(match.group(3)), int(match.group(4)))
    else:
        match = _STABLE_RE.fullmatch(value)
        if not match:
            _bad(value)
        version = Version(int(match.group(1)), int(match.group(2)), int(match.group(3)))
    if version.base < MINIMUM_STABLE:
        _bad(value, 'older than the supported Mio 2.1 release line')
    return version


def parse_tag(tag):
    """Parse an exact ``vX.Y.Z`` or ``vX.Y.Z-rc.N`` release tag."""
    if not isinstance(tag, str) or not _TAG_RE.fullmatch(tag):
        raise VersionError('%r is not a Mio release tag (expected vX.Y.Z or vX.Y.Z-rc.N)' % (tag,))
    return parse_version(tag[1:])


def parse_candidate(value):
    """Parse a PR candidate version (``X.Y.Z-pr.<12 hex>``)."""
    if not isinstance(value, str) or not _CANDIDATE_RE.fullmatch(value):
        raise VersionError('%r is not a Mio PR candidate version' % (value,))
    match = _CANDIDATE_RE.fullmatch(value)
    version = Version(int(match.group(1)), int(match.group(2)), int(match.group(3)))
    if version.base < MINIMUM_STABLE:
        _bad(value, 'older than the supported Mio 2.1 release line')
    return version


def is_stable(value):
    try:
        return parse_version(value).is_stable
    except VersionError:
        return False


def is_rc(value):
    try:
        return parse_version(value).is_rc
    except VersionError:
        return False


def stable_version(value):
    """Backward-compatible predicate: exactly a stable release version."""
    return is_stable(value)


def is_build_version(value):
    """Stable, RC or PR candidate version accepted in a built archive manifest."""
    try:
        if _CANDIDATE_RE.fullmatch(value):
            parse_candidate(value)
        else:
            parse_version(value)
        return True
    except (TypeError, VersionError):
        return False


def release_kind(value):
    version = parse_version(value)
    return 'rc' if version.is_rc else 'stable'


def tag_for(value):
    return 'v' + str(parse_version(value))


def branch_for(value):
    """Resolve the permanent archive branch for a version or tag.

    ``v2.1.0`` (and its historical RC tags) remain on Mio-2.1. Every later
    version, including future majors, maps to Mio-<major>.<minor>.<patch>.
    """
    version = parse_tag(value) if isinstance(value, str) and value.startswith('v') else parse_version(value)
    if version.base == HISTORICAL_VERSION:
        return HISTORICAL_BRANCH
    return 'Mio-%d.%d.%d' % version.base


def is_historical(value):
    return (parse_tag(value) if isinstance(value, str) and value.startswith('v') else parse_version(value)).base == HISTORICAL_VERSION


def _git(root, *args, check=True):
    result = subprocess.run(['git', *[str(arg) for arg in args]], cwd=str(root), capture_output=True, text=True)
    if check and result.returncode:
        raise RefError((result.stderr or result.stdout).strip() or 'git %s failed' % ' '.join(map(str, args)))
    return result


def tag_commit(root, tag):
    """Resolve an exact tag ref (never a branch or a short name) to a commit."""
    parse_tag(tag)
    result = _git(root, 'rev-parse', '--verify', '--quiet', 'refs/tags/%s^{commit}' % tag, check=False)
    if result.returncode:
        raise RefError('release tag %s does not exist' % tag)
    return result.stdout.strip()


def branch_commit(root, branch, remote='origin'):
    """Resolve an archive branch, preferring the remote ref when it exists."""
    if not branch or '/' in branch or branch.startswith('-'):
        raise RefError('invalid archive branch name %r' % (branch,))
    candidates = ['refs/remotes/%s/%s' % (remote, branch), 'refs/heads/%s' % branch]
    for ref in candidates:
        result = _git(root, 'rev-parse', '--verify', '--quiet', ref, check=False)
        if not result.returncode:
            return result.stdout.strip(), ref
    raise RefError('version archive branch %s does not exist' % branch)


def is_ancestor(root, ancestor, descendant):
    result = _git(root, 'merge-base', '--is-ancestor', ancestor, descendant, check=False)
    if result.returncode == 0:
        return True
    if result.returncode == 1:
        return False
    raise RefError(result.stderr.strip() or 'could not compare Git revisions')


def verify_release(root, tag, source_sha=None):
    """Verify that a release tag is exactly its version branch tip.

    Returns a JSON-friendly record. Historical v2.1.0 is the only version that
    may be an ancestor of Mio-2.1 instead of its tip, because that release
    predates the per-version archive branches and must remain unchanged.
    """
    version = parse_tag(tag)
    expected = source_sha if source_sha is not None else tag_commit(root, tag)
    if not re.fullmatch(r'[0-9a-f]{40}', expected or ''):
        raise RefError('expected a full 40-character source commit')
    resolved = tag_commit(root, tag)
    if resolved != expected:
        raise RefError('%s resolves to %s, which does not match the expected source %s' % (tag, resolved, expected))
    branch = branch_for(version)
    head, ref = branch_commit(root, branch)
    if version.base == HISTORICAL_VERSION:
        if not is_ancestor(root, resolved, head):
            raise RefError('historical %s is not an ancestor of %s' % (tag, branch))
        mode = 'historical-ancestor'
    else:
        if resolved != head:
            raise RefError('%s resolves to %s but %s is at %s; the release tag must be the exact '
                           'version branch tip' % (tag, resolved, branch, head))
        mode = 'exact-branch-tip'
    return dict(tag=tag, version=str(version), kind='rc' if version.is_rc else 'stable',
                branch=branch, branch_ref=ref, tag_commit=resolved, branch_commit=head, mode=mode)


def verify_source(root, tag, source_sha):
    """Require the requested source revision to be the exact tagged commit."""
    resolved = tag_commit(root, tag)
    if source_sha != resolved:
        raise RefError('source revision %s does not match %s at %s' % (source_sha, tag, resolved))
    return resolved


def expected_dispatch_ref(version, kind):
    parsed = parse_version(version)
    if kind not in ('stable', 'rc'):
        raise VersionError('release kind must be stable or rc')
    if kind == 'stable' and parsed.is_rc:
        raise VersionError('stable release requested with an RC version')
    if kind == 'rc' and not parsed.is_rc:
        raise VersionError('RC release requested with a stable version')
    return 'refs/tags/' + tag_for(parsed)


def verify_dispatch(version, kind, tag=None, ref=None, event=None, repository=None, official=REPOSITORY):
    """Validate a workflow_dispatch release request before any build starts."""
    ref = ref if ref is not None else os.environ.get('GITHUB_REF', '')
    event = event if event is not None else os.environ.get('GITHUB_EVENT_NAME', '')
    repository = repository if repository is not None else os.environ.get('GITHUB_REPOSITORY', '')
    expected_tag = tag_for(parse_version(version))
    parsed = parse_version(version)
    parsed_kind = release_kind(version)
    if kind == 'stable' and parsed.is_rc:
        raise VersionError('stable release requested with an RC version')
    if kind == 'rc' and not parsed.is_rc:
        raise VersionError('RC release requested with a stable version')
    if kind != parsed_kind:
        raise VersionError('requested %s release but %s is a %s version' % (kind, version, parsed_kind))
    if tag is not None and tag != expected_tag:
        raise VersionError('requested version %s does not match tag %s' % (version, tag))
    if repository != official:
        raise RefError('release dispatch is restricted to %s' % official)
    if event != 'workflow_dispatch':
        raise RefError('release builds require workflow_dispatch on the exact release tag')
    if ref != expected_dispatch_ref(version, kind):
        raise RefError('release build requires dispatch on %s' % expected_dispatch_ref(version, kind))
    return dict(version=str(parse_version(version)), tag=expected_tag, kind=kind,
                branch=branch_for(version), ref=ref)


def info(tag):
    version = parse_tag(tag)
    return dict(tag=tag, version=str(version), kind='rc' if version.is_rc else 'stable',
                branch=branch_for(version), historical=version.base == HISTORICAL_VERSION)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='command', required=True)

    info_parser = commands.add_parser('info', help='print version and archive branch metadata')
    info_parser.add_argument('--tag', required=True)

    verify_parser = commands.add_parser('verify', help='verify the tag against its archive branch')
    verify_parser.add_argument('--tag', required=True)
    verify_parser.add_argument('--source-sha')
    verify_parser.add_argument('--root', default='.')

    dispatch_parser = commands.add_parser('dispatch', help='validate a release workflow dispatch context')
    dispatch_parser.add_argument('--version', required=True)
    dispatch_parser.add_argument('--kind', choices=('stable', 'rc'), required=True)
    dispatch_parser.add_argument('--tag')

    args = parser.parse_args(argv)
    try:
        if args.command == 'info':
            result = info(args.tag)
        elif args.command == 'verify':
            result = verify_release(args.root, args.tag, args.source_sha)
        else:
            result = verify_dispatch(args.version, args.kind, args.tag)
    except VersionError as error:
        print('error: ' + str(error), file=sys.stderr)
        return 1
    json.dump(result, sys.stdout, indent=2, sort_keys=True)
    sys.stdout.write('\n')
    return 0


if __name__ == '__main__':
    sys.exit(main())
