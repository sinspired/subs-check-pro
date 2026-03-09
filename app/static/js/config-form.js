/**
 * config-form.js — 表单式配置 UI
 *   import { initConfigForm, renderConfigForm, collectConfigForm } from './config-form.js';
 *
 *   initConfigForm();                  // 页面加载后调用一次
 *   renderConfigForm(parsedYamlObj);   // 登录 + 获取配置后调用
 *   const data = collectConfigForm();  // 保存前调用，返回完整配置对象
 */

/* 
   Schema — 字段定义
   type: 'text' | 'password' | 'number' | 'toggle' | 'select' | 'chips' | 'url-list'
*/
const SCHEMA = [

  // ── 检测 ────────────────────────────────────────────────
  {
    tab: 'detection',
    sections: [
      {
        title: '并发控制',
        fields: [
          {
            key: 'alive-concurrent', label: '测活并发', type: 'number',
            min: 0, max: 1000,
            hint: '0 = 自动，建议 10–1000',
            placeholder: '200',
          },
          {
            key: 'speed-concurrent', label: '测速并发', type: 'number',
            min: 0, max: 64,
            hint: '0 = 自动，建议 4–32',
            placeholder: '8',
          },
          {
            key: 'media-concurrent', label: '媒体并发', type: 'number',
            min: 0, max: 200,
            hint: '0 = 自动，建议 10–200',
            placeholder: '50',
          },
          {
            key: 'concurrent', label: '基准线程', type: 'number',
            min: 1, max: 100,
            hint: '自动并发计算基准，最大 100',
            placeholder: '32',
          },
        ],
      },
      {
        title: '检测阈值',
        fields: [
          {
            key: 'timeout', label: '超时时间 (ms)', type: 'number',
            min: 100, max: 60000,
            hint: '节点延迟上限，建议 3000–30000',
            placeholder: '10000',
          },
          {
            key: 'success-limit', label: '节点保存上限', type: 'number',
            min: 0,
            hint: '0 = 不限制',
            placeholder: '0',
          },
          {
            key: 'threshold', label: '相似度阈值', type: 'number',
            min: 0, max: 1, step: 0.05,
            hint: '0.75 ≈ /24 同网段去重，越小越严格',
            placeholder: '0.75',
          },
          {
            key: 'node-prefix', label: '节点前缀', type: 'text',
            hint: '依赖"按 IP 重命名"开关',
            placeholder: '🌟 ',
          },
        ],
      },
      {
        title: '功能开关',
        fields: [
          { key: 'rename-node', label: '重命名节点', hint: '启用后将根据节点 IP 归属地重命名', type: 'toggle' },
          { key: 'enhanced-tag', label: '增强位置标签', hint: '显示出口 + CDN 双位置', type: 'toggle' },
          { key: 'isp-check', label: 'ISP 类型检测', hint: '检测原生 / 住宅 / 机房', type: 'toggle' },
          { key: 'drop-bad-cf-nodes', label: '丢弃 CF 不可达', hint: '可能误杀，谨慎开启', type: 'toggle' },
          { key: 'keep-success-proxies', label: '保留历次成功节点', hint: '防上游更新丢失节点', type: 'toggle' },
        ],
      },
      {
        title: '流媒体检测',
        fields: [
          { key: 'media-check', label: '流媒体检测', hint: '启用后将检测流媒体和AI解锁情况', type: 'toggle' },
          {
            key: 'platforms', label: '检测平台', type: 'chips',
            options: ['iprisk', 'openai', 'gemini', 'youtube', 'tiktok', 'netflix', 'disney', 'x'],
          },
        ],
      },
      {
        title: '节点过滤',
        fields: [
          {
            key: 'node-type', label: '协议筛选', type: 'chips',
            hint: '留空 = 检测全部协议',
            options: ['ss', 'vmess', 'vless', 'trojan', 'hysteria', 'hy2', 'tuic'],
          },
          { key: 'ipv6', label: '启用 IPv6', type: 'toggle' },
        ],
      },
    ],
  },

  // ── 任务 ────────────────────────────────────────────────
  {
    tab: 'schedule',
    sections: [
      {
        title: '检测周期',
        fields: [
          {
            key: 'check-interval', label: '检测间隔 (分钟)', type: 'number',
            min: 1,
            hint: 'Cron 表达式优先，建议 720–1440',
            placeholder: '720',
          },
          {
            key: 'cron-expression', label: 'Cron 表达式', type: 'text',
            hint: '定时检测任务，留空则使用 check-interval 间隔',
            placeholder: '0 4,16 * * *',
          },
        ],
      },
      {
        // print-progress / progress-mode 独立分组
        title: '输出显示',
        fields: [
          { key: 'print-progress', label: '终端显示进度', type: 'toggle' },
          {
            key: 'progress-mode', label: '进度条模式', type: 'select',
            options: [
              { value: 'auto', label: '自动 (auto)' },
              { value: 'stage', label: '分阶段 (stage)' },
            ],
          },
        ],
      },
    ],
  },

  // ── 订阅 ────────────────────────────────────────────────
  {
    tab: 'subscriptions',
    sections: [
      {
        title: '获取订阅参数',
        fields: [
          {
            key: 'sub-urls-retry', label: '重试次数', type: 'number',
            min: 0,
            hint: '获取订阅失败后的重试次数',
            placeholder: '3',
          },
          {
            key: 'sub-urls-timeout', label: '请求超时 (s)', type: 'number',
            min: 1,
            hint: '网络差可调大，建议 10–60',
            placeholder: '15',
          },
          {
            key: 'success-rate', label: '成功率阈值 (%)', type: 'number',
            min: 0, max: 100,
            hint: '节点成功率低于此值时在日志中显示订阅 URL',
            placeholder: '40',
          },
        ],
      },
      {
        title: '测速参数',
        fields: [
          {
            key: 'speed-test-url', label: '测速地址', type: 'text',
            hint: 'random = 随机测速地址，留空则关闭测速',
            placeholder: 'random',
          },
          {
            key: 'min-speed', label: '最低速度 (KB/s)', type: 'number',
            min: 0,
            hint: '低于此值的节点将被丢弃，0 = 不过滤',
            placeholder: '500',
          },
          {
            key: 'download-timeout', label: '下载超时 (s)', type: 'number',
            min: 1,
            hint: '测速单节点超时，建议 10 秒',
            placeholder: '10',
          },
          {
            key: 'download-mb', label: '单节点上限 (MB)', type: 'number',
            min: 0,
            hint: '每节点最大下载量，0 = 不限，建议设置避免测速过度消耗流量',
            placeholder: '20',
          },
          {
            key: 'total-speed-limit', label: '总带宽限制 (MB/s)', type: 'number',
            min: 0,
            hint: '全局测速带宽上限，0 = 不限',
            placeholder: '0',
          },
        ],
      },
      {
        title: '远程订阅清单',
        fields: [
          {
            key: 'sub-urls-remote', label: '远程订阅列表', type: 'url-list',
            hint: '集中维护订阅源，支持 txt / yaml / json',
          },
        ],
      },
      {
        title: '本地订阅地址',
        fields: [
          {
            key: 'sub-urls', label: '订阅地址', type: 'url-list',
            hint: '支持 Clash / V2Ray / Base64，末尾 #备注 可添加标签',
          },
        ],
      },
    ],
  },

  // ── 通知 ────────────────────────────────────────────────
  {
    tab: 'notify',
    sections: [
      {
        title: 'Apprise 通知',
        fields: [
          {
            key: 'apprise-api-server', label: 'Apprise API 地址', type: 'text',
            hint: '内置服务或自建实例的 notify 接口',
            placeholder: 'https://apprise.example.com/notify',
          },
          {
            key: 'notify-title', label: '通知标题', type: 'text',
            placeholder: '🔔 Subs-Check-Pro 检测报告',
          },
          {
            key: 'recipient-url', label: '通知渠道', type: 'url-list',
            hint: '支持 tgram:// bark:// mailto:// ntfy:// 等 Apprise 协议',
          },
        ],
      },
    ],
  },

  // ── 存储 ────────────────────────────────────────────────
  {
    tab: 'storage',
    sections: [
      {
        title: '存储方式',
        fields: [
          {
            key: 'output-dir', label: '输出目录', type: 'text',
            hint: '留空 = 程序目录 /output',
            placeholder: '/data/output',
          },
          {
            key: 'save-method', label: '存储方式', type: 'select',
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
          { key: 'webdav-url', label: 'WebDAV 地址', type: 'text', placeholder: 'https://dav.example.com/remote.php/dav/files/user/' },
          { key: 'webdav-username', label: '用户名', type: 'text', placeholder: 'admin' },
          { key: 'webdav-password', label: '密码', type: 'password', placeholder: '••••••••' },
        ],
      },
      {
        title: 'GitHub Gist', conditional: 'gist',
        fields: [
          {
            key: 'github-gist-id', label: 'Gist ID', type: 'text',
            placeholder: 'a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4',
          },
          {
            key: 'github-token', label: 'GitHub Token', type: 'password',
            placeholder: 'ghp_xxxxxxxxxxxxxxxxxxxx',
          },
          {
            key: 'github-api-mirror', label: 'API 镜像', type: 'text',
            hint: '可选，留空使用 api.github.com',
            placeholder: 'https://ghproxy.com/',
          },
        ],
      },
      {
        title: 'S3 / MinIO / R2', conditional: 's3',
        fields: [
          { key: 's3-endpoint', label: 'Endpoint', type: 'text', placeholder: '127.0.0.1:9000' },
          { key: 's3-access-id', label: 'Access Key ID', type: 'text', placeholder: 'minioadmin' },
          { key: 's3-secret-key', label: 'Secret Key', type: 'password', placeholder: '••••••••' },
          { key: 's3-bucket', label: 'Bucket', type: 'text', placeholder: 'subs-check' },
          { key: 's3-use-ssl', label: '使用 SSL', type: 'toggle' },
          {
            key: 's3-bucket-lookup', label: 'Bucket 寻址', type: 'select',
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

  // ── 高级 ────────────────────────────────────────────────
  {
    tab: 'advanced',
    sections: [
      {
        title: 'WebUI',
        fields: [
          {
            key: 'listen-port', label: '监听端口', type: 'text',
            hint: '监听端口，用于直接返回节点信息，方便订阅转换',
            placeholder: ':8199',
          },
          { key: 'enable-web-ui', label: '是否启用Web控制面板，访问地址：http://127.0.0.1:8199/admin', type: 'toggle' },
          {
            key: 'api-key', label: 'API 密钥', type: 'password',
            hint: '留空则启动时自动生成',
            placeholder: '••••••••',
          },
          {
            key: 'share-password', label: '分享密码', type: 'password',
            hint: '用于订阅分享接口，访问 http://127.0.0.1:8199/sub 验证后查看分享的订阅文件列表',
            placeholder: '••••••••',
          },
        ],
      },
      {
        title: 'Sub-Store',
        fields: [
          {
            key: 'sub-store-port', label: '监听端口', type: 'text',
            hint: '留空则不启动 Sub-Store',
            placeholder: ':8299',
          },
          {
            key: 'sub-store-path', label: '访问路径', type: 'text',
            hint: '建议设置以避免泄露节点',
            placeholder: '/sub-store-path',
          },
          {
            key: 'mihomo-overwrite-url', label: 'Mihomo 覆写 URL', type: 'text',
            hint: 'Mihomo/Clash 系代理的分流规则文件',
            placeholder: 'http://127.0.0.1:8199/Sinspired_Rules_CDN.yaml',
          },
          {
            key: 'sub-store-sync-cron', label: '同步 Gist Cron', type: 'text',
            hint: '定时将订阅/文件上传到私有 Gist',
            placeholder: '0 2 * * *',
          },
          {
            key: 'sub-store-produce-cron', label: '更新订阅 Cron', type: 'text',
            placeholder: '0 3 * * *',
          },
          {
            key: 'sub-store-push-service', label: 'Push 推送服务', type: 'text',
            placeholder: 'https://push.example.com',
          },
        ],
      },
      {
        title: '代理设置',
        fields: [
          {
            key: 'system-proxy', label: '系统代理', type: 'text',
            hint: '用于拉取订阅和推送通知',
            placeholder: 'http://127.0.0.1:10808',
          },
          {
            key: 'github-proxy', label: 'GitHub 代理', type: 'text',
            hint: '加速 GitHub Release 下载',
            placeholder: 'https://ghfast.top/',
          },
          {
            key: 'ghproxy-group', label: 'GitHub 代理列表', type: 'url-list',
            hint: '自动筛选可用代理，按序尝试',
          },
        ],
      },
      {
        title: '自动更新',
        fields: [
          { key: 'update', label: '自动更新', hint: '关闭时仅提醒新版本', type: 'toggle' },
          { key: 'update-on-startup', label: '启动时检查更新', type: 'toggle' },
          { key: 'prerelease', label: '使用预发布版本', hint: '包含 beta / rc 版本', type: 'toggle' },
          {
            key: 'cron-check-update', label: '检查更新 Cron', type: 'text',
            placeholder: '0 9,21 * * *',
          },
          {
            key: 'update-timeout', label: '下载超时 (分钟)', type: 'number',
            min: 1,
            hint: '更新包下载超时，建议 2–10',
            placeholder: '5',
          },
        ],
      },
      {
        title: '其他',
        fields: [
          {
            key: 'maxmind-db-path', label: 'MaxMind DB 路径', type: 'text',
            hint: '留空则使用内置 GeoLite2 数据库',
            placeholder: '/data/GeoLite2-City.mmdb',
          },
          {
            key: 'callback-script', label: '回调脚本路径', type: 'text',
            hint: '检测完成后执行，可用于自定义通知或其他操作',
            placeholder: '/data/scripts/notify.sh',
          },
        ],
      },
    ],
  },
];

/* ════════════════════════════════════════════════════════════
   内部状态
════════════════════════════════════════════════════════════ */
let _cfg = {};
const _built = new Set();

/* ════════════════════════════════════════════════════════════
   DOM 工具
════════════════════════════════════════════════════════════ */
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

/* ════════════════════════════════════════════════════════════
   字段渲染器
════════════════════════════════════════════════════════════ */

function mkInput(field, value) {
  const inp = el('input', {
    class: 'cfg-input',
    type: field.type === 'password' ? 'password' : 'text',
    'data-key': field.key,
    placeholder: field.placeholder ?? '',
  });
  inp.value = value ?? '';
  return inp;
}

function mkNumber(field, value) {
  const wrap = el('div', { class: 'cfg-number-wrap' });
  const inp = el('input', {
    type: 'number',
    'data-key': field.key,
    min: String(field.min ?? ''),
    max: String(field.max ?? ''),
    step: String(field.step ?? 1),
    placeholder: field.placeholder ?? '',
  });
  // inp.value = value ?? 0;
  inp.value = (value !== undefined && value !== null && value !== '') ? value : 0;

  const makeBtn = (sym, dir) => {
    const b = el('button', { class: 'cfg-step-btn', type: 'button', textContent: sym });
    b.addEventListener('click', () => {
      const cur = parseFloat(inp.value) || 0;
      const step = field.step ?? 1;
      const next = dir > 0 ? cur + step : cur - step;
      const lo = field.min ?? -Infinity;
      const hi = field.max ?? Infinity;
      inp.value = Math.max(lo, Math.min(hi, +next.toFixed(10)));
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
  const sel = el('select', { class: 'cfg-select', 'data-key': field.key });
  for (const opt of field.options) {
    const o = el('option', { value: opt.value, textContent: opt.label });
    if (opt.value === (value ?? field.options[0]?.value)) o.selected = true;
    sel.appendChild(o);
  }
  return sel;
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
    const inp = el('input', { class: 'cfg-input', type: 'text', placeholder: 'https://' });
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

function mkField(fieldDef, value) {
  const wide = fieldDef.type === 'url-list' || fieldDef.type === 'chips';
  const row = el('div', { class: `cfg-field${wide ? ' full-width' : ''}`, 'data-key': fieldDef.key });

  const lbl = el('div', { class: 'cfg-label' });
  lbl.appendChild(el('span', { class: 'cfg-label-text', textContent: fieldDef.label }));
  if (fieldDef.hint) {
    lbl.appendChild(el('span', { class: 'cfg-label-hint', textContent: fieldDef.hint }));
  }

  let ctrl;
  switch (fieldDef.type) {
    case 'number': ctrl = mkNumber(fieldDef, value); break;
    case 'toggle': ctrl = mkToggle(fieldDef.key, value); break;
    case 'select': ctrl = mkSelect(fieldDef, value); break;
    case 'chips': ctrl = mkChips(fieldDef, value); break;
    case 'url-list': ctrl = mkUrlList(fieldDef, value); break;
    default: ctrl = mkInput(fieldDef, value); break;
  }

  row.append(lbl, ctrl);
  return row;
}

/* ════════════════════════════════════════════════════════════
   面板构建
════════════════════════════════════════════════════════════ */
function buildPanel(tabId) {
  const panel = document.getElementById(`panel-${tabId}`);
  if (!panel) return;

  const schema = SCHEMA.find(s => s.tab === tabId);
  if (!schema) return;

  panel.innerHTML = '';

  for (const sec of schema.sections) {
    if (sec.conditional) {
      // 条件分组：整体包裹，由存储方式联动控制显隐
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

  // 存储方式联动
  if (tabId === 'storage') {
    // 加 select 标签，避免匹配到外层 cfg-field div
    const sel = panel.querySelector('select[data-key="save-method"]');
    if (sel) {
      const syncGroups = (method) => {
        panel.querySelectorAll('.cfg-cond-group[data-cond]').forEach(g => {
          const cond = g.dataset.cond;
          const show = cond === method || (method === 'r2' && cond === 's3');
          g.style.display = show ? '' : 'none';
        });
      };
      sel.addEventListener('change', () => syncGroups(sel.value));
      syncGroups(sel.value);
    }
  }

  _built.add(tabId);
}

/* ════════════════════════════════════════════════════════════
   值收集
════════════════════════════════════════════════════════════ */
function collectPanel(tabId) {
  const panel = document.getElementById(`panel-${tabId}`);
  if (!panel || !_built.has(tabId)) return {};

  const schema = SCHEMA.find(s => s.tab === tabId);
  if (!schema) return {};

  const out = {};

  for (const sec of schema.sections) {
    for (const field of sec.fields) {
      const { key, type } = field;
      switch (type) {
        case 'toggle': {
          const cb = panel.querySelector(`input[type="checkbox"][data-key="${key}"]`);
          if (cb) out[key] = cb.checked;
          break;
        }
        case 'number': {
          const inp = panel.querySelector(`input[type="number"][data-key="${key}"]`);
          if (inp) { const v = parseFloat(inp.value); out[key] = isNaN(v) ? 0 : v; }
          break;
        }
        case 'select': {
          const sel = panel.querySelector(`select[data-key="${key}"]`);
          if (sel) out[key] = sel.value;
          break;
        }
        case 'chips': {
          const wrap = panel.querySelector(`.cfg-chips[data-key="${key}"]`);
          if (wrap) out[key] = Array.from(wrap.querySelectorAll('input:checked')).map(c => c.value);
          break;
        }
        case 'url-list': {
          const wrap = panel.querySelector(`.cfg-url-list[data-key="${key}"]`);
          if (wrap) {
            out[key] = Array.from(wrap.querySelectorAll('.cfg-url-item input'))
              .map(i => i.value.trim()).filter(Boolean);
          }
          break;
        }
        default: {
          const inp = panel.querySelector(`input[data-key="${key}"]`);
          if (inp) out[key] = inp.value;
          break;
        }
      }
    }
  }

  return out;
}

/* ════════════════════════════════════════════════════════════
   公开 API
════════════════════════════════════════════════════════════ */

/**
 * 初始化 Tab 切换，页面加载后调用一次
 */
export function initConfigForm() {
  const tabBar = document.getElementById('cfgTabBar');
  if (!tabBar) return;

  tabBar.addEventListener('click', e => {
    const btn = e.target.closest('.cfg-tab[data-tab]');
    if (!btn) return;

    const id = btn.dataset.tab;

    tabBar.querySelectorAll('.cfg-tab').forEach(t => {
      t.classList.toggle('active', t === btn);
      t.setAttribute('aria-selected', String(t === btn));
    });

    document.querySelectorAll('#cfgPanels .cfg-panel').forEach(p => {
      p.classList.toggle('active', p.id === `panel-${id}`);
    });

    // 懒渲染：首次切换到该 Tab 才构建面板
    if (!_built.has(id)) buildPanel(id);
  });
}

/**
 * 根据配置对象渲染表单，登录成功并获取配置后调用
 * @param {Object} configObj — YAML 解析后的配置对象
 */
export function renderConfigForm(configObj) {
  _cfg = configObj ?? {};
  _built.clear();

  // 立即渲染当前激活的 Tab
  const activeBtn = document.querySelector('#cfgTabBar .cfg-tab.active');
  buildPanel(activeBtn?.dataset.tab ?? SCHEMA[0].tab);
}

/**
 * 收集表单当前值，返回完整配置对象
 * 未渲染的 Tab 保留原始配置值
 * @returns {Object}
 */
export function collectConfigForm() {
  const result = { ..._cfg };
  for (const { tab } of SCHEMA) {
    if (_built.has(tab)) Object.assign(result, collectPanel(tab));
  }
  return result;
}