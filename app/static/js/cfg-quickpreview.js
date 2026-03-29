/**
 * cfg-quickpreview.js — 快速配置预览浮动面板
 *
 */

import { FIELD_VALIDATORS } from './config-form.js';

// ── 要展示的关键配置分组 ────────────────────────────────────
const PREVIEW_GROUPS = [
  {
    title: '代理 & 通知',
    icon: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.07 4.93a10 10 0 0 1 0 14.14M4.93 4.93a10 10 0 0 0 0 14.14"/></svg>`,
    items: [
      { key: 'github-proxy',          label: 'GitHub 代理',   fmt: v => v || null, warnIfEmpty: '未配置，国内拉取 GitHub 订阅易超时' },
      { key: 'recipient-url',         label: '通知渠道',      fmt: v => Array.isArray(v) ? v.filter(Boolean).length + ' 个' : (v ? '已配置' : null), warnIfEmpty: '未配置，检测结果无法推送' },
      { key: 'media-check',           label: '流媒体检测',    fmt: v => v !== false ? '开启' : '关闭' },
      { key: 'keep-success-proxies',  label: '保留成功节点',  fmt: v => v !== false ? '开启' : '关闭', warnIfFalse: '关闭后上游更新可能清空可用节点' },
      { key: 'speed-test-url',        label: '测速地址',      fmt: v => v || null, warnIfEmpty: '未配置，测速功能关闭' },
    ],
  },
  {
    title: '并发 & 速度',
    icon: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="22 12 18 12 15 21 9 3 6 12 2 12"/></svg>`,
    items: [
      { key: 'alive-concurrent',   label: '测活并发',      fmt: v => v > 0 ? String(v) : '自动' },
      { key: 'speed-concurrent',   label: '测速并发',      fmt: v => v > 0 ? String(v) : '自动' },
      { key: 'min-speed',          label: '最低测速',      fmt: v => v > 0 ? v + ' KB/s' : '未设置', warnIfZero: '未设置，极慢节点均会保留' },
      { key: 'download-timeout',   label: '测速超时',      fmt: v => v > 0 ? v + ' s' : '未设置',   warnIfZero: '未设置，测速可能阻塞' },
      { key: 'check-interval',     label: '检测间隔',      fmt: (v, cfg) => cfg['cron-expression'] ? ('cron: ' + cfg['cron-expression']) : (v > 0 ? v + ' 分钟' : '未设置') },
    ],
  },
  {
    title: '存储 & 安全',
    icon: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3"/><path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5"/></svg>`,
    items: [
      { key: 'save-method',     label: '存储方式',      fmt: v => v || 'local' },
      { key: 'share-password',  label: '分享密码',      fmt: v => v ? '已设置' : null, warnIfEmpty: '未设置，订阅分享功能未启用' },
      { key: 'update',          label: '自动更新',      fmt: v => v !== false ? '开启' : '关闭', warnIfFalse: '已关闭，建议保持开启以获取最新修复' },
      { key: 'system-proxy',    label: '系统代理',      fmt: v => v || null, optional: true },
    ],
  },
];

// ── SVG 图标 ──────────────────────────────────────────────
const ICON_OK   = `<svg class="pv-status-icon ok"   viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>`;
const ICON_WARN = `<svg class="pv-status-icon warn" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>`;
const ICON_INFO = `<svg class="pv-status-icon info" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>`;
const ICON_DASH = `<svg class="pv-status-icon muted" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="5" y1="12" x2="19" y2="12"/></svg>`;

function _esc(s) {
  return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
}

/**
 * 渲染一个预览卡片 (cfg-card 风格)
 */
function _renderCard(group, cfg) {
  const rows = group.items.map(item => {
    const rawVal = cfg[item.key];
    const displayVal = item.fmt(rawVal, cfg);

    // 确定状态和 tooltip
    let statusIcon = ICON_OK;
    let statusCls  = 'ok';
    let tooltip    = '';

    if (displayVal === null || displayVal === undefined) {
      // 值为空
      if (item.optional) {
        statusIcon = ICON_DASH;
        statusCls  = 'muted';
      } else if (item.warnIfEmpty) {
        statusIcon = ICON_WARN;
        statusCls  = 'warn';
        tooltip    = item.warnIfEmpty;
      } else {
        statusIcon = ICON_INFO;
        statusCls  = 'info';
      }
    } else if (item.warnIfFalse && rawVal === false) {
      statusIcon = ICON_WARN;
      statusCls  = 'warn';
      tooltip    = item.warnIfFalse;
    } else if (item.warnIfZero && (rawVal === 0 || rawVal === '0' || !rawVal)) {
      statusIcon = ICON_INFO;
      statusCls  = 'info';
      tooltip    = item.warnIfZero;
    } else {
      // 对 number 字段跑 FIELD_VALIDATORS
      const validator = FIELD_VALIDATORS[item.key];
      if (validator) {
        const result = validator(rawVal);
        if (result) {
          statusIcon = result.level === 'warn' ? ICON_WARN : ICON_INFO;
          statusCls  = result.level;
          tooltip    = result.msg;
        }
      }
    }

    const shownVal = displayVal ?? '未设置';
    const titleAttr = tooltip ? ` title="${_esc(tooltip)}"` : '';
    const valCls = displayVal ? (statusCls === 'ok' ? '' : statusCls) : 'muted';

    return `<div class="pv-kv"${titleAttr}>
      <span class="pv-k">${_esc(item.label)}</span>
      <span class="pv-status-wrap">${statusIcon}<span class="pv-v ${valCls}" title="${_esc(String(shownVal))}">${_esc(String(shownVal))}</span></span>
    </div>`;
  }).join('');

  return `<div class="pv-card">
    <div class="pv-card-title">${group.icon}${_esc(group.title)}</div>
    ${rows}
  </div>`;
}

/**
 * 构建并挂载浮动预览面板
 */
function _buildPanel(cfg) {
  // 计算整体健康度
  let warnCount = 0;
  for (const group of PREVIEW_GROUPS) {
    for (const item of group.items) {
      const rawVal = cfg[item.key];
      const displayVal = item.fmt(rawVal, cfg);
      if (!item.optional) {
        if ((displayVal === null || displayVal === undefined) && item.warnIfEmpty) warnCount++;
        else if (item.warnIfFalse && rawVal === false) warnCount++;
        else if (FIELD_VALIDATORS[item.key]) {
          const r = FIELD_VALIDATORS[item.key](rawVal);
          if (r?.level === 'warn') warnCount++;
        }
      }
    }
  }

  const healthLabel = warnCount === 0 ? '配置正常' : `${warnCount} 项待优化`;
  const healthCls   = warnCount === 0 ? 'ok' : 'warn';
  const healthIcon  = warnCount === 0 ? ICON_OK : ICON_WARN;

  const cardsHTML = PREVIEW_GROUPS.map(g => _renderCard(g, cfg)).join('');

  const panel = document.createElement('div');
  panel.id = 'cfgPreviewPanel';
  panel.className = 'cfg-preview-panel';
  panel.setAttribute('role', 'dialog');
  panel.setAttribute('aria-label', '快速配置预览');
  panel.innerHTML = `
    <div class="pv-header">
      <div class="pv-title">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
        配置快览
      </div>
      <div class="pv-health ${healthCls}">${healthIcon}<span>${healthLabel}</span></div>
      <button class="pv-close" id="cfgPreviewClose" aria-label="关闭">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
      </button>
    </div>
    <div class="pv-body">
      <div class="pv-grid">${cardsHTML}</div>
      <div class="pv-footer">
        <span class="pv-footer-hint">悬停查看详情 · 完整建议见</span>
        <a href="/analysis" target="_blank" rel="noopener" class="pv-footer-link">分析报告</a>
      </div>
    </div>`;

  return panel;
}

/* ── 公开入口 ─────────────────────────────────────────────── */

/**
 * 初始化快速预览按钮
 * @param {() => string|null} getKey — 返回当前 sessionKey 的函数
 * @param {() => Object|null} getCfg — （可选）返回已缓存配置对象的函数，没有则重新 fetch
 */
export function initQuickPreview(getKey, getCfg) {
  const btn = document.getElementById('cfgPreviewBtn');
  if (!btn) return;

  let panelEl = null;
  let loading  = false;

  function close() {
    panelEl?.remove();
    panelEl = null;
    document.removeEventListener('keydown', onEsc);
    document.removeEventListener('click', onOutside);
  }

  function onEsc(e) { if (e.key === 'Escape') close(); }
  function onOutside(e) {
    if (panelEl && !panelEl.contains(e.target) && e.target !== btn) close();
  }

  async function open() {
    if (panelEl) { close(); return; }   // toggle
    if (loading) return;

    const key = getKey?.();
    if (!key) return;

    // 先尝试从缓存取
    let cfg = getCfg?.();
    if (!cfg) {
      loading = true;
      btn.disabled = true;
      try {
        const r = await fetch('/api/config', { headers: { Authorization: `Bearer ${key}` } });
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        const data = await r.json();
        const raw = typeof data?.content === 'string' ? data.content
          : (typeof data === 'string' ? data : '');
        // 用全局 YAML（admin.js 已全局引入 yaml.bundle.js）
        cfg = (window.YAML?.parse(raw)) ?? {};
      } catch (e) {
        console.warn('[cfgPreview] fetch failed:', e);
        cfg = {};
      } finally {
        loading = false;
        btn.disabled = false;
      }
    }

    panelEl = _buildPanel(cfg);

    // 定位：优先出现在按钮下方，靠右对齐
    const btnRect = btn.getBoundingClientRect();
    panelEl.style.cssText = `
      position: fixed;
      top: ${btnRect.bottom + 8}px;
      right: ${window.innerWidth - btnRect.right}px;
    `;

    document.body.appendChild(panelEl);

    // 边界保护：若超出视口底部则向上展开
    requestAnimationFrame(() => {
      if (!panelEl) return;
      const rect = panelEl.getBoundingClientRect();
      if (rect.bottom > window.innerHeight - 16) {
        panelEl.style.top = `${btnRect.top - rect.height - 8}px`;
      }
    });

    panelEl.querySelector('#cfgPreviewClose')?.addEventListener('click', close);
    document.addEventListener('keydown', onEsc);
    setTimeout(() => document.addEventListener('click', onOutside), 0);
  }

  btn.addEventListener('click', open);

  // 登录后启用按钮
  return { enable: () => { btn.disabled = false; }, disable: () => { btn.disabled = true; close(); } };
}