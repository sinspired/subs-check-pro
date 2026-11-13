// 节点裂变脚本
function operator(proxies = []) {
  let expandedCount = 0

  const result = proxies.flatMap((p = {}) => {
    const ips = p._resolved_ips
    if (!Array.isArray(ips) || ips.length === 0) return [p]

    expandedCount++
    const list = ips.map((server, i) => ({ ...p, name: `${p.name}|+${i + 1}`, server }))
    if (p._domain) list.push({ ...p, name: `${p.name}|已裂变`, server: p._domain })
    return list
  })

  console.log(
    expandedCount
      ? `[Node-Expand] 🔀 节点裂变完成：${expandedCount}/${proxies.length} 个节点已展开，总数 ${proxies.length} → ${result.length}`
      : `[Node-Expand] 🔀 未检测到可裂变 IP，节点总数保持 ${proxies.length}`
  )

  return result
}