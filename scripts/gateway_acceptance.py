"""Fail closed when required gateway evidence is missing, skipped or failed."""
import json
import os
import re
from pathlib import Path
import sys

REQUIRED = ('contracts', 'deployment', 'replicas', 'browser', 'rollback')


def evaluate(directory):
    results = {}
    for name in REQUIRED:
        path = directory / (name + '-exit-code.txt')
        results[name] = path.exists() and path.read_text().strip() == '0'
    try:
        report = json.loads((directory / 'browser.json').read_text())
        stats = report['stats']
        results['browser'] &= stats['expected'] == 2 and stats['unexpected'] == 0 and stats['skipped'] == 0 and stats['flaky'] == 0
    except (OSError, ValueError, KeyError, TypeError):
        results['browser'] = False
    try:
        report = json.loads((directory / 'contracts.json').read_text())
        results['contracts'] &= report['tests'] >= 6 and report['failed'] == 0 and report['skipped'] == 0
    except (OSError, ValueError, KeyError, TypeError):
        results['contracts'] = False
    try:
        certificates = json.loads((directory / 'certificates.json').read_text())
        phases = {item['phase']: item for item in certificates}
        fingerprints = [phases[phase]['verified_leaf_sha256'] for phase in ('https', 'renew')]
        results['certificate_identity'] = (all(isinstance(value, str) and re.fullmatch('[0-9a-f]{64}', value) for value in fingerprints)
                                           and fingerprints[0] != fingerprints[1]
                                           and all(phases[phase]['origin'] == 'https://sicp-gateway.test:28080' for phase in ('https', 'renew'))
                                           and phases['http']['origin'] == 'http://sicp-gateway.test:28080')
    except (OSError, ValueError, KeyError, TypeError):
        results['certificate_identity'] = False
    return results


if __name__ == '__main__':
    directory = Path('artifacts/gateway')
    results = evaluate(directory)
    passed = all(results.values())
    report = {'sha': os.environ.get('GITHUB_SHA', ''), 'run': os.environ.get('GITHUB_RUN_ID', ''),
              'passed': passed, 'checks': results}
    directory.mkdir(parents=True, exist_ok=True)
    (directory / 'acceptance.json').write_text(json.dumps(report, indent=2) + '\n')
    print('Gateway required scenarios: ' + ('passed' if passed else 'NOT passed'))
    sys.exit(0 if passed else 1)
