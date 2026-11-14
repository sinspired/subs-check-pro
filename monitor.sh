#!/usr/bin/env bash

# ============================================================
# subs-check-pro Linux 监控脚本
#
# 功能：
#   1. 根据进程名自动寻找业务进程
#   2. 多实例时自动选择最新启动的实例
#   3. 每 5 秒输出一次
#   4. 监控私有内存、RSS、VSZ、CPU、线程、FD
#   5. 监控 TCP Listen / Established / TimeWait / CloseWait / Total
#   6. 自动保存到 monitor.csv
#
# 使用：
#   chmod +x monitor.sh
#   ./monitor.sh
# ============================================================


# ------------------------------------------------------------
# 配置
# ------------------------------------------------------------

PROCESS_NAME="subs-check-pro"
INTERVAL_SEC=5

# CSV 保存到脚本所在目录
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
CSV_FILE="${SCRIPT_DIR}/monitor.csv"


# ------------------------------------------------------------
# 查找业务进程
#
# 如果存在多个 subs-check-pro：
# 选择最新启动的那个。
# etimes 越小，表示启动时间越近。
# ------------------------------------------------------------

get_process_pid()
{
    ps -eo pid=,etimes=,comm= 2>/dev/null |
        awk -v name="$PROCESS_NAME" '
            $3 == name {
                print $2, $1
            }
        ' |
        sort -n |
        awk 'NR == 1 { print $2 }'
}


# ------------------------------------------------------------
# 获取 Private Memory
#
# Linux:
#   Private_Clean + Private_Dirty
#
# 这部分更适合作为“进程私有驻留内存”观察指标。
#
# 返回：MB，保留两位小数
# ------------------------------------------------------------

get_private_memory_mb()
{
    local pid="$1"
    local value

    if [[ ! -r "/proc/${pid}/smaps_rollup" ]]; then
        echo "N/A"
        return
    fi

    value=$(
        awk '
            /^Private_Clean:/ { sum += $2 }
            /^Private_Dirty:/ { sum += $2 }
            END {
                if (sum > 0)
                    printf "%.2f", sum / 1024
                else
                    printf "0.00"
            }
        ' "/proc/${pid}/smaps_rollup" 2>/dev/null
    )

    if [[ -z "$value" ]]; then
        echo "N/A"
    else
        echo "$value"
    fi
}


# ------------------------------------------------------------
# 获取 RSS / VSZ / CPU / Threads
#
# ps:
#   rss     = Resident Set Size，单位 KB
#   vsz     = Virtual Memory Size，单位 KB
#   nlwp    = 线程数
#   cputime = 累计 CPU 时间
# ------------------------------------------------------------

get_process_info()
{
    local pid="$1"

    ps -p "$pid" -o rss=,vsz=,nlwp=,cputime= 2>/dev/null
}


# ------------------------------------------------------------
# 获取打开的文件描述符数量
#
# Linux 没有 Windows Handle 概念。
# 这里使用 FD（File Descriptor）代替。
#
# FD 包括：
#   socket
#   file
#   pipe
#   eventfd
#   epoll
#   等等
# ------------------------------------------------------------

get_fd_count()
{
    local pid="$1"

    if [[ -d "/proc/${pid}/fd" ]]; then
        find "/proc/${pid}/fd" -mindepth 1 -maxdepth 1 \
            -printf '.' 2>/dev/null |
            wc -c
    else
        echo "0"
    fi
}


# ------------------------------------------------------------
# 获取 TCP 连接统计
#
# 使用 ss。
#
# 必须保证目标进程属于当前用户，或者以 root 运行，
# 否则 ss -p 可能无法显示 PID 信息。
#
# 输出：
#   listen
#   established
#   time_wait
#   close_wait
#   total
# ------------------------------------------------------------

get_tcp_stats()
{
    local pid="$1"

    local tcp
    local listen
    local established
    local timewait
    local closewait
    local total

    tcp=$(
        ss -Htanp 2>/dev/null |
            grep -F "pid=${pid}," || true
    )

    if [[ -z "$tcp" ]]; then
        echo "0 0 0 0 0"
        return
    fi

    listen=$(
        printf '%s\n' "$tcp" |
            awk '$1 == "LISTEN" { count++ } END { print count + 0 }'
    )

    established=$(
        printf '%s\n' "$tcp" |
            awk '$1 == "ESTAB" { count++ } END { print count + 0 }'
    )

    timewait=$(
        printf '%s\n' "$tcp" |
            awk '$1 == "TIME-WAIT" { count++ } END { print count + 0 }'
    )

    closewait=$(
        printf '%s\n' "$tcp" |
            awk '$1 == "CLOSE-WAIT" { count++ } END { print count + 0 }'
    )

    total=$(
        printf '%s\n' "$tcp" |
            grep -c . || true
    )

    echo "$listen $established $timewait $closewait $total"
}


# ------------------------------------------------------------
# 初始化 CSV
# ------------------------------------------------------------

if [[ ! -f "$CSV_FILE" ]]; then
    printf '%s\n' \
        '"Timestamp","PID","MEM_MB","RSS_MB","VSZ_MB","CPU","THREADS","FD","LISTEN","ESTABLISHED","TIME_WAIT","CLOSE_WAIT","TOTAL"' \
        > "$CSV_FILE"
fi


# ------------------------------------------------------------
# 启动信息
# ------------------------------------------------------------

echo "监控进程: $PROCESS_NAME"
echo "间隔: ${INTERVAL_SEC}s"
echo "CSV: $CSV_FILE"
echo "Ctrl+C 停止"
echo ""


# ------------------------------------------------------------
# 主循环
# ------------------------------------------------------------

while true
do
    PID="$(get_process_pid)"

    # --------------------------------------------------------
    # 进程不存在
    # --------------------------------------------------------

    if [[ -z "$PID" ]]; then
        echo "$(date '+%H:%M:%S') Process not found"

        sleep "$INTERVAL_SEC"
        continue
    fi


    # --------------------------------------------------------
    # 获取进程信息
    # --------------------------------------------------------

    PROCESS_INFO="$(get_process_info "$PID")"

    if [[ -z "$PROCESS_INFO" ]]; then
        sleep 1
        continue
    fi

    read -r RSS_KB VSZ_KB THREADS CPU <<< "$PROCESS_INFO"


    # --------------------------------------------------------
    # 内存
    # --------------------------------------------------------

    MEM_MB="$(get_private_memory_mb "$PID")"

    RSS_MB="$(
        awk -v kb="$RSS_KB" '
            BEGIN {
                printf "%.2f", kb / 1024
            }
        '
    )"

    VSZ_MB="$(
        awk -v kb="$VSZ_KB" '
            BEGIN {
                printf "%.2f", kb / 1024
            }
        '
    )"


    # --------------------------------------------------------
    # FD
    # --------------------------------------------------------

    FD="$(get_fd_count "$PID")"


    # --------------------------------------------------------
    # TCP
    # --------------------------------------------------------

    read -r LISTEN ESTABLISHED TIME_WAIT CLOSE_WAIT TOTAL <<< "$(
        get_tcp_stats "$PID"
    )"


    # --------------------------------------------------------
    # 时间
    # --------------------------------------------------------

    DISPLAY_TIME="$(date '+%H:%M:%S')"
    CSV_TIME="$(date '+%Y-%m-%d %H:%M:%S')"


    # --------------------------------------------------------
    # 屏幕输出
    # --------------------------------------------------------

    echo "${DISPLAY_TIME} MEM=${MEM_MB}MB RSS=${RSS_MB}MB VSZ=${VSZ_MB}MB CPU=${CPU} THREADS=${THREADS} FD=${FD} LISTEN=${LISTEN} ESTABLISHED=${ESTABLISHED} TIME_WAIT=${TIME_WAIT} CLOSE_WAIT=${CLOSE_WAIT} TOTAL=${TOTAL}"


    # --------------------------------------------------------
    # 写入 CSV
    # --------------------------------------------------------

    printf '"%s","%s","%s","%s","%s","%s","%s","%s","%s","%s","%s","%s","%s"\n' \
        "$CSV_TIME" \
        "$PID" \
        "$MEM_MB" \
        "$RSS_MB" \
        "$VSZ_MB" \
        "$CPU" \
        "$THREADS" \
        "$FD" \
        "$LISTEN" \
        "$ESTABLISHED" \
        "$TIME_WAIT" \
        "$CLOSE_WAIT" \
        "$TOTAL" \
        >> "$CSV_FILE"


    # --------------------------------------------------------
    # 等待下一次采样
    # --------------------------------------------------------

    sleep "$INTERVAL_SEC"
done