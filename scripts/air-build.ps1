# Windows 下由 Air 调用，保留与 get_version.sh 一致的版本元数据。
param(
    # 构建产物路径，默认与 .air.toml 的 Windows 启动路径一致。
    [string]$OutputPath = "./tmp/main.exe"
)

# 文件读取和命令调用失败时立即停止，避免 Air 启动旧的构建产物。
$ErrorActionPreference = "Stop"
# 后端入口包，相对于项目根目录。
$MainPackage = "./cmd/server"
# 版本字段所在的 Go 包，保持与 get_version.sh 中的链接参数一致。
$VersionPackage = "github.com/Tencent/WeKnora/internal/handler"

$ProjectRoot = Split-Path -Parent $PSScriptRoot
Push-Location $ProjectRoot
try {
    $Version = "unknown"
    if (Test-Path -LiteralPath "VERSION") {
        $Version = (Get-Content -LiteralPath "VERSION" -Raw -Encoding UTF8).Trim()
    }
    $Edition = "standard"
    if ($env:EDITION) {
        $Edition = $env:EDITION
    }

    $CommitId = "unknown"
    if ($env:GITHUB_SHA) {
        $CommitId = $env:GITHUB_SHA.Substring(0, [Math]::Min(7, $env:GITHUB_SHA.Length))
    } elseif (Get-Command git -ErrorAction SilentlyContinue) {
        # 源码归档可能没有 Git 元数据，此时沿用 unknown，不阻止构建。
        try {
            $GitCommit = git rev-parse --short HEAD 2>$null
            if ($LASTEXITCODE -eq 0) {
                $CommitId = $GitCommit
            }
        } catch {
            $CommitId = "unknown"
        }
    }

    $BuildTime = [DateTime]::UtcNow.ToString("yyyy-MM-dd HH:mm:ss 'UTC'", [Globalization.CultureInfo]::InvariantCulture)
    $GoVersion = go version
    if ($LASTEXITCODE -ne 0) {
        exit $LASTEXITCODE
    }
    $LinkerFlags = @(
        "-X '$VersionPackage.Version=$Version'"
        "-X '$VersionPackage.Edition=$Edition'"
        "-X '$VersionPackage.CommitID=$CommitId'"
        "-X '$VersionPackage.BuildTime=$BuildTime'"
        "-X '$VersionPackage.GoVersion=$GoVersion'"
        "-X 'google.golang.org/protobuf/reflect/protoregistry.conflictPolicy=warn'"
    ) -join " "

    # 使用等号传参，防止 Windows PowerShell 丢弃空的 GO_BUILD_TAGS 参数。
    go build "-tags=$env:GO_BUILD_TAGS" "-ldflags=$LinkerFlags" -o $OutputPath $MainPackage
    exit $LASTEXITCODE
} finally {
    Pop-Location
}
