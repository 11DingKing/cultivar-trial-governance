# syntax=docker/dockerfile:1
# 依赖下载固定在构建主机架构执行，避免跨架构模拟器参与网络请求。
FROM --platform=$BUILDPLATFORM golang:1.24.0 AS dependencies

ARG GOPROXY=https://proxy.golang.com.cn,direct

WORKDIR /deps
COPY go.mod go.sum ./
RUN --mount=type=cache,id=cultivar-go-mod,target=/go/pkg/mod GOPROXY=$GOPROXY go mod download \
  && mkdir -p /out/pkg \
  && cp -a /go/pkg/mod /out/pkg/mod

# 最终镜像保持指定目标架构，并提供完整 Go 工具链和源码。
FROM golang:1.24.0

WORKDIR /app
COPY --from=dependencies /out/pkg/mod /go/pkg/mod
COPY . .

# 依赖已复制到镜像，预编译不再访问网络。
RUN GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go build ./...

CMD ["bash"]
