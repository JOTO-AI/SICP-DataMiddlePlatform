"""Remove fixture credentials from diagnostic streams and browser reports."""
from pathlib import Path
import re
import sys

SYNTHETIC_VALUES = ('synthetic-admin-password', 'synthetic-query-secret', 'synthetic-auth',
                    'synthetic-key', 'synthetic-cookie', 'synthetic-csrf', 'synthetic-only')


def redact(value):
    for secret in SYNTHETIC_VALUES:
        value = value.replace(secret, '[redacted]')
    return re.sub(r'-----BEGIN [^-]*PRIVATE KEY-----.*?-----END [^-]*PRIVATE KEY-----',
                  '[redacted private key]', value, flags=re.S)


if __name__ == '__main__':
    if len(sys.argv) == 2:
        directory = Path(sys.argv[1])
        for path in directory.rglob('*'):
            if path.is_file() and path.suffix in ('.json', '.txt', '.md'):
                path.write_text(redact(path.read_text()), encoding='utf-8')
    else:
        for line in sys.stdin:
            print(redact(line), end='', flush=True)
