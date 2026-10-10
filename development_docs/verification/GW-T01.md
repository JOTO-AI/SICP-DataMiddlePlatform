# GW-T01 协议与配置规则测试记录

更新：2026-10-10。状态：**GW-T01-01—03 的四个测试方法已在 Linux CI 通过；完整 CI 因工件配额失败，尚不能进入下一小步。** GW-T01-04—06 未开发，GW-T01 整组未验收通过。

## GW-P01.01 对应测试

文件：`scripts/test_gateway_config.py`。只使用合成路径，不读取真实 `.env`、证书、私钥或上游。每次启动独立 `/bin/sh -eu`，环境仅包含固定 PATH、C locale 和用例显式配置；预期值不调用被测函数生成。

| 编号 | 实际测试方法 | 断言 |
| --- | --- | --- |
| GW-T01-01 | `GatewayProtocol.test_gw_t01_01_unset_empty_and_mixed_empty_use_http` | 两项未设置、仅一项空值及双空值均返回 0、精确输出 `http` 且无标准错误 |
| GW-T01-02 | `GatewayProtocol.test_gw_t01_02_paired_paths_and_environment_port_independence` | 成对路径返回 `https`；环境未设置/空值/development/production/staging/test，与端口未设置/空值/18080/443/18443 的组合均不改变 HTTP/HTTPS 判定 |
| GW-T01-03 | `GatewayProtocol.test_gw_t01_03_half_configuration_rejected_without_value_disclosure` | 证书或私钥单独配置、配对项未设置或空值，均返回 1、标准输出为空、错误精确限定为缺失项与 `missing_pair` |
| GW-T01-03 | `GatewayProtocol.test_gw_t01_03_whitespace_rejected_before_pair_selection` | 空格、TAB、CR、LF、VT、FF 及混合空白，与另一项未设置/空值/非空值及双空白组合均拒绝，精确输出字段名与 `whitespace_only` |

## Linux CI 执行入口与证据

现有 `.github/workflows/ci.yml` → unit → `scripts/check.sh` 执行：

```sh
sh -n deploy/gateway-config.sh
python3 -m unittest discover -v -s scripts -p 'test_gateway_config.py'
```

`sh -n` 属于语法检查，不计为单元测试。单测输出和实际退出码分别进入 `artifacts/gateway-config-tests.txt`、`artifacts/gateway-config-exit-code.txt`，随现有 `linux-unit-evidence` 工件保存。用例包含四个顶层测试方法及多个子用例，执行次数不能混算为 unittest 顶层通过数量。Linux 以外的跳过不能计为通过。

GW-T01-04—06 尚待 GW-P01.02 开发；GW-T01 整组未完成。本记录不代替真实 Nginx、挂载、权限或最终 HTTP/HTTPS 集成验收。

## 实际运行：2ea9176

- SHA：`2ea9176841a042492b11f508079391016db14da8`；[Actions 38040043167](https://github.com/JOTO-AI/SICP-DataMiddlePlatform/actions/runs/38040043167)，unit job `114178289373`。
- CI 日志中上述四个 `GatewayProtocol` 测试方法均为 `ok`，汇总为 `Ran 4 tests in 0.065s` / `OK`：通过 4、失败 0、跳过 0。`scripts/check.sh` 单测及构建步骤 success，证明受非零退出门禁保护的命令成功；独立退出码工件因上传失败未能取得，不伪造工件记录。
- 复用历史 `ci_counts.py` 统计 Go JSON 测试日志：含子用例通过记录 1482、失败 0、跳过 0；该数量不是顶层测试数量，也不包含本次四个 Python 测试方法。
- 完整 workflow 为 failure：frontend、unit、compose 的工件上传均失败，GitHub 错误为存储配额耗尽；integration 为 skipped，不计通过。须恢复工件存储后重跑并取得完整成功及工件证据。
- 查询结果与原始 Actions 日志由历史工具保存在本地忽略目录 `artifacts/ci-2ea9176841a042492b11f508079391016db14da8.json` 和 `artifacts/logs-2ea9176841a042492b11f508079391016db14da8.zip`；它们不替代未上传成功的 CI 工件，也不入 Git。
