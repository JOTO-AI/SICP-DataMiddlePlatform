# Docker 构建解析器下载超时修复（2026-10-16）

- [x] 移除 deploy/Dockerfile 的外部 syntax 声明，避免构建前额外拉取 docker/dockerfile:1。
- [x] Code review：该文件仅使用常规多阶段构建语法；其余内容逐字保留，Go/运行时基础镜像、编译命令、迁移入口及非 root 用户均未变化。query、worker、migrate 共用此文件。
- [x] 静态检查：Docker Compose 全 profile 配置在 --no-interpolate 模式下解析通过；git diff --check 通过。本次不涉及应用逻辑或 Mock 数据，无新增单元测试。
- [ ] 服务器实际镜像构建：本机 Docker daemon 未运行，未执行镜像构建。配置解析不代表环境变量校验、镜像拉取或构建通过。

错误发生在获取 Docker Hub 的解析器镜像匿名令牌时，尚未执行迁移。移除声明后使用 BuildKit 内置解析器，但基础镜像、Go 模块及 Debian 软件包仍需网络访问；本次不宣称已修复服务器网络。

服务器拉取 feat/frontend-development 更新后，重新执行原构建/部署命令即可，无需删除容器或数据卷。若后续报错指向 golang、debian 等基础镜像且仍为 auth.docker.io 超时，应继续检查 Docker 构建器所在环境的网络/代理。
