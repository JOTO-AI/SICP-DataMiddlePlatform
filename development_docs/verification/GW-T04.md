# GW-T04 运维顺序与失败处理

通过证据：提交 `4644ff2d0f8776dd38ef2773e752b74616ac280c`，[CI 38044353260](https://github.com/JOTO-AI/SICP-DataMiddlePlatform/actions/runs/38044353260)，unit job `114190756739` 的完整测试步骤通过。

`scripts/test_gateway_runtime.py` 的 Operations 六个实际方法：

- `test_gw_t04_01_order_and_worker_failure`：预检、停止网关、worker、网关顺序；worker 失败不继续。
- `test_gw_t04_02_renew_only_gateway_and_reject_drift`：仅续期服务集合、目标/运行模式与配置漂移拒绝。
- `test_gw_t04_03_preflight_and_url_fail_before_mutation`：非法 URL、协议/端口错配、预检失败均无变更。
- `test_gw_t04_04_recreate_failure_never_verifies`：重建非零不继续成功验证。
- `test_gw_t04_05_identity_failure_has_no_fallback`：身份失败和健康等待超时均失败、不改成 HTTP。
- `test_gw_t04_06_missing_skipped_failed_evidence_rejected`：缺失、失败或跳过的必需证据拒绝汇总通过。

该轮 Python 网关方法总数 20，全部通过（T01 8、T03 6、T04 6）。真实协议切换、可信身份/指纹比较、续期会话保留和手动失败恢复在 GW-I05 实际通过。后续 `570827a` 追加证书指纹 JSON 的缺项验证，待该提交运行结果，不修改已有通过事实。
