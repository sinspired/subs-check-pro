// entry.mjs - CodeMirror 6 ESM 入口
import { EditorView, basicSetup } from "codemirror";   // 核心（EditorView + basicSetup）
import {
  keymap,
  WidgetType,
  Decoration,
  ViewPlugin,
  MatchDecorator
} from "@codemirror/view";                             // 视图相关
import { EditorState } from "@codemirror/state";       // 状态
import { yaml } from "@codemirror/lang-yaml";          // YAML 语言支持
import { oneDark } from "@codemirror/theme-one-dark";  // 主题
import { indentWithTab } from "@codemirror/commands";  // Tab 缩进
import { autocompletion, startCompletion } from "@codemirror/autocomplete"; // 自动补全
import { linter } from "@codemirror/lint";             // Lint 支持
import * as YAML from "yaml";                          // YAML 解析库

import { search, openSearchPanel, closeSearchPanel, searchPanelOpen } from "@codemirror/search"; // 搜索功能
import { highlightSelectionMatches } from "@codemirror/search"; // 选择高亮

// 配置键的自动完成列表（基于config.yaml配置模板）
const configCompletions = [
  { label: "print-progress", type: "property", detail: "是否显示检测进度（终端）", section: "进度条", isArray: false },
  { label: "progress-mode", type: "property", detail: "进度条显示模式", section: "进度条", isArray: false },
  { label: "update", type: "property", detail: "是否开启新版本更新", section: "版本更新", isArray: false },
  { label: "update-on-startup", type: "property", detail: "启动时检查更新", section: "版本更新", isArray: false },
  { label: "cron-check-update", type: "property", detail: "定时检查更新", section: "版本更新", isArray: false },
  { label: "prerelease", type: "property", detail: "使用预发布版本", section: "版本更新", isArray: false },
  { label: "update-timeout", type: "property", detail: "下载新版本超时(分钟)", section: "版本更新", isArray: false },
  { label: "concurrent", type: "property", detail: "并发线程数", section: "检测参数", isArray: false },
  { label: "alive-concurrent", type: "property", detail: "测活并发数，建议：10-1000", section: "检测参数", isArray: false },
  { label: "speed-concurrent", type: "property", detail: "测速并发数，建议：4-32", section: "检测参数", isArray: false },
  { label: "media-concurrent", type: "property", detail: "媒体解锁并发数，建议：4-200", section: "检测参数", isArray: false },
  { label: "ipv6", type: "property", detail: "是否启用IPv6", section: "检测参数", isArray: false },
  { label: "check-interval", type: "property", detail: "检查间隔(分钟)", section: "检测参数", isArray: false },
  { label: "cron-expression", type: "property", detail: "定时检测", section: "检测参数", isArray: false },
  { label: "success-limit", type: "property", detail: "成功节点数量限制", section: "检测参数", isArray: false },
  { label: "timeout", type: "property", detail: "单个检测超时(毫秒)", section: "检测参数", isArray: false },
  { label: "speed-test-url", type: "property", detail: "测速地址(留空关闭测速)", section: "检测参数", isArray: false },
  { label: "min-speed", type: "property", detail: "最低下载速度(KB/s)", section: "检测参数", isArray: false },
  { label: "download-timeout", type: "property", detail: "下载测试时间(s)", section: "检测参数", isArray: false },
  { label: "download-mb", type: "property", detail: "单节点下载数据(MB)限制，0为不限", section: "检测参数", isArray: false },
  { label: "total-speed-limit", type: "property", detail: "总下载速度限制(MB/s)，0为不限", section: "检测参数", isArray: false },
  { label: "threshold", type: "property", detail: "节点乱序，相似度阈值", section: "检测参数", isArray: false },
  { label: "rename-node", type: "property", detail: "是否重命名节点", section: "节点处理", isArray: false },
  { label: "node-prefix", type: "property", detail: "节点前缀", section: "节点处理", isArray: false },
  { label: "node-type", type: "property", detail: "只测试指定协议的节点", section: "节点处理", isArray: true },
  { label: "isp-check", type: "property", detail: "是否开启isp检测", section: "节点质量", isArray: false },
  { label: "media-check", type: "property", detail: "是否开启解锁检测", section: "媒体解锁", isArray: false },
  { label: "platforms", type: "property", detail: "流媒体检测平台列表", section: "媒体解锁", isArray: true },
  { label: "drop-bad-cf-nodes", type: "property", detail: "是否丢弃无法访问 cloudflare 的节点", section: "节点处理", isArray: false },
  { label: "enhanced-tag", type: "property", detail: "增强位置显示开关", section: "节点标签", isArray: false },
  { label: "maxmind-db-path", type: "property", detail: "MaxMind 地理数据库路径", section: "节点标签", isArray: false },
  { label: "output-dir", type: "property", detail: "输出目录", section: "输出设置", isArray: false },
  { label: "keep-success-proxies", type: "property", detail: "保留之前测试成功的节点", section: "节点处理", isArray: false },
  { label: "listen-port", type: "property", detail: "WebUI端口", section: "Web UI", isArray: false },
  { label: "enable-web-ui", type: "property", detail: "是否启用Web控制面板", section: "Web UI", isArray: false },
  { label: "api-key", type: "property", detail: "Web控制面板的api-key", section: "Web UI", isArray: false },
  { label: "share-password", type: "property", detail: "分享码", section: "Web UI", isArray: false },
  { label: "callback-script", type: "property", detail: "回调脚本路径", section: "回调脚本", isArray: false },
  { label: "apprise-api-server", type: "property", detail: "apprise API server 地址", section: "通知设置", isArray: false, info: "示例: https://notify.xxxx.us.kg/notify" },
  { label: "recipient-url", type: "property", detail: "apprise 通知目标", section: "通知设置", isArray: true, info: "详细格式请参照 https://github.com/caronc/apprise" },
  { label: "notify-title", type: "property", detail: "自定义通知标题", section: "通知设置", isArray: false, info: "默认标题: 🔔 节点状态更新" },
  { label: "sub-store-port", type: "property", detail: "sub-store 端口", section: "sub-store", isArray: false },
  { label: "sub-store-path", type: "property", detail: "sub-store 自定义路径", section: "sub-store", isArray: false },
  { label: "mihomo-overwrite-url", type: "property", detail: "mihomo 覆写订阅地址", section: "sub-store", isArray: false },
  { label: "singbox-latest", type: "property", detail: "singbox latest 版本配置", section: "singbox规则", isArray: false },
  { label: "singbox-old", type: "property", detail: "singbox 1.11 版本配置（iOS 兼容）", section: "singbox规则", isArray: false },
  { label: "sub-store-sync-cron", type: "property", detail: "sub-store同步gist定时任务", section: "sub-store定时", isArray: false },
  { label: "sub-store-produce-cron", type: "property", detail: "定时更新订阅", section: "sub-store定时", isArray: false },
  { label: "sub-store-push-service", type: "property", detail: "sub-store推送服务地址", section: "sub-store定时", isArray: false, info: "例如：Brak: \'SUB_STORE_PUSH_SERVICE=https://api.day.app/XXXXXXXXXXXX/[推送标题]/[推送内容]\'" },
  { label: "save-method", type: "property", detail: "保存方法", section: "保存方法", isArray: false, info: "目前支持的保存方法: r2, local, gist, webdav, s3" },
  { label: "webdav-url", type: "property", detail: "webdav 地址", section: "webdav", isArray: false },
  { label: "webdav-username", type: "property", detail: "webdav 用户名", section: "webdav", isArray: false },
  { label: "webdav-password", type: "property", detail: "webdav 密码", section: "webdav", isArray: false },
  { label: "github-gist-id", type: "property", detail: "gist id", section: "gist", isArray: false },
  { label: "github-token", type: "property", detail: "github token", section: "gist", isArray: false },
  { label: "github-api-mirror", type: "property", detail: "github api mirror", section: "gist", isArray: false },
  { label: "worker-url", type: "property", detail: "将测速结果推送到Worker的地址", section: "Worker", isArray: false },
  { label: "worker-token", type: "property", detail: "Worker令牌", section: "Worker", isArray: false },
  { label: "s3-endpoint", type: "property", detail: "将测速结果推送到S3/Minio的地址", section: "S3", isArray: false },
  { label: "s3-access-id", type: "property", detail: "S3的访问凭证 ID", section: "S3", isArray: false },
  { label: "s3-secret-key", type: "property", detail: "S3的访问凭证 Key", section: "S3", isArray: false },
  { label: "s3-bucket", type: "property", detail: "S3的Bucket名称", section: "S3", isArray: false },
  { label: "s3-use-ssl", type: "property", detail: "是否使用SSL", section: "S3", isArray: false },
  { label: "s3-bucket-lookup", type: "property", detail: "默认自动判断dns还是path", section: "S3", isArray: false, info: "可选值：auto, path, dns" },
  { label: "system-proxy", type: "property", detail: "系统代理设置", section: "代理设置", isArray: false, info: "即使未设置,也会检测常见端口(v2ray\\clash)的系统代理自动设置" },
  { label: "github-proxy", type: "property", detail: "Github 代理", section: "代理设置", isArray: false, info: "获取订阅、添加覆写地址时使用" },
  { label: "ghproxy-group", type: "property", detail: "GitHub 代理列表", section: "代理设置", isArray: true, info: "程序会自动筛选可用的 GitHub 代理" },
  { label: "sub-urls-retry", type: "property", detail: "重试次数(获取订阅失败后重试次数)", section: "订阅设置", isArray: false },
  { label: "sub-urls-timeout", type: "property", detail: "获取订阅超时(默认10s)", section: "订阅设置", isArray: false },
  { label: "sub-urls-stats", type: "property", detail: "统计订阅链接有效性和成功率", section: "订阅设置", isArray: false },
  { label: "success-rate", type: "property", detail: "节点订阅成功率", section: "订阅设置", isArray: false },
  { label: "sub-urls-remote", type: "property", detail: "远程订阅清单地址", section: "订阅设置", isArray: true },
  { label: "sub-urls", type: "property", detail: "订阅地址", section: "订阅设置", isArray: true },
  // singbox 子字段
  { label: "version", type: "property", detail: "singbox 版本号", section: "singbox规则", isArray: false },
  { label: "json", type: "property", detail: "singbox 分流规则 JSON 地址", section: "singbox规则", isArray: false },
  { label: "js", type: "property", detail: "singbox 节点处理脚本 JS 地址", section: "singbox规则", isArray: false },

  // sub-process 及子字段
  { label: "sub-process", type: "property", detail: "sub-store 节点后处理配置", section: "sub-store", isArray: false },
  { label: "resolve-domain", type: "property", detail: "开启 DNS 解析（固定 Ali/IPv6/缓存）", section: "sub-process", isArray: false },
  { label: "node-split", type: "property", detail: "节点裂变：将多 IP 展开为独立节点", section: "sub-process", isArray: false },
  { label: "regex-sort", type: "property", detail: "正则排序表达式列表，按匹配顺序排序", section: "sub-process", isArray: true },
  { label: "sub-info", type: "property", detail: "注入订阅流量信息节点", section: "sub-process", isArray: false },
];

const keySet = new Set(configCompletions.map(c => c.label));

// 值补全映射表（扩展为对象数组，添加 detail，布尔值优先 true，基于模板）
const valueCompletions = {
  // 布尔开关类（true 优先）
  "print-progress": [
    { label: "true", detail: "显示进度条" },
    { label: "false", detail: "不显示（默认）" }
  ],
  "update": [
    { label: "true", detail: "自动更新（默认）" },
    { label: "false", detail: "不更新（会提醒新版本）" }
  ],
  "update-on-startup": [
    { label: "true", detail: "启动时检查更新（默认）" },
    { label: "false", detail: "不检查" }
  ],
  "prerelease": [
    { label: "true", detail: "使用预发布版本" },
    { label: "false", detail: "使用稳定版本（默认）" }
  ],
  "rename-node": [
    { label: "true", detail: "启用节点重命名（默认）" },
    { label: "false", detail: "禁用" }
  ],
  "isp-check": [
    { label: "true", detail: "启用isp检测" },
    { label: "false", detail: "禁用（默认）" }
  ],
  "media-check": [
    { label: "true", detail: "启用流媒体解锁检测" },
    { label: "false", detail: "禁用（默认）" }
  ],
  "drop-bad-cf-nodes": [
    { label: "true", detail: "丢弃" },
    { label: "false", detail: "保留无法访问 Cloudflare 的节点（默认）" }
  ],
  "enhanced-tag": [
    { label: "true", detail: "启用增强位置标签（默认）" },
    { label: "false", detail: "禁用" }
  ],
  "keep-success-proxies": [
    { label: "true", detail: "保留之前测试成功的节点（默认）" },
    { label: "false", detail: "不保留" }
  ],
  "enable-web-ui": [
    { label: "true", detail: "启用 Web 控制面板（默认）" },
    { label: "false", detail: "禁用" }
  ],
  "s3-use-ssl": [
    { label: "true", detail: "使用 SSL" },
    { label: "false", detail: "不使用 SSL（默认）" }
  ],

  // 枚举 / 模式类
  "progress-mode": [
    { label: "auto", detail: "根据测活-测速-媒体检测的阶段权重自动显示（默认）" },
    { label: "stage", detail: "每个阶段完成,显示下一阶段剩余任务" }
  ],
  "s3-bucket-lookup": [
    { label: "auto", detail: "自动判断 dns 还是 path（默认）" },
    { label: "path", detail: "使用 path 风格" },
    { label: "dns", detail: "使用 dns 风格" }
  ],
  "save-method": [
    { label: "local", detail: "本地保存（默认）" },
    { label: "r2", detail: "R2 云存储" },
    { label: "gist", detail: "GitHub Gist" },
    { label: "webdav", detail: "WebDAV" },
    { label: "s3", detail: "S3/Minio" }
  ],

  // cron 表达式示例（作为字符串选项）
  "cron-check-update": [
    { label: "\"0 0,9,21 * * *\"", detail: "默认每天0点,9点,21点检查更新" },
    { label: "\"*/30 * * * *\"", detail: "每30分钟检查" },
    { label: "\"0 */6 * * *\"", detail: "每6小时检查" }
  ],
  "cron-expression": [
    { label: "\"0 */12 * * *\"", detail: "每12小时的整点执行" },
    { label: "\"0 0 */2 * *\"", detail: "每2天的0点执行" },
    { label: "\"0 0 1 * *\"", detail: "每月1日0点执行" },
    { label: "\"* 4,16 * * *\"", detail: "每天4点、16点执行" }
  ],
  "sub-store-sync-cron": [
    { label: "\"55 23 * * *\"", detail: "每天 23 点 55 分(避开部分机场后端每天0点定时重启)" },
    { label: "\"0 0 * * *\"", detail: "每天 0 点执行" }
  ],
  "sub-store-produce-cron": [
    { label: "\"0 */2 * * *\"", detail: "每 2 小时处理一次" },
    { label: "\"0 */3 * * *\"", detail: "每 3 小时处理一次" }
  ],

  // system-proxy 示例
  "system-proxy": [
    { label: "\"http://127.0.0.1:10808\"", detail: "v2rayN 默认代理端口" },
    { label: "\"http://127.0.0.1:7890\"", detail: "clash/mihomo 默认代理端口" },
    { label: "\"http://username:password@127.0.0.1:7890\"", detail: "HTTP 代理示例" },
    { label: "\"socks5://username:password@127.0.0.1:7890\"", detail: "SOCKS5 代理示例" }
  ],

  // github-proxy 示例
  "github-proxy": [
    { label: "\"https://ghfast.top/\"", detail: "GHFast 代理" },
    { label: "\"https://ghproxy.com/\"", detail: "GHProxy 代理" }
  ],

  // notify-title 示例
  "notify-title": [
    { label: "\"🔔 节点状态更新\"", detail: "默认通知标题" }
  ],

  "version": [
    { label: "1.12", detail: "singbox 最新版（1.12.x）" },
    { label: "1.11", detail: "singbox 旧版（1.11.x，iOS 兼容）" },
    { label: "1.10", detail: "singbox 1.10.x" },
  ],
  "resolve-domain": [
    { label: "true", detail: "开启 DNS 解析" },
    { label: "false", detail: "关闭（默认）" },
  ],
  "node-split": [
    { label: "true", detail: "启用节点裂变" },
    { label: "false", detail: "关闭（默认）" },
  ],
  "sub-info": [
    { label: "true", detail: "注入订阅流量信息节点" },
    { label: "false", detail: "关闭（默认）" },
  ],
};

// 数组项补全（用于 platforms 等，当输入 - 时补全子项）
const arrayItemCompletions = {
  "platforms": [
    { label: "iprisk", detail: "IP 风险检测" },
    { label: "openai", detail: "OpenAI 解锁检测" },
    { label: "gemini", detail: "Gemini 解锁检测" },
    { label: "tiktok", detail: "TikTok 解锁检测" },
    { label: "youtube", detail: "YouTube 解锁检测" },
    { label: "netflix", detail: "Netflix 解锁检测" },
    { label: "disney", detail: "Disney+ 解锁检测" },
    { label: "x", detail: "X (Twitter) 兼容性检测" }
  ],
  "node-type": [
    { label: "ss", detail: "Shadowsocks 协议" },
    { label: "vmess", detail: "VMess 协议" },
    { label: "vless", detail: "VLESS 协议" },
    { label: "trojan", detail: "Trojan 协议" },
    { label: "shadowsocks", detail: "Shadowsocks 协议" },
  ],
  "recipient-url": [
    { label: "bark://api.day.app/xxxxxxxxxxxxxxx", detail: "Bark(iOS) 通知格式：bark://api.day.app/{device_key}" },
    { label: "ntfy://mytopic", detail: "Ntfy ntfy://{topic}" },
    { label: "tgram://xxxxxx/-1002149239223", detail: "Telegram 通知格式：tgram://{bot_token}/{chat_id}" },
    { label: "dingtalk://xxxxxx@xxxxxxx", detail: "钉钉通知格式：dingtalk://{Secret}@{ApiKey}" },
    { label: "mailto://xxxxx:xxxxxx@qq.com", detail: "QQ邮箱：mailto://QQ号:邮箱授权码@qq.com" }
  ],
  "ghproxy-group": [
    { label: "https://ghproxy.net/", detail: "GHProxy 代理 1" },
    // { label: "https://git.llvho.com/", detail: "GHProxy 代理 2" },
    // { label: "https://hub.885666.xyz/", detail: "GHProxy 代理 3" },
    // { label: "https://p.jackyu.cn/", detail: "GHProxy 代理 4" },
    // { label: "https://github.cnxiaobai.com/", detail: "GHProxy 代理 5" }
  ],
  "sub-urls-remote": [
    { label: "https://example.com/sub-list.txt", detail: "纯文本订阅清单（按行分隔）" },
    { label: "https://example.com/sub-list.yaml", detail: "YAML 订阅清单" },
    // { label: "https://raw.githubusercontent.com/beck-8/sub-urls/main/%E5%B0%8F%E8%80%8C%E7%BE%8E.txt", detail: "示例远程订阅文件，支持 # 注释" }
  ],
  "sub-urls": [
    // { label: "https://example.com/sub.txt", detail: "基础订阅链接（clash/mihomo/v2ray/base64）" },
    // { label: "https://example.com/sub?token=43fa8f0dc9bb00dcfec2afb21b14378a", detail: "带 token 的订阅" },
    // { label: "https://example.com/sub?token=43fa8f0dc9bb00dcfec2afb21b14378a&flag=clash.meta", detail: "Clash Meta 格式订阅" },
    { label: "https://raw.githubusercontent.com/example/repo/main/config/{Ymd}.yaml", detail: "带时间占位符的订阅" },
    { label: "https://example.com/sub.txt#我是备注", detail: "带备注的订阅（备注加到节点命名）" }
  ],

  "regex-sort": [
    { label: "\".*\\\\bHK\\\\b.*\"", detail: "香港节点优先" },
    { label: "\".*\\\\bSG\\\\b.*\"", detail: "新加坡节点优先" },
    { label: "\".*\\\\bUS\\\\b.*\"", detail: "美国节点优先" },
    { label: "\".*\\\\bJP\\\\b.*\"", detail: "日本节点优先" },
    { label: "\".*\\\\bGPT⁺.*\"", detail: "GPT+ 解锁节点优先" },
    { label: "\".*\\\\bGM\\\\b.*\"", detail: "Gemini 解锁节点优先" },
    { label: "\"(.*GPT⁺.*)(.*GM.*)\"", detail: "GPT+ 且 Gemini 同时满足" },
    { label: "\".*\\\\bNF\\\\b.*\"", detail: "Netflix 解锁节点优先" },
  ],
};

const arrayKeys = Object.keys(arrayItemCompletions);

// yaml自动补全逻辑（支持空输入时的数组项和值补全）
const yamlConfigSource = (context) => {
  const word = context.matchBefore(/[\w-.:\/@%\-+]*$/); // 扩展匹配以支持URL-like值
  const currentInput = word ? word.text : '';
  const { from: wordFrom, to: wordTo } = word || { from: context.pos, to: context.pos };

  const line = context.state.doc.lineAt(context.pos);
  const lineText = line.text;
  const lineStart = line.from;
  const col = context.pos - lineStart;
  const leadingSpacesStr = lineText.match(/^\s*/) || '';
  const leadingSpaces = leadingSpacesStr.length;
  const trimmed = lineText.trimLeft();
  const textBeforeCursor = lineText.slice(0, col);

  // 如果当前行是注释行，直接返回 null，不触发补全
  if (/^\s*#/.test(lineText)) {
    return null;
  }

  // 检查是否在数组项位置（行以 - 开头，缩进匹配）
  if (trimmed.startsWith('- ') || trimmed.startsWith('-')) {
    // 动态计算缩进级别（查找父键的缩进）
    let parentIndent = leadingSpaces;
    let parentKey = '';
    let searchLineNum = line.number - 1;
    while (searchLineNum >= 1 && parentIndent > 0) {
      const prevLine = context.state.doc.line(searchLineNum);
      const prevText = prevLine.text;
      const prevLeadingSpaces = (prevText.match(/^\s*/) || '')[0].length;
      const prevTrimmed = prevText.trim();
      if (prevLeadingSpaces < parentIndent && prevTrimmed.endsWith(':')) {
        parentKey = prevTrimmed.slice(0, -1).trim();
        parentIndent = prevLeadingSpaces;
        break;
      }
      searchLineNum--;
    }
    if (parentKey && arrayKeys.includes(parentKey)) {
      const items = arrayItemCompletions[parentKey] || [];
      const dashOffset = trimmed.startsWith('- ') ? 2 : 1;
      const matchingItems = items
        .filter(item => item.label.startsWith(currentInput))
        .map(item => ({
          label: item.label,
          type: "constant",
          detail: item.detail,
          apply: (view, completion, from, to) => {
            // 插入项标签，替换当前输入
            view.dispatch({
              changes: { from, to, insert: item.label }
            });
            // 添加下一空项行
            const currentHead = view.state.selection.main.head;
            const currentLine = view.state.doc.lineAt(currentHead);
            const lineEnd = currentLine.to;
            const nextItemText = `\n${leadingSpacesStr}- `;
            view.dispatch({
              changes: { from: lineEnd, to: lineEnd, insert: nextItemText }
            });
            // 显式设置光标到下一 - 后
            const newCursorPos = lineEnd + nextItemText.length;
            view.dispatch({
              selection: { anchor: newCursorPos }
            });
            startCompletion(view);
          }
        }));
      if (matchingItems.length > 0) {
        return {
          from: wordFrom,
          options: matchingItems
        };
      }
    }
    return null;
  }

  // 值补全尝试（标量值）
  const lastColonGlobal = lineText.lastIndexOf(':', col);
  if (lastColonGlobal !== -1) {
    const beforeColon = lineText.slice(0, lastColonGlobal).trim();
    const currentKey = beforeColon;
    if (keySet.has(currentKey)) {
      const afterStart = lastColonGlobal + 1;
      const afterTextBeforeCursor = lineText.slice(afterStart, col);
      const spacesMatch = afterTextBeforeCursor.match(/^\s*/);
      const spacesLen = spacesMatch ? spacesMatch[0].length : 0;
      const valueFrom = lineStart + afterStart + spacesLen;
      const currentValue = afterTextBeforeCursor.slice(spacesLen);
      if (valueCompletions[currentKey]) {
        const matching = valueCompletions[currentKey].filter(v => v.label.startsWith(currentValue));
        if (matching.length > 0) {
          return {
            from: valueFrom,
            options: matching.map(v => ({
              label: v.label,
              type: "value",
              detail: v.detail,
              apply: v.label
            }))
          };
        }
      }
      // 无匹配，返回 null 允许自由输入
      return null;
    } else {
      // 未知键，返回 null 允许自由输入
      return null;
    }
  }

  // 键位置补全（无冒号前，且行中无冒号）
  if (textBeforeCursor.includes(':')) return null;
  const keyWord = context.matchBefore(/[\w-]*$/);
  if (keyWord) {
    const { from: keyFrom, to: keyTo, text: keyText } = keyWord;
    const matchingKeys = configCompletions
      .filter(opt => opt.label.startsWith(keyText))
      .map(opt => ({
        label: opt.label,
        type: opt.type,
        detail: opt.detail,
        section: opt.section,
        apply: (view, completion, from, to) => {
          const insertText = opt.isArray
            ? `${opt.label}:\n${'  '}- `  // 数组键：插入 key:\n  - ，光标在 - 后
            : `${opt.label}: `;           // 非数组：当前行 key: 
          view.dispatch({
            changes: { from, to, insert: insertText }
          });
          // 光标在插入末尾，立即触发补全（值或数组项）
          startCompletion(view);
        }
      }));
    if (matchingKeys.length > 0) {
      return { from: keyFrom, options: matchingKeys };
    }
  }

  return null;
};

// 添加yaml校验
function yamlLinter() {
  return linter(view => {
    const diagnostics = [];
    const text = view.state.doc.toString();

    try {
      const doc = YAML.parseDocument(text);

      // 收集错误
      if (doc.errors && doc.errors.length > 0) {
        for (const err of doc.errors) {
          const pos = err.pos?.[0] ?? 0;
          const line = view.state.doc.lineAt(pos); // 获取整行
          diagnostics.push({
            from: line.from,
            to: line.to,
            severity: "error",
            message: err.message
          });
        }
      }

      // 收集警告
      if (doc.warnings && doc.warnings.length > 0) {
        for (const warn of doc.warnings) {
          const pos = warn.pos?.[0] ?? 0;
          const line = view.state.doc.lineAt(pos);
          diagnostics.push({
            from: line.from,
            to: line.to,
            severity: "warning",
            message: warn.message
          });
        }
      }
    } catch (e) {
      // 如果解析直接抛异常，就标记整篇文档
      diagnostics.push({
        from: 0,
        to: text.length,
        severity: "error",
        message: e.message
      });
    }

    return diagnostics;
  });
}

// -------------------- 占位符原子替换 --------------------
class PlaceholderWidget extends WidgetType {
  constructor(name) {
    super();
    this.name = name;
  }
  eq(other) { return other.name === this.name }
  toDOM() {
    let span = document.createElement("span");
    span.className = "cm-placeholder";
    span.textContent = this.name;
    return span;
  }
  ignoreEvent() { return false }
}

const placeholderMatcher = new MatchDecorator({
  regexp: new RegExp(
    [
      // 匹配所有 configCompletions 中的 label
      '(?<=^[ \t]*)(print-progress|progress-mode|update|update-on-startup|cron-check-update|prerelease|update-timeout|concurrent|alive-concurrent|speed-concurrent|media-concurrent|ipv6|check-interval|cron-expression|success-limit|timeout|speed-test-url|min-speed|download-timeout|download-mb|total-speed-limit|threshold|rename-node|node-prefix|node-type|isp-check|media-check|platforms|drop-bad-cf-nodes|enhanced-tag|maxmind-db-path|output-dir|keep-success-proxies|listen-port|enable-web-ui|api-key|share-password|callback-script|apprise-api-server|recipient-url|notify-title|sub-store-port|sub-store-path|mihomo-overwrite-url|singbox-latest|singbox-old|sub-store-sync-cron|sub-store-produce-cron|sub-store-push-service|save-method|webdav-url|webdav-username|webdav-password|github-gist-id|github-token|github-api-mirror|worker-url|worker-token|s3-endpoint|s3-access-id|s3-secret-key|s3-bucket|s3-use-ssl|s3-bucket-lookup|system-proxy|github-proxy|ghproxy-group|sub-urls-retry|sub-urls-timeout|sub-urls-stats|success-rate|sub-urls-remote|sub-urls|sub-process|resolve-domain|node-split|regex-sort|sub-info|version|json|js)(?=\s*:\s*)',

      // 列表项：- openai / - "openai"
      '(?<=^[ \\t]*-\\s*["\']?)(openai|iprisk|gemini|tiktok|youtube|disney|netflix|x|ss|trojan|vless|vmess|shadowsocks)(?=["\']?\\b)',

      // 订阅备注（行内 #...）
      '(?<=^[ \\t]*-\\s*[^#]*)(#.*$)',

      // 列表项中匹配 {Ymd} 或 {Y}-{m}-{d}
      '(?<=^[ \\t]*-\\s*[^#]*)({Ymd}|{Y}-{m}-{d}|{Y}|{m}|{mm}|{d}|{dd})(?=[^#]*(?:#.*)?$)',

      // threshold 1 0.75 0.50 0.25
      '(?<=^[ \\t]*threshold:\\s*["\']?)(1.00|0.75|0.50|0.25|1|0.5)(?=["\']?)',

      // 列表项：- tgram / dingtalk / mailto
      '(?<=^[ \\t]*-\\s*["\']?)(bark|ntfy|tgram|x|twitter|line|slack|qq|discord|whatsapp|dingtalk|wecom|feishu|mailto)(?=["\']?\\b)',

      // // 注释内的占位符 {xxx},避免小白误解
      // '(?<=#.*?)(\{[A-Za-z0-9_-]+\})(?=.*$)',
      // // 注释内仅中文占位符 {中文...}
      // '(?<=#.*?)(\{[\u4e00-\u9fa5]+\})(?=.*$)',

      // 注释内的占位符 {xxx},避免小白误解（支持中英文混合）
      '(?<=#.*?)(\{[A-Za-z0-9\u4e00-\u9fa5_-]+\})(?=.*$)',
    ].join('|'),
    'mg'
  ),

  decoration: match => {
    const groups = Array.from(match).slice(1);
    const value = groups.find(g => g !== undefined && g !== null && g !== '');
    if (!value) return null;
    return Decoration.replace({
      widget: new PlaceholderWidget(value),
      inclusive: false
    });
  }
});


const placeholderPlugin = ViewPlugin.fromClass(class {
  constructor(view) {
    this.decorations = placeholderMatcher.createDeco(view) || Decoration.none;
  }
  update(update) {
    if (update.docChanged || update.viewportChanged) {
      this.decorations = placeholderMatcher.createDeco(update.view) || Decoration.none;
    }
  }
}, {
  decorations: v => v.decorations,
  provide: plugin => EditorView.atomicRanges.of(view => view.plugin(plugin)?.decorations || Decoration.none)
});

// -------------------- 全局暴露 --------------------

window.CodeMirror = {
  createEditor: (container, initialValue = '', theme = 'light') => {
    if (!container || !(container instanceof HTMLElement)) {
      throw new Error('Invalid parent: must be a valid HTMLElement (e.g., <div>)');
    }

    const extensions = [
      basicSetup,
      yaml(),
      EditorView.lineWrapping,
      keymap.of([indentWithTab]),
      autocompletion({ override: [yamlConfigSource] }),
      yamlLinter(),
      placeholderPlugin,    // 占位符原子替换
      search({ top: true }), // 内置搜索面板，置于顶部
      highlightSelectionMatches(),

      theme === 'dark' ? oneDark : null
    ].filter(Boolean);

    const state = EditorState.create({
      doc: initialValue,
      extensions
    });

    const view = new EditorView({
      state,
      parent: container
    });

    // 全局暴露变量
    window.searchView = view;
    window.openSearchPanel = openSearchPanel;
    window.closeSearchPanel = closeSearchPanel;
    window.searchPanelOpen = searchPanelOpen;

    return view;
  },
  getValue: (view) => view.state.doc.toString(),
  setValue: (view, value) => view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: value } }),
  focus: (view) => view.focus(),
  destroy: (view) => view.destroy()
};