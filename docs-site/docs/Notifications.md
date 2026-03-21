# 🔔 通知渠道配置（Apprise）

📦 支持 100+ 通知渠道，通过 [Apprise](https://github.com/sinspired/apprise_vercel) 发送通知。

- 中文文档镜像：[文档](https://sinspired.github.io/apprise_vercel/)

## 🌐 Vercel 部署

点击下方按钮，一键部署到你的 `Vercel` 账户：

[![Deploy with Vercel](https://vercel.com/button)](https://vercel.com/new/clone?repository-url=https://github.com/sinspired/apprise_vercel&project-name=apprise-vercel&repository-name=apprise-vercel&demo-title=Apprise%20Vercel%20Notify&demo-description=轻量无服务器消息推送，支持%20Bark、ntfy、Discord%20等%20100%2B%20渠道&demo-url=https://apprise.linkpc.dpdns.org&demo-image=https://apprise.linkpc.dpdns.org/static/Apprise_OG.png&envLink=https://github.com/sinspired/apprise_vercel/wiki/Deploy)

部署后获取 API 链接，如 `https://projectName.vercel.app/notify`。

建议为 Vercel 项目设置自定义域名（国内访问 Vercel 可能受限）。

## 🐳 Docker 部署（不支持 arm/v7）

```bash
# 基础运行
docker run --name apprise -p 8000:8000 --restart always -d caronc/apprise:latest

# 使用代理运行
docker run --name apprise \
  -p 8000:8000 \
  -e HTTP_PROXY=http://192.168.1.1:7890 \
  -e HTTPS_PROXY=http://192.168.1.1:7890 \
  --restart always \
  -d caronc/apprise:latest
```

## 📝 配置示例（config.yaml）

```yaml
# -----------通知设置-----------
# 配置通知渠道，将自动发送检测结果通知，新版本通知
# 访问 https://github.com/sinspired/apprise_vercel 部署通知 API 服务
# 按提示部署，建议为 Vercel 项目设置自定义域名（国内访问 Vercel 可能受限）。
# 填写搭建的apprise API server 地址
# 示例：https://notify.xxxx.us.kg/notify
# 内置apprise服务，不想搭建仅需填写 recipient-url 即可
apprise-api-server: "https://apprise.linkpc.dpdns.org/notify"
# 通知渠道
# 支持100+ 个通知渠道，详细格式请参照 https://github.com/caronc/apprise
# 格式参考：
# telegram格式：tgram://{bot_token}/{chat_id}
# 钉钉格式：dingtalk://{Secret}@{ApiKey}
# QQ邮箱：mailto://{QQ号}:{邮箱授权码}@qq.com
# 邮箱授权码：设置-账号-POP3/IMAP/SMTP/Exchange/CardDAV/CalDAV服务-开通-继续获取授权码
# 访问 https://sinspired.github.io/apprise_vercel 查看设置 3分钟搞定通知渠道 教程
# 访问 https://apprise.linkpc.dpdns.org 测试通知渠道是否正确
recipient-url:
  # - "bark://api.day.app/xxxxxxxxxxxxxxx"
  # - "ntfy://mytopic"
  # - "tgram://xxxxxx/-1002149239223"
  # - "dingtalk://xxxxxx@xxxxxxx"
  # - "mailto://xxxxx:xxxxxx@qq.com"
```
