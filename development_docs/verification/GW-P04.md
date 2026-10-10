# GW-P04 切换续期与交付复核

2026-10-10：GW-P04.01—02 实现、测试代码及静态复核完成；运行与验收结果待 Linux CI。

`gateway-apply --url` 在 URL/CA、Compose/应用/网关预检完成后，按停网关 → 重建 worker 并等待 → 重建网关并等待 → 校验外部页面的顺序执行。HTTPS 比较同一可信连接上的叶证书指纹与目标挂载。每项失败非零退出，不改配置、不删卷、不自动开放 HTTP。首次 `deploy` 也执行外部入口验证。

`gateway-renew` 先确认运行网关/worker 均为受管理 HTTPS，比较 worker/query/console/gateway 配置 hash，拒绝混入其他服务的配置更新；预检后仅重建网关，重新绑定原子替换文件。仅重启进程不是续期流程。

`test_gateway_runtime.py` 通过命令替身断言调用集合、顺序和失败后停止；覆盖非法 URL、端口 0、配置 hash 漂移、预检失败、重建失败、身份失败及验收缺项/跳过。Linux 真实部署用 `gateway_deployment_checks.py`、独立 `playwright.gateway.config.ts` 和 `gateway_rollback_check.py` 验证双向切换、保留旧浏览器、退出与未退出场景、证书原子替换、会话保留、预检保留旧服务、失败中断/手动恢复和固定旧版本整套回退。新 CA、私钥和 .env 位于临时目录，不上传；浏览器不保存 trace、视频、截图或 storage state。

CI `gateway` 作业独立于 unit 的工件上传状态，实际测试失败仍使其失败。`gateway_acceptance.py` 要求 contracts/deployment/replicas/browser/rollback 五组退出码、至少六项真实容器用例及两项未跳过浏览器测试，绑定 SHA/run 输出小型验收 JSON。报告不能将缺失证据记作成功。

限制：单网关重建存在停机窗口；无事务式容器回滚。HTTPS → HTTP 须处理 Secure Cookie/HSTS。回退不删除数据，运行证据通过后才能判定专项验收完成。
