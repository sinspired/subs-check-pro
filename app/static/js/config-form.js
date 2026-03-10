/**
 * config-form.js  v4
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

  /* ── 1. 订阅 ────────────────────────────── */
  {
    tab: 'subscriptions',
    sections: [
      {
        title: '获取参数',
        fields: [
          { key: 'sub-urls-retry', label: '重试次数', type: 'number', min: 1, max: 5, placeholder: '3', hint: '获取订阅失败后的重试次数' },
          { key: 'sub-urls-timeout', label: '请求超时 (s)', type: 'number', min: 5, max: 60, placeholder: '10', hint: '网络差可调大，建议 10–60' },
          { key: 'success-rate', label: '成功率阈值 (%)', type: 'number', min: 0, max: 100, placeholder: '0', hint: '低于此值将把订阅标记为失效' },
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

  /* ── 2. 检测 ────────────────────────────────── */
  {
    tab: 'detection',
    sections: [
      {
        title: '并发控制',
        fields: [
          { key: 'alive-concurrent', label: '测活并发', type: 'number', min: 0, max: 1000, placeholder: '200', hint: '0 = 自动；建议 10–300' },
          { key: 'speed-concurrent', label: '测速并发', type: 'number', min: 0, max: 64, placeholder: '8', hint: '0 = 自动；建议 4–32' },
          { key: 'media-concurrent', label: '媒体并发', type: 'number', min: 0, max: 200, placeholder: '50', hint: '0 = 自动；建议 10–200' },
          { key: 'concurrent', label: '基准线程', type: 'number', min: 1, max: 100, placeholder: '20', hint: '影响获取订阅任务，最大 100' },
        ],
      },
      {
        /* 修复：原来与"测速参数"合并在同一对象导致键覆盖，现拆分为两个独立 section */
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
        title: '流媒体检测',
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
          { key: 'keep-success-proxies', label: '保留历次成功节点', type: 'toggle', hint: '防上游更新丢失节点，强烈建议开启' },
          { key: 'enhanced-tag', label: '增强位置标签', type: 'toggle' },
          { key: 'isp-check', label: 'ISP 类型检测', type: 'toggle' },
          { key: 'drop-bad-cf-nodes', label: '丢弃 CF 不可达', type: 'toggle', hint: '可能误杀，谨慎开启' },
          { key: 'ipv6', label: '启用 IPv6', type: 'toggle' },
        ],
      },
    ],
  },

  /* ── 3. 任务 ──────────────────── */
  {
    tab: 'schedule',
    sections: [
      {
        title: '检测计划',
        fields: [
          { key: 'cron-expression', label: 'Cron 表达式', type: 'text', fullWidth: true, placeholder: '0 4,16 * * *', hint: '优先级高于检测间隔；推荐凌晨 4 点和 16 点执行' },
          { key: 'check-interval', label: '检测间隔 (分钟)', type: 'number', min: 1, placeholder: '720', hint: 'Cron 为空时生效；建议 720–1440' },
          { key: 'print-progress', label: '终端显示进度', type: 'toggle' },
          {
            key: 'progress-mode', label: '进度条模式', type: 'select',
            selectWidth: '160px',
            options: [
              { value: 'auto', label: '自动 (auto)' },
              { value: 'stage', label: '分阶段 (stage)' },
            ],
          },
        ],
      },
      {
        title: '节点输出控制',
        fields: [
          { key: 'success-limit', label: '节点保存上限', type: 'number', min: 0, placeholder: '200', hint: '0 = 不限制' },
          {
            key: 'threshold', label: '相似度阈值', type: 'select', numericOptions: true,
            selectWidth: '160px',
            hint: '按网段乱序去重',
            options: [
              { value: '1.00', label: '1.00 — /32' },
              { value: '0.75', label: '0.75 — /24' },
              { value: '0.50', label: '0.50 — /16' },
              { value: '0.25', label: '0.25 — /8' },
            ],
          },
          { key: 'rename-node', label: '重命名节点', type: 'toggle', hint: '根据节点 IP 归属地自动重命名' },
          { key: 'node-prefix', label: '节点前缀', type: 'text', placeholder: '🌟 ', hint: '依赖"重命名节点"开关' },
          {
            key: 'node-type', label: '协议筛选', type: 'chips',
            hint: '留空 = 检测全部协议',
            options: ['ss', 'vmess', 'vless', 'trojan', 'hysteria', 'hy2', 'tuic'],
          },
        ],
      },
      {
        title: '代理设置',
        fields: [
          {
            key: 'system-proxy', label: '系统代理', type: 'text', fullWidth: true,
            placeholder: 'http://127.0.0.1:10808',
            hint: '用于拉取订阅和推送通知；留空则自动检测；修改需重启',
          },
          {
            key: 'github-proxy', label: 'GitHub 代理', type: 'text', fullWidth: true,
            placeholder: 'https://ghfast.top/',
            hint: '加速 GitHub Release 下载；国内强烈建议配置',
            links: [{ label: '自建 CF 代理', href: 'https://github.com/sinspired/CF-Proxy', icon: 'github' }],
          },
          { key: 'ghproxy-group', label: 'GitHub 代理列表', type: 'url-list', hint: '程序自动筛选可用代理，优先级低于 github-proxy' },
        ],
      },
    ],
  },

  /* ── 4. 通知 ─────────────────────────────────────────── */
  {
    tab: 'notify',
    sections: [
      {
        title: 'Apprise 通知',
        fields: [
          {
            key: 'apprise-api-server', label: 'Apprise API 地址', type: 'text', fullWidth: true,
            placeholder: 'https://apprise.example.com/notify',
            hint: '内置服务或自建实例的 notify 接口',
            links: [{ label: '部署通知服务', href: 'https://github.com/sinspired/apprise_vercel', icon: 'github' }],
          },
          {
            key: 'recipient-url', label: '通知渠道', type: 'url-list',
            hint: '支持 tgram:// bark:// mailto:// ntfy:// 等 Apprise 协议',
            links: [{ label: '渠道配置文档', href: 'https://sinspired.github.io/apprise_vercel/docs/QuicSet', icon: 'docs' }],
          },
          { key: 'notify-title', label: '通知标题', type: 'text', placeholder: '🔔 节点状态更新' },
        ],
      },
    ],
  },

  /* ── 5. 存储 ─────────────────────────────────────────── */
  {
    tab: 'storage',
    sections: [
      {
        title: '存储方式',
        fields: [
          { key: 'output-dir', label: '输出目录', type: 'text', placeholder: '/data/output', hint: '留空 = 程序目录 /output' },
          {
            key: 'save-method', label: '存储方式', type: 'select',
            selectWidth: '170px',
            options: [
              { value: 'local', label: '本地 (local)' },
              { value: 'webdav', label: 'WebDAV' },
              { value: 'gist', label: 'GitHub Gist' },
              { value: 's3', label: 'S3 / MinIO / R2' },
            ],
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
          { key: 'github-api-mirror', label: 'API 镜像', type: 'text', placeholder: 'https://ghproxy.com/', hint: '可选，留空使用 api.github.com' },
        ],
      },
      {
        title: 'S3 / MinIO / R2', conditional: 's3',
        fields: [
          { key: 's3-endpoint', label: 'Endpoint', type: 'text', placeholder: '127.0.0.1:9000' },
          { key: 's3-access-id', label: 'Access Key ID', type: 'text', placeholder: 'minioadmin' },
          { key: 's3-secret-key', label: 'Secret Key', type: 'password', placeholder: '输入密钥' },
          { key: 's3-bucket', label: 'Bucket', type: 'text', placeholder: 'subs-check' },
          { key: 's3-use-ssl', label: '使用 SSL', type: 'toggle' },
          {
            key: 's3-bucket-lookup', label: 'Bucket 寻址', type: 'select',
            selectWidth: '160px',
            options: [
              { value: 'auto', label: '自动 (auto)' },
              { value: 'path', label: 'Path 寻址' },
              { value: 'dns', label: 'DNS 寻址' },
            ],
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
        title: 'WebUI',
        fields: [
          { key: 'listen-port', label: '监听端口', type: 'text', placeholder: ':8199' },
          { key: 'enable-web-ui', label: '启用 Web 控制面板', type: 'toggle' },
          { key: 'api-key', label: 'API 密钥', type: 'password', placeholder: '留空自动生成', hint: '留空则启动时自动生成，在终端查看' },
          { key: 'share-password', label: '分享密码', type: 'password', placeholder: '输入密码', hint: '访问 /sub/{password}/all.yaml 分享订阅' },
        ],
      },
      {
        title: '自动更新',
        fields: [
          { key: 'update', label: '自动更新', type: 'toggle', hint: '关闭时仅提醒新版本' },
          { key: 'update-on-startup', label: '启动时检查更新', type: 'toggle' },
          { key: 'prerelease', label: '使用预发布版本', type: 'toggle', hint: '包含 beta / rc 版本' },
          { key: 'cron-check-update', label: '检查更新 Cron', type: 'text', fullWidth: true, placeholder: '0 9,21 * * *' },
          { key: 'update-timeout', label: '下载超时 (分钟)', type: 'number', min: 1, placeholder: '2' },
        ],
      },
      {
        title: 'Sub-Store',
        fields: [
          { key: 'sub-store-port', label: '监听端口', type: 'text', placeholder: ':8299' },
          { key: 'sub-store-path', label: '访问路径', type: 'text', placeholder: '/sub-store-path', hint: '建议设置以避免泄露；留空自动生成随机路径' },
          { key: 'mihomo-overwrite-url', label: 'Mihomo 覆写 URL', type: 'text', fullWidth: true, placeholder: 'http://127.0.0.1:8199/Sinspired_Rules_CDN.yaml' },
          { key: 'sub-store-sync-cron', label: '同步 Gist Cron', type: 'text', fullWidth: true, placeholder: '55 5-23/2 * * *' },
          { key: 'sub-store-produce-cron', label: '更新订阅 Cron', type: 'text', fullWidth: true, placeholder: '0 */2 * * *,sub,sub' },
          { key: 'sub-store-push-service', label: 'Push 推送服务', type: 'text', placeholder: 'https://push.example.com' },
        ],
      },
      {
        title: '其他',
        fields: [
          { key: 'maxmind-db-path', label: 'MaxMind DB 路径', type: 'text', placeholder: '/data/GeoLite2-City.mmdb', hint: '留空则使用内置数据库' },
          { key: 'callback-script', label: '回调脚本路径', type: 'text', placeholder: '/data/scripts/notify.sh', hint: '检测完成后执行' },
        ],
      },
    ],
  },
];


/* ═══════════════════════════ 字段校验规则 ═══════════════════════════ */
const FIELD_VALIDATORS = {
  'alive-concurrent': v => { const n = Number(v); if (n > 500) return { level: 'warn', msg: `并发 ${n} 过高，超出多数路由器处理能力，建议 100–300` }; if (n > 300) return { level: 'info', msg: `并发 ${n} 偏高，请确认机器性能` }; if (n === 0) return { level: 'ok', msg: '自动模式：根据 concurrent 基准自动计算' }; return null; },
  'speed-concurrent': v => { const n = Number(v); if (n > 32) return { level: 'warn', msg: `并发 ${n} 较高，测速会占用大量带宽，建议配合 total-speed-limit` }; if (n === 0) return { level: 'ok', msg: '自动模式' }; return null; },
  'timeout': v => { const n = Number(v); if (n < 1000) return { level: 'warn', msg: `超时 ${n}ms 过短，可能大量误杀正常节点` }; if (n > 30000) return { level: 'info', msg: `超时 ${n}ms 较长，单次检测耗时会明显增加` }; return null; },
  'check-interval': v => { const n = Number(v); if (!n) return null; if (n < 120) return { level: 'warn', msg: `间隔 ${n} 分钟过于频繁，易触发运营商阻断，建议 ≥ 720` }; if (n < 360) return { level: 'info', msg: `间隔 ${n} 分钟偏短，建议 720+` }; if (n >= 720) return { level: 'ok', msg: `间隔 ${n} 分钟（约 ${Math.round(n / 60)} 小时），频率合理` }; return null; },
  'min-speed': v => { const n = Number(v); if (n === 0) return { level: 'info', msg: '未设置最低速度，极慢节点均会保留' }; if (n > 2000) return { level: 'warn', msg: `${n} KB/s 偏高，建议 ≤ 500` }; return null; },
  'download-timeout': v => { if (Number(v) === 0) return { level: 'warn', msg: '未设置，极慢节点会阻塞测速队列，建议设为 10s' }; return null; },
  'download-mb': v => { if (Number(v) === 0) return { level: 'info', msg: '未限制单节点下载量，高并发时可能消耗大量流量，建议 20 MB' }; return null; },
  'success-limit': v => { const n = Number(v); if (n > 0 && n < 30) return { level: 'info', msg: `保存上限 ${n} 较少，建议 ≥ 100` }; return null; },
};


/* ═══════════════════════════ 模块级共享状态 ═══════════════════════════ */
let _cfg = {};
const _built = new Set();

let _leftTab = null;
let _rightTab = null;
let _splitOn = false;
let _pendingSlot = null;
let _lastTab = null;


/* ════════════════════════════════════════════════════════════
   宽度计算
════════════════════════════════════════════════════════════ */
const SIDEBAR_W = 200;
const LAYOUT_GAPS = 16;
const MIN_PANEL_W = 280;

function _editorW() { return (window.innerWidth - SIDEBAR_W - LAYOUT_GAPS) / 2; }
function _canSplit() { return _editorW() >= MIN_PANEL_W * 2; }


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
  const fn = FIELD_VALIDATORS[fieldDef.key];
  if (!fn || fieldDef.type !== 'number') return;
  const inp = row.querySelector('input[type="number"]');
  if (!inp) return;
  const run = () => _updateInlineHint(row, fn(inp.value));
  inp.addEventListener('input', run);
  inp.addEventListener('change', run);
  requestAnimationFrame(run);
}


/* ═══════════════════════════ 链接徽章 ═══════════════════════════ */
const LINK_ICONS = {
  github: `<svg viewBox="0 0 24 24" fill="currentColor"><path d="M12 2C6.477 2 2 6.477 2 12c0 4.42 2.865 8.166 6.839 9.489.5.092.682-.217.682-.482 0-.237-.008-.866-.013-1.7-2.782.603-3.369-1.342-3.369-1.342-.454-1.155-1.11-1.463-1.11-1.463-.908-.62.069-.608.069-.608 1.003.07 1.531 1.03 1.531 1.03.892 1.529 2.341 1.087 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.11-4.555-4.943 0-1.091.39-1.984 1.029-2.683-.103-.253-.446-1.27.098-2.647 0 0 .84-.269 2.75 1.025A9.564 9.564 0 0 1 12 6.844a9.59 9.59 0 0 1 2.504.337c1.909-1.294 2.747-1.025 2.747-1.025.546 1.377.202 2.394.1 2.647.64.699 1.028 1.592 1.028 2.683 0 3.842-2.339 4.687-4.566 4.935.359.309.678.919.678 1.852 0 1.336-.012 2.415-.012 2.741 0 .267.18.578.688.48C19.138 20.163 22 16.418 22 12c0-5.523-4.477-10-10-10z"/></svg>`,
  docs: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/><polyline points="10 9 9 9 8 9"/></svg>`,
  link: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/></svg>`,
};

const _SVG_EYE = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>`;
const _SVG_EYE_OFF = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"/><line x1="1" y1="1" x2="23" y2="23"/></svg>`;

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
    autocomplete: 'current-password',
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

function mkInput(field, value) {
  if (field.type === 'password') return mkPassword(field, value);
  const inp = el('input', { class: 'cfg-input', type: 'text', 'data-key': field.key, placeholder: field.placeholder ?? '' });
  inp.value = value ?? '';
  return inp;
}

function mkNumber(field, value) {
  const wrap = el('div', { class: 'cfg-number-wrap' });
  const inp = el('input', {
    type: 'number', 'data-key': field.key,
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
  const addBtn = el('button', { class: 'cfg-url-add', type: 'button', textContent: '+ 添加' });
  wrap.appendChild(addBtn);
  function addRow(val = '') {
    const row = el('div', { class: 'cfg-url-item' });
    const inp = el('input', { class: 'cfg-input cfg-url-input', type: 'text', placeholder: 'https://' });
    inp.value = val;
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
   字段行构建
═══════════════════════════════════════════════════════════════ */
function mkField(fieldDef, value) {
  const isFull = fieldDef.type === 'url-list' || fieldDef.type === 'chips' || !!fieldDef.fullWidth;
  const row = el('div', { class: `cfg-field${isFull ? ' full-width' : ''}`, 'data-key': fieldDef.key });

  if (!isFull) {
    const labelCol = el('div', { class: 'cfg-label-col' });
    labelCol.appendChild(el('span', { class: 'cfg-label-text', textContent: fieldDef.label }));
    if (fieldDef.links?.length) labelCol.appendChild(mkLinks(fieldDef.links));
    row.appendChild(labelCol);
  } else {
    row.appendChild(el('span', { class: 'cfg-label-text', textContent: fieldDef.label }));
  }

  const ctrlWrap = el('div', { class: 'cfg-ctrl' });
  if (fieldDef.ctrlWidth) ctrlWrap.style.maxWidth = fieldDef.ctrlWidth;
  let ctrl;
  switch (fieldDef.type) {
    case 'number': ctrl = mkNumber(fieldDef, value); break;
    case 'toggle': ctrl = mkToggle(fieldDef.key, value); break;
    case 'select': ctrl = mkSelect(fieldDef, value); break;
    case 'chips': ctrl = mkChips(fieldDef, value); break;
    case 'url-list': ctrl = mkUrlList(fieldDef, value); break;
    default: ctrl = mkInput(fieldDef, value); break;
  }
  ctrlWrap.appendChild(ctrl);
  row.appendChild(ctrlWrap);

  if (fieldDef.hint) row.appendChild(el('span', { class: 'cfg-label-hint', textContent: fieldDef.hint }));
  if (isFull && fieldDef.links?.length) row.appendChild(mkLinks(fieldDef.links));

  _attachValidator(row, fieldDef);
  return row;
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
  if (tabId === 'storage') {
    const sel = panel.querySelector('select.cfg-select-native[data-key="save-method"]');
    if (sel) {
      const sync = m => panel.querySelectorAll('.cfg-cond-group[data-cond]').forEach(g => {
        g.style.display = (g.dataset.cond === m || (m === 'r2' && g.dataset.cond === 's3')) ? '' : 'none';
      });
      sel.addEventListener('change', () => sync(sel.value));
      sync(sel.value);
    }
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
  const isSplit = _splitOn && _canSplit() && Boolean(_leftTab) && Boolean(_rightTab);

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
    splitBtn.classList.toggle('split-active', isSplit);
    splitBtn.setAttribute('aria-pressed', String(isSplit));
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

    if (!_splitOn || !_canSplit()) {
      _leftTab = id;
      _lastTab = id;
      applyPanels();
      return;
    }

    const isLeft = id === _leftTab;
    const isRight = id === _rightTab;

    if (isLeft || isRight) {
      if (isLeft) { _leftTab = null; _pendingSlot = 'left'; }
      else { _rightTab = null; _pendingSlot = 'right'; }
    } else {
      const slot = _pendingSlot
        ?? (_leftTab === null ? 'left' : null)
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
    if (!_canSplit()) {
      window.showToast?.('当前窗口宽度不足以开启双栏', 'info', 2000);
      return;
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
    splitBtn.style.display = (isYaml || !_canSplit()) ? 'none' : '';
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
        const shouldSplit = _canSplit();
        if (shouldSplit && !_splitOn) {
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
        } else if (!shouldSplit && _splitOn) {
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

  /* resize 防抖 */
  let _resizeTimer;
  window.addEventListener('resize', () => {
    clearTimeout(_resizeTimer);
    _resizeTimer = setTimeout(() => {
      const isYaml = editorContainer?.classList.contains('editor-mode-yaml') ?? false;

      if (!_canSplit() && _splitOn) {
        /* 宽度不足，强制关闭双栏 */
        _splitOn = false;
        _leftTab = _lastTab ?? _leftTab ?? allTabIds()[0];
        _rightTab = null;
        _pendingSlot = null;
      }

      updateSplitBtnVisibility();
      applyPanels();
    }, 150);
  }, { passive: true });

  /* 初始化 */
  updateSplitBtnVisibility();
  _updateModeToggle('form'); /* 默认表单模式 */
  autoInit();
}


/**
 * renderConfigForm(configObj)
 */
export function renderConfigForm(configObj) {
  _cfg = configObj ?? {};
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
  return result;
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
        case 'number': { const inp = panel.querySelector(`input[type="number"][data-key="${key}"]`); if (inp) { const v = parseFloat(inp.value); out[key] = isNaN(v) ? 0 : v; } break; }
        case 'select': { const sel = panel.querySelector(`select.cfg-select-native[data-key="${key}"]`); if (sel) out[key] = field.numericOptions ? parseFloat(sel.value) : sel.value; break; }
        case 'chips': { const w = panel.querySelector(`.cfg-chips[data-key="${key}"]`); if (w) out[key] = Array.from(w.querySelectorAll('input:checked')).map(c => c.value); break; }
        case 'url-list': { const w = panel.querySelector(`.cfg-url-list[data-key="${key}"]`); if (w) out[key] = Array.from(w.querySelectorAll('.cfg-url-item input')).map(i => i.value.trim()).filter(Boolean); break; }
        default: { const inp = panel.querySelector(`input[data-key="${key}"]`); if (inp) out[key] = inp.value; break; }
      }
    }
  }
  return out;
}

export { FIELD_VALIDATORS };