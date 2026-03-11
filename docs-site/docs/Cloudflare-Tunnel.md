# ⚙️ Cloudflare Tunnel（隧道映射）外网访问

> WebUI 经过全新设计，添加了 logo 图标等资源，本地化了所有依赖，因此需要额外增加一个 `static` 资源路径。

## 🚀 简易操作步骤

1. 🔑 登录 Cloudflare (CF)，左侧菜单栏点击 `Zero Trust`
2. 🌐 在新页面，左侧菜单栏点击 `网络` → `Tunnels` → `创建隧道` → `选择 Cloudflared`
3. 🛠️ 按提示操作：
   - 为隧道命名
   - 安装并运行连接器
   - 路由隧道
4. ✅ 创建完成后，在 `Tunnels` 页面会出现你创建的隧道，点击隧道名称 → 编辑
5. ➕ 在隧道详情页点击 `已发布应用程序路由` → `添加已发布应用程序路由`
6. 🌍 配置主机名和服务：
   - 示例：`sub.你的域名.com/path`
     - `sub` → (可选) 子域
     - `你的域名` → 域名
     - `path` → (可选) 路径
   - 服务类型 → 选择 `http`
   - URL → 输入 `localhost:8199` 或 `localhost:8299`

## 📒 需添加的路由条目

> 本项目需要 `share-password` 才能访问 `./output`，可放心设置，谨慎分享。

### 🔀 使用路径映射端口

| 外网访问地址                           | 本地服务地址    | 用途说明          |
|--------------------------------------|-----------------|-------------------|
| `sub.你的域名.com/admin`              | `localhost:8199`| 配置管理主页       |
| `sub.你的域名.com/static`             | `localhost:8199`| ico, js, css 文件 |
| `sub.你的域名.com/api`                | `localhost:8199`| 软件运行状态       |
| `sub.你的域名.com/analysis`           | `localhost:8199`| 检测结果分析报告   |
| `sub.你的域名.com/files`              | `localhost:8199`| 内置文件服务      |
| `sub.你的域名.com/sub`                | `localhost:8199`| 🔒分享码分享         |
| `sub.你的域名.com/more`               | `localhost:8199`| 🔒无密码分享         |
| `sub_store_for_subs_check.你的域名.com/*` | `localhost:8299`| ❗必须               |
| `sub.你的域名.com/{sub-store-path}`   | `localhost:8299`| sub-store 后端     |
| `sub.你的域名.com/share`              | `localhost:8299`| sub-store 分享     |

### 🌐 使用子域映射端口

> `sub_store_for_subs_check` 为订阅管理保留子域，请勿修改！

| 🏷️ 外网访问地址                                | 💻 本地服务地址   | 💡 用途说明  |
|-----------------------------------------------|-------------------|------------|
| `sub.你的域名.com/*`                           | `localhost:8199`  | subs-check-pro |
| `sub_store_for_subs_check.你的域名.com/*`      | `localhost:8299`  | sub-store  |

## 🎉 使用方法

打开浏览器访问 `sub.你的域名.com/admin` → 输入 apiKey → 开始使用。
