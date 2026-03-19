/**
 * config-form.js
 *
 * 双栏逻辑：
 *   · 初始化时根据编辑器可用宽度自动决定是否开启双栏
 *   · 首次开启双栏：左=tab[0]、右=tab[1]
 *   · 点击双栏按钮：左=当前激活 tab，右=下一个 tab（循环）
 *   · 双栏中点击已显示的 tab：清空该槽，等待用户选择替换项
 *   · 双栏中点击未显示的 tab：填入空槽（或最近清空的槽）
 *   · 关闭双栏：_lastTab 恢复为单栏激活值
 *   · YAML 模式：自动退出双栏，模式切换按钮更新外观
 *
 * 模式切换按钮（#cfgModeToggle）：
 *   · 单按钮设计，点击在表单/YAML 间切换
 *   · 内部通过点击隐藏的 [data-mode] 按钮，保持与 admin.js 的兼容性
 *   · MutationObserver 监听 editorContainer class 变化，同步按钮外观
 */

const SCHEMA = [
  /* ── 1. 任务 ──────────────────── */
  {
    tab: 'schedule',
    sections: [
      {
        title: '检测计划',
        fields: [
          {
            key: 'cron-expression', label: 'Cron 表达式', type: 'cron', fullWidth: true,
            placeholder: '0 4,16 * * *',
            hint: '优先级高于检测间隔；推荐凌晨 4 点和 16 点执行',
            hintExamples: ['0 4,16 * * *'],
            links: [
              { label: 'Cron 语法文档', href: 'https://crontab.guru/examples.html', icon: 'docs' },
              { label: '在线测试', href: 'https://crontab.guru', icon: 'link' },
            ],
          },
          { key: 'check-interval', label: '检测间隔 (分钟)', type: 'number', min: 1, placeholder: '720', hint: 'Cron 为空时生效；建议 720–1440' },
        ],
      },
      {
        title: '代理设置',
        fields: [
          {
            key: 'system-proxy', label: '系统代理', type: 'text', fullWidth: true,
            placeholder: 'http://127.0.0.1:10808',
            hint: '用于拉取订阅和推送通知；direct = 强制直连不使用代理；留空则自动检测。',
          },
          {
            key: 'github-proxy', label: 'GitHub 代理', type: 'text', fullWidth: true,
            placeholder: 'https://ghfast.top/',
            hint: '加速 GitHub Release 下载；建议配置',
            links: [{ label: '自建 CF 代理', href: 'https://github.com/sinspired/CF-Proxy', icon: 'github' }],
          },
          { key: 'ghproxy-group', label: 'GitHub 代理列表', type: 'url-list', hint: '程序自动筛选可用代理，优先级低于 github-proxy' },
        ],
      },
    ],
  },

  /* ── 2. 订阅 ────────────────────────────── */
  {
    tab: 'subscriptions',
    sections: [
      {
        title: '线程基准',
        fields: [
          {
            key: 'concurrent',
            label: '基准并发',
            type: 'number', min: 1, max: 100, placeholder: '20',
            hint: '拉取订阅的并发数；并作为自动并发的计算基准'
          },
        ],
      },
      {
        title: '获取参数',
        fields: [
          { key: 'sub-urls-retry', label: '重试次数', type: 'number', min: 1, max: 5, placeholder: '3', hint: '获取订阅失败重试次数' },
          { key: 'sub-urls-timeout', label: '下载超时 (s)', type: 'number', min: 5, max: 30, placeholder: '10', hint: '建议 10–60' },
          {
            key: 'success-rate', label: '成功率提醒阈值 (‰)', type: 'number', min: 0.01, max: 1000, placeholder: '0', hint: '低于此值将打印成功率信息，并写入统计文件',
            hintExamples: [
              "subs-filter.yaml"
            ]
          },
        ],
      },
      {
        title: '订阅处理',
        fields: [
          {
            key: 'threshold', label: '节点乱序', type: 'select', numericOptions: true,
            selectWidth: '500px',
            hint: '按网段智能乱序',
            options: [
              { value: '1.00', label: '1.00 — /32' },
              { value: '0.75', label: '0.75 — /24' },
              { value: '0.50', label: '0.50 — /16' },
              { value: '0.25', label: '0.25 — /8' },
            ],
          },
        ],
      },
      {
        title: '远程订阅清单',
        fields: [
          {
            key: 'sub-urls-remote', label: '远程订阅列表', type: 'url-list',
            hint: '集中维护订阅源，支持 txt / yaml / json；支持 mihomo proxy-providers 格式',
          },
        ],
      },
      {
        title: '本地订阅地址',
        fields: [
          {
            key: 'sub-urls', label: '订阅地址', type: 'url-list',
            hint: '支持 Clash / V2Ray / Base64；末尾 #备注 可添加标签；支持 {Ymd} 日期占位符',
          },
        ],
      },
    ],
  },

  /* ── 3. 检测 ────────────────────────────────── */
  {
    tab: 'detection',
    sections: [
      {
        title: '并发控制',
        fields: [
          { key: 'alive-concurrent', label: '测活并发', type: 'number', min: 0, max: 1000, placeholder: '200', hint: '0 = 自动；建议 10–300' },
          { key: 'speed-concurrent', label: '测速并发', type: 'number', min: 0, max: 64, placeholder: '8', hint: '0 = 自动；建议 4–32' },
          { key: 'media-concurrent', label: '媒体并发', type: 'number', min: 0, max: 200, placeholder: '50', hint: '0 = 自动；建议 10–200' },
        ],
      },
      {
        title: '节点要求',
        fields: [
          { key: 'success-limit', label: '节点数量上限', type: 'number', min: 0, placeholder: '200', hint: '0 = 不限制，建议 100-200' },
        ],
      },
      {
        title: '延迟参数',
        fields: [
          { key: 'timeout', label: '超时时间 (ms)', type: 'number', min: 1000, max: 15000, placeholder: '6000', hint: '节点延迟上限，建议 3000–10000' },
        ],
      },
      {
        title: '测速参数',
        fields: [
          {
            key: 'speed-test-url', label: '测速地址', type: 'text', fullWidth: true,
            placeholder: 'random',
            hint: 'random = 随机测速；留空关闭测速；Speedtest/CF 链接易被屏蔽，建议使用自建地址',
          },
          { key: 'min-speed', label: '最低速度 (KB/s)', type: 'number', min: 0, placeholder: '128', hint: '低于此值的节点将被丢弃，0 = 不过滤' },
          { key: 'download-timeout', label: '下载超时 (s)', type: 'number', min: 0, max: 30, placeholder: '10', hint: '测速单节点超时，建议 10s' },
          { key: 'download-mb', label: '单节点上限 (MB)', type: 'number', min: 0, max: 100, placeholder: '20', hint: '每节点最大下载量，0 = 不限' },
          { key: 'total-speed-limit', label: '总带宽 (MB/s)', type: 'number', min: 0, max: 2000, placeholder: '0', hint: '全局测速带宽上限，0 = 不限' },
        ],
      },
      {
        title: '解锁检测',
        fields: [
          { key: 'media-check', label: '流媒体检测', type: 'toggle', hint: '检测流媒体和 AI 服务解锁情况' },
          {
            key: 'platforms', label: '检测平台', type: 'chips',
            options: ['iprisk', 'openai', 'gemini', 'youtube', 'tiktok', 'netflix', 'disney', 'x'],
          },
        ],
      },
      {
        title: '功能开关',
        fields: [
          { key: 'keep-success-proxies', label: '保留历次成功节点', type: 'toggle', hint: '防上游更新丢失节点，建议开启' },
          { key: 'rename-node', label: '重命名节点', type: 'toggle', hint: '根据节点 IP 归属地自动重命名' },
          { key: 'enhanced-tag', label: '增强位置标签', type: 'toggle', hint: '添加形如 KR¹-US⁰，SG² 的角标，精确位置' },
          { key: 'isp-check', label: 'ISP 类型检测', type: 'toggle', hint: '检测 isp 类型，比如: [原生|住宅]、[广播|机房]' },
          { key: 'drop-bad-cf-nodes', label: '丢弃 CF 不可达', type: 'toggle', hint: '可能误杀，谨慎开启' },
          { key: 'ipv6', label: '启用 IPv6', type: 'toggle', hint: '建议关闭' },
        ],
      },
      {
        title: '协议筛选',
        fields: [
          {
            key: 'node-type', label: '协议筛选', type: 'chips',
            hint: '留空 = 检测全部协议',
            options: ['ss', 'ssr', 'vmess', 'vless', 'snell', 'trojan', 'hysteria', 'hysteria2', 'tuic', 'wireguard', 'ssh', 'mieru', 'anytls', 'sudoku', 'masque', 'trusttunnel', 'socks5', 'http'],
          },
        ],
      },
    ],
  },

  /* ── 4. 存储 ─────────────────────────────────────────── */
  {
    tab: 'storage',
    sections: [
      {
        title: '存储方式',
        fields: [
          {
            key: 'save-method', label: '存储方式', type: 'select', hint: '支持上传 webdav，GitHub gist，R2，s3',
            selectWidth: '500px',
            options: [
              { value: 'local', label: '本地 (local)' },
              { value: 'webdav', label: 'WebDAV' },
              { value: 'gist', label: 'GitHub Gist' },
              { value: 'r2', label: 'R2 存储' },
              { value: 's3', label: 'S3 / MinIO' },
            ],
          },
          {
            key: 'output-dir', label: '输出目录', type: 'text', placeholder: '/data/output',
            hint: '留空 = 程序目录 /output',
            links: [{ label: '内置文件服务', href: '/files', icon: 'files' }],
          },
        ],
      },
      {
        title: 'WebDAV', conditional: 'webdav',
        fields: [
          { key: 'webdav-url', label: 'WebDAV 地址', type: 'text', fullWidth: true, placeholder: 'https://dav.example.com/remote.php/dav/files/user/' },
          { key: 'webdav-username', label: '用户名', type: 'text', placeholder: 'admin' },
          { key: 'webdav-password', label: '密码', type: 'password', placeholder: '输入密码' },
        ],
      },
      {
        title: 'GitHub Gist', conditional: 'gist',
        fields: [
          { key: 'github-gist-id', label: 'Gist ID', type: 'text', placeholder: 'a1b2c3d4e5f6...' },
          { key: 'github-token', label: 'GitHub Token', type: 'password', placeholder: 'ghp_xxxxxxxxxxxx' },
          { key: 'github-api-mirror', label: 'API 镜像', type: 'text', placeholder: 'https://api.github.com/', hint: '可选，留空使用 api.github.com' },
        ],
      },
      {
        title: 'CF R2 存储', conditional: 'r2',
        fields: [
          { key: 'worker-url', label: 'Woker 地址', type: 'text', placeholder: 'https://example.worker.dev', hint: "将测速结果推送到 Cloudflare Worker的地址" },
          { key: 'worker-token', label: 'Worker令牌', type: 'password', placeholder: '1234567890' },
        ],
      },
      {
        title: 'S3 / MinIO', conditional: 's3',
        fields: [
          { key: 's3-endpoint', label: 'Endpoint', type: 'text', placeholder: '127.0.0.1:9000' },
          { key: 's3-access-id', label: 'Access Key ID', type: 'text', placeholder: 'minioadmin' },
          { key: 's3-secret-key', label: 'Secret Key', type: 'password', placeholder: '输入密钥' },
          { key: 's3-bucket', label: 'Bucket', type: 'text', placeholder: 'subs-check' },
          { key: 's3-use-ssl', label: '使用 SSL', type: 'toggle' },
          {
            key: 's3-bucket-lookup', label: 'Bucket 寻址', type: 'select',
            selectWidth: '500px',
            options: [
              { value: 'auto', label: '自动 (auto)' },
              { value: 'path', label: 'Path 寻址' },
              { value: 'dns', label: 'DNS 寻址' },
            ],
          },
        ],
      },
      {
        title: '节点标签',
        fields: [
          { key: 'node-prefix', label: '节点前缀', type: 'text', placeholder: 'Ubuntu-', hint: '依赖"检测 - 重命名节点"开关' },
        ],
      },
      {
        title: '订阅操作 (Sub-Store)',
        fields: [
          {
            key: 'sub-process.resolve-domain', label: 'DNS 解析', type: 'toggle',
            hint: '解析节点域名为 IP；固定使用 Ali DNS / IPv6 / 缓存启用',
          },
          {
            key: 'sub-process.node-split', label: '节点裂变', type: 'toggle',
            hint: '将 DNS 解析到的多个 IP 展开为独立节点；自动开启 DNS 解析',
          },
          {
            key: 'sub-process.sub-info', label: '注入流量信息节点', type: 'toggle',
            hint: '在订阅开头注入虚拟节点，用于在客户端展示剩余流量、更新时间等信息',
          },
        ],
      },
      {
        title: '订阅筛选 (Sub-Store)',
        fields: [
          {
            key: 'sub-process.regex-filter-keep',
            label: '筛选模式',
            type: 'select',
            hint: '白名单=仅保留匹配节点；黑名单=丢弃匹配节点',
            options: [
              { value: 'true', label: '白名单丨保留模式' },
              { value: 'false', label: '黑名单丨过滤模式' },
            ],
          },
          {
            key: 'sub-process.regex-filter',
            label: '正则筛选',
            type: 'url-list',
            hint: '每行一条正则，模式由上方"筛选模式"决定；留空不筛选',
            hintExamples: [
              '(.*GPT⁺.*)(.*GM.*)',
              '.*\\bUS[¹²]\\b.*',
            ],
          },
        ],
      },
      {
        title: '订阅排序 (Sub-Store)',
        fields: [
          {
            key: 'sub-process.regex-sort', label: '正则排序', type: 'url-list',
            hint: '按优先级填写正则表达式，匹配的节点排在前面；留空不排序',
            hintExamples: [
              '.*\\bSG[¹²]\\b.*',
              '(.*GPT⁺.*)(.*GM.*)',
              '.*GPT⁺.*',
              '.*\\bYT\\b(?!-CN).*',
            ],
          },
        ],
      },
    ],
  },

  /* ── 5. 通知 ─────────────────────────────────────────── */
  {
    tab: 'notify',
    sections: [
      {
        title: 'Apprise 通知',
        fields: [
          {
            key: 'apprise-api-server', label: 'Apprise API 地址', type: 'text', fullWidth: true,
            placeholder: 'https://apprise.example.com/notify',
            hint: '填写搭建的apprise API server 地址，配置后可向 100+ 渠道发送通知',
            links: [{ label: '部署通知服务', href: 'https://github.com/sinspired/apprise_vercel', icon: 'github' }],
          },
          {
            key: 'recipient-url', label: '通知渠道', type: 'url-list',
            hint: '支持 100+ 通知渠道，覆盖版本更新、节点状态和内置数据库更新通知，建议配置！',
            hintExamples: [
              "tgram://",
              "bark://",
              "mailto://",
              "ntfy://",
              "dingtalk://",
            ],
            links: [{ label: '渠道配置文档', href: 'https://sinspired.github.io/apprise_vercel/docs/QuicSet', icon: 'docs' }],
          },
          {
            key: 'notify-title', label: '通知标题', type: 'text', fullWidth: true, placeholder: '🔔 节点状态更新',
            hint: '自定义检测完成后发送可用节点数量的通知标题'
          },
        ],
      },
    ],
  },

  /* ── 6. 高级 ─────────────────────────────────────────── */
  {
    tab: 'advanced',
    sections: [
      {
        title: '终端显示',
        fields: [
          { key: 'print-progress', label: '终端显示进度', type: 'toggle' },
          {
            key: 'progress-mode', label: '进度条模式', type: 'select', hint: '分阶段 = 当前阶段进度完成后显示下一阶段进度',
            selectWidth: '500px',
            options: [
              { value: 'auto', label: '自动 (auto)' },
              { value: 'stage', label: '分阶段 (stage)' },
            ],
          },
        ],
      },
      {
        title: 'WebUI',
        fields: [
          { key: 'listen-port', label: '监听端口', type: 'text', placeholder: ':8199', hint: '监听端口，用于 WebUI，直接返回节点信息等' },
          { key: 'enable-web-ui', label: '启用 Web 控制面板', type: 'toggle' },
          { key: 'api-key', label: 'API 密钥', type: 'password', placeholder: '留空自动生成', hint: '留空则启动时自动生成，需在终端查看' },
          {
            key: 'share-password', label: '分享密码', type: 'password', placeholder: '输入密码', hint: '访问 /sub/{password}/all.yaml 分享订阅',
            links: [{ label: '加密分享', href: '/share', icon: 'files' }],
          },
        ],
      },
      {
        title: '自动更新',
        fields: [
          { key: 'update', label: '自动更新', type: 'toggle', hint: '关闭时仅提醒新版本' },
          { key: 'update-on-startup', label: '启动时检查更新', type: 'toggle' },
          { key: 'prerelease', label: '使用预发布版本', type: 'toggle', hint: '包含 beta / rc 版本' },
          { key: 'cron-check-update', label: '检查更新 Cron', type: 'cron', fullWidth: true, placeholder: '0 9,21 * * *', hint: '# 定时检查版本更新' },
          { key: 'update-timeout', label: '下载超时 (分钟)', type: 'number', min: 1, placeholder: '2', hint: '下载更新文件的最大时间，如更新失败或网络环境恶劣，可适当调大' },
        ],
      },
      {
        title: 'Sub-Store',
        fields: [
          { key: 'sub-store-port', label: '监听端口', type: 'text', placeholder: ':8299', hint: 'sub-store的启动端口，为空则不启动sub-store' },
          { key: 'sub-store-path', label: '访问路径', type: 'text', placeholder: '/sub-store-path', hint: '建议设置以避免泄露；留空自动生成随机路径' },
          { key: 'mihomo-overwrite-url', label: 'Mihomo 覆写 URL', type: 'text', fullWidth: true, placeholder: 'http://127.0.0.1:8199/Sinspired_Rules_CDN.yaml', hint: '用于生成带指定规则的 mihomo/clash.meta 订阅链接', },
          { key: 'sub-store-sync-cron', label: '同步 Gist Cron', type: 'cron', fullWidth: true, placeholder: '55 5-23/2 * * *', hint: '定时将订阅/文件上传到私有 Gist. 在前端, 叫做 同步 或 同步配置.', },
          { key: 'sub-store-produce-cron', label: '更新订阅 Cron', type: 'text', fullWidth: true, placeholder: '0 */2 * * *,sub,sub', hint: ' 0 */2 * * *,sub,sub_A;0 */3 * * *,col,col_B = 每 2 小时处理一次单条订阅 sub_A，每 3 小时处理一次组合订阅 col_B。', },
          { key: 'sub-store-push-service', label: 'Push 推送服务', type: 'text', fullWidth: true, placeholder: 'https://push.example.com', hint: '例如：Bark: https://api.day.app/XXXXXXXXXXXX/[推送标题]/[推送内容]，在拉取失败时发送通知', },
        ],
      },
      {
        title: 'Singbox 规则',
        fields: [
          {
            key: 'singbox-latest.version', label: '最新版本号', type: 'text',
            placeholder: '1.12',
            hint: 'singbox 最新版，Android / Windows 客户端首选',
          },
          {
            key: 'singbox-latest.json', label: '最新版规则 JSON', type: 'text', fullWidth: true,
            placeholder: 'https://raw.githubusercontent.com/sinspired/sub-store-template/main/1.12.x/sing-box.json',
            hint: '分流规则文件地址，留空使用内置默认值',
            links: [{ label: '查看模板仓库', href: 'https://github.com/sinspired/sub-store-template', icon: 'github' }],
          },
          {
            key: 'singbox-latest.js', label: '最新版处理脚本', type: 'text', fullWidth: true,
            placeholder: 'https://raw.githubusercontent.com/sinspired/sub-store-template/main/1.12.x/sing-box.js',
            hint: '节点处理脚本地址，留空使用内置默认值',
          },
          {
            key: 'singbox-old.version', label: '兼容版本号', type: 'text',
            placeholder: '1.11',
            hint: 'iOS 等旧客户端兼容版本（如 1.11）',
          },
          {
            key: 'singbox-old.json', label: '兼容版规则 JSON', type: 'text', fullWidth: true,
            placeholder: 'https://raw.githubusercontent.com/sinspired/sub-store-template/main/1.11.x/sing-box.json',
          },
          {
            key: 'singbox-old.js', label: '兼容版处理脚本', type: 'text', fullWidth: true,
            placeholder: 'https://raw.githubusercontent.com/sinspired/sub-store-template/main/1.11.x/sing-box.js',
          },
        ],
      },
      {
        title: '其他',
        fields: [
          { key: 'maxmind-db-path', label: 'MaxMind DB 路径', type: 'text', fullWidth: true, placeholder: '/data/GeoLite2-City.mmdb', hint: '留空则使用内置数据库' },
          { key: 'callback-script', label: '回调脚本路径', type: 'text', fullWidth: true, placeholder: '/data/scripts/notify.sh', hint: '检测完成后执行的回调脚本路径', },
        ],
      },
    ],
  },
];

/* ════════════════════════════════════════════════════════════
   并发估算工具（对应后端 NewLogDecay / NewExpDecay / NewPowerDecay）
════════════════════════════════════════════════════════════ */

const _logDecay = (amp, k, base) => x => { const v = Math.log(1 + k * x); return base + amp * (v > 0 ? v / (1 + v) : 0); };
const _expDecay = (amp, b, base) => x => base + amp * (1 - Math.exp(-b * x));
const _powerDecay = (amp, p, alpha, base) => x => { if (x <= 0) return base; const xp = x ** p; return base + amp * xp / (xp + alpha); };
const _roundInt = v => Math.round(v);

/** 从表单读取 number 字段当前值，找不到则返回 fallback */
function _readNum(key, fallback = 0) {
  const inp = document.querySelector(`input[type="number"][data-key="${key}"]`);
  const v = parseFloat(inp?.value);
  return (isNaN(v) || v < 0) ? fallback : v;
}

/**
 * _estimateAuto — 根据 concurrent 基准估算三个阶段的自动并发数。
 * 对应后端 NewProxyChecker 中的自动分支，proxyCount 未知时以 concurrent 代替。
 *
 * @returns {{ alive: number, speed: number, media: number, base: number }}
 */
function _estimateAuto() {
  const base = Math.max(1, _readNum('concurrent', 20));

  const alive = _roundInt(_logDecay(400, 0.005, 400)(base));

  // 测速：有 total-speed-limit 时按带宽/速度比估算，否则用 PowerDecay
  const totalLimit = _readNum('total-speed-limit', 0);
  const minSpeedKB = Math.max(1, _readNum('min-speed', 128));
  let speed;
  if (totalLimit > 0) {
    const r = minSpeedKB / 1024;          // KB/s → MB/s
    speed = Math.max(1, Math.min(Math.round(totalLimit / r), base));
  } else {
    speed = Math.min(base, _roundInt(_powerDecay(32, 1.1, 32, 1)(base)));
  }

  const media = _roundInt(_expDecay(400, 0.001, 100)(base));

  return { alive, speed, media, base };
}

/** 判断三个并发字段中是否有任意一个为 0（触发联动自动模式）*/
function _anyAutoMode() {
  return _readNum('alive-concurrent') === 0
    || _readNum('speed-concurrent') === 0
    || _readNum('media-concurrent') === 0;
}

/* ═══════════════════════════ 字段校验规则 ═══════════════════════════ */
const FIELD_VALIDATORS = {
  'concurrent': v => {
    if (!_anyAutoMode()) return null;
    const n = Number(v);
    const { alive, speed, media } = _estimateAuto();
    if (n > 100) return { level: 'warn', msg: `并发 ${n} 过高，将影响拉取订阅的成功率` };
    return { level: 'info', msg: `自动模式基准 ${n} → 测活 ≈ ${alive} · 测速 ≈ ${speed} · 媒体 ≈ ${media}` };
  },
  'alive-concurrent': v => {
    const n = Number(v);
    if (n === 0 || _anyAutoMode()) {
      const { alive, speed, media, base } = _estimateAuto();
      return { level: 'ok', msg: `自适应模式（基准 =${base}）· 测活 ≈ ${alive}` };
    }
    if (n > 1000) return { level: 'warn', msg: `并发 ${n} 过高，超出多数路由器处理能力，可能影响正常上网，建议 100–300` };
    if (n > 500) return { level: 'warn', msg: `并发 ${n} 过高，超出多数路由器处理能力，建议 100–300` };
    if (n > 300) return { level: 'info', msg: `并发 ${n} 偏高，请确认机器性能` };
    return null;
  },

  'speed-concurrent': v => {
    const n = Number(v);
    if (n === 0 || _anyAutoMode()) {
      const { alive, speed, media, base } = _estimateAuto();
      return { level: 'ok', msg: `自适应模式（基准 =${base}）· 测速 ≈ ${speed}` };
    }
    if (n > 32) return { level: 'warn', msg: `并发 ${n} 较高，测速会占用大量带宽，建议配合 total-speed-limit` };
    return null;
  },

  'media-concurrent': v => {
    const n = Number(v);
    if (n === 0 || _anyAutoMode()) {
      const { alive, speed, media, base } = _estimateAuto();
      return { level: 'ok', msg: `自适应模式（基准 =${base}）· 媒体 ≈ ${media}` };
    }
    if (n > 200) return { level: 'warn', msg: `并发 ${n} 较高，建议不超过 200` };
    if (n > 0 && n <= 100) return { level: 'info', msg: `并发 ${n} 合理` };
    return null;
  },
  'timeout': v => { const n = Number(v); if (n < 3000) return { level: 'warn', msg: `超时 ${n}ms 过短，可能大量误杀正常节点` }; if (n > 15000) return { level: 'info', msg: `超时 ${n}ms 较长，单次检测耗时会明显增加` }; return null; },
  'check-interval': v => { const n = Number(v); if (!n) return null; if (n < 120) return { level: 'warn', msg: `间隔 ${n} 分钟过于频繁，易触发运营商阻断，建议 ≥ 720` }; if (n < 360) return { level: 'info', msg: `间隔 ${n} 分钟偏短，建议 720+` }; if (n >= 720) return { level: 'ok', msg: `间隔 ${n} 分钟（约 ${Math.round(n / 60)} 小时），频率合理` }; return null; },
  'min-speed': v => { const n = Number(v); if (n === 0) return { level: 'info', msg: '未设置最低速度，极慢节点均会保留' }; if (n > 2000) return { level: 'warn', msg: `${n} KB/s 偏高，建议 ≤ 500` }; return null; },
  'download-timeout': v => { if (Number(v) === 0) return { level: 'warn', msg: '未设置，极慢节点会阻塞测速队列，建议设为 10s' }; if (Number(v) > 15) return { level: 'warn', msg: '下载超时不宜设置过高，节点易被测死' }; return null; },
  'download-mb': v => { if (Number(v) === 0) return { level: 'info', msg: '未限制单节点下载量，高并发时可能消耗大量流量，建议 20 MB' }; if (Number(v) >= 100) return { level: 'warn', msg: '过大的下载量将对代理节点造成较大压力，建议 20 MB' }; return null; },
  'success-limit': v => { const n = Number(v); if (n > 0 && n < 5) return { level: 'info', msg: `保存上限 ${n} 较少，建议 100-200` }; if (n >= 100 && n < 200) return { level: 'info', msg: `保存上限 ${n}，视手机性能，mihomo 类 VPN 超过 100 个节点会增加分组切换压力` }; if (n > 200) return { level: 'warn', msg: `保存上限 ${n} 较多，建议不超过 200` }; return null; },

  /* success-rate 校验：入参为界面显示值（0–100%），存储值为其 ÷1000 */
  'success-rate': v => {
    const n = Number(v);

    // 辅助函数：根据数值选择显示单位
    const formatRate = (val) => {
      if (val < 10) {
        // <1% 时显示千分比
        return `${val}‰`;
      }
      // ≥1% 时显示百分比
      return `${(val / 10).toFixed(1)}%`;
    };

    if (n === 0) {
      return { level: 'info', msg: '0 = 不打印成功率信息' };
    }
    if (n > 0 && n <= 10) {
      return { level: 'ok', msg: `阈值 ${formatRate(n)} 推荐，仅打印几乎完全失效的订阅源信息` };
    }
    if (n > 10 && n <= 50) {
      return { level: 'info', msg: `阈值 ${formatRate(n)}` };
    }
    if (n > 50 && n <= 100) {
      return { level: 'warn', msg: `阈值 ${formatRate(n)} 考验订阅压力，将打印更多订阅成功率信息` };
    }
    if (n > 100 && n <= 300) {
      return { level: 'warn', msg: `阈值 ${formatRate(n)} 达标订阅比较少，打印成功率信息将淹没日志` };
    }
    if (n > 300) {
      return { level: 'warn', msg: `阈值 ${formatRate(n)} 过高，打印成功率信息将淹没日志` };
    }

    // 兜底提示
    return { level: 'info', msg: `阈值 ${formatRate(n)} 已应用` };
  },
};


/* ═══════════════════════════ 字段值双向变换 ═══════════════════════════
 * load : 从配置对象 → 界面显示值
 * save : 从界面显示值 → 配置对象
 * ─────────────────────────────────────────────────────────────────── */
const VALUE_TRANSFORM = {
  /**
   * success-rate：后端存 0–1（如 0.5），界面显示 0–100（如 50%）
   *   load: 0.5  → 50
   *   save: 50   → 0.5
   */
  'success-rate': {
    load: v => {
      if (v == null || v === '') return v;
      return +(Number(v) * 1000).toFixed(4);   // 0.03 → 3.0（避免浮点尾巴）
    },
    save: v => {
      const n = parseFloat(v);
      if (isNaN(n)) return 0;
      return +(n / 1000).toFixed(6);            // 3 → 0.03
    },
  },
  // sub-process.regex-filter-keep：后端存 bool，select 控件存 "true"/"false" 字符串
  'sub-process.regex-filter-keep': {
    load: v => (v === false || v === 'false') ? 'false' : 'true',  // bool → string
    save: v => v !== 'false',   // string → bool
  },
};


/* ═══════════════════════════ 特殊保留值定义 ═══════════════════════════
 * 当文本框的值与列表中某项完全匹配时，显示高亮样式 + 标签徽章。
 * ─────────────────────────────────────────────────────────────────── */
const SPECIAL_INPUT_VALUES = {
  'speed-test-url': [
    { value: 'random', label: '随机测速', hint: '从内置地址列表随机选择测速目标' },
    { value: '', label: '关闭测速', hint: '留空，不进行下载测速' },
  ],
  'system-proxy': [
    { value: 'direct', label: '直连', hint: '强制直连，不使用任何系统代理' },
    { value: '', label: '自动', hint: '留空，自动检测系统代理' },
  ],
  'sub-store-port': [
    { value: '', label: '禁用 Sub Store 服务', hint: '未设置端口，Sub Store 服务禁用' },
  ]
};

/* ═══════════════════════════ Cron 段元数据 ═══════════════════════════ */
const _CRON_SEGMENTS = [
  { label: '分钟', title: '分钟 (0–59)' },
  { label: '小时', title: '小时 (0–23)' },
  { label: '日期', title: '日期 (1–31)' },
  { label: '月份', title: '月份 (1–12)' },
  { label: '星期', title: '星期 (0–7，0=周日)' },
];


/* ═══════════════════════════ Cron 值解析 ═══════════════════════════ */
function _parseCronValue(v) {
  const raw = (v ?? '').trim();
  const commented = raw.startsWith('#');
  const expr = commented ? raw.replace(/^#\s*/, '').trim() : raw;
  const parts = expr ? expr.split(/\s+/) : [];
  // 统一使用增强版 _isValidCron，避免两处重复逻辑
  const valid = _isValidCron(expr);
  return { raw, commented, expr, parts, valid };
}

/* ═══════════════════════════ 模块级共享状态 ═══════════════════════════ */
let _cfg = {};
const _built = new Set();

let _leftTab = null;
let _rightTab = null;
let _splitOn = false;
let _pendingSlot = null;
let _lastTab = null;
let _resizeTimer = null;


/* ════════════════════════════════════════════════════════════
   宽度计算
════════════════════════════════════════════════════════════ */
const SIDEBAR_W = 160;
const LAYOUT_GAPS = 16;          // ← 补回这行
const MIN_PANEL_W_SHOW = 280;
const MIN_PANEL_W_AUTO = 340;

function _editorW() { return (window.innerWidth - SIDEBAR_W - LAYOUT_GAPS) / 2; }
function _canShowSplitBtn() { return _editorW() >= MIN_PANEL_W_SHOW * 2; }
function _canSplit() { return _editorW() >= MIN_PANEL_W_AUTO * 2; }


/* ════════════════════════════════════════════════════════════
   嵌套对象 ↔ 点分隔键 互转
   ─────────────────────────────────────────────────────────
   · SCHEMA 中 key = 'singbox-latest.version' 即为嵌套路径
   · renderConfigForm 时将配置对象展平为点分隔键，存入 _cfg
   · collectConfigForm 时将 _cfg 还原为嵌套对象后返回
   · 不影响任何现有平铺键（如 'save-method'）
════════════════════════════════════════════════════════════ */
function _flattenCfg(obj, prefix = '', out = {}) {
  for (const [k, v] of Object.entries(obj ?? {})) {
    const key = prefix ? `${prefix}.${k}` : k;
    if (v !== null && typeof v === 'object' && !Array.isArray(v)) {
      _flattenCfg(v, key, out);
    } else {
      out[key] = v;
    }
  }
  return out;
}

function _unflattenCfg(flat) {
  const out = {};
  for (const [key, val] of Object.entries(flat)) {
    const parts = key.split('.');
    let cur = out;
    for (let i = 0; i < parts.length - 1; i++) {
      // 若已存在但不是普通对象（如被赋为数组/基本类型），覆盖为对象
      if (typeof cur[parts[i]] !== 'object' || cur[parts[i]] === null || Array.isArray(cur[parts[i]])) {
        cur[parts[i]] = {};
      }
      cur = cur[parts[i]];
    }
    cur[parts[parts.length - 1]] = val;
  }
  return out;
}

/* ═══════════════════════════ DOM 工具 ═══════════════════════════ */
function el(tag, attrs = {}) {
  const e = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (k === 'class') e.className = v;
    else if (k === 'textContent') e.textContent = v;
    else if (k === 'innerHTML') e.innerHTML = v;
    else if (k.startsWith('data-')) e.dataset[k.slice(5)] = v;
    else e.setAttribute(k, v);
  }
  return e;
}


/* ═══════════════════════════ 内联校验 ═══════════════════════════ */
const _ICON = {
  warn: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>`,
  info: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>`,
  ok: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>`,
};

function _updateInlineHint(row, result) {
  let h = row.querySelector('.cfg-inline-hint');
  if (!result) { h?.remove(); return; }
  if (!h) { h = el('div', { class: 'cfg-inline-hint' }); row.appendChild(h); }
  h.className = `cfg-inline-hint lvl-${result.level}`;
  h.innerHTML = `${_ICON[result.level] || _ICON.info}<span>${result.msg}</span>`;
}

function _attachValidator(row, fieldDef) {
  if (fieldDef.type !== 'number') return;
  const inp = row.querySelector('input[type="number"]');
  if (!inp) return;

  // ── 有校验函数的字段：绑定校验逻辑 ──
  const fn = FIELD_VALIDATORS[fieldDef.key];
  if (fn) {
    const run = () => _updateInlineHint(row, fn(inp.value));
    inp.addEventListener('input', run);
    inp.addEventListener('change', run);
    requestAnimationFrame(run);
  }

  // ── 三个并发字段互相联动（同 tab 直接触发）──
  const CONCURRENT_KEYS = ['alive-concurrent', 'speed-concurrent', 'media-concurrent'];
  if (CONCURRENT_KEYS.includes(fieldDef.key)) {
    inp.addEventListener('input', () => {
      CONCURRENT_KEYS.filter(k => k !== fieldDef.key).forEach(k => {
        document.querySelector(`input[type="number"][data-key="${k}"]`)
          ?.dispatchEvent(new Event('change'));
      });
    });
  }

  // ── 基准字段：防抖 1s 后串行保存 + 重载 ──
  const TRIGGER_KEYS = ['concurrent', 'total-speed-limit', 'min-speed', 'alive-concurrent', 'speed-concurrent', 'media-concurrent'];
  if (TRIGGER_KEYS.includes(fieldDef.key)) {
    let _basisTimer = null;
    inp.addEventListener('input', () => {
      clearTimeout(_basisTimer);
      _basisTimer = setTimeout(async () => {
        await window.saveConfigWithValidation?.();
        await window.loadConfigValidated?.();
      }, 1500);
    });
  }
}

/* ═══════════════════════════ 链接徽章 ═══════════════════════════ */
const LINK_ICONS = {
  github: `<svg viewBox="0 0 24 24" fill="currentColor"><path d="M12 2C6.477 2 2 6.477 2 12c0 4.42 2.865 8.166 6.839 9.489.5.092.682-.217.682-.482 0-.237-.008-.866-.013-1.7-2.782.603-3.369-1.342-3.369-1.342-.454-1.155-1.11-1.463-1.11-1.463-.908-.62.069-.608.069-.608 1.003.07 1.531 1.03 1.531 1.03.892 1.529 2.341 1.087 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.11-4.555-4.943 0-1.091.39-1.984 1.029-2.683-.103-.253-.446-1.27.098-2.647 0 0 .84-.269 2.75 1.025A9.564 9.564 0 0 1 12 6.844a9.59 9.59 0 0 1 2.504.337c1.909-1.294 2.747-1.025 2.747-1.025.546 1.377.202 2.394.1 2.647.64.699 1.028 1.592 1.028 2.683 0 3.842-2.339 4.687-4.566 4.935.359.309.678.919.678 1.852 0 1.336-.012 2.415-.012 2.741 0 .267.18.578.688.48C19.138 20.163 22 16.418 22 12c0-5.523-4.477-10-10-10z"/></svg>`,
  docs: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/><polyline points="10 9 9 9 8 9"/></svg>`,
  link: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/></svg>`,
  files: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width=2 ><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z" /></svg>`,
};

const _SVG_EYE = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>`;
const _SVG_EYE_OFF = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"/><line x1="1" y1="1" x2="23" y2="23"/></svg>`;

/* Cron 状态徽标用的时钟图标 */
const _SVG_CLOCK = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>`;

/* 模式切换按钮：表单 / YAML 的 SVG 内容 */
const _SVG_MODE_FORM = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" width="13" height="13" style="flex-shrink:0"><line x1="9" y1="6" x2="20" y2="6"/><line x1="9" y1="12" x2="20" y2="12"/><line x1="9" y1="18" x2="20" y2="18"/><circle cx="4" cy="6" r="1.4" fill="currentColor" stroke="none"/><circle cx="4" cy="12" r="1.4" fill="currentColor" stroke="none"/><circle cx="4" cy="18" r="1.4" fill="currentColor" stroke="none"/></svg>`;
const _SVG_MODE_YAML = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" width="13" height="13" style="flex-shrink:0"><polyline points="7 8 3 12 7 16"/><line x1="13" y1="6" x2="11" y2="18"/><polyline points="17 8 21 12 17 16"/></svg>`;

function mkLinks(links) {
  const wrap = el('div', { class: 'cfg-links' });
  for (const lk of links) {
    const a = el('a', {
      class: `cfg-link${lk.icon === 'github' ? ' cfg-link-github' : ''}`,
      href: lk.href, target: '_blank', rel: 'noopener noreferrer', title: lk.href,
    });
    a.innerHTML = (LINK_ICONS[lk.icon] ?? LINK_ICONS.link) + lk.label;
    wrap.appendChild(a);
  }
  return wrap;
}


/* ═══════════════════════════ 控件渲染器 ═══════════════════════════ */

function mkPassword(field, value) {
  const wrap = el('div', { class: 'cfg-pw-wrap' });
  const inp = el('input', {
    class: 'cfg-input cfg-pw-input', type: 'password',
    'data-key': field.key, placeholder: field.placeholder ?? '••••••••',
    autocomplete: 'new-password',
  });
  inp.value = value ?? '';
  const btn = el('button', { type: 'button', class: 'cfg-pw-toggle', title: '显示/隐藏密码' });
  btn.innerHTML = _SVG_EYE;
  btn.addEventListener('click', () => {
    const hidden = inp.type === 'password';
    inp.type = hidden ? 'text' : 'password';
    btn.innerHTML = hidden ? _SVG_EYE_OFF : _SVG_EYE;
    btn.classList.toggle('active', hidden);
  });
  wrap.append(inp, btn);
  return wrap;
}

/**
 * mkInput — 文本输入框
 * 若字段在 SPECIAL_INPUT_VALUES 中定义，则在值匹配保留值时：
 *   · 输入框添加 .cfg-input--special 高亮样式
 *   · 右侧显示小徽章（如"随机测速"/"直连"）
 */
function mkInput(field, value) {
  if (field.type === 'password') return mkPassword(field, value);

  const isExpandable = field.type === 'text' && field.fullWidth;
  const tag = isExpandable ? 'textarea' : 'input';
  const attrs = {
    class: 'cfg-input', 'data-key': field.key, placeholder: field.placeholder ?? '',
  };
  if (isExpandable) {
    attrs.rows = '1'; attrs.spellcheck = 'false';
    attrs.autocomplete = 'off'; attrs.autocorrect = 'off'; attrs.autocapitalize = 'none';
    attrs.class += ' cfg-expandable-input';
  } else {
    attrs.type = 'text';
  }
  const inp = el(tag, attrs);
  inp.value = value ?? '';

  if (isExpandable) {
    let _singleLineH = 0;
    const autoResize = () => {
      if (!inp.matches(':focus')) return;
      inp.style.height = '1px';
      let sh = inp.scrollHeight;
      if (!_singleLineH) {
        const pv = inp.value; inp.value = 'x';
        _singleLineH = inp.scrollHeight;
        inp.value = pv; sh = inp.scrollHeight;
      }
      inp.style.height = sh > _singleLineH ? Math.min(sh, 300) + 'px' : '';
    };
    inp.addEventListener('input', autoResize);
    inp.addEventListener('focus', autoResize);
    inp.addEventListener('blur', () => { inp.style.height = ''; });
    inp.addEventListener('keydown', e => { if (e.key === 'Enter') e.preventDefault(); });
  }

  const specialDefs = SPECIAL_INPUT_VALUES[field.key];
  if (!specialDefs) return inp;

  /* ── 重新设计：胶囊 + 右侧行内提示 ── */
  const wrap = el('div', { class: 'cfg-special-wrap' });

  // 输入框 + 右侧提示层
  const inputWrap = el('div', { class: 'cfg-special-input-wrap' });
  const hintOverlay = el('span', { class: 'cfg-special-hint' });
  inputWrap.append(inp, hintOverlay);

  // 胶囊行
  const chipsRow = el('div', { class: 'cfg-special-chips' });
  specialDefs.forEach(spec => {
    const chip = el('button', {
      type: 'button',
      class: 'cfg-special-chip',
      title: spec.hint,
      textContent: spec.label,
    });
    chip.addEventListener('click', () => {
      // 已激活 → 再次点击清空（回到 placeholder 状态）
      inp.value = inp.value === spec.value ? '' : spec.value;
      inp.dispatchEvent(new Event('input'));
      inp.focus();
    });
    chipsRow.appendChild(chip);
  });

  wrap.append(inputWrap, chipsRow);

  const syncState = () => {
    const v = inp.value;
    const spec = specialDefs.find(s => s.value === v);

    // 右侧提示
    if (spec?.hint) {
      hintOverlay.textContent = spec.hint;
      hintOverlay.classList.add('visible');
    } else {
      hintOverlay.classList.remove('visible');
    }

    // 输入框淡染
    inp.classList.toggle('cfg-input--special', !!spec);

    // 胶囊激活态
    chipsRow.querySelectorAll('.cfg-special-chip').forEach((chip, i) => {
      chip.classList.toggle('active', specialDefs[i].value === v);
    });
  };

  inp.addEventListener('input', syncState);
  requestAnimationFrame(syncState);

  return wrap;
}

function mkNumber(field, value) {
  const wrap = el('div', { class: 'cfg-number-wrap' });
  const inp = el('input', {
    type: 'number', 'data-key': field.key,
    // min: String(field.min ?? ''),
    // max: String(field.max ?? ''),
    step: String(field.step ?? 1), placeholder: field.placeholder ?? '',
  });
  inp.value = (value !== undefined && value !== null && value !== '') ? value : '';
  const makeBtn = (sym, dir) => {
    const b = el('button', { class: 'cfg-step-btn', type: 'button', textContent: sym });
    b.addEventListener('click', () => {
      const cur = parseFloat(inp.value) || 0, step = field.step ?? 1;
      const next = dir > 0 ? cur + step : cur - step;
      inp.value = Math.max(field.min ?? -Infinity, Math.min(field.max ?? Infinity, +next.toFixed(10)));
      inp.dispatchEvent(new Event('input'));
    });
    return b;
  };
  wrap.append(makeBtn('−', -1), inp, makeBtn('+', 1));
  return wrap;
}

function mkToggle(key, value) {
  const wrap = el('div', { class: 'cfg-toggle-wrap' });
  const label = el('label', { class: 'cfg-toggle' });
  const cb = el('input', { type: 'checkbox', 'data-key': key });
  const slider = el('span', { class: 'cfg-toggle-slider' });
  cb.checked = Boolean(value);
  label.append(cb, slider);
  wrap.appendChild(label);
  return wrap;
}

function mkSelect(field, value) {
  const currentVal = (value !== undefined && value !== null) ? String(value) : String(field.options[0]?.value ?? '');
  const currentLabel = field.options.find(o => String(o.value) === currentVal)?.label ?? currentVal;

  const native = el('select', { class: 'cfg-select-native', 'data-key': field.key, 'aria-hidden': 'true' });
  native.style.cssText = 'position:absolute;opacity:0;pointer-events:none;width:0;height:0;';
  for (const opt of field.options) {
    const o = el('option', { value: String(opt.value), textContent: opt.label });
    if (String(opt.value) === currentVal) o.selected = true;
    native.appendChild(o);
  }

  const trigger = el('button', { type: 'button', class: 'cfg-sel-trigger', 'aria-haspopup': 'listbox', 'aria-expanded': 'false' });
  trigger.innerHTML = `<span class="cfg-sel-value">${currentLabel}</span>
    <svg class="cfg-sel-arrow" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="6 9 12 15 18 9"/></svg>`;

  const dropdown = el('div', { class: 'cfg-sel-dropdown', role: 'listbox' });
  dropdown.style.display = 'none';

  for (const opt of field.options) {
    const optVal = String(opt.value);
    const item = el('div', {
      class: `cfg-sel-option${optVal === currentVal ? ' selected' : ''}`,
      role: 'option', 'aria-selected': String(optVal === currentVal),
      'data-value': optVal, textContent: opt.label,
    });
    item.addEventListener('mousedown', e => e.preventDefault());
    item.addEventListener('click', () => {
      trigger.querySelector('.cfg-sel-value').textContent = opt.label;
      native.value = optVal;
      native.dispatchEvent(new Event('change', { bubbles: true }));
      dropdown.querySelectorAll('.cfg-sel-option').forEach(i => {
        i.classList.toggle('selected', i.dataset.value === optVal);
        i.setAttribute('aria-selected', String(i.dataset.value === optVal));
      });
      closeDropdown();
    });
    dropdown.appendChild(item);
  }

  const wrap = el('div', { class: 'cfg-sel-wrap' });
  /* 字段级 selectWidth 覆盖 CSS 默认 max-width */
  if (field.selectWidth) wrap.style.maxWidth = field.selectWidth;
  wrap.append(native, trigger, dropdown);

  const openDropdown = () => { dropdown.style.display = ''; trigger.setAttribute('aria-expanded', 'true'); trigger.classList.add('open'); dropdown.querySelector('.cfg-sel-option.selected')?.scrollIntoView({ block: 'nearest' }); };
  const closeDropdown = () => { dropdown.style.display = 'none'; trigger.setAttribute('aria-expanded', 'false'); trigger.classList.remove('open'); };

  trigger.addEventListener('click', () => dropdown.style.display === 'none' ? openDropdown() : closeDropdown());
  trigger.addEventListener('blur', () => setTimeout(closeDropdown, 120));
  trigger.addEventListener('keydown', e => {
    const items = [...dropdown.querySelectorAll('.cfg-sel-option')];
    const cur = items.findIndex(i => i.classList.contains('selected'));
    if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); dropdown.style.display === 'none' ? openDropdown() : closeDropdown(); }
    else if (e.key === 'ArrowDown') { e.preventDefault(); if (dropdown.style.display === 'none') openDropdown(); items[Math.min(cur + 1, items.length - 1)]?.click(); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); items[Math.max(cur - 1, 0)]?.click(); }
    else if (e.key === 'Escape') { closeDropdown(); }
  });
  return wrap;
}

function mkChips(field, values) {
  const active = new Set(Array.isArray(values) ? values : []);
  const wrap = el('div', { class: 'cfg-chips', 'data-key': field.key });
  for (const opt of field.options) {
    const chip = el('label', { class: `cfg-chip${active.has(opt) ? ' active' : ''}` });
    const cb = el('input', { type: 'checkbox', value: opt });
    cb.checked = active.has(opt);
    cb.addEventListener('change', () => chip.classList.toggle('active', cb.checked));
    chip.append(cb, document.createTextNode(opt));
    wrap.appendChild(chip);
  }
  return wrap;
}

function mkUrlList(field, values) {
  const list = Array.isArray(values) ? values : (values ? [values] : []);
  const wrap = el('div', { class: 'cfg-url-list', 'data-key': field.key });

  // 折行按钮挂到 wrap 上，由 mkField 负责放置到 label 行
  const wrapToggle = el('button', {
    type: 'button',
    class: 'cfg-url-wrap-toggle',
    title: '切换折行',
    textContent: '↵ 折行',
  });
  wrap._wrapToggle = wrapToggle;  // ← 暴露给 mkField

  let wrapOn = false;
  wrapToggle.addEventListener('click', () => {
    wrapOn = !wrapOn;
    wrap.classList.toggle('wrap-mode', wrapOn);
    wrapToggle.classList.toggle('active', wrapOn);
    wrapToggle.textContent = wrapOn ? '→ 单行' : '↵ 折行';
    wrap.querySelectorAll('.cfg-url-input').forEach(t => {
      if (wrapOn) {
        t.style.height = 'auto';
        t.style.height = Math.min(t.scrollHeight, 300) + 'px';
      } else {
        t.style.height = '';
      }
    });
  });

  const addBtn = el('button', { class: 'cfg-url-add', type: 'button', textContent: '+ 添加' });
  wrap.appendChild(addBtn);

  function addRow(val = '') {
    const row = el('div', { class: 'cfg-url-item' });
    const inp = el('textarea', {
      class: 'cfg-input cfg-url-input',
      rows: '1',
      placeholder: 'https://',
      spellcheck: 'false',
      autocomplete: 'off',
      autocorrect: 'off',
      autocapitalize: 'none',
    });
    inp.value = val;

    let _singleLineH = 0;   // ← 每个输入框独立缓存单行基线

    const autoResize = () => {
      if (!inp.matches(':focus') && !wrap.classList.contains('wrap-mode')) return;

      inp.style.height = 'auto';
      let sh = inp.scrollHeight;

      if (!_singleLineH) {
        const pv = inp.value;
        inp.value = 'x'; // 填入单字符获取真实的单行文本高度
        _singleLineH = inp.scrollHeight;
        inp.value = pv;
        sh = inp.scrollHeight; // 还原文本后重新获取当前内容的真实高度
      }

      if (wrap.classList.contains('wrap-mode') || sh > _singleLineH) {
        inp.style.height = Math.min(sh, 300) + 'px';
      } else {
        inp.style.height = ''; // 单行且未处于折行模式时，清除内联高度，回退使用 CSS 的默认 32px
      }
    };

    inp.addEventListener('input', autoResize);
    inp.addEventListener('focus', autoResize);
    inp.addEventListener('blur', () => { inp.style.height = ''; });

    const del = el('button', { class: 'cfg-url-del', type: 'button', title: '删除', textContent: '×' });
    del.addEventListener('click', () => row.remove());
    row.append(inp, del);
    wrap.insertBefore(row, addBtn);
  }

  addBtn.addEventListener('click', () => addRow());
  list.forEach(v => addRow(v));
  return wrap;
}

/* ═══════════════════════════════════════════════════════════════
   Cron 高亮输入控件  mkCronInput
   ───────────────────────────────────────────────────────────────
   结构：
     .cfg-cron-wrap
       .cfg-cron-display     ← 着色展示层（非编辑态可见）
       .cfg-cron-labels-row  ← 段标签行（分钟/小时/日期/月份/星期）
       input.cfg-cron-input  ← 真实 input（编辑态可见，display:none 时 DOM 仍可查）

   · 点击 display / Tab → 进入编辑模式（input 出现）
   · input 失焦 → 退出编辑，重绘 display
   · 外部通过 'cfg-cron-refresh' 事件触发重绘（不切换模式）
═══════════════════════════════════════════════════════════════ */
function mkCronInput(field, value) {
  const wrap = el('div', { class: 'cfg-cron-wrap' });

  /* 真实 input */
  const inp = el('input', {
    class: 'cfg-input cfg-cron-input',
    type: 'text',
    'data-key': field.key,
    placeholder: field.placeholder ?? '0 4,16 * * *',
    spellcheck: 'false',
    autocomplete: 'off',
    autocorrect: 'off',
    autocapitalize: 'none',
  });
  inp.value = value ?? '';

  /* 着色展示层 */
  const display = el('div', {
    class: 'cfg-cron-display',
    tabindex: '0',
    role: 'textbox',
    'aria-label': `${field.label}，点击编辑`,
  });

  /* 段标签行 */
  const labelsRow = el('div', { class: 'cfg-cron-labels-row' });
  _CRON_SEGMENTS.forEach(({ label }, i) =>
    labelsRow.appendChild(el('span', { class: `cfg-cron-label cfg-cron-label-${i}`, textContent: label }))
  );

  /* ── 重绘着色层 ── */
  function renderDisplay() {
    const { raw, commented, parts, valid } = _parseCronValue(inp.value);
    display.className = 'cfg-cron-display';
    display.innerHTML = '';
    // 默认先重置 display，再设 visibility 占位
    labelsRow.style.display = '';
    labelsRow.style.visibility = 'hidden';

    if (!raw) {
      // 暂停/空值时完全隐藏标签行，不占空间，display 层自然上移
      labelsRow.style.display = 'none';
      display.appendChild(el('span', { class: 'cfg-cron-placeholder', textContent: field.placeholder ?? '0 4,16 * * *' }));
      return;
    }
    if (commented) {
      display.appendChild(el('span', { class: 'cfg-cron-comment-mark', textContent: '#\u2009' }));
    }
    if (valid) {
      parts.forEach((part, i) => {
        display.appendChild(el('span', {
          class: `cfg-cron-seg cfg-cron-seg-${i}${commented ? ' cfg-cron-seg--muted' : ''}`,
          textContent: part,
          title: _CRON_SEGMENTS[i].title,
        }));
      });
      labelsRow.style.visibility = '';  // 有效时显示标签行
    } else {
      // 非空但格式非法：加错误高亮
      display.classList.add('cfg-cron-display--raw', 'cfg-cron-display--error');
      display.appendChild(el('span', { class: 'cfg-cron-raw-text', textContent: inp.value }));
      labelsRow.style.display = 'none';
    }
  }

  /* ── 模式切换 ── */
  function enterEditMode() {
    display.style.display = 'none';
    labelsRow.style.display = 'none';
    inp.style.display = '';
    requestAnimationFrame(() => { inp.focus(); inp.select(); });
  }

  function exitEditMode() {
    /* ── 退出时做完整校验（含字段数量）── */
    const err = _validateCronFull(inp.value);
    if (err) {
      window.showToast?.(err, 'warn', 3500);
      // 错误样式留到 display 层接管
    }
    inp.classList.remove('cfg-cron-input--error');
    clearTimeout(_cronToastTimer);
    inp.style.display = 'none';
    display.style.display = '';
    labelsRow.style.display = '';
    renderDisplay();
  }

  /* ── 实时校验（编辑态） ──────────────────────────────────────────
 * · 空值不报错（允许用户清空）
 * · 非空且非法 → 输入框红边，防抖 600ms 后 showToast 给出具体原因
 * · 合法或清空 → 移除错误样式，取消 toast
 * ─────────────────────────────────────────────────────────────── */
  let _cronToastTimer = null;

  /* ── 实时校验：只校验已输入的字段，不检查字段数量，防抖 700ms 后 toast 报错 ── */
  inp.addEventListener('input', () => {
    const err = _validateCronPartial(inp.value);
    inp.classList.toggle('cfg-cron-input--error', err !== null);

    clearTimeout(_cronToastTimer);
    if (!err) return;

    // 防抖：用户停止输入后再弹出，避免逐字打扰
    _cronToastTimer = setTimeout(() => {
      window.showToast?.(`Cron 错误：${err}`, 'warn', 3000);
    }, 700);
  });

  inp.addEventListener('blur', exitEditMode);
  display.addEventListener('click', enterEditMode);
  display.addEventListener('keydown', e => {
    if (e.key === 'Enter' || e.key === 'F2' || (e.key === ' ' && !e.ctrlKey)) { e.preventDefault(); enterEditMode(); }
  });

  /* 外部重绘钩子（_bindCronInterval 注释切换后调用） */
  inp.addEventListener('cfg-cron-refresh', renderDisplay);

  /* 初始：显示态 */
  inp.style.display = 'none';
  wrap.append(display, labelsRow, inp);
  requestAnimationFrame(renderDisplay);
  return wrap;
}

function renderHintBlock(fieldDef) {
  const wrap = el('span', { class: 'cfg-label-hint' });
  if (fieldDef.hint) {
    wrap.appendChild(document.createTextNode(fieldDef.hint));
  }
  if (fieldDef.hintExamples?.length) {
    // 与 hint 文字之间留一点间距
    if (fieldDef.hint) {
      wrap.appendChild(document.createTextNode('\u2002')); // en space
    }
    fieldDef.hintExamples.forEach(ex => {
      const c = document.createElement('code');
      c.className = 'cfg-hint-code';
      c.textContent = ex;
      wrap.appendChild(c);
    });
  }
  return wrap;
}

/* ═══════════════════════════════════════════════════════════════
   字段行构建
═══════════════════════════════════════════════════════════════ */
function mkField(fieldDef, value) {
  /* 应用加载时的值变换（如 success-rate ×100） */
  const xf = VALUE_TRANSFORM[fieldDef.key];
  const displayValue = xf ? xf.load(value) : value;

  const isFull = ['url-list', 'chips', 'cron'].includes(fieldDef.type) || !!fieldDef.fullWidth;
  const row = el('div', { class: `cfg-field${isFull ? ' full-width' : ''}`, 'data-key': fieldDef.key });

  if (!isFull) {
    const labelCol = el('div', { class: 'cfg-label-col' });
    labelCol.appendChild(el('span', { class: 'cfg-label-text', textContent: fieldDef.label }));
    // hint 添加code示例
    if (fieldDef.hint || fieldDef.hintExamples?.length) {
      labelCol.appendChild(renderHintBlock(fieldDef));
    }
    if (fieldDef.links?.length) labelCol.appendChild(mkLinks(fieldDef.links));
    row.appendChild(labelCol);
  } else {
    // url-list 需要在 label 右侧放折行按钮，先占位，ctrl 构建后再填入
    const labelRow = el('div', { class: 'cfg-label-row' });
    labelRow.appendChild(el('span', { class: 'cfg-label-text', textContent: fieldDef.label }));
    row.appendChild(labelRow);
  }

  const ctrlWrap = el('div', { class: 'cfg-ctrl' });
  if (fieldDef.ctrlWidth) ctrlWrap.style.maxWidth = fieldDef.ctrlWidth;
  let ctrl;
  switch (fieldDef.type) {
    case 'number': ctrl = mkNumber(fieldDef, displayValue); break;
    case 'toggle': ctrl = mkToggle(fieldDef.key, displayValue); break;
    case 'select': ctrl = mkSelect(fieldDef, displayValue); break;
    case 'chips': ctrl = mkChips(fieldDef, displayValue); break;
    case 'url-list': ctrl = mkUrlList(fieldDef, displayValue); break;
    case 'cron': ctrl = mkCronInput(fieldDef, displayValue); break;
    default: ctrl = mkInput(fieldDef, displayValue); break;
  }
  ctrlWrap.appendChild(ctrl);
  row.appendChild(ctrlWrap);

  // url-list：将折行按钮插入 label 行右侧
  if (fieldDef.type === 'url-list') {
    // 加空值防御，避免 ctrl 被包裹或重构后静默失效
    const toggle = ctrl?._wrapToggle;
    if (toggle) {
      row.querySelector('.cfg-label-row')?.appendChild(toggle);
    }
  }

  // full-width 字段的 hint 仍挂在 row 上（跨全列）
  // 非 full-width 的 hint 已在上方 labelCol 内添加，此处跳过
  if (isFull && (fieldDef.hint || fieldDef.hintExamples?.length)) {
    row.appendChild(renderHintBlock(fieldDef));
  }
  if (isFull && fieldDef.links?.length) row.appendChild(mkLinks(fieldDef.links));

  _attachValidator(row, fieldDef);
  return row;
}

/* ── Cron 字段元数据（复用于校验提示） ── */
const _CRON_FIELD_RANGES = [
  { name: '分钟', min: 0, max: 59 },
  { name: '小时', min: 0, max: 23 },
  { name: '日期', min: 1, max: 31 },
  { name: '月份', min: 1, max: 12 },
  { name: '星期', min: 0, max: 7 },
];

/**
 * _validateCronSegment — 校验 Cron 单个段（不含逗号）
 * 支持：* | n | n-m | *\/step | n/step | n-m/step
 * @returns {string|null} null = 合法；string = 错误描述
 */
function _validateCronSegment(seg, fieldIdx) {
  const { name, min, max } = _CRON_FIELD_RANGES[fieldIdx];
  const stepMatch = seg.match(/^(.+?)\/(\d+)$/);
  const base = stepMatch ? stepMatch[1] : seg;
  const step = stepMatch ? Number(stepMatch[2]) : null;

  if (step !== null && step < 1)
    return `${name}：步进值必须 ≥ 1`;

  const inRange = n => { const v = Number(n); return Number.isInteger(v) && v >= min && v <= max; };

  if (base === '*') return null;                          // * 或 */n

  const rangeMatch = base.match(/^(\d+)-(\d+)$/);
  if (rangeMatch) {
    const [, lo, hi] = rangeMatch;
    if (!inRange(lo) || !inRange(hi))
      return `${name}：范围 ${lo}-${hi} 超出 ${min}–${max}`;
    if (Number(lo) > Number(hi))
      return `${name}：起始值 ${lo} 大于结束值 ${hi}`;
    return null;
  }

  if (/^\d+$/.test(base)) {
    if (!inRange(base)) return `${name}：${base} 超出范围 ${min}–${max}`;
    return null;
  }

  return `${name}：含非法字符 "${base}"`;
}

/**
 * _isValidCron — 完整校验（5字段 + 每字段合法）
 */
function _isValidCron(expr) {
  if (!expr?.trim()) return false;
  const parts = expr.trim().split(/\s+/);
  if (parts.length !== 5) return false;
  return parts.every((p, i) =>
    p.split(',').every(seg => seg.length > 0 && _validateCronSegment(seg, i) === null)
  );
}

/**
 * _validateCronPartial — 只校验已输入的字段（用于实时 input 事件）
 * 字段数量不足不报错，只校验已有字段的合法性
 * @returns {string|null}
 */
function _validateCronPartial(val) {
  if (!val?.trim()) return null;
  const parts = val.trim().split(/\s+/);
  for (let i = 0; i < Math.min(parts.length, 5); i++) {
    for (const seg of parts[i].split(',')) {
      const err = _validateCronSegment(seg, i);
      if (err) return err;
    }
  }
  return null;
}

/**
 * _validateCronFull — 退出时完整校验，包含字段数量检查
 * @returns {string|null}
 */
function _validateCronFull(val) {
  if (!val?.trim()) return null;
  const parts = val.trim().split(/\s+/);
  if (parts.length !== 5)
    return `需要 5 个字段（分 时 日 月 周），当前 ${parts.length} 个`;
  return _validateCronPartial(val);
}


/* ═══════════════════════════ Cron ↔ 检测间隔联动 ═══════════════════════
 * 在 schedule 面板构建完成后调用。
 * · Cron 合法 → 在 Cron 字段下方显示"定时计划已启用"徽标
 *               → 检测间隔字段视觉禁用（pointer-events:none + opacity）
 * · Cron 为空或非法 → 徽标隐藏，检测间隔恢复可用
 * · 徽标仅 UI 展示，不写入配置
 * ─────────────────────────────────────────────────────────────────── */
function _bindCronInterval(panel) {
  const cronRow = panel.querySelector('.cfg-field[data-key="cron-expression"]');
  const intervalRow = panel.querySelector('.cfg-field[data-key="check-interval"]');
  if (!cronRow || !intervalRow) return;

  const cronInput = cronRow.querySelector('input[data-key="cron-expression"]');
  if (!cronInput) return;

  /* badge 插到 cronRow 内部 hint/links 之后，而非行间 */
  const badge = el('div', {
    class: 'cfg-cron-badge',
    role: 'button',
    tabindex: '0',
  });
  // 追加到 cronRow 末尾（在 .cfg-ctrl 之后）
  cronRow.appendChild(badge);

  /* 需要禁用的控件 */
  const intervalControls = [
    ...intervalRow.querySelectorAll('input[type="number"]'),
    ...intervalRow.querySelectorAll('button.cfg-step-btn'),
  ];

  // intervalInput 从 intervalRow 取，保持与 intervalControls 来源一致
  const intervalInput = intervalRow.querySelector('input[type="number"]');

  function update() {
    const val = cronInput.value.trim();
    const isValid = _isValidCron(val);
    // 暂停态：值为空 且 存有上次的合法值
    const isPaused = val === '' && !!cronInput.dataset.pausedValue;

    if (isValid) {
      /* ── 启用态：绿色 ── */
      badge.className = 'cfg-cron-badge cfg-cron-badge--active';
      badge.title = '点击暂停定时计划';
      badge.innerHTML = `${_SVG_CLOCK}<span>定时计划已启用 · 禁用检测间隔</span>`;
      intervalControls.forEach(c => { c.disabled = true; });
      intervalRow.classList.add('cfg-field--muted');
    } else if (isPaused) {
      /* ── 暂停态：橙色 ── */
      badge.className = 'cfg-cron-badge cfg-cron-badge--active cfg-cron-badge--paused';
      badge.title = '点击恢复定时计划';
      badge.innerHTML = `${_SVG_CLOCK}<span>定时计划已暂停 · 检测间隔生效</span>`;
      intervalControls.forEach(c => { c.disabled = false; });
      intervalRow.classList.remove('cfg-field--muted');
    } else {
      /* ── 未配置 / 输入中：始终显示，文案根据 check-interval 是否有值区分 ── */
      const ivVal = intervalInput?.value.trim();
      const hasInterval = ivVal !== '' && !isNaN(Number(ivVal)) && Number(ivVal) > 0;
      badge.className = 'cfg-cron-badge cfg-cron-badge--active cfg-cron-badge--paused';
      badge.title = '点击启用定时计划';
      badge.innerHTML = hasInterval
        ? `${_SVG_CLOCK}<span>定时计划未配置 · 检测间隔 ${ivVal} 分钟</span>`
        : `${_SVG_CLOCK}<span>定时计划未配置 · 检测间隔生效</span>`;

      if (cronInput.dataset.pausedValue)
        intervalControls.forEach(c => { c.disabled = false; });
      intervalRow.classList.remove('cfg-field--muted');
    }
  }

  // check-interval 值变化时同步更新 badge 文案
  intervalInput?.addEventListener('input', update);

  function toggleCron() {
    const val = cronInput.value.trim();
    if (_isValidCron(val)) {
      /* 启用 → 暂停：存档当前值，清空 */
      cronInput.dataset.pausedValue = val;
      cronInput.value = '';
    } else if (val === '' && cronInput.dataset.pausedValue) {
      /* 暂停（存档）→ 恢复 */
      cronInput.value = cronInput.dataset.pausedValue;
      delete cronInput.dataset.pausedValue;
    } else if (val === '') {
      /* 未配置 → 启用：用 placeholder 作为默认值填入 */
      cronInput.value = cronInput.placeholder || '0 4,16 * * *';
      delete cronInput.dataset.pausedValue;
    } else {
      return; // 输入中但非法，不响应
    }
    cronInput.dispatchEvent(new Event('input'));
    cronInput.dispatchEvent(new Event('cfg-cron-refresh'));
    requestAnimationFrame(update);

    requestAnimationFrame(() => window.saveConfigWithValidation?.());
  }

  badge.addEventListener('click', toggleCron);
  badge.addEventListener('keydown', e => {
    if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); toggleCron(); }
  });

  cronInput.addEventListener('input', () => {
    // 用户手动输入时，若非空则清除暂停存储，防止误恢复到旧值
    if (cronInput.value.trim() !== '') delete cronInput.dataset.pausedValue;
    update();
  });

  requestAnimationFrame(update);
}

/* ═══════════════════════════ 面板构建 ═══════════════════════════ */
function buildPanel(tabId) {
  const panel = document.getElementById(`panel-${tabId}`); if (!panel) return;
  const schema = SCHEMA.find(s => s.tab === tabId); if (!schema) return;
  panel.innerHTML = '';
  for (const sec of schema.sections) {
    if (sec.conditional) {
      const group = el('div', { class: 'cfg-cond-group', 'data-cond': sec.conditional });
      group.appendChild(el('div', { class: 'cfg-section', textContent: sec.title }));
      sec.fields.forEach(f => group.appendChild(mkField(f, _cfg[f.key])));
      group.style.display = 'none';
      panel.appendChild(group);
    } else {
      panel.appendChild(el('div', { class: 'cfg-section', textContent: sec.title }));
      sec.fields.forEach(f => panel.appendChild(mkField(f, _cfg[f.key])));
    }
  }

  /* 存储：条件分组切换 */
  if (tabId === 'storage') {
    const sel = panel.querySelector('select.cfg-select-native[data-key="save-method"]');
    if (sel) {
      const sync = m => panel.querySelectorAll('.cfg-cond-group[data-cond]').forEach(g => {
        g.style.display = (g.dataset.cond === m) ? '' : 'none';
      });
      sel.addEventListener('change', () => sync(sel.value));
      sync(sel.value);
    }
  }

  /* 任务：Cron ↔ 检测间隔联动 */
  if (tabId === 'schedule') {
    _bindCronInterval(panel);
  }

  _built.add(tabId);
}


/* ════════════════════════════════════════════════════════════
   模式切换按钮外观同步
   ─────────────────────────────────────────────────────────
   · form 模式：按钮显示表单图标 + "表单"，使用 .active 样式
   · yaml 模式：按钮显示 YAML 图标 + "YAML"，使用 .active-yaml 样式
════════════════════════════════════════════════════════════ */
function _updateModeToggle(mode) {
  const btn = document.getElementById('cfgModeToggle');
  if (!btn) return;
  const isYaml = mode === 'yaml';
  btn.innerHTML = isYaml
    ? `${_SVG_MODE_FORM}<span>表单</span>`
    : `${_SVG_MODE_YAML}<span>YAML</span>`;
  btn.classList.toggle('active', !isYaml);
  btn.classList.toggle('active-yaml', isYaml);
  btn.title = isYaml ? '切换到表单模式' : '切换到 YAML 编辑器';
}


/* ════════════════════════════════════════════════════════════
   DOM 状态同步
════════════════════════════════════════════════════════════ */
function applyPanels() {
  const tabBar = document.getElementById('cfgTabBar');
  const panelsWrap = document.getElementById('cfgPanels');
  const splitBtn = document.getElementById('splitViewBtn');
  if (!tabBar) return;

  /* 只有双槽均有内容时才真正进入 split-mode；
     单槽时保持 _splitOn=true 但视觉上铺满单列，
     防止 split-mode 的 flex 布局让空面板撑开空白 */
  // 用显示阈值，让手动开启的双栏在 280-340 也能生效
  const isSplit = _splitOn && _canShowSplitBtn() && Boolean(_leftTab) && Boolean(_rightTab);

  tabBar.querySelectorAll('.cfg-tab[data-tab]').forEach(t => {
    const id = t.dataset.tab;
    t.classList.toggle('active', id === _leftTab);
    t.classList.toggle('active-right', isSplit && id === _rightTab);
    t.setAttribute('aria-selected', String(id === _leftTab || (isSplit && id === _rightTab)));
  });

  document.querySelectorAll('#cfgPanels .cfg-panel').forEach(p => {
    const id = p.id.replace('panel-', '');
    p.classList.toggle('active', id === _leftTab);
    p.classList.toggle('active-right', isSplit && id === _rightTab);
  });

  panelsWrap?.classList.toggle('split-mode', isSplit);

  /* 双栏按钮状态同步：active 状态 + aria-pressed */
  if (splitBtn) {
    splitBtn.classList.toggle('split-active', _splitOn && _canShowSplitBtn());
    splitBtn.setAttribute('aria-pressed', String(_splitOn && _canShowSplitBtn()));
  }
}


/* ════════════════════════════════════════════════════════════
   公开 API
════════════════════════════════════════════════════════════ */

/**
 * initConfigForm()
 */
export function initConfigForm() {
  const tabBar = document.getElementById('cfgTabBar');
  const splitBtn = document.getElementById('splitViewBtn');
  const editorContainer = document.getElementById('editorContainer');
  if (!tabBar) return;

  const allTabIds = () =>
    [...tabBar.querySelectorAll('.cfg-tab[data-tab]')].map(t => t.dataset.tab);

  /* ── 初始布局 ─────────────────────────────────────────── */
  function autoInit() {
    const ids = allTabIds();
    if (ids.length === 0) return;

    if (_canSplit()) {
      _splitOn = true;
      _leftTab = ids[0];
      _rightTab = ids[1] ?? ids[0];
      _lastTab = _leftTab;
      if (!_built.has(_leftTab)) buildPanel(_leftTab);
      if (!_built.has(_rightTab)) buildPanel(_rightTab);
    } else {
      _splitOn = false;
      _leftTab = ids[0];
      _rightTab = null;
      _lastTab = _leftTab;
      if (!_built.has(_leftTab)) buildPanel(_leftTab);
    }
    applyPanels();
  }

  /* ── Tab 点击 ─────────────────────────────────────────── */
  function activateTab(id) {
    if (!_built.has(id)) buildPanel(id);

    // 改为 _canShowSplitBtn()，与 applyPanels / toggleSplit 保持一致
    if (!_splitOn || !_canShowSplitBtn()) {
      _leftTab = id;
      _lastTab = id;
      applyPanels();
      return;
    }

    const isLeft = id === _leftTab;
    const isRight = id === _rightTab;

    if (isLeft || isRight) {
      if (isLeft) {
        // 左槽被清空：将右槽提升到左槽，右槽等待填入
        _leftTab = _rightTab;
        _rightTab = null;
        _pendingSlot = 'right';
      } else {
        _rightTab = null;
        _pendingSlot = 'right';
      }
    } else {
      const slot = (_leftTab === null ? 'left' : null)
        ?? _pendingSlot
        ?? (_rightTab === null ? 'right' : null)
        ?? 'right';

      if (slot === 'left') _leftTab = id;
      else _rightTab = id;

      _pendingSlot = null;
      _lastTab = id;
    }

    applyPanels();
  }

  /* ── 双栏按钮 ─────────────────────────────────────────── */
  function toggleSplit() {
    if (!_canShowSplitBtn()) return; // 按钮不可见时不响应
    if (!_canSplit()) {
      // 280-340 区间：手动强制开启，但告知可能偏窄
      // window.showToast?.('窗口略窄，双栏已开启但显示可能偏紧', 'info', 2500);
    }

    _splitOn = !_splitOn;

    if (_splitOn) {
      const ids = allTabIds();
      const cur = _leftTab ?? ids[0];
      const curIdx = ids.indexOf(cur);
      _leftTab = cur;
      _rightTab = ids[(curIdx + 1) % ids.length];
      _pendingSlot = null;
      _lastTab = _leftTab;
      if (!_built.has(_leftTab)) buildPanel(_leftTab);
      if (!_built.has(_rightTab)) buildPanel(_rightTab);
      window.showToast?.('已开启双栏 · 点击正在显示的 Tab 可关闭该槽', 'info', 3000);
    } else {
      _leftTab = _lastTab ?? _leftTab ?? allTabIds()[0];
      _rightTab = null;
      _pendingSlot = null;
      window.showToast?.('已关闭双栏模式', 'info', 2000);
    }

    applyPanels();
  }

  /* ── splitBtn / 模式切换按钮可见性 ──────────────────────── */
  function updateSplitBtnVisibility() {
    if (!splitBtn) return;
    const isYaml = editorContainer?.classList.contains('editor-mode-yaml') ?? false;
    const show = !isYaml && _canShowSplitBtn();
    splitBtn.style.display = show ? '' : 'none';
    // 分隔线与双栏按钮同步显隐
    const divider = document.getElementById('splitDivider');
    if (divider) divider.style.display = show ? '' : 'none';
  }

  /* ── 模式切换：单按钮逻辑 ─────────────────────────────────
   * 1. 直接切换 class（保证始终生效）
   * 2. 仅切换到 YAML 时触发 compat click（CodeMirror init 等副作用）
   *    切换回表单时不触发——admin.js 可能再次切换 class 造成状态污染
   * 3. 后续由 MutationObserver 统一处理 UI / 双栏状态
   * ─────────────────────────────────────────────────────── */
  const modeToggle = document.getElementById('cfgModeToggle');
  if (modeToggle && editorContainer) {
    modeToggle.addEventListener('click', () => {
      const isYaml = editorContainer.classList.contains('editor-mode-yaml');
      const targetMode = isYaml ? 'form' : 'yaml';
      editorContainer.classList.remove('editor-mode-yaml', 'editor-mode-form');
      editorContainer.classList.add(`editor-mode-${targetMode}`);
      if (!isYaml) {
        requestAnimationFrame(() => {
          const yamlBtn = document.getElementById('_cfgBtnYaml');
          if (yamlBtn) yamlBtn.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
        });
      }
    });
  }

  /* ── MutationObserver：监听 YAML/表单 class 切换 ─────────────
   * 进入 YAML → 退出双栏，隐藏双栏按钮
   * 返回表单 → 按当前宽度重新计算双栏状态，恢复按钮显隐
   * ─────────────────────────────────────────────────────────── */
  if (editorContainer) {
    new MutationObserver(() => {
      const isYaml = editorContainer.classList.contains('editor-mode-yaml');
      _updateModeToggle(isYaml ? 'yaml' : 'form');
      updateSplitBtnVisibility();

      if (isYaml) {
        if (_splitOn) {
          _splitOn = false;
          _leftTab = _lastTab ?? _leftTab ?? allTabIds()[0];
          _rightTab = null;
          _pendingSlot = null;
          applyPanels();
        }
      } else {
        /* 返回表单：按当前宽度重新决定双栏状态（不依赖历史意图） */
        const shouldAutoSplit = _canSplit();
        const canShow = _canShowSplitBtn();
        if (shouldAutoSplit && !_splitOn) {
          const ids = allTabIds();
          const cur = _leftTab ?? ids[0];
          const curIdx = ids.indexOf(cur);
          _leftTab = cur;
          _rightTab = ids[(curIdx + 1) % ids.length];
          _pendingSlot = null;
          _lastTab = _leftTab;
          _splitOn = true;
          if (!_built.has(_leftTab)) buildPanel(_leftTab);
          if (!_built.has(_rightTab)) buildPanel(_rightTab);
        } else if (!canShow && _splitOn) {
          // 窗口太窄，强制关闭
          _splitOn = false;
          _leftTab = _lastTab ?? _leftTab ?? allTabIds()[0];
          _rightTab = null;
          _pendingSlot = null;
        }
        applyPanels();
      }
    }).observe(editorContainer, { attributes: true, attributeFilter: ['class'] });
  }

  /* ── 事件绑定 ─────────────────────────────────────────── */
  tabBar.addEventListener('click', e => {
    const btn = e.target.closest('.cfg-tab[data-tab]');
    if (btn) activateTab(btn.dataset.tab);
  });

  if (splitBtn) splitBtn.addEventListener('click', toggleSplit);

  // YAML 按钮点击 → 立即隐藏双栏按钮并退出双栏
  document.querySelector('#cfgModeBar [data-mode="yaml"]')
    ?.addEventListener('click', () => {
      if (splitBtn) splitBtn.style.display = 'none';
      if (_splitOn) {
        _splitOn = false;
        _leftTab = _lastTab ?? _leftTab ?? allTabIds()[0];
        _rightTab = null;
        _pendingSlot = null;
        applyPanels();
      }
    });

  // 按宽度恢复双栏按钮
  document.querySelector('#cfgModeBar [data-mode="form"]')
    ?.addEventListener('click', () => {
      if (_canSplit() && splitBtn) splitBtn.style.display = '';
      applyPanels();
    });


  //  宽度不足时关闭双栏，宽度恢复时自动开启双栏
  window.addEventListener('resize', () => {
    clearTimeout(_resizeTimer);
    _resizeTimer = setTimeout(() => {
      const isYaml = editorContainer?.classList.contains('editor-mode-yaml') ?? false;

      if (!_canShowSplitBtn() && _splitOn) {
        // 连按钮都不该显示了，强制关闭
        _splitOn = false;
        _leftTab = _lastTab ?? _leftTab ?? allTabIds()[0];
        _rightTab = null;
        _pendingSlot = null;
      } else if (_canSplit() && !_splitOn && !isYaml) {
        // 宽度恢复且不在 YAML 模式 → 自动开启
        const ids = allTabIds();
        const cur = _leftTab ?? ids[0];
        const curIdx = ids.indexOf(cur);
        _leftTab = cur;
        _rightTab = ids[(curIdx + 1) % ids.length];
        _pendingSlot = null;
        _lastTab = _leftTab;
        _splitOn = true;
        if (!_built.has(_leftTab)) buildPanel(_leftTab);
        if (!_built.has(_rightTab)) buildPanel(_rightTab);
      }

      updateSplitBtnVisibility();
      applyPanels();
    }, 150);
  }, { passive: true });


  /* 初始化 */
  updateSplitBtnVisibility();
  _updateModeToggle('form'); /* 默认表单模式 */
  autoInit();
  updateSplitBtnVisibility();
}


/**
 * renderConfigForm(configObj)
 */
export function renderConfigForm(configObj) {
  _cfg = _flattenCfg(configObj ?? {});
  _built.clear();

  const toRebuild = new Set([_leftTab, _rightTab].filter(Boolean));
  if (toRebuild.size === 0) {
    _leftTab = SCHEMA[0].tab;
    toRebuild.add(_leftTab);
  }
  toRebuild.forEach(id => buildPanel(id));

  applyPanels();
}


/**
 * collectConfigForm()
 */
export function collectConfigForm() {
  const result = { ..._cfg };
  for (const { tab } of SCHEMA) {
    if (_built.has(tab)) Object.assign(result, collectPanel(tab));
  }
  return _unflattenCfg(result);
}

function collectPanel(tabId) {
  const panel = document.getElementById(`panel-${tabId}`); if (!panel || !_built.has(tabId)) return {};
  const schema = SCHEMA.find(s => s.tab === tabId); if (!schema) return {};
  const out = {};
  for (const sec of schema.sections) {
    for (const field of sec.fields) {
      const { key, type } = field;
      switch (type) {
        case 'toggle': { const cb = panel.querySelector(`input[type="checkbox"][data-key="${key}"]`); if (cb) out[key] = cb.checked; break; }
        case 'number': {
          const inp = panel.querySelector(`input[type="number"][data-key="${key}"]`);
          if (inp) {
            /* 应用保存时的值变换（如 success-rate ÷100） */
            const xf = VALUE_TRANSFORM[key];
            if (xf) {
              out[key] = xf.save(inp.value);
            } else {
              const v = parseFloat(inp.value);
              out[key] = isNaN(v) ? 0 : v;
            }
          }
          break;
        }
        case 'select': {
          const sel = panel.querySelector(`select.cfg-select-native[data-key="${key}"]`);
          if (sel) {
            const raw = field.numericOptions ? parseFloat(sel.value) : sel.value;
            const xf = VALUE_TRANSFORM[key];
            out[key] = xf ? xf.save(raw) : raw;  // ← 新增 xf 判断
          }
          break;
        }
        case 'chips': {
          const w = panel.querySelector(`.cfg-chips[data-key="${key}"]`);
          if (w) out[key] = Array.from(w.querySelectorAll('input:checked')).map(c => c.value);
          break;
        }
        case 'url-list': {
          const w = panel.querySelector(`.cfg-url-list[data-key="${key}"]`)
          if (w) out[key] = Array.from(w.querySelectorAll('.cfg-url-item .cfg-url-input'))
            .flatMap(t => t.value.split('\n'))
            .map(s => s.trim()
              .replace(/\x08/g, '\\b')   // ← 防御存量脏数据
              .replace(/\t/g, '\\t')
            )
            .filter(Boolean)
          break
        }
        default: {
          /* text / password：适配 input 和 textarea */
          const inp = panel.querySelector(`input[data-key="${key}"], textarea[data-key="${key}"]`);
          if (inp) out[key] = inp.value;
          break;
        }
      }
    }
  }
  return out;
}

export { FIELD_VALIDATORS };