# GW-T01 协议与配置规则测试记录

更新：2026-10-10。状态：**测试代码已编写，未执行，未验收通过。** 当前无提交 SHA、Actions 运行链接、退出码或通过/失败/跳过统计，不能据此进入下一小步。

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

待运行后补录：实际提交 SHA、Actions 链接、上述命令退出码、通过/失败/跳过数量及完整 CI 结果。GW-T01-04—06 尚待 GW-P01.02 开发；GW-T01 整组未完成。本记录不代替真实 Nginx、挂载、权限或最终 HTTP/HTTPS 集成验收。
