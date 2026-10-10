#!/bin/sh
# Source this POSIX sh library from gateway/worker entrypoints.
# This function selects a protocol only; callers must also validate paths and
# (in the gateway) mounted TLS files before starting a listener.
# It prints one protocol on success, or a fixed field/category error on stderr.
# Sourcing the library does not change shell options or environment variables.
sicp_gateway_protocol() {
    case "${SICP_TLS_CERT_FILE-}" in
        ''|*[![:space:]]*) ;;
        *) printf '%s\n' 'SICP_TLS_CERT_FILE: whitespace_only' >&2; return 1 ;;
    esac
    case "${SICP_TLS_KEY_FILE-}" in
        ''|*[![:space:]]*) ;;
        *) printf '%s\n' 'SICP_TLS_KEY_FILE: whitespace_only' >&2; return 1 ;;
    esac

    if [ -z "${SICP_TLS_CERT_FILE-}" ] && [ -z "${SICP_TLS_KEY_FILE-}" ]; then
        printf '%s\n' 'http'
    elif [ -n "${SICP_TLS_CERT_FILE-}" ] && [ -n "${SICP_TLS_KEY_FILE-}" ]; then
        printf '%s\n' 'https'
    elif [ -z "${SICP_TLS_CERT_FILE-}" ]; then
        printf '%s\n' 'SICP_TLS_CERT_FILE: missing_pair' >&2
        return 1
    else
        printf '%s\n' 'SICP_TLS_KEY_FILE: missing_pair' >&2
        return 1
    fi
}
