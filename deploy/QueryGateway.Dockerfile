FROM nginxinc/nginx-unprivileged:1.28-alpine
COPY query-gateway.conf /etc/nginx/conf.d/default.conf
COPY query-gateway-entrypoint.sh /usr/local/bin/query-gateway-entrypoint.sh
ENTRYPOINT ["/bin/sh", "/usr/local/bin/query-gateway-entrypoint.sh"]
STOPSIGNAL SIGQUIT
