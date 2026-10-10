"""Fail closed when required gateway evidence is missing, skipped or failed."""
import json
import os
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
