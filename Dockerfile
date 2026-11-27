# 适配 Glibc 动态链接
FROM golang:1.27-bookworm AS builder

# Debian 镜像地址，可通过构建参数替换
ARG DEBIAN_MIRROR=https://mirrors.tuna.tsinghua.edu.cn

# 安装 UPX
RUN find /etc/apt -type f \( -name '*.list' -o -name '*.sources' \) \
    -exec sed -i \
    -e "s|https\?://deb\.debian\.org/debian|${DEBIAN_MIRROR}/debian|g" \
    -e "s|https\?://security\.debian\.org/debian-security|${DEBIAN_MIRROR}/debian-security|g" \
    {} + \
    && apt-get update \
    && apt-get install -y --no-install-recommends wget xz-utils \
    && wget https://github.com/upx/upx/releases/download/v4.2.2/upx-4.2.2-amd64_linux.tar.xz \
    && tar -xf upx-4.2.2-amd64_linux.tar.xz \
    && mv upx-4.2.2-amd64_linux/upx /usr/local/bin/upx \
    && rm -rf upx-4.2.2-amd64_linux* /var/lib/apt/lists/*

WORKDIR /src
ENV GOPROXY=https://goproxy.cn,direct

COPY go.mod go.sum ./
RUN go mod tidy
COPY . .

RUN CGO_CFLAGS="-Os -g0" CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
    go build -trimpath \
    -ldflags="-s -w" \
    -o /app/subs-check-pro .

# 执行 UPX 压缩
RUN upx -5 /app/subs-check-pro

# 基础镜像使用带有 glibc 的 busybox
FROM busybox:glibc

# 复制证书和时区
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo/Asia/Shanghai /etc/localtime
COPY --from=builder /usr/share/zoneinfo/Asia/Shanghai /usr/share/zoneinfo/Asia/Shanghai

WORKDIR /app
ENV TZ=Asia/Shanghai
ENV RUNNING_IN_DOCKER=true

# 镜像描述标签
LABEL org.opencontainers.image.title="subs-check-pro" \
    org.opencontainers.image.description="高性能代理检测筛选工具，支持高并发测活、测速、媒体检测"

COPY --from=builder /app/subs-check-pro /app/subs-check-pro

EXPOSE 8199 8299
CMD ["/app/subs-check-pro"]
