# GW-P02 网关模板与启动复核

2026-10-10：GW-P02.01、GW-P02.02 实现、测试代码及静态复核完成，运行测试待本次提交的 Linux CI。

完整配置写入 `/tmp/sicp-nginx/nginx.conf`，共用 main/routes/headers/management 片段，外部选择 HTTP 或 HTTPS，内部 8080 仅保留 query/探针。Nginx 运行变量不做环境替换，证书始终引用固定容器路径。启动检查与正式启动使用同一完整配置，UID/GID 65532、只读根目录、16 MiB tmpfs。

HTTPS 先检查两个挂载为可读普通文件，再限时 15 秒执行 `nginx -t`、关闭标准输入；失败只输出固定类别，不降级。`--check-config` 不发布端口、不启动监听。副本边界保留 1—32；普通启动最终 exec Nginx，信号沿用 SIGQUIT。

复核项：相对重定向由模式片段中的 `absolute_redirect off` 保证；移除重复指令，避免 Nginx 拒绝配置。管理 location 每项均包含完整可信转发头配置并清除 API key/Authorization，防止 Nginx 继承规则漏项。变量上游保留原 URI，动态 DNS 2 秒；响应落盘关闭，POST 未启用 non_idempotent 重试。日志仅记录后端地址/状态。

`scripts/gateway_container_checks.py` 的六个方法覆盖 GW-T02-01—06，同时提供 GW-I02/I03/I04 的合成上游证据。真实容器覆盖两套模板、内外路由白名单、头部、无依赖预检、无效/不匹配/加密/不可读证书、错误域名/CA/过期、请求限制、35 秒超时、两个上游的 POST 断开不重放和 16 MiB 慢读响应。对应退出码与统计由独立 gateway 作业收集；尚无本次运行结果。

回退必须与 Compose、worker 和 console 成套恢复，不删除数据。

最终静态复核补齐 FastCGI/uWSGI/SCGI 模块默认临时目录到 `/tmp`，避免完整主配置绕过基础镜像配置后，在只读根目录初始化默认缓存路径失败；模板目录固定为镜像内路径，不额外暴露环境覆盖入口。
