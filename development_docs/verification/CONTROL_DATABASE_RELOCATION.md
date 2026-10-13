# Control 数据库位置调整验证

日期：2026-10-13。目标：将固定 control 数据库从 db-m 迁到 db-log，与两个日志库共用实例；HTTP 契约和 M/S 业务槽位不变。

## 本地验证

- `go test ./internal/storage ./internal/storage/migrate ./internal/config`：对应包在本次组合命令中通过，覆盖新 control 端点、物理隔离、迁移计划和配置校验。
- `go test ./internal/observability -run '^TestControlRelocationMarkerContract$' -count=1`：通过。Go 运行时代码与 Python 迁移脚本共享旧/新拓扑 HMAC 测试向量，防止标记编码不兼容。
- `python -m unittest discover -s scripts -p test_migrate_control.py`：7 项通过，覆盖恢复失败后续跑、恢复摘要不一致、非空目标拒绝覆盖、备份损坏、源身份被替换、显式删除和重复收尾、配置保留与日志标记绑定。数据库调用使用模拟对象，此结果不是真实 PG 迁移验收。
- `scripts/check-compose-config.py`：通过，验证生产/开发配置、默认 db-log/control 目标、凭据分配、持久目录与迁移启动顺序。
- Go 三个服务入口以 `-buildvcs=false` 编译通过；assembly 测试二进制编译通过。该参数仅绕过本地沙箱 Git ownership 导致的 VCS stamping 错误，不改变应用逻辑。
- 修改的 Bash 脚本语法、Python 解析、gofmt 和 `git diff --check` 检查通过。

## 未通过或未执行的检查

`go test ./internal/assembly` 在 Windows 主机失败，错误为 `PERSISTENT_DIRECTORY: invalid or missing runtime configuration`。测试把路径改为 Windows `t.TempDir()`，而生产配置验证要求 POSIX 绝对路径并拒绝反斜杠。没有把这些装配测试记为通过，也未为本次迁移放宽生产路径规则。

本机 Docker daemon 不可用，尚未执行真实 PostgreSQL dump/restore、文件锁崩溃恢复、旧库 DROP 或前端在线验收。正式执行前应在同版本 Linux 隔离环境演练 [迁移方案](../../deploy/CONTROL_DATABASE_MIGRATION.md)中的完整流程，并验证前端任务列表、详情、通知配置及业务查询。

## 代码复核

本轮由主代理复核，未使用子代理。已处理：

- 保留旧 control 地址的迁移兼容，同时允许新逻辑库与日志共用实例；仍拒绝同一物理日志/控制库和日志别名指向业务实例。
- 恢复到临时库并核验表、序列、schema/权限，避免覆盖已有目标。
- 只有成功启用新库、保留有效备份且旧库处于封存状态时才删除旧 control。
- 临时库重新恢复前验证已登记的物理身份；切换或启动阶段重试不重新覆盖目标数据。
- 配置和日志标记原子更新，保留全部事件文件及锁 inode，失败后不自动丢弃新库写入并回滚。
- 拒绝远程 Docker、活跃挂载内的备份目录及未知日志拓扑；原 `.env` 和备份仅写入私有目录，不输出密码。
- 网关旧版本回滚夹具显式保持旧 M 控制布局，避免用不支持新布局的旧二进制回滚已迁移部署。

真实 `.env` 的控制地址未修改，数据库迁移和删除未执行。由迁移脚本在成功校验后切换现有配置。
