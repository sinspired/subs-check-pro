// 节点裂变脚本
function operator(proxies = []) {
  let addedCount = 0
  const splitDetails = []

  const result = proxies.flatMap((p = {}) => {
    const ips = p._resolved_ips
    if (!Array.isArray(ips) || !ips.length) return [p]

    const list = ips.map((server, i) => ({
      ...p,
      name: `${p.name}|+${i + 1}`,
      server
    }))

    if (p._domain) list.push({ ...p, name: `${p.name}|已裂变`, server: p._domain })

    addedCount += list.length - 1
    splitDetails.push(`「${p.type}」${p.name} 已裂变 +${ips.length} IP ➟ ${list.length} 节点`)

    return list
  })

  splitDetails.forEach(detail => console.log(`[节点裂变] ${detail}`))

  console.log(
    splitDetails.length
      ? `[节点裂变] 🔀 节点裂变完成 ${proxies.length} ➟ ${result.length}（+${addedCount}，${splitDetails.length} 个节点）`
      : `[节点裂变] ℹ️ 无可用解析 IP，${proxies.length} 个节点保持不变`
  )

  return result
}