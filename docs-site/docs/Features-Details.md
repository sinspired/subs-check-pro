# ✨ 新增功能与性能优化详情

## 1. 测活-测速-媒体检测，分阶段流水线，自适应高并发

通过将测活阶段并发数提升 100-1000（主要受限于设备 CPU 和路由器芯片性能，几乎不占用带宽），同时将测速阶段并发数保持在较低水平（如 8-32，以减轻带宽竞争）。大幅提高性能，数倍缩短整体检测时间，并使测速结果更准确。

```yaml
# 新增设置项:
alive-concurrent: 200  # 测活并发数
speed-concurrent: 32   # 测速并发数
media-concurrent: 100  # 流媒体检测并发数
```

## 2. 增强位置标签

示例：🇺🇸US¹-SG⁰_3|2.5MB/s|6%|GPT⁺|TK-US|YT-US|NF|D+|X

- BadCFNode（无法访问 CF 网站的节点）：`HK⁻¹`
- CFNodeWithSameCountry（实际位置与 CDN 位置一致）：`HK¹⁺`
- CFNodeWithDifferentCountry（实际位置与 CDN 位置不一致）：`HK¹-US⁰`
- NodeWithoutCF（未使用 CF 加速的节点）：`HK²`

```yaml
# 新增设置项:
drop-bad-cf-nodes: false  # 是否丢弃低质量的 CF 节点
enhanced-tag: false       # 是否开启 增强位置标签
maxmind-db-path: ""       # 指定位置数据库
```

## 3. 优化内存

检测期下降 18%，检测结束下降 49%。对内存敏感可使用 i386 版本，对内存不敏感可使用 x64 版本（性能略有提升，CPU 占用更低）。

- 去重后释放原数据
- 结束检测手动释放节点缓存
- 每个检测任务结束，结束空闲 TCP 连接占用
- pre-release 使用绿茶垃圾回收（测试中）

```powershell
# 内存监控数据:
[19:13:30] Start: PID=9040 mem=667.80 MB
[19:26:38] BigChange(>=20%) reached in 13m8.0320213s, mem=102.71 MB
[19:44:37] Down 1 step(s) of 10MB, mem=98.72 MB
[20:37:40] Down 1 step(s) of 10MB, mem=83.64 MB
[20:42:41] Down 3 step(s) of 10MB, mem=59.54 MB
```

## 4. 智能节点乱序，减少节点被测速“测死”的概率

```yaml
# 新增配置项:
# 相似度阈值(Threshold)大致对应网段
# 1.00 /32（完全相同 IP）
# 0.75 /24（前三段相同）
# 0.50 /16（前两段相同）
# 0.25 /8（第一段相同）
# 以下设置仅能 [减少] 概率，无法避免被“反代机房”中断节点
threshold:  0.75
```

## 5. 保存并加载“历次”检测可用节点

可有效缓解网络环境恶劣导致的问题。

```powershell
# 保存并加载 "上次检测成功的节点" 和 "历次检测成功的节点"
# use-last-result: true
# use-history-result: true
2025-09-25 15:52:25 INF 已获取节点数量: 15872
2025-09-25 15:52:25 INF 去重后节点数量: 11788
2025-09-25 15:52:25 INF 已加载上次检测可用节点，数量: 110
2025-09-25 15:52:25 INF 已加载历次检测可用节点，数量: 536
2025-09-25 15:52:25 INF 节点乱序, 相同 CIDR/24 范围 IP 的最小间距: 785
2025-09-25 15:52:25 INF 开始检测节点
2025-09-25 15:52:25 INF 当前参数 enable-speedtest=true media-check=true drop-bad-cf-nodes=false auto-concurrent=true concurrent=100 :alive=515 :speed=28 :media=138 timeout=5000 min-speed=512 download-timeout=10 download-mb=20
进度: [===========================================> ] 95.7% (11280/11788) 可用: 133
```

## 6. 自动检查更新，无缝升级新版本

- 软件启动时更新：重启后打开新窗口
- 定时更新任务：静默重启，如需关闭任务，直接关闭终端控制台即发送关闭信号

```yaml
# 是否开启新版本更新
update: false
# 启动时检查更新版本
update-on-startup: true
# 定时检查更新（默认每天 0/9/21 点）
cron-chek-update: "0 0,9,21 * * *"
# 使用预发布版本
prerelease: false
```

## 7. 统计订阅链接总数、可用节点、成功率

可自动生成剔除无效订阅的 `sub-urls:`，将在 `output/stats/` 生成统计文件：

```bash
output/
└── stats/
    ├── sub-urls.yaml       # 剔除失效链接并排序后的订阅列表
    ├── subs-filter.yaml    # 订阅质量统计
    └── subs-analysis.yaml  # 订阅节点质量分析报告
```

设置项：

```yaml
# 统计订阅链接有效性和成功率
sub-urls-stats: true
```
