# 适配 Glibc 动态链接
FROM golang:1.27-bookworm AS builder

# 安装 UPX（Debian 系列包名是 upx-ucl）
RUN apt-get update -o Acquire::Check-Valid-Until=false \
    && apt-get install -y --no-install-recommends wget xz-utils \
    && wget https://github.com/upx/upx/releases/download/v4.2.2/upx-4.2.2-amd64_linux.tar.xz \
    && tar -xf upx-4.2.2-amd64_linux.tar.xz \
    && mv upx-4.2.2-amd64_linux/upx /usr/local/bin/upx \
    && rm -rf upx-4.2.2-amd64_linux*

WORKDIR /src

ENV GOPROXY=https://goproxy.cn,direct

COPY go.mod go.sum ./
RUN go mod tidy
COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath \
    -ldflags="-s -w" \
    -o /app/subs-check-pro .

# 执行 UPX 压缩
RUN upx -5 /app/subs-check-pro

# 提取运行时依赖
FROM debian:trixie-slim AS provider

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
    libatomic1 \
    libstdc++6 \
    libgcc-s1 \
    ca-certificates \
    tzdata \
    && rm -rf /var/lib/apt/lists/*


# 最终运行镜像
FROM busybox:glibc

WORKDIR /app

# 证书和时区
COPY --from=provider /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=provider /usr/share/zoneinfo/Asia/Shanghai /etc/localtime
COPY --from=provider /usr/share/zoneinfo/Asia/Shanghai /usr/share/zoneinfo/Asia/Shanghai

# Node.js 运行时依赖
COPY --from=provider /usr/lib/*-linux-*/libgcc_s.so.1 /lib/
COPY --from=provider /usr/lib/*-linux-*/libstdc++.so.6* /lib/
COPY --from=provider /usr/lib/*-linux-*/libatomic.so.1* /lib/
COPY --from=provider /usr/lib/*-linux-*/libdl.so.2 /lib/
COPY --from=provider /usr/lib/*-linux-*/librt.so.1 /lib/

ENV TZ=Asia/Shanghai
ENV RUNNING_IN_DOCKER=true

# 镜像描述标签
LABEL org.opencontainers.image.title="subs-check-pro" \
    org.opencontainers.image.description="高性能代理检测筛选工具，支持高并发测活、测速、媒体检测" \
    org.opencontainers.image.url="https://github.com/sinspired/subs-check-pro" \
    org.opencontainers.image.source="https://github.com/sinspired/subs-check-pro" \
    org.opencontainers.image.documentation="https://github.com/sinspired/subs-check-pro/wiki" \
    org.opencontainers.image.vendor="sinspired" \
    org.opencontainers.image.authors="Sinspired https://github.com/sinspired"

# Go 主程序
COPY --from=builder --chmod=755 /app/subs-check-pro /app/subs-check-pro

EXPOSE 8199 8299
CMD ["/app/subs-check-pro"]
