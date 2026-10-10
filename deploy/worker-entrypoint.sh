#!/bin/sh
# Keep development source/mail selection at the container boundary.
set -eu
. "$(dirname "$0")/gateway-config.sh"
sicp_gateway_cookie
mail_enabled() {
    [ -n "${MAIL_ADDRESS:-}${MAIL_TLS:-}${MAIL_FROM:-}${MAIL_TO:-}${MAIL_USERNAME:-}${MAIL_PASSWORD:-}" ]
}
case "${ENVIRONMENT:-production}" in
  development)
    export SOURCE_ORIGIN=http://mock-source:8090 SOURCE_TOKEN=synthetic-source-token
    export SOURCE_APP_ID= SOURCE_APP_SECRET=
    export SOURCE_SCOPE=synthetic-sicp-source
    export SOURCE_QUALITY='{"dwd_bzksbjsjzlb":{"MinimumRows":1,"MaximumDeletedRows":1000,"AllowEmpty":false},"dwd_bzkscjzlb":{"MinimumRows":1,"MaximumDeletedRows":1000,"AllowEmpty":false},"dwd_bzksgrtxsjzlb":{"MinimumRows":1,"MaximumDeletedRows":1000,"AllowEmpty":false},"dwd_bzksjbsjzlb":{"MinimumRows":1,"MaximumDeletedRows":1000,"AllowEmpty":false},"dwd_bzkskbsjzlb":{"MinimumRows":1,"MaximumDeletedRows":1000,"AllowEmpty":false},"dwd_bzkskcsjzlb":{"MinimumRows":1,"MaximumDeletedRows":1000,"AllowEmpty":false},"dwd_bzksnjzysjzlb":{"MinimumRows":1,"MaximumDeletedRows":1000,"AllowEmpty":false},"dwd_bzkspksjzlb":{"MinimumRows":1,"MaximumDeletedRows":1000,"AllowEmpty":false},"dwd_bzksxjjbsjzlb":{"MinimumRows":1,"MaximumDeletedRows":1000,"AllowEmpty":false},"dwd_bzkszyxxsjzlb":{"MinimumRows":1,"MaximumDeletedRows":1000,"AllowEmpty":false},"dwd_grtxfszlb":{"MinimumRows":1,"MaximumDeletedRows":1000,"AllowEmpty":false},"dwd_jzgjcsjzlb":{"MinimumRows":1,"MaximumDeletedRows":1000,"AllowEmpty":false},"dwd_xqjbsjzlb":{"MinimumRows":1,"MaximumDeletedRows":1000,"AllowEmpty":false},"dwd_yxsdwjbsjzlb":{"MinimumRows":1,"MaximumDeletedRows":1000,"AllowEmpty":false}}'
    if mail_enabled; then
      export MAIL_ADDRESS=mock-mail:1025 MAIL_TLS=none
      export MAIL_FROM=worker@example.invalid MAIL_TO=operator@example.invalid
      export MAIL_USERNAME= MAIL_PASSWORD=
    fi
    ;;
  production|staging|test)
    : "${SOURCE_ORIGIN:?Configure the real upstream SOURCE_ORIGIN}"
    : "${SOURCE_APP_ID:?Configure the upstream SOURCE_APP_ID}"
    : "${SOURCE_APP_SECRET:?Configure the upstream SOURCE_APP_SECRET}"
    unset SOURCE_TOKEN
    : "${SOURCE_SCOPE:?Configure the real upstream SOURCE_SCOPE}"
    : "${SOURCE_QUALITY:?Configure the real upstream SOURCE_QUALITY}"
    source_host=${SOURCE_ORIGIN#*://}
    source_host=${source_host%%/*}
    source_host=${source_host%%:*}
    source_host=$(printf '%s' "$source_host" | tr '[:upper:]' '[:lower:]')
    case "$source_host" in
      mock|mock-source|mock-mail)
        echo 'Development source is not allowed outside development mode' >&2; exit 1 ;;
    esac
    if mail_enabled; then
      : "${MAIL_ADDRESS:?Configure the real SMTP MAIL_ADDRESS}"
      : "${MAIL_FROM:?Configure MAIL_FROM}" "${MAIL_TO:?Configure MAIL_TO}"
      mail_host=$(printf '%s' "${MAIL_ADDRESS%:*}" | tr '[:upper:]' '[:lower:]')
      case "$mail_host" in
        mock|mock-source|mock-mail) echo 'Development mail is not allowed outside development mode' >&2; exit 1 ;;
      esac
      case "${MAIL_TLS:-}" in
        tls|starttls) ;;
        *) echo 'Real SMTP requires MAIL_TLS=tls or starttls' >&2; exit 1 ;;
      esac
    fi
    ;;
  *) echo 'Unknown ENVIRONMENT' >&2; exit 1 ;;
esac
if [ "$#" -eq 0 ]; then set -- /usr/local/bin/service; fi
exec "$@"
