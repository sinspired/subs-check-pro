# 📲 订阅使用方法

> 💡 内置 Sub-Store，可生成多种订阅格式；高级玩家可 DIY 很多功能。

## 🚀 通用订阅（不带分流规则）

```bash
# 通用订阅
http://127.0.0.1:8299/{sub-store-path}/download/sub

# URI 订阅
http://127.0.0.1:8299/{sub-store-path}/download/sub?target=URI

# Mihomo/ClashMeta
http://127.0.0.1:8299/{sub-store-path}/download/sub?target=ClashMeta

# Clash
http://127.0.0.1:8299/{sub-store-path}/download/sub?target=Clash

# V2Ray
http://127.0.0.1:8299/{sub-store-path}/download/sub?target=V2Ray

# ShadowRocket
http://127.0.0.1:8299/{sub-store-path}/download/sub?target=ShadowRocket

# Quantumult
http://127.0.0.1:8299/{sub-store-path}/download/sub?target=QX

# Sing-Box
http://127.0.0.1:8299/{sub-store-path}/download/sub?target=sing-box

# Surge
http://127.0.0.1:8299/{sub-store-path}/download/sub?target=Surge

# Surfboard
http://127.0.0.1:8299/{sub-store-path}/download/sub?target=Surfboard
```

## 🚀 Mihomo/Clash 订阅（带分流规则）

默认使用：

[mihomo 覆写文档](https://raw.githubusercontent.com/sinspired/proxy-rules/main/Sinspired_Rules_CDN.yaml)

可在配置中更改 `mihomo-overwrite-url`。

```bash
# 如果未设置 sub-store-path
http://127.0.0.1:8299/api/file/mihomo

# 如果设置了 sub-store-path: "/path"（建议设置）
http://127.0.0.1:8299/path/api/file/mihomo
```

## 🚀 sing-box 订阅（带分流规则）

项目默认支持 `sing-box` 最新版 `1.14.x`和 `1.15.x`（额外）规则，可自定义规则。

✨ 在 WebUI 点击“分享订阅”获取订阅链接：

![singbox-shareMenu](https://raw.githubusercontent.com/sinspired/subs-check-pro/main/doc/images/share-menu.png)

请查阅最新配置文件示例：[默认配置示例](https://github.com/sinspired/subs-check-pro/blob/main/config/config.yaml.example)

```yaml
# singbox 规则配置
# json 文件为分流规则
# js 脚本用来根据规则对节点进行处理
# singbox 每个版本规则不兼容，须根据客户端版本选择合适的规则
# singbox 最新版
singbox-latest:
  version: 1.14
  json:
    - https://raw.githubusercontent.com/sinspired/sub-store-template/main/1.14.x/sing-box.json
  js:
    - https://raw.githubusercontent.com/sinspired/sub-store-template/main/1.14.x/sing-box.js

# SingboxExtra 由于singbox各个版本配置不同，额外配置很有用
singbox-extra:
  version: 1.15-pre
  json:
    - https://raw.githubusercontent.com/sinspired/sub-store-template/main/1.15.x/sing-box.json
  js:
    - https://raw.githubusercontent.com/sinspired/sub-store-template/main/1.15.x/sing-box.js
```

## 🚀 shadowrocket 配置和规则

提供小火箭分流配置

### GitHub 仓库

```bash
https://raw.githubusercontent.com/sinspired/proxy-rules/main/shadowrocket-raw.conf
```

CDN 加速链接：

```bash
https://testingcf.jsdelivr.net/gh/sinspired/proxy-rules@main/shadowrocket-cdn.conf
```

### 本地访问

```bash
# 通用订阅
http://localhost:{subs-check-pro管理端口}/Shadowrocket-Rules-CDN.conf
```
