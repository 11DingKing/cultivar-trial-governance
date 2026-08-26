# BENZHI_README

这是一个面向育种机构、试验站、审定专家和区域规划人员的 Go 品种试验治理后端，用于在机构与区域隔离下协同完成申报、资源分配、观测、复核、发布、采用和撤销追溯。

## 环境要求

- Go 1.24.0 或兼容的更高版本。
- 项目 module 为 `github.com/11DingKing/cultivar-trial-governance`。
- 核心持久化使用内嵌 SQLite，无需连接在线服务。

## 标准构建、运行和测试命令

进入容器或项目根目录后执行：

```bash
# 编译
GOTOOLCHAIN=local go build ./...

# 启动
GOTOOLCHAIN=local go run ./cmd/server

# 测试
GOTOOLCHAIN=local go test ./... -count=1
GOTOOLCHAIN=local go test -race ./... -count=1
GOTOOLCHAIN=local go vet ./...
```

服务默认监听 `:8080`，数据库默认位于 `data/cultivar.db`。可通过 `HTTP_ADDR`、`DATABASE_DSN`、`SESSION_TTL`、worker 相关环境变量和 bootstrap 管理员环境变量覆盖默认配置。启动后可访问 `/healthz` 检查存活状态，访问 `/readyz` 检查数据库就绪状态。

## Docker 构建和进入容器

`benzhi.Dockerfile` 保留完整 Go 工具链、下载后的 module cache 和项目源码，默认进入 Bash，便于在容器内编译、测试和检查代码：

```bash
chmod +x build_benzhi_docker.sh
./build_benzhi_docker.sh cultivar-trial-governance-amd64 linux/amd64
./build_benzhi_docker.sh cultivar-trial-governance-arm64 linux/arm64
docker run -it cultivar-trial-governance-amd64
docker run -it --platform linux/arm64 cultivar-trial-governance-arm64
```

项目运行镜像使用根目录 `Dockerfile`，默认入口会启动 `./cmd/server` 构建出的服务：

```bash
docker build --platform linux/amd64 -t cultivar-trial-governance:amd64 .
docker run --rm -p 8080:8080 cultivar-trial-governance:amd64
```
