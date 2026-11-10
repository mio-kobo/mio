#!/usr/bin/env python3
"""Track the public CCR state of the two v2.1.0 packages in the release issue."""
import json
import os
import re
import subprocess
import xml.etree.ElementTree as ET

from chocolatey_publish import ODATA, PACKAGE_NAMES, REPAIR_VERSION, metadata_body


MARKER = '<!-- mio-ccr-2.1.0 -->'
STATUS_FIELDS = {
    'PackageValidationResultStatus': 'validation',
    'PackageTestResultStatus': 'installation verification',
    'PackageScanStatus': 'scan',
    'PackageScanFlagResult': 'scan severity',
}


def status(name):
    body = metadata_body(name, REPAIR_VERSION)
    if body is None:
        return 'Record unavailable', {}
    entry = ET.fromstring(body)

    def field(key):
        value = entry.findtext(f'm:properties/d:{key}', default='', namespaces=ODATA)
        if not re.fullmatch(r'[a-zA-Z0-9]*', value):
            raise ValueError(f'Unexpected CCR field {key} for {name}')
        return value

    if (entry.tag != '{' + ODATA['a'] + '}entry'
            or entry.findtext('a:title', namespaces=ODATA) != name
            or field('IsApproved') not in ('true', 'false')
            or entry.findtext('m:properties/d:Version', namespaces=ODATA) != REPAIR_VERSION):
        raise ValueError(f'Unexpected CCR record for {name}')
    state = field('PackageStatus')
    approved = field('IsApproved') == 'true'
    if state == 'Submitted':
        state = field('PackageSubmittedStatus') or state
    if not state or approved != (state == 'Approved'):
        raise ValueError(f'Unexpected CCR moderation state for {name}')
    return state, {label: field(key) for key, label in STATUS_FIELDS.items()}


def gh(*args):
    return subprocess.check_output(['gh', *args], text=True)


def main():
    issue = os.environ['TRACKING_ISSUE']
    endpoint = f"repos/{os.environ['GITHUB_REPOSITORY']}/issues/{issue}/comments"
    rows = [MARKER, 'Chocolatey Community Repository status for v' + REPAIR_VERSION]
    states = []
    for name in PACKAGE_NAMES:
        state, checks = status(name)
        states.append(state)
        link = f'https://community.chocolatey.org/packages/{name}/{REPAIR_VERSION}'
        details = ', '.join(f'{label} {value}' for label, value in checks.items() if value)
        rows.append(f'- [{name}]({link}) **{state}**' + (f' ({details})' if details else ''))
    snapshot = '\n'.join(rows)
    print(snapshot)

    pages = json.loads(gh('api', '--paginate', '--slurp', endpoint))
    previous = next(
        (comment['body'].split('\n\n', 1)[0]
         for page in reversed(pages) for comment in reversed(page)
         if comment['user']['login'] == 'github-actions[bot]' and comment['body'].startswith(MARKER + '\n')),
        None,
    )
    changed = snapshot != previous
    if changed:
        run = f"{os.environ['GITHUB_SERVER_URL']}/{os.environ['GITHUB_REPOSITORY']}/actions/runs/{os.environ['GITHUB_RUN_ID']}"
        gh('api', endpoint, '--method', 'POST', '-f', f'body={snapshot}\n\n[Workflow run]({run})')

    verify = all(state == 'Approved' for state in states) and (
        changed or os.environ['GITHUB_EVENT_NAME'] == 'workflow_dispatch'
    )
    with open(os.environ['GITHUB_OUTPUT'], 'a') as output:
        print(f'verify={str(verify).lower()}', file=output)


if __name__ == '__main__':
    main()
