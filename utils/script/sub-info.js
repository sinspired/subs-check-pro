/**
 *  使用说明：
 *   - sub-store，点击订阅编辑-脚本操作-添加脚本-添加参数 #showLastUpdate=true
 *   https://raw.githubusercontent.com/sinspired/sub-store-scripts/main/surge/modules/sub-store-scripts/sub-info/node.js#showLastUpdate
 * 支持的 URL 参数（通过订阅链接 # 后的 JSON 或 querystring 传入）：
 *   - resetDay    : 重置日（数字）
 *   - startDate   : 计费周期起始日期（YYYY-MM-DD）
 *   - cycleDays   : 计费周期日（数字）
 *   - showRemaining : 是否显示剩余流量而不是已用流量（布尔）
 *   - showLastUpdate : 是否显示最后更新时间（布尔）
 *
 * 服务端返回的扩展字段（subscription-userinfo 中的非标准字段）：
 *   - last_update : 最近一次更新的时间戳字符串（格式 "YYYY-MM-DD HH:mm:ss"）
 *   - next_update : 下次重置的绝对时间字符串（格式 "YYYY-MM-DD HH:mm:ss"）
 *   - plan_name   : 套餐名称（字符串）
 *   - reset_hour  : 距离下次重置的小时数（数字，<24h 时存在）
 *   - reset_day   : 距离下次重置的整天数（数字，≥24h 时存在）
 *
 * 预期的 name 格式：
 *   - 当 showLastUpdate = true 且 last_update 存在：
 *       "♾️ 03-14 10:55 | 151.71 MB | 22 点重置 | Subs-Check-Pro [🇰🇿]"
 *       （更新时间、已用/剩余流量、重置提示、套餐名，最后保留原始节点国旗）
 *
 *   - 当 showLastUpdate = false：
 *       "🇨🇳 流量 1.2 GB / 10 GB | 5 天 | 2026-03-14 [🇨🇳]"
 *       （流量信息、剩余天数、到期日期，最后保留原始节点名）
 */
async function operator(proxies = [], targetPlatform, context) {
  function log(msg, level = 'INFO') {
    const badge = level === 'ERROR' ? '❌ ' : level === 'WARN' ? '⚠️ ' : ''
    const text = `[注入流量信息节点] ${badge}${msg}`
    if (level === 'ERROR' && typeof console.error === 'function') console.error(text)
    else if (level === 'WARN' && typeof console.warn === 'function') console.warn(text)
    else console.log(text)
  }

  let args = $arguments || {}
  const $ = $substore
  const { parseFlowHeaders, getFlowHeaders, flowTransfer, getRmainingDays, normalizeFlowHeader } = flowUtils
  const sub = context?.source?.[proxies?.[0]?._subName || proxies?.[0]?.subName]

  if (!sub) {
    log(`未找到订阅上下文信息，跳过信息节点生成（当前节点 ${proxies.length} 个）`, 'WARN')
    return proxies
  }

  let subInfo
  let flowInfo
  let rawSubInfo = ''

  if (sub.source !== 'local' || ['localFirst', 'remoteFirst'].includes(sub.mergeSources)) {
    try {
      let url =
        `${sub.url}`
          .split(/[\r\n]+/)
          .map(i => i.trim())
          .filter(i => i.length)?.[0] || ''

      let urlArgs = {}
      const rawArgs = url.split('#')
      url = rawArgs[0]
      if (rawArgs.length > 1) {
        try {
          // 支持 `#${encodeURIComponent(JSON.stringify({arg1: "1"}))}`
          urlArgs = JSON.parse(decodeURIComponent(rawArgs[1]))
        } catch (e) {
          for (const pair of rawArgs[1].split('&')) {
            const key = pair.split('=')[0]
            const value = pair.split('=')[1]
            // 部分兼容之前的逻辑 const value = pair.split('=')[1] || true;
            urlArgs[key] = value == null || value === '' ? true : decodeURIComponent(value)
          }
        }
      }

      if (!urlArgs.noFlow && /^https?/.test(url)) {
        // forward flow headers
        flowInfo = await getFlowHeaders(
          urlArgs?.insecure ? `${url}#insecure` : url,
          urlArgs.flowUserAgent,
          undefined,
          sub.proxy,
          urlArgs.flowUrl
        )
        if (flowInfo) {
          const headers = normalizeFlowHeader(flowInfo, true)
          if (headers?.['subscription-userinfo']) {
            subInfo = headers['subscription-userinfo']
            rawSubInfo = typeof flowInfo === 'string'
              ? flowInfo
              : (flowInfo['subscription-userinfo'] || Object.values(flowInfo).join(';'))
          }
        }
      }
      args = { ...urlArgs, ...args }
    } catch (err) {
      log(`订阅「${sub.name}」获取流量信息失败：${err?.message || err}`, 'WARN')
      $.error(`订阅 ${sub.name} 获取流量信息时发生错误: ${JSON.stringify(err)}`)
      $.error(err?.message)
      $.error(err?.stack)
    }
  }

  if (sub.subUserinfo) {
    let subUserInfo
    if (/^https?:\/\//.test(sub.subUserinfo)) {
      try {
        subUserInfo = await getFlowHeaders(
          undefined,
          undefined,
          undefined,
          sub.proxy,
          sub.subUserinfo
        )
      } catch (e) {
        log(`订阅「${sub.name}」读取自定义流量链接失败（${sub.subUserinfo}）：${e?.message ?? e}`, 'WARN')
        $.error(
          `订阅 ${sub.name} 使用自定义流量链接 ${sub.subUserinfo} 获取流量信息时发生错误: ${e?.message ?? e}`
        )
        $.error(e?.stack)
      }
    } else {
      subUserInfo = sub.subUserinfo
    }

    const parts = [subUserInfo, flowInfo]
      .filter(i => i != null)
      .map(i => (typeof i === 'string' ? i : JSON.stringify(i)))

    const headers = normalizeFlowHeader(parts.join(';'), true)

    if (headers?.['subscription-userinfo']) {
      subInfo = headers['subscription-userinfo']
      rawSubInfo = parts.join(';')
    }
  }

  // 解析扩展字段（last_update / next_update / plan_name / reset_hour / reset_day 等非标准字段）
  // 添加 decodeURIComponent 以修复编码问题
  function parseExtendedFields(raw = '') {
    const result = {}
    for (const segment of raw.split(/[;,]/)) {
      const eqIdx = segment.indexOf('=')
      if (eqIdx === -1) continue
      const key = segment.slice(0, eqIdx).trim()
      let value = segment.slice(eqIdx + 1).trim().replace(/^['"]|['"]$/g, '')

      try {
        // 对值进行 URL 解码
        value = decodeURIComponent(value)
      } catch (e) {
        // 如果解码失败（比如包含非法的 % 字符），保持原样
      }

      result[key] = value
    }
    return result
  }


  /**
   * 格式化重置提示文案。
   *
   * 信息字段语义：
   *   next_update  - 下次重置的绝对时间字符串（始终存在），格式 "YYYY-MM-DD HH:mm:ss"
   *   reset_hour   - 重置时刻的小时数，仅当距重置 < 24h 时存在
   *   reset_day    - 距下次重置的剩余整天数，仅当距重置 ≥ 24h 时存在
   *
   * 显示逻辑（优先级从高到低）：
   *   1. next_update 已过期（服务端新一轮检测已完成）→ "订阅可更新"
   *   2. reset_day 存在（≥ 24h）                    → "N 天后重置"
   *   3. reset_hour 存在（< 24h）                   → "今日 H 点重置" / "X 小时后重置"
   *   4. 兜底                                        → ''
   */
  function formatResetTime(extFields) {
    const nextUpdateStr = extFields['next_update']
    const resetHourStr = extFields['reset_hour']
    const resetDayStr = extFields['reset_day']

    // 1. 解析 next_update，判断是否已过期
    if (nextUpdateStr) {
      // 服务端格式 "2006-01-02 15:04:05"，替换空格为 T 以兼容 Safari/iOS
      const nextTime = new Date(nextUpdateStr.replace(' ', 'T'))
      if (!isNaN(nextTime.getTime()) && nextTime.getTime() - Date.now() <= 0) {
        // 下次重置时刻已过：服务端已完成新一轮检测，有新节点可用
        return '订阅可更新'
      }
    }


    // 2. reset_day：剩余整天数（服务端 remaining ≥ 24h 时写入）
    if (resetDayStr != null && resetDayStr !== '') {
      const days = parseInt(resetDayStr, 10)
      if (!isNaN(days) && days > 0) return `${days} 天后重置`
    }

    // 3. reset_hour：重置时刻小时数（Go 端 remaining < 24h 时写入）
    if (resetHourStr != null && resetHourStr !== '') {
      const hour = parseInt(resetHourStr, 10)
      if (!isNaN(hour)) {
        // 用 next_update 算剩余小时数
        if (nextUpdateStr) {
          const nextTime = new Date(nextUpdateStr.replace(' ', 'T'))
          if (!isNaN(nextTime.getTime())) {
            const remainingHours = Math.ceil((nextTime.getTime() - Date.now()) / 3_600_000)
            if (remainingHours > 0) {
              return remainingHours === 1 ? '1 小时后重置' : `${hour} 点重置`
            }
          }
        }
        return `${hour} 点重置`
      }
    }

    return ''
  }

  // 解析单节点质量：解锁数 -> 下载速度 -> 完整解锁质量 -> 丢包率
  const RE_TAG_SPEED = /^([\d.]+)\s*(MB|KB)\/s$/i
  const RE_TAG_LOSS = /^([\d.]+)%$/
  const RE_TAG_YT = /^YT(?:⁻|-[A-Za-z0-9]+)?$/i
  const RE_TAG_GM = /^GM(?:⁻)?(?:-[A-Za-z0-9]+)?$/i
  const RE_TAG_TK = /^TK(?:-[A-Za-z0-9]+)?$/i

  function evaluateNode(name = '') {
    const segments = String(name).split('|')
    const unlocked = new Set()
    let speed = 0
    let quality = 0
    let loss = 999

    for (let i = 1; i < segments.length; i++) {
      const tok = segments[i].trim()
      if (!tok) continue

      const speedMatch = RE_TAG_SPEED.exec(tok)
      if (speedMatch) {
        const val = parseFloat(speedMatch[1])
        speed = speedMatch[2].toUpperCase() === 'MB' ? val : val / 1024
        continue
      }

      const lossMatch = RE_TAG_LOSS.exec(tok)
      if (lossMatch) {
        loss = parseFloat(lossMatch[1])
        continue
      }

      if (tok === 'GPT⁺') {
        unlocked.add('openai')
        quality++
      } else if (tok === 'GPT') {
        unlocked.add('openai')
      } else if (tok === 'CP') {
        unlocked.add('copilot')
        quality++
      } else if (tok === 'CP⁻') {
        unlocked.add('copilot')
      } else if (tok === 'X') {
        unlocked.add('x')
        quality++
      } else if (tok === 'NF') {
        unlocked.add('netflix')
        quality++
      } else if (tok === 'D+') {
        unlocked.add('disney')
        quality++
      } else if (RE_TAG_YT.test(tok)) {
        unlocked.add('youtube')
        if (!tok.includes('⁻')) quality++
      } else if (RE_TAG_GM.test(tok)) {
        unlocked.add('gemini')
        if (!tok.includes('⁻')) quality++
      } else if (RE_TAG_TK.test(tok)) {
        unlocked.add('tiktok')
        quality++
      }
    }

    return { unlockCount: unlocked.size, speed, quality, loss }
  }

  function isBetterScore(a, b) {
    if (!b) return true
    if (a.unlockCount !== b.unlockCount) return a.unlockCount > b.unlockCount
    if (a.speed !== b.speed) return a.speed > b.speed
    if (a.quality !== b.quality) return a.quality > b.quality
    return a.loss < b.loss
  }

  function pickBestTemplateNode(list, compatibleTypes) {
    let bestProxy = null
    let bestScore = null
    // 倒序遍历：有标签时严格按最优胜出；若全无标签，自动回退到末位兼容节点
    for (let i = list.length - 1; i >= 0; i--) {
      const p = list[i]
      if (!p || !compatibleTypes.has(p.type?.toLowerCase())) continue
      const score = evaluateNode(p.name)
      if (isBetterScore(score, bestScore)) {
        bestProxy = p
        bestScore = score
      }
    }
    return { bestProxy, bestScore }
  }

  function buildNodeName(templateProxy, newName) {
    const oldName = templateProxy?.name || ''
    const flagMatch = oldName.match(/\p{RI}{2}/u)
    const flag = flagMatch ? flagMatch[0] : ''
    return `♾️ ${newName}${flag ? ` [${flag}]` : ''}`
  }

  if (!subInfo) {
    log(`订阅「${sub.name}」未获取到有效流量信息 (subscription-userinfo)，跳过信息节点生成`, 'WARN')
    return proxies
  }

  let {
    expires,
    total,
    usage: { upload, download },
  } = parseFlowHeaders(subInfo)

  // 解析扩展字段
  const extFields = parseExtendedFields(rawSubInfo)
  const lastUpdate = extFields['last_update']
  const planName = extFields['plan_name']

  if (args.hideExpire) {
    expires = undefined
  }
  const date = expires ? new Date(expires * 1000).toLocaleDateString('sv') // YYYY-MM-DD
    : ''

  let show = upload + download
  if (args.showRemaining) {
    show = total - show
  }
  const showT = flowTransfer(Math.abs(show))
  showT.value = show < 0 ? '-' + showT.value : showT.value
  const totalT = flowTransfer(total)

  let name
  if (args.showLastUpdate && lastUpdate) {
    const shortTime = lastUpdate.slice(5, 16)
    name = `${shortTime} | ${showT.value} ${showT.unit}`

    const resetStr = formatResetTime(extFields)
    if (resetStr) name = `${name} | ${resetStr}`
    if (planName) name = `${name} | ${planName}`
  } else {
    // 仅在非 showLastUpdate 模式下才需要 remainingDays
    // 使用 URL 参数中的 cycleDays / startDate 计算剩余天数
    let remainingDays
    try {
      remainingDays = getRmainingDays({
        resetDay: args.resetDay,   // URL 参数：计费周期每月重置日
        startDate: args.startDate,  // URL 参数：计费周期起始日
        cycleDays: args.cycleDays,  // URL 参数：计费周期天数
      })
    } catch (e) { }

    name = `流量 ${showT.value} ${showT.unit} / ${totalT.value} ${totalT.unit}`
    if (remainingDays) name = `${name} | ${remainingDays} 天`
    if (date) name = `${name} | ${date}`
  }

  // 三端兼容协议白名单
  const COMPATIBLE_TYPES = new Set(['ss', 'trojan', 'vmess', 'vless'])

  // proxies 中选择解锁最全，下载速度最大的节点
  const { bestProxy, bestScore } = pickBestTemplateNode(proxies, COMPATIBLE_TYPES)

  const dummyNode = {
    type: 'ss',
    server: '1.0.0.1',
    port: 443,
    cipher: 'aes-128-gcm',
    password: 'password',
  }

  const finalName = buildNodeName(bestProxy, name)
  proxies.unshift({
    ...(bestProxy || dummyNode),
    name: finalName,
  })

  const templateDesc = bestProxy
    ? (bestScore.unlockCount > 0 || bestScore.speed > 0
      ? `检测到最优 ${bestProxy.type} 节点：${bestProxy.name || '未命名'} ➟ [解锁 ${bestScore.unlockCount}/8 | ${bestScore.speed.toFixed(1)}MB/s]`
      : `复用 ${bestProxy.type} 节点：「${bestProxy.name || '未命名'}」`)
    : '兜底虚拟 ss'

  log(`订阅「${sub.name}」共 ${proxies.length} 个节点，${templateDesc}`)
  log(`订阅「${sub.name}」已添加流量到期信息节点：${finalName}`)

  return proxies
}
