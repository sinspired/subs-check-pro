$ProcessName = "subs-check-pro"
$IntervalSec = 5

# CSV 保存到 monitor.ps1 所在目录
$CsvPath = Join-Path $PSScriptRoot "monitor.csv"


# ------------------------------------------------------------
# 获取 Private Working Set
# 对应 Windows 任务管理器：
# “内存（专用工作集）”
# ------------------------------------------------------------
function Get-PrivateWorkingSetMB
{
    param (
        [int]$ProcessId
    )

    try
    {
        $perf = Get-CimInstance `
            -ClassName Win32_PerfFormattedData_PerfProc_Process `
            -Filter "IDProcess = $ProcessId" `
            -ErrorAction Stop

        if ($perf)
        {
            return [Math]::Round(
                [double]$perf.WorkingSetPrivate / 1MB,
                2
            )
        }
    }
    catch
    {
        # 进程刚好退出等情况，返回空值
    }

    return $null
}


# ------------------------------------------------------------
# 创建 CSV 文件和表头
# ------------------------------------------------------------
if (-not (Test-Path $CsvPath))
{
    $header = [PSCustomObject]@{
        Timestamp    = "Timestamp"
        PID          = "PID"
        MEM_MB       = "MEM_MB"
        COMMIT_MB    = "COMMIT_MB"
        CPU_SEC      = "CPU_SEC"
        THREADS      = "THREADS"
        HANDLES      = "HANDLES"
        LISTEN       = "LISTEN"
        ESTABLISHED  = "ESTABLISHED"
        TIME_WAIT    = "TIME_WAIT"
        CLOSE_WAIT   = "CLOSE_WAIT"
        TOTAL        = "TOTAL"
    }

    $header |
        Export-Csv `
            -Path $CsvPath `
            -NoTypeInformation `
            -Encoding utf8NoBOM
}


Write-Host "监控进程: $ProcessName"
Write-Host "间隔: ${IntervalSec}s"
Write-Host "CSV: $CsvPath"
Write-Host "Ctrl+C 停止"
Write-Host ""


# ------------------------------------------------------------
# 主循环
# ------------------------------------------------------------

while ($true)
{
    # 按进程名查找
    # 多实例时取最新启动的那个
    $proc = Get-Process `
        -Name $ProcessName `
        -ErrorAction SilentlyContinue |
        Sort-Object StartTime -Descending |
        Select-Object -First 1


    if (-not $proc)
    {
        Write-Host "$(Get-Date -Format 'HH:mm:ss') Process not found"

        Start-Sleep -Seconds $IntervalSec
        continue
    }


    $processId = $proc.Id


    try
    {
        # ----------------------------------------------------
        # TCP 连接
        # ----------------------------------------------------

        $tcp = @(
            Get-NetTCPConnection `
                -OwningProcess $processId `
                -ErrorAction SilentlyContinue
        )

        $listen = @(
            $tcp | Where-Object State -eq 'Listen'
        ).Count

        $established = @(
            $tcp | Where-Object State -eq 'Established'
        ).Count

        $timeWait = @(
            $tcp | Where-Object State -eq 'TimeWait'
        ).Count

        $closeWait = @(
            $tcp | Where-Object State -eq 'CloseWait'
        ).Count

        $total = $tcp.Count


        # ----------------------------------------------------
        # 内存
        # ----------------------------------------------------

        # 任务管理器：
        # “内存（专用工作集）”
        $memoryMB = Get-PrivateWorkingSetMB `
            -ProcessId $processId

        # Private Bytes / Commit
        $commitMB = [Math]::Round(
            [double]$proc.PrivateMemorySize64 / 1MB,
            2
        )


        # ----------------------------------------------------
        # CPU / Threads / Handles
        # ----------------------------------------------------

        $cpuSec = [Math]::Round(
            [double]$proc.TotalProcessorTime.TotalSeconds,
            2
        )

        $threads = $proc.Threads.Count
        $handles = $proc.HandleCount


        # ----------------------------------------------------
        # 时间
        # ----------------------------------------------------

        # 屏幕只显示时间
        $displayTime = Get-Date -Format 'HH:mm:ss'

        # CSV 保存完整日期时间
        $csvTime = Get-Date -Format 'yyyy-MM-dd HH:mm:ss'


        # ----------------------------------------------------
        # 屏幕输出
        # ----------------------------------------------------

        if ($null -eq $memoryMB)
        {
            $memoryText = "N/A"
        }
        else
        {
            $memoryText = "${memoryMB}MB"
        }

        Write-Host (
            "{0} MEM={1} COMMIT={2}MB CPU={3}s THREADS={4} HANDLES={5} LISTEN={6} ESTABLISHED={7} TIME_WAIT={8} CLOSE_WAIT={9} TOTAL={10}" -f
            $displayTime,
            $memoryText,
            $commitMB,
            $cpuSec,
            $threads,
            $handles,
            $listen,
            $established,
            $timeWait,
            $closeWait,
            $total
        )


        # ----------------------------------------------------
        # 写入 CSV
        # ----------------------------------------------------

        $record = [PSCustomObject]@{
            Timestamp   = $csvTime
            PID         = $processId
            MEM_MB      = $memoryMB
            COMMIT_MB   = $commitMB
            CPU_SEC     = $cpuSec
            THREADS     = $threads
            HANDLES     = $handles
            LISTEN      = $listen
            ESTABLISHED = $established
            TIME_WAIT   = $timeWait
            CLOSE_WAIT  = $closeWait
            TOTAL       = $total
        }

        $record |
            Export-Csv `
                -Path $CsvPath `
                -NoTypeInformation `
                -Append `
                -Encoding utf8NoBOM
    }
    catch
    {
        Write-Host (
            "{0} Error: {1}" -f
            (Get-Date -Format 'HH:mm:ss'),
            $_.Exception.Message
        )
    }


    Start-Sleep -Seconds $IntervalSec
}