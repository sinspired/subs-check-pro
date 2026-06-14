# 编译阶段
FROM golang AS builder

WORKDIR /src
ENV GOPROXY=https://goproxy.cn,direct
COPY go.mod go.sum ./
RUN go mod tidy

COPY . .
RUN CGO_ENABLED=0 GOEXPERIMENT=jsonv2 go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /app/subs-check-pro .

# 提取 glibc 运行时依赖和时区、证书
FROM debian:bookworm-slim AS provider

RUN apt-get update && apt-get install -y --no-install-recommends \
    libatomic1 libstdc++6 libgcc-s1 \
    ca-certificates \
    tzdata \
    && rm -rf /var/lib/apt/lists/*

# 最终镜像
FROM busybox:glibc

WORKDIR /app

# 注入时区和证书
COPY --from=provider /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=provider /usr/share/zoneinfo                 /usr/share/zoneinfo

# 注入动态链接库
COPY --from=provider /usr/lib/*-linux-*/libgcc_s.so.1   /lib/
COPY --from=provider /usr/lib/*-linux-*/libstdc++.so.6* /lib/
COPY --from=provider /usr/lib/*-linux-*/libatomic.so.1* /lib/
COPY --from=provider /usr/lib/*-linux-*/libdl.so.2      /lib/

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

# 确保复制的二进制文件具有执行权限
COPY --from=builder --chmod=755 /app/subs-check-pro /app/subs-check-pro

EXPOSE 8199
EXPOSE 8299

CMD ["/app/subs-check-pro"]