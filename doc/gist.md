# gist 保存方法

## 部署

- 随意创建一个Gist

- 将 gist id 配置到 `config.yaml` 中

- 将 gist token 配置到 `config.yaml` 中

## Worker 反代 GIthub API

- 将 [worker](./cloudflare/worker.js) 部署到 cloudflare workers

- `变量和机密` 设置`GITHUB_USER`为你的 github 用户名

- `变量和机密` 设置`GITHUB_ID`为你的 gist id

- `变量和机密` 设置`AUTH_TOKEN`为访问密钥

- 将 `github-api-mirror` 配置为你的 worker 地址

```
    github-api-mirror: "https://your-worker-url/github"
```

## 获取订阅

> 如果配置了Woker , 将 `key` 修改为对应的即可
> 订阅格式为 `https://your-worker-url/gist?key=all.yaml&token=AUTH_TOKEN`

- yaml格式的订阅

```
https://gist.githubusercontent.com/YOUR_GITHUB_USERNAME/YOUR_GIST_ID/raw/all.yaml
```

- base64编码的订阅

```
https://gist.githubusercontent.com/YOUR_GITHUB_USERNAME/YOUR_GIST_ID/raw/base64.txt
```

- 带规则的mihomo.yaml文件

```
https://gist.githubusercontent.com/YOUR_GITHUB_USERNAME/YOUR_GIST_ID/raw/mihomo.yaml
```
