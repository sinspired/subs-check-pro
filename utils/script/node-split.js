// 节点裂变脚本
function operator(proxies = []) {
  let splitCount = 0

  const result = proxies.flatMap((p = {}) => {
    const ips = p._resolved_ips
    if (!Array.isArray(ips) || ips.length === 0) return [p]

    splitCount++
    const list = ips.map((server, i) => ({ ...p, name: `${p.name}|+${i + 1}`, server }))
    if (p._domain) list.push({ ...p, name: `${p.name}|已裂变`, server: p._domain })
    return list
  })

  console.log(
    splitCount
      ? `[Node-Split] 🔀 节点裂变完成：${splitCount}/${proxies.length} 个节点已裂变，总数 ${proxies.length} → ${result.length}`
      : `[Node-Split] 🔀 未检测到可裂变 IP，可尝试更换 DNS。节点总数将保持 ${proxies.length} 不变`
  )

  return result
}