#!/usr/bin/env python3
"""Reviewed bootstrap seed profiles for Mio release builds.

A bootstrap seed is an immutable, reviewed native compiler that roots the
native fixed-point chain. A seed is never selected dynamically from the network
or from an untagged source tree: the version-to-profile range below is reviewed
source, and the seed bytes are verified separately against the pinned release
identity and SHA-256 in ``seed_evidence.py``.

Profiles are intentionally narrow. A new minor or major release must receive an
explicit, reviewed profile entry before it can produce a stable distribution.
Until then the release workflow fails closed instead of silently trusting a
different seed.
"""
import argparse
import json
import sys
from dataclasses import dataclass

from seed_evidence import GF4, NATIVE_V21, seed_identity
from versions import VersionError, parse_version

# Tuple ranges are inclusive lower bounds and exclusive upper bounds on the
# (major, minor, patch) release version. RC versions use the same base version.
PROFILE_RANGES = (
    # The frozen GF4 seed is retained only for the already published v2.1.0
    # evidence format. It is not used for any new release.
    dict(kind=GF4, minimum=(2, 1, 0), below=(2, 1, 1),
         reason='historical v2.1.0 release evidence only'),
    # The reviewed v2.1 native seed roots the v2.1.x maintenance line. A new
    # minor/major line needs a new reviewed entry rather than inheriting this.
    dict(kind=NATIVE_V21, minimum=(2, 1, 1), below=(2, 2, 0),
         reason='reviewed native v2.1 bootstrap seed for the v2.1.x line'),
)


class BootstrapProfileMissing(VersionError):
    """No reviewed bootstrap seed profile is registered for this version."""


@dataclass(frozen=True)
class BootstrapProfile:
    kind: str
    minimum: tuple
    below: tuple
    reason: str

    def accepts(self, version):
        return self.minimum <= version.base < self.below

    @property
    def artifact(self):
        identity = seed_identity(self.kind)
        return identity.artifact if identity is not None else None

    @property
    def report_name(self):
        identity = seed_identity(self.kind)
        return identity.report_name if identity is not None else None

    def describe(self):
        return dict(kind=self.kind, minimum='%d.%d.%d' % self.minimum,
                    below='%d.%d.%d' % self.below, reason=self.reason,
                    artifact=self.artifact, report_name=self.report_name)


PROFILES = tuple(BootstrapProfile(**entry) for entry in PROFILE_RANGES)


def profiles():
    return [profile.describe() for profile in PROFILES]


def profile_for(version):
    """Return the reviewed bootstrap profile for a stable version.

    Fails closed for an unregistered future line, or for a profile whose seed
    identity has not been registered in ``seed_evidence.py``.
    """
    parsed = parse_version(version)
    if parsed.is_rc:
        raise BootstrapProfileMissing('stable release required to select a bootstrap seed profile')
    for profile in PROFILES:
        if profile.accepts(parsed):
            if seed_identity(profile.kind) is None:
                raise BootstrapProfileMissing(
                    'bootstrap seed %s is registered for Mio %s but its immutable identity is not '
                    'defined in scripts/distribution/seed_evidence.py' % (profile.kind, version))
            return profile
    raise BootstrapProfileMissing(
        'no reviewed bootstrap seed profile is registered for Mio %s; register the immutable seed '
        'pins in scripts/distribution/seed_evidence.py and its version range in '
        'scripts/distribution/bootstrap.py before releasing' % version)


def seed_kind_for(version):
    return profile_for(version).kind


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--version', required=True)
    parser.add_argument('--list', action='store_true', help='list every registered profile')
    args = parser.parse_args(argv)
    try:
        if args.list:
            result = dict(profiles=profiles())
        else:
            result = profile_for(args.version).describe()
    except VersionError as error:
        print('error: ' + str(error), file=sys.stderr)
        return 1
    json.dump(result, sys.stdout, indent=2, sort_keys=True)
    sys.stdout.write('\n')
    return 0


if __name__ == '__main__':
    sys.exit(main())
