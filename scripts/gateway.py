#!/usr/bin/env python3
"""Gateway preflight, ordered protocol changes and certificate renewal."""
import argparse
import sys
from gateway_runtime import (Compose, GatewayConfig, GatewayError, apply_gateway, client_origin,
                             expected_fingerprint, target_origin, tls_context, validate, verify_entry)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('operation', choices=('validate', 'verify', 'origin', 'gateway-apply', 'gateway-renew'))
    parser.add_argument('--env-file')
    parser.add_argument('--url')
    parser.add_argument('--ca-file')
    args = parser.parse_args()
    compose = Compose(env_file=args.env_file)
    try:
        if args.operation == 'origin':
            print(client_origin(GatewayConfig.from_model(compose.model()), args.url))
        elif args.operation == 'validate':
            config = validate(compose)
            print('Gateway preflight passed: ' + config.mode)
        elif args.operation == 'verify':
            config = GatewayConfig.from_model(compose.model())
            origin = target_origin(config, client_origin(config, args.url))
            verify_entry(origin, tls_context(args.ca_file), expected_fingerprint(compose) if config.mode == 'https' else None)
            print('Gateway verified: ' + origin)
        else:
            if not args.url:
                raise GatewayError('url: required')
            origin = apply_gateway(compose, args.operation, args.url, args.ca_file)
            print('Gateway verified: ' + origin)
        return 0
    except (GatewayError, ValueError, KeyError, TypeError) as error:
        # Only our fixed-category errors are safe to print.
        detail = str(error) if isinstance(error, GatewayError) else 'configuration: invalid'
        print(detail + '; operation failed, no automatic protocol fallback', file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
