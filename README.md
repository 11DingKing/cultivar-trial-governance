# 品种试验治理服务

这是一个生产级 Go 后端，用于让育种机构、试验站、品种审定专家、种子保管人员和区域规划人员在机构、区域及职责分离约束下协同推进候选品种试验。系统覆盖申报受理、资质核验、方案审批、种子与地块分配、试验执行、观测数据锁定、专家复核、结论版本发布、区域采用及撤销追溯。

## 核心保证

- 使用 SQLite 真实关系数据库和两版内嵌 migration，从空库启动并支持重启恢复。
- 资源分配在一个事务中完成种子预留、地块季次占用、allocation、申请状态和审计写入。
- 申请、种子批次、地块季次、观测批次和结论使用版本或条件更新保护并发不变量。
- 登录 Token 仅以 SHA-256 保存；支持到期拒绝、退出撤销和数据库实时鉴权。
- 角色同时受到职责、机构和区域限制，审批人与申报人、专家与申请机构之间执行职责分离。
- worker job 使用持久化租约、超时恢复、退避重试、最大次数、永久失败和 context 取消。
- 所有授权、修改、审批、发布和撤销都记录请求关联、操作者、政策引用及 before/after 证据。

## 目录

```text
cmd/server              HTTP 服务和 worker 运行入口
internal/domain         状态机、角色、资源、观测、复核及 worker 领域规则
internal/service        跨实体业务流程与事务编排
internal/store          SQLite repository、乐观更新、任务租约与查询
internal/migrate        内嵌版本化 migration
internal/auth           密码、会话 Token、退出撤销和过期校验
internal/httpapi        JSON API、中间件、稳定错误和健康检查
internal/worker         持久化后台任务运行器与业务 handler
internal/audit          审计事件工厂和请求关联
```

## 环境与启动

需要 Go 1.24.0 或更高版本。服务不依赖外部在线 API。复制 `.env.example` 中的环境变量到自己的运行环境，生产部署必须修改 bootstrap 管理员密码。

```bash
go mod download
GOTOOLCHAIN=local go run ./cmd/server
```

默认监听 `:8080`，默认数据库 DSN 是 `file:data/cultivar.db`。首次启动会执行 migration，并创建由 `BOOTSTRAP_ADMIN_EMAIL` 和 `BOOTSTRAP_ADMIN_PASSWORD` 控制的管理员。配置校验失败、migration checksum 冲突或数据库不可用时服务拒绝启动。

## HTTP API

公共端点：

- `GET /healthz`：进程存活。
- `GET /readyz`：数据库依赖就绪。
- `POST /v1/login`：登录并取得可撤销 Bearer Token。

鉴权端点包括退出、申请列表/提交/核验、方案审批、资源分配、试验启动、观测批次、观测上报、数据锁定、专家复核、结论草拟/发布、区域采用/撤销和审计查询。写入接口使用严格 JSON；申请提交必须提供 `Idempotency-Key`。错误响应统一为：

```json
{
  "error": {
    "code": "conflict",
    "message": "业务状态或资源已被其他操作改变",
    "request_id": "request_xxx"
  }
}
```

## 测试和质量检查

```bash
GOTOOLCHAIN=local go test ./... -count=1
GOTOOLCHAIN=local go test -race ./... -count=1
GOTOOLCHAIN=local go vet ./...
GOTOOLCHAIN=local go build ./...
```

测试覆盖领域状态机、事务回滚、乐观版本冲突、确定性并发竞争、SQLite 重启恢复、会话生命周期、HTTP 合同、worker 租约恢复/重试/取消、复核法定人数、结论发布和采用撤销追溯。

## Docker

根目录 `Dockerfile` 构建可直接启动的服务镜像，并保留完整 Go 工具链。默认入口是从真实 `./cmd/server` 编译出的 `/usr/local/bin/cultivar-server`：

```bash
docker build --platform linux/amd64 -t cultivar-trial-governance:amd64 .
docker run --rm -p 8080:8080 cultivar-trial-governance:amd64
curl -fsS http://127.0.0.1:8080/healthz
curl -fsS http://127.0.0.1:8080/readyz
```

`benzhi.Dockerfile` 和 `build_benzhi_docker.sh` 用于构建保留源码、依赖缓存和 Bash 入口的评测环境，具体命令见 `BENZHI_README.md`。
