# 前端测试数据基线 v1

仅供开发和测试，禁止从 `src/` 导入。正常查询输出存于 `v1/business.json`，来自未改写的 `mock_data/snapshots/initial` 经真实后端 HTTP Pipeline 的导出；14 源成功及超末页空响应直接引用原有文件，不复制或重新拼装源记录。

使用 `backendCapture(operation, scenario)` 取得请求、响应及实际响应头，`sourceResponse(table, scenario)` 返回原始文本以保留数字字面量。每次业务样例调用返回独立副本。目录来自后端 Catalog，目录 request_example/success_example 仅为接口文档示例，不作为正常业务查询结果。

场景范围：九接口各有 success/empty/invalid_argument，七分页接口另有 snapshot_expired，共 34 条；null 来自实际成功响应；unknown 使用后端目录 readiness 及现有 supplemental/unknown_business_code；缺失/null/空字符串/字面 null 直接引用已有 supplemental 输入和预期。源协议故障继续引用 mock_data/invalid_responses 中已有场景，不把错误源对象塞入正常快照。

`manifest.json` 固定原始 mock、捕获文件、后端查询/目录代码、既有 I/O 测试适配器和独立预期的 SHA-256。`check-fixtures.mjs` 对照已存在的 querymock 固定预期检查组织、学生、教职工、教学班和名单字段及人数关联。导出文件本身不是独立业务正确性预期；成绩/课表等完整业务验收仍由后端原有测试负责。

从仓库根目录校验：

```powershell
node frontend/scripts/check-fixtures.mjs
```

只有后端契约/原始 mock 经复核改变时才显式重导出，不能为了通过前端断言修改样例：

```powershell
$env:SICP_FRONTEND_FIXTURE_OUTPUT = Join-Path (Get-Location) 'frontend/fixtures/v1'
go test ./internal/query/handlers -run '^TestFrontendFixtureExport$' -count=1
node frontend/scripts/check-fixtures.mjs --write
```

导出器默认跳过，只在显式指定输出时运行；使用既有内存 I/O 适配器和固定时钟，不连接数据库、Redis、上游或邮箱。此命令只执行前端夹具导出入口，不恢复暂停的 T/OI 套件，也不等同真实网关/部署验收。Go 版本按根目录 go.mod；当前本地可使用 `artifacts/toolchain/go/bin/go.exe`，缓存位于 artifacts，均不提交。

现有 console.spec.ts 的手写协议/边界场景保留，后续页面迁移时正常路径改用本目录；不得通过删掉有效旧测试回避回归。

## 分页链样本（F06-08）

`v1/pagination.json` 另存七个分页接口的真实同快照链，共 82 条（68 页正常结果、7 条改页大小冲突、7 条原组合不可读后的快照冲突）。使用 `paginationCapture(operation, page)` 或场景名 `snapshot_conflict` / `snapshot_expired` 读取。所有正常页都由现有内存后端管线从原始 mock 生成，每页 1 条；校验器核对合并结果与原 34 条基线中的成功响应逐行一致，原基线与原始 mock 未改写。

新增入口只导出此分页文件，不重新生成已有 `business.json`：

```powershell
$env:GOCACHE = Join-Path $PWD 'artifacts/gocache'
$env:GOMODCACHE = Join-Path $PWD 'artifacts/gomodcache'
$env:GOTOOLCHAIN = 'local'
$env:GOPROXY = 'off'
$env:SICP_FRONTEND_PAGINATION_OUTPUT = Join-Path $PWD 'frontend/fixtures/v1'
& './artifacts/toolchain/go/bin/go.exe' test ./internal/query/handlers -run '^TestFrontendPaginationFixtureExport$' -count=1
node frontend/scripts/check-fixtures.mjs --write
```

仅执行这个显式导出入口，不运行暂停的 T/OI 套件、不连接外部服务。新增分页样本/导出器变更须复核后才更新 149 项来源哈希；普通测试运行使用不带 `--write` 的校验命令。


## 日志联动夹具（F06-12b）

logTests.ts 复用 operations_http_examples.json 的 log_history 空页和 log_detail 既有事件，将详情的 EventItem 基础字段投影为列表项。查询范围、筛选回显、游标和高水位仅作为协议场景元数据调整；不新增日志业务记录、不声称这些调整是实测数据库响应。源日志使用已有空页，正常业务查询与源测试继续使用既有 captures。
