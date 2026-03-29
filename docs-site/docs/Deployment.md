# 🛠️ 安装与部署

> 首次运行会在当前目录生成默认配置文件。

## 🌏 WebUI 控制面板

WebUI 集成了配置编辑、订阅分享、订阅管理，内置文件服务，检测结果分析报告，日志查看等功能，请务必使用 WebUI 作为主要管理入口

请主动修改 `config.yaml` `api-key` 作为 WebUI 访问密码，如未设置，请查看终端日志获取 `api-key`

浏览器输入 `http://localhost:8199/admin` 或 `http://127.0.0.1:8199/admin` 访问 WebUI

注意 `8199` 为默认监听端口，如已修改，请替换为实际端口

## 📦 二进制文件运行

下载 Releases 中适合的版本，解压后直接运行即可。

```powershell
./subs-check-pro.exe -f ./config/config.yaml
```

## 🖥️ 源码运行

欢迎提交 PR

```bash
git lfs install
git clone https://github.com/sinspired/subs-check-pro
cd subs-check-pro
go run . -f ./config/config.yaml
```

## 🐳 Docker 运行

> 注意：
>
> - 限制内存请使用 `--memory="500m"`。
> - 可通过环境变量 `API_KEY` 设置 Web 控制面板的 API Key。

```bash
# 基础运行
docker run -d \
  --name subs-check-pro \
  -p 8299:8299 \
  -p 8199:8199 \
  -v ./config:/app/config \
  -v ./output:/app/output \
  --restart always \
  ghcr.io/sinspired/subs-check-pro:latest

# 使用代理运行
docker run -d \
  --name subs-check-pro \
  -p 8299:8299 \
  -p 8199:8199 \
  -e HTTP_PROXY=http://192.168.1.1:7890 \
  -e HTTPS_PROXY=http://192.168.1.1:7890 \
  -v ./config:/app/config \
  -v ./output:/app/output \
  --restart always \
  ghcr.io/sinspired/subs-check-pro:latest
```

## 📜 Docker Compose

```yaml
version: "3"
services:
  subs-check-pro:
    image: ghcr.io/sinspired/subs-check-pro:latest
    container_name: subs-check-pro
    volumes:
      - ./config:/app/config
      - ./output:/app/output
    ports:
      - "8299:8299"
      - "8199:8199"
    environment:
      - TZ=Asia/Shanghai
      # - HTTP_PROXY=http://192.168.1.1:7890
      # - HTTPS_PROXY=http://192.168.1.1:7890
      # - API_KEY=subs-check-pro
    restart: always
    network_mode: bridge
```

## 🔁 使用 WatchTower 自动更新并通知

### 基础命令，每小时检查更新

```bash
docker run -d \
  --name watchtower \
  -e WATCHTOWER_POLL_INTERVAL=3600 \
  -v /var/run/docker.sock:/var/run/docker.sock \
  containrrr/watchtower subs-check-pro
```

### 配置 shoutrrr 格式的 Telegram 通知

```bash
docker run -d \
  --name watchtower \
  -e WATCHTOWER_NOTIFICATIONS=shoutrrr \
  -e WATCHTOWER_NOTIFICATION_URL=telegram://<bot_token>@telegram?channels=<chat_id> \
  -v /var/run/docker.sock:/var/run/docker.sock \
  containrrr/watchtower subs-check-pro
```

### 通过 webhook 使用 apprise 通知

```bash
docker run -d \
  --name watchtower \
  --restart always \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -e WATCHTOWER_POLL_INTERVAL=3600 \
  -e WATCHTOWER_NOTIFICATIONS=shoutrrr \
  -e WATCHTOWER_NOTIFICATION_URL="webhook://<server-ip>:8000/notify?urls=telegram://<bot_token>@telegram?chat_id=<chat_id>,mailto://user:pass@smtp.example.com/?from=watchtower@example.com&to=you@example.com" \
  containrrr/watchtower subs-check-pro
```

## 📱 安卓手机运行subs-check-pro教程

参考教程 [安卓手机运行subs-check-pro教程](android)
