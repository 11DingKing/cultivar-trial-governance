# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.24.0 AS build

ARG TARGETOS
ARG TARGETARCH
ARG GOPROXY=https://proxy.golang.com.cn,direct

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,id=cultivar-go-mod,target=/go/pkg/mod GOPROXY=$GOPROXY go mod download

COPY . .
RUN --mount=type=cache,id=cultivar-go-mod,target=/go/pkg/mod \
  CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOTOOLCHAIN=local \
  go build -trimpath -o /out/cultivar-server ./cmd/server

FROM alpine:3.21

RUN apk add --no-cache ca-certificates \
  && addgroup -S cultivar \
  && adduser -S -G cultivar cultivar \
  && mkdir -p /app/data \
  && chown -R cultivar:cultivar /app

WORKDIR /app
COPY --from=build /out/cultivar-server /usr/local/bin/cultivar-server

ENV HTTP_ADDR=:8080
ENV DATABASE_DSN=file:/app/data/cultivar.db

USER cultivar
EXPOSE 8080

HEALTHCHECK --interval=5s --timeout=3s --start-period=5s --retries=10 \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1

CMD ["/usr/local/bin/cultivar-server"]
