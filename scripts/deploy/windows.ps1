#requires -Version 5.1
[CmdletBinding()]
param(
    # 默认从源码构建并启动；其他操作复用同一套本地配置。
    [ValidateSet('Deploy', 'Build', 'Start', 'Stop', 'Status', 'Logs', 'Check')]
    [string]$Action = 'Deploy',
    # 构建器的 CPU 总限额，同时限制 Go 和 Rust 编译并行数。
    [ValidateRange(1, 16)]
    [int]$BuildCpus = 2,
    # 构建器内存上限，内存与交换区合计也限制为此值。
    [ValidateRange(4, 64)]
    [int]$BuildMemoryGB = 6,
    # 仅创建 .env 时生效；已有部署沿用 .env 中的端口。
    [ValidateRange(1, 65535)]
    [int]$FrontendPort = 8081,
    # 首次启动和数据库迁移的健康检查等待时间，单位为秒。
    [ValidateRange(60, 1800)]
    [int]$StartupTimeout = 180
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

# 路径根据脚本位置计算，支持从任意目录执行和包含空格的源码路径。
$Repository = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))
$ProjectName = 'weknora-windows'
$Utf8 = [Text.UTF8Encoding]::new($false, $true)
$Services = @('app', 'docreader', 'frontend')
$ComposeArgs = @(
    'compose', '--project-name', $ProjectName, '--project-directory', $Repository,
    '--env-file', (Join-Path $Repository '.env'),
    '-f', (Join-Path $Repository 'docker-compose.yml'),
    '-f', (Join-Path $PSScriptRoot 'compose.windows.yml')
)

function Invoke-Docker {
    param([Parameter(Mandatory)][string[]]$DockerArgs)
    & docker.exe @DockerArgs
    if ($LASTEXITCODE -ne 0) {
        throw "Docker 命令失败，退出码 $LASTEXITCODE。请查看上方错误；修复后可重新运行，已有配置和数据会保留。"
    }
}

function New-Secret {
    param([int]$Bytes)
    $buffer = New-Object byte[] $Bytes
    $random = [Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $random.GetBytes($buffer)
        return [BitConverter]::ToString($buffer).Replace('-', '').ToLowerInvariant()
    } finally {
        $random.Dispose()
    }
}

function Initialize-Environment {
    $envPath = Join-Path $Repository '.env'
    if (Test-Path -LiteralPath $envPath) {
        Write-Host '沿用现有 .env，密码、密钥和数据连接不变。'
        return
    }
    $content = [IO.File]::ReadAllText((Join-Path $Repository '.env.example'), $Utf8)
    $settings = @{
        # 独立部署使用本地数据库、文件存储及随机密钥。
        WEKNORA_VERSION = 'custom-windows'
        FRONTEND_PORT = [string]$FrontendPort
        DB_PASSWORD = New-Secret 16
        REDIS_PASSWORD = New-Secret 16
        JWT_SECRET = New-Secret 32
        SYSTEM_AES_KEY = New-Secret 16
        # 默认软件源适配当前 Debian 镜像，关闭模板中的可选监控集成。
        APK_MIRROR_ARG = ''
        LANGFUSE_PUBLIC_KEY = ''
        LANGFUSE_SECRET_KEY = ''
    }
    foreach ($item in $settings.GetEnumerator()) {
        $pattern = '(?m)^' + [regex]::Escape($item.Key) + '=[^\r\n]*'
        if ([regex]::Matches($content, $pattern).Count -ne 1) {
            throw ".env.example 中的配置项不唯一或缺失：$($item.Key)"
        }
        $content = [regex]::Replace($content, $pattern, $item.Key + '=' + $item.Value)
    }
    # CreateNew 防止并发操作或意外重复执行覆盖现有密钥。
    $file = [IO.File]::Open($envPath, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
    try {
        $bytes = $Utf8.GetBytes($content)
        $file.Write($bytes, 0, $bytes.Length)
    } finally {
        $file.Dispose()
    }
    Write-Host '已生成独立的 .env。请保管此文件，更新时无需重新生成。'
}

function Build-Images {
    $buildxVersion = (Invoke-Docker @('buildx', 'version')) -join "`n"
    if ($buildxVersion -notmatch '\bv(\d+)\.(\d+)\.(\d+)') {
        throw '无法识别 Buildx 版本，请更新 Docker Desktop。'
    }
    if ([version]($Matches[1] + '.' + $Matches[2] + '.' + $Matches[3]) -lt [version]'0.14.0') {
        throw '需要 Buildx 0.14 或更新版本，请更新 Docker Desktop。'
    }
    $buildHelp = (Invoke-Docker @('compose', 'build', '--help')) -join "`n"
    if ($buildHelp -notmatch '--builder') {
        throw '当前 Compose 不支持选择限额构建器，请更新 Docker Desktop。'
    }

    $builder = 'weknora-win-' + $RepositoryHash
    $builders = @(Invoke-Docker @('buildx', 'ls', '--format', '{{.Name}}'))
    if ($builders -contains $builder) {
        # 同一源码目录受文件锁保护，可清理上次中断留下的专用构建器。
        Invoke-Docker @('buildx', 'rm', '--keep-state', $builder)
    }
    $created = $false
    try {
        Invoke-Docker @(
            'buildx', 'create', '--name', $builder, '--node', $builder,
            '--driver', 'docker-container',
            '--buildkitd-config', (Join-Path $PSScriptRoot 'buildkitd.toml'),
            '--driver-opt', 'cpu-period=100000',
            '--driver-opt', ('cpu-quota=' + ($BuildCpus * 100000)),
            '--driver-opt', ('memory=' + $BuildMemoryGB + 'g'),
            '--driver-opt', ('memory-swap=' + $BuildMemoryGB + 'g'),
            '--driver-opt', 'default-load=true'
        )
        $created = $true
        $commit = 'unknown'
        if ((Get-Command git.exe -CommandType Application -ErrorAction SilentlyContinue) -and
            (Test-Path -LiteralPath (Join-Path $Repository '.git'))) {
            $gitCommit = & git.exe -C $Repository rev-parse --short HEAD
            if ($LASTEXITCODE -eq 0) { $commit = $gitCommit.Trim() }
        }
        $nodeHeap = [Math]::Min(4096, ($BuildMemoryGB - 1) * 1024)
        foreach ($service in $Services) {
            Write-Host "正在构建 $service，限额 $BuildCpus 核 / $BuildMemoryGB GiB。首次需要下载依赖，请等待。"
            $buildArgs = @('--parallel', '1', 'build', '--builder', $builder)
            if ($service -eq 'app') {
                $buildArgs += @(
                    '--build-arg', "BUILD_JOBS=$BuildCpus", '--build-arg', "NODE_MAX_OLD_SPACE_SIZE=$nodeHeap",
                    '--build-arg', "COMMIT_ID_ARG=$commit", '--build-arg', 'VERSION_ARG=custom-windows',
                    '--build-arg', ('BUILD_TIME_ARG=' + [DateTime]::UtcNow.ToString('o'))
                )
            } elseif ($service -eq 'frontend') {
                $buildArgs += @('--build-arg', "VITE_FRONTEND_COMMIT=$commit", '--build-arg', "NODE_MAX_OLD_SPACE_SIZE=$nodeHeap")
            }
            Invoke-Docker ($ComposeArgs + $buildArgs + $service)
        }
    } finally {
        if ($created) {
            # 停止并移除本次专用构建容器，缓存卷留给下次同名节点使用。
            Invoke-Docker @('buildx', 'rm', '--keep-state', $builder)
        }
    }
}

$lock = $null
$exitCode = 0
try {
    if (-not (Get-Command docker.exe -CommandType Application -ErrorAction SilentlyContinue)) {
        throw '未找到 Docker。请安装并启动 Docker Desktop，启用 Linux 容器后重新打开 PowerShell。'
    }
    $info = ((Invoke-Docker @('info', '--format', '{{json .}}')) -join "`n") | ConvertFrom-Json
    if ($info.OSType -ne 'linux') {
        throw '需要 Linux 容器。请在 Docker Desktop 中切换到 Linux containers。'
    }
    Invoke-Docker @('compose', 'version')

    $hash = [Security.Cryptography.SHA256]::Create()
    try {
        $RepositoryHash = [BitConverter]::ToString($hash.ComputeHash($Utf8.GetBytes($Repository.ToLowerInvariant()))).Replace('-', '').Substring(0, 12).ToLowerInvariant()
    } finally {
        $hash.Dispose()
    }
    $lockPath = Join-Path ([IO.Path]::GetTempPath()) ('weknora-win-' + $RepositoryHash + '.lock')
    $lock = [IO.File]::Open($lockPath, [IO.FileMode]::OpenOrCreate, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None)

    if ($Action -in @('Deploy', 'Build', 'Start', 'Check')) {
        Initialize-Environment
        Invoke-Docker ($ComposeArgs + @('config', '-q'))
    }
    if ($Action -in @('Deploy', 'Build')) {
        if ($info.MemTotal -lt ($BuildMemoryGB * 1GB)) {
            throw "Docker 可用内存不足 $BuildMemoryGB GiB。请在 Docker Desktop / WSL 设置中增加内存，或用 -BuildMemoryGB 调整限额。"
        }
        Build-Images
    }
    switch ($Action) {
        { $_ -in @('Deploy', 'Start') } {
            # 只拉取数据库和队列镜像；应用、前端与解析器必须使用本地修改版。
            Invoke-Docker ($ComposeArgs + @('pull', 'postgres', 'redis'))
            Invoke-Docker ($ComposeArgs + @('up', '-d', '--no-build', '--pull', 'never', '--wait', '--wait-timeout', [string]$StartupTimeout, 'frontend', 'app', 'docreader', 'postgres', 'redis'))
            Invoke-Docker ($ComposeArgs + 'ps')
            $bindings = @(Invoke-Docker ($ComposeArgs + @('port', 'frontend', '80')))
            $port = ($bindings[0].Trim() -split ':')[-1]
            Write-Host "部署完成，请打开 http://localhost:$port，注册账号并配置模型。"
        }
        'Build' { Write-Host '本地镜像构建完成。可使用 -Action Start 启动服务。' }
        'Stop' { Invoke-Docker ($ComposeArgs + 'stop') }
        'Status' { Invoke-Docker ($ComposeArgs + 'ps') }
        'Logs' { Invoke-Docker ($ComposeArgs + @('logs', '--tail', '100', 'app', 'docreader', 'frontend')) }
        'Check' { Write-Host 'Docker Linux 引擎和 Compose 配置检查通过，尚未构建或启动服务。' }
    }
} catch {
    [Console]::Error.WriteLine($_.Exception.Message)
    $exitCode = 1
} finally {
    if ($null -ne $lock) {
        $lock.Dispose()
    }
}
exit $exitCode
