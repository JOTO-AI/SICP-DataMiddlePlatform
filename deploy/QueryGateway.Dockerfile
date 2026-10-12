FROM nginxinc/nginx-unprivileged:1.28-alpine
COPY gateway /etc/nginx/sicp
COPY gateway-config.sh /usr/local/bin/gateway-config.sh
COPY query-gateway-entrypoint.sh /usr/local/bin/query-gateway-entrypoint.sh
ENTRYPOINT ["/bin/sh", "/usr/local/bin/query-gateway-entrypoint.sh"]
STOPSIGNAL SIGQUIT
