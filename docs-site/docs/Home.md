# Subs-Check⁺ PRO

高性能代理订阅检测器，支持测活、测速、媒体解锁，PC/移动端友好的现代 WebUI，自动生成 Mihomo/Clash 与 sing-box 订阅，集成 sub-store，支持一键分享与无缝自动更新。

![preview](https://sinspired.github.io/subs-check-pro/img/Subs-Check-PRO_OG.png)

## ⚡️ 快速入口

- 🧭 [入门与部署](Deployment)
- 📘 [Cloudflare Tunnel 外网访问](Cloudflare-Tunnel)
- 🚀 [自建测速地址](Speedtest)
- ✨ [新增功能与性能优化](Features-Details)
- 📙 [订阅使用方法](Subscriptions)
- 📕 [内置文件服务](File-Service)
- 📗 [通知渠道（Apprise）](Notifications)
- 🚦 [系统与 GitHub 代理](System-Proxy)
- 💾 [保存方法](Storage)

## 🚀 快速开始

### 🌏 WebUI 控制面板

WebUI 集成了配置编辑、订阅分享、订阅管理，内置文件服务，检测结果分析报告，日志查看等功能，请务必使用 WebUI 作为主要管理入口

请主动修改 `config.yaml` `api-key` 作为 WebUI 访问密码，如未设置，请查看终端日志获取 `api-key`

浏览器输入 `http://localhost:8199/admin` 或 `http://127.0.0.1:8199/admin` 访问 WebUI

注意 `8199` 为默认监听端口，如已修改，请替换为实际端口

### 📦 二进制文件运行

下载 Releases 中适合的版本，解压后直接运行即可。

```powershell
./subs-check-pro.exe -f ./config/config.yaml
```

### 🐳 Docker（最简）

```bash
docker run -d \
  --name subs-check-pro \
  -p 8299:8299 \
  -p 8199:8199 \
  -v ./config:/app/config \
  -v ./output:/app/output \
  --restart always \
  ghcr.io/sinspired/subs-check-pro:latest
```

- 配置示例：
  - [查看默认配置](https://github.com/sinspired/subs-check-pro/blob/main/config/config.yaml.example)

## 👥 社区

- Telegram 群组：[加入群组](https://t.me/subs_check_pro)
- Telegram 频道：[关注频道](https://t.me/sinspired_ai)

## 🤝 贡献

欢迎提交 PR 与 Issue。如果要本地开发，请注意仓库使用 Git LFS 管理大文件：

```bash
git lfs install
git clone https://github.com/sinspired/subs-check-pro
cd subs-check-pro
# 如已克隆后再启用 LFS：
git lfs pull
```

更多文档请通过左侧侧边栏或以上入口访问对应页面
