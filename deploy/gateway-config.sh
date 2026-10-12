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

sicp_gateway_path() {
    case "$2" in
        '') return 0 ;;
        *[[:cntrl:]]*) printf '%s: control_character\n' "$1" >&2; return 1 ;;
        /*) return 0 ;;
        *) printf '%s: absolute_path_required\n' "$1" >&2; return 1 ;;
    esac
}

sicp_gateway_validate() {
    sicp_gateway_protocol >/dev/null || return 1
    sicp_gateway_path SICP_TLS_CERT_FILE "${SICP_TLS_CERT_FILE-}" || return 1
    sicp_gateway_path SICP_TLS_KEY_FILE "${SICP_TLS_KEY_FILE-}" || return 1
    sicp_gateway_protocol
}

sicp_gateway_port() {
    sicp_port=${SICP_GATEWAY_PORT:-18080}
    case "$sicp_port" in
        *[!0-9]*) printf '%s\n' 'SICP_GATEWAY_PORT: invalid_port' >&2; return 1 ;;
    esac
    while [ "${sicp_port#0}" != "$sicp_port" ]; do sicp_port=${sicp_port#0}; done
    if [ -z "$sicp_port" ] || [ "${#sicp_port}" -gt 5 ] || [ "$sicp_port" -gt 65535 ]; then
        printf '%s\n' 'SICP_GATEWAY_PORT: invalid_port' >&2; return 1
    fi
    printf '%s\n' "$sicp_port"
}

sicp_gateway_cookie() {
    case "${SICP_GATEWAY_MANAGED-}" in
        '') return 0 ;;
        true)
            sicp_mode=$(sicp_gateway_validate) || return 1
            if [ "$sicp_mode" = https ]; then CONSOLE_COOKIE_SECURE=true; else CONSOLE_COOKIE_SECURE=false; fi
            export CONSOLE_COOKIE_SECURE
            ;;
        *) printf '%s\n' 'SICP_GATEWAY_MANAGED: invalid_marker' >&2; return 1 ;;
    esac
}
