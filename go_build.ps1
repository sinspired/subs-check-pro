# ==============================================================================
# Go 应用程序跨平台编译脚本 (PowerShell) - Zig 交叉编译 (支持 QuickJS CGO)
# ==============================================================================

param(
    [string]$Version,
    [string]$Commit,
    [switch]$Clean,
    [switch]$Debug,
    [switch]$Experiment,
    [switch]$Win,
    [switch]$Lin
)

# --- 1. 配置 ---
$relativeOutputDir = "bin"
$outputDir = Join-Path $PSScriptRoot $relativeOutputDir
$execName = "subs-check-pro"

# --- 2. 编译目标平台 ---
$targets = @(
    @{ GOOS = "windows"; GOARCH = "amd64"; OutputFile = "$execName.exe" },
    @{ GOOS = "windows"; GOARCH = "386"; OutputFile = "$execName.exe" },
    @{ GOOS = "linux"; GOARCH = "amd64"; OutputFile = "$execName" },
    @{ GOOS = "linux"; GOARCH = "arm64"; OutputFile = "$execName" },
    @{ GOOS = "linux"; GOARCH = "arm"; OutputFile = "$execName" },
    @{ GOOS = "darwin"; GOARCH = "amd64"; OutputFile = "$execName" },
    @{ GOOS = "darwin"; GOARCH = "arm64"; OutputFile = "$execName" }
)

# Debug 模式
if ($Debug) {
    $Clean = $false

    if ($Win -and -not $Lin) {
        # 仅 Windows
        $targets = @(
            @{ GOOS = "windows"; GOARCH = "amd64"; OutputFile = "$execName.exe" }
        )
        Write-Host "🐞 Debug 模式，仅编译 Windows/amd64" -ForegroundColor Blue
    }
    elseif ($Lin -and -not $Win) {
        # 仅 Linux
        $targets = @(
            @{ GOOS = "linux"; GOARCH = "amd64"; OutputFile = "$execName" }
        )
        Write-Host "🐞 Debug 模式，仅编译 Linux/amd64" -ForegroundColor Blue
    }
    else {
        # 默认：Windows + Linux
        $targets = @(
            @{ GOOS = "linux"; GOARCH = "amd64"; OutputFile = "$execName" },
            @{ GOOS = "windows"; GOARCH = "amd64"; OutputFile = "$execName.exe" }
        )
        Write-Host "🐞 Debug：默认 Win+Lin（单独编译用 -Win / -Lin）" -ForegroundColor Blue
    }
}

# --- 3. 开始编译 ---
Write-Host "🚀 开始交叉编译 Go 应用程序..." -ForegroundColor Cyan

if ($Version -ne "") {
    if ($Commit -eq "") {
        $Commit = git rev-parse --short HEAD
    }
    Write-Host "🎫 指定编译版本：$Version-$Commit"
}
else {
    Write-Host "🎫 未指定版本，默认为：dev-unknown"
}

if (-not (Test-Path $outputDir)) {
    Write-Host "  -> 创建输出目录: $outputDir"
    New-Item -ItemType Directory -Path $outputDir | Out-Null
}

# 清理缓存
if ($Clean) {
    Write-Host "🧹 清理 Go 构建缓存..." -ForegroundColor Yellow
    Push-Location $PSScriptRoot
    go clean -cache -r
}

# 启用实验特性
if ($Experiment) {
    # Write-Host "🧪 启用实验特性: JSON v2 编码器" -ForegroundColor Yellow
    # $env:GOEXPERIMENT = "jsonv2"
}

$successALL = $true

try {
    # 跨平台编译循环
    foreach ($target in $targets) {
        try {
            $env:GOOS = $target.GOOS
            $env:GOARCH = $target.GOARCH

            # --- 架构映射：GOARCH → goreleaser 归档命名风格 ---
            switch ($target.GOARCH) {
                "amd64" { $archStr = "x86_64" }
                "386" { $archStr = "i386" }
                "arm64" { $archStr = "aarch64" }
                "arm" { $archStr = "armv7" }
                default { $archStr = $target.GOARCH }
            }

            $platformIdentifier = "{0}_{1}" -f ($target.GOOS.Substring(0, 1).ToUpper() + $target.GOOS.Substring(1)), $archStr
            $platformDir = Join-Path $outputDir $platformIdentifier
            if (-not (Test-Path $platformDir)) {
                New-Item -ItemType Directory -Path $platformDir | Out-Null
            }

            $outputPath = Join-Path $platformDir $target.OutputFile
            $archiveName = "{0}_{1}_{2}" -f $execName, ($target.GOOS.Substring(0, 1).ToUpper() + $target.GOOS.Substring(1)), $archStr
            $archiveExt = if ($target.GOOS -eq "windows") { "zip" } else { "tar.gz" }
            $archivePath = Join-Path $outputDir "$archiveName.$archiveExt"

            Write-Host "  -> 编译 $platformIdentifier ..." -ForegroundColor White
            go build -ldflags "-s -w -X main.Version=$Version -X main.CurrentCommit=$Commit" -trimpath -o $outputPath

            if ($LASTEXITCODE -ne 0) {
                Write-Host "❌ 编译 $platformIdentifier 失败！" -ForegroundColor Red
                $successALL = $false
                continue
            }

            # Docker 兼容输出
            if ($target.GOOS -eq "linux") {
                $dockerArch = switch ($target.GOARCH) {
                    "amd64" { "amd64" }
                    "arm64" { "arm64" }
                    "arm" { "arm" }
                    default { "" }
                }
                if ($dockerArch -ne "") {
                    $dockerBin = Join-Path $outputDir "$execName-linux-$dockerArch"
                    Copy-Item $outputPath $dockerBin -Force
                    Write-Host "  -> Docker 二进制: bin/$execName-linux-$dockerArch" -ForegroundColor DarkCyan
                }
            }

            if (!$Debug) {
                # 打包
                Write-Host "  -> 打包为: $relativeOutputDir/$archiveName.$archiveExt" -ForegroundColor DarkGray
                if ($target.GOOS -eq "windows") {
                    Compress-Archive -Path "$platformDir/*" -DestinationPath $archivePath -Force
                }
                else {
                    Push-Location $outputDir
                    tar -czf $archivePath $platformIdentifier
                    Pop-Location
                }
            }
        }
        finally {
            # 保证每次循环后环境干净
            Remove-Item Env:GOOS -ErrorAction SilentlyContinue
            Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
        }
    }
}
catch {
    $successALL = $false
    Write-Host "❌ 编译过程中出现错误: $_" -ForegroundColor Red
}
finally {
    Write-Host "🧹 清理全局环境变量..." -ForegroundColor Yellow
    Remove-Item Env:GOEXPERIMENT -ErrorAction SilentlyContinue
    Remove-Item Env:GODEBUG -ErrorAction SilentlyContinue
    Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue
}

if ($successALL) {
    Write-Host "✅ 所有目标平台编译完成！" -ForegroundColor Green
    Write-Host "📂 输出文件位于 '$outputDir' 目录中。" -ForegroundColor Green
}
else {
    Write-Host "⚠️ 部分目标平台编译失败，请检查并重试。" -ForegroundColor Red
}