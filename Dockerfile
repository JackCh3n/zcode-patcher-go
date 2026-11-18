# zcode-patcher 运行镜像：Go 源码多阶段构建，静态二进制 + alpine 运行。
# 构建：docker build -t ghcr.io/OWNER/zcode-patcher:VERSION .
# 运行：docker run --rm -v "C:\Program Files\ZCode\resources:/res" ghcr.io/OWNER/zcode-patcher:v1 check /res/app.asar
ARG VERSION=0.1.0

FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY scripts ./scripts
RUN cp scripts/zcode-tps.js cmd/embed/ && cp scripts/zcode-model-puller.js cmd/embed/ && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/zcode-patcher ./cmd

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S zp && adduser -S -G zp zp
COPY --from=builder /out/zcode-patcher /usr/local/bin/zcode-patcher
COPY LICENSE README.md /app/
WORKDIR /app
USER zp
LABEL org.opencontainers.image.title="zcode-patcher" \
      org.opencontainers.image.version="${VERSION}"
ENTRYPOINT ["zcode-patcher"]