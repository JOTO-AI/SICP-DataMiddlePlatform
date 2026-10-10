#!/bin/sh
set -eu
. "$(dirname "$0")/gateway-config.sh"
case "${1-}" in
    ''|--check-config) ;;
    *) echo 'query-gateway: invalid_argument' >&2; exit 1 ;;
esac
[ "$#" -le 1 ] || { echo 'query-gateway: invalid_argument' >&2; exit 1; }
QUERY_REPLICAS=${QUERY_REPLICAS-1}
case "${QUERY_REPLICAS-1}" in
    ''|*[!0-9]*) echo 'QUERY_REPLICAS must be an integer from 1 to 32' >&2; exit 1 ;;
esac
if [ "${#QUERY_REPLICAS}" -gt 2 ] || [ "$QUERY_REPLICAS" -lt 1 ] || [ "$QUERY_REPLICAS" -gt 32 ]; then
    echo 'QUERY_REPLICAS must be an integer from 1 to 32' >&2
    exit 1
fi
mode=$(sicp_gateway_validate)
if [ "$mode" = https ]; then
    for file in /etc/nginx/tls/fullchain.pem /etc/nginx/tls/privkey.pem; do
        if [ ! -f "$file" ] || [ ! -r "$file" ]; then
            echo 'query-gateway: TLS file must be a readable regular file' >&2; exit 1
        fi
    done
fi
umask 077
config_dir=/tmp/sicp-nginx
mkdir -p "$config_dir/client" "$config_dir/proxy"
# Concatenate fixed fragments; never substitute Nginx runtime variables.
templates=/etc/nginx/sicp
{
    cat "$templates/main.conf"
    printf '\nserver {\n'
    cat "$templates/$mode.conf" "$templates/routes.conf"
    printf '\n}\n}\n'
} > "$config_dir/nginx.conf"
# EOF and a deadline prevent encrypted keys waiting for an interactive password.
if ! timeout 15 nginx -t -c "$config_dir/nginx.conf" </dev/null >"$config_dir/check.log" 2>&1; then
    echo 'query-gateway: nginx_config_invalid_or_timeout' >&2
    exit 1
fi
[ "${1-}" != --check-config ] || exit 0
exec nginx -c "$config_dir/nginx.conf" -g 'daemon off;'
