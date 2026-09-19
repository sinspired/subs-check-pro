# 📱 安卓手机运行 Subs-Check-Pro 教程

⚠️ Android 手机 APP 已发布，请至 [Subs Free](https://github.com/sinspired/subs-free) 下载使用

> 使用 Termux

## 前置条件

- 确保网络连接正常
- 建议使用 Android 7.0 及以上系统
- 你有一定的技术/折腾能力，小白误入

## ⚠️ 注意

- **v2 内核**需要安装并配置 `nodejs`
- **v3 内核**无需安装 `nodejs`，更轻量

## 安装依赖

### v2 内核

```bash
pkg update && pkg add nodejs ca-certificates which proot termux-exec -y
```

### v3 内核

```bash
pkg update && pkg add ca-certificates which proot termux-exec -y
```

## 切换环境

每次打开终端运行 subs-check-pro 前，建议进入完整的 Linux 环境：

```bash
termux-chroot
```

如遇到 DNS 问题，可修改 `/etc/resolv.conf`：

```bash
echo "nameserver 223.5.5.5" > /etc/resolv.conf
```

## 设置环境变量

### v2 内核

```bash
# 临时设置
export SSL_CERT_FILE="/data/data/com.termux/files/usr/etc/tls/cert.pem"
export NODEBIN_PATH="$(which node)"

# 持久设置
echo 'export SSL_CERT_FILE="/data/data/com.termux/files/usr/etc/tls/cert.pem"' >> ~/.bashrc
echo 'export NODEBIN_PATH="$(which node)"' >> ~/.bashrc
source ~/.bashrc
```

### v3 内核

```bash
# 临时设置
export SSL_CERT_FILE="/data/data/com.termux/files/usr/etc/tls/cert.pem"

# 持久设置
echo 'export SSL_CERT_FILE="/data/data/com.termux/files/usr/etc/tls/cert.pem"' >> ~/.bashrc
source ~/.bashrc
```

## 运行程序

```bash
./subs-check-pro
```

## 常见问题

1. **证书错误** → 确保已正确设置 `SSL_CERT_FILE`
2. **权限不足** → 执行 `chmod 755 subs-check-pro`
3. **DNS 问题** → 修改 `/etc/resolv.conf`
4. **v2 内核找不到 node** → 确保已正确设置 `NODEBIN_PATH`
