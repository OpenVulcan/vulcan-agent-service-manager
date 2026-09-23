# Download, verify, and launch only the standalone vasm manager.
# 仅下载、校验并启动独立的 vasm 管理器。
$ErrorActionPreference = 'Stop'

# Resolve bootstrap choices from environment variables for irm | iex usage.
# 从环境变量读取引导选项，以兼容 irm | iex。
$sourceName = if ($env:VASM_SOURCE) { $env:VASM_SOURCE } else { 'github' }
$mirrorBase = if ($env:VASM_MIRROR_BASE) { $env:VASM_MIRROR_BASE } else { 'https://gh-proxy.com' }
$managerVersion = if ($env:VASM_VERSION) { $env:VASM_VERSION } else { 'latest' }
if ($managerVersion -ne 'latest' -and $managerVersion -notmatch '^v[0-9]+\.[0-9]+\.[0-9]+$') {
    throw 'Invalid manager release tag'
}
if ($sourceName -notin @('github', 'mirror')) {
    throw 'VASM_SOURCE must be github or mirror'
}
if ($sourceName -eq 'mirror' -and $mirrorBase -notmatch '^https://[^\s]+$') {
    throw 'VASM_MIRROR_BASE must be an HTTPS URL'
}

# Windows x64 is the only published Windows manager target.
# Windows x64 是唯一已发布的 Windows 管理器目标。
if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture -ne [System.Runtime.InteropServices.Architecture]::X64) {
    throw 'Only Windows x64 is supported by this installer'
}
$assetName = 'vasm-windows-x64.zip'
$releaseBase = 'https://github.com/OpenVulcan/vulcan-agent-service-manager/releases'
if ($managerVersion -eq 'latest') {
    $officialUrl = "$releaseBase/latest/download/$assetName"
} else {
    $officialUrl = "$releaseBase/download/$managerVersion/$assetName"
}
$archiveUrl = if ($sourceName -eq 'mirror') { "$($mirrorBase.TrimEnd('/'))/$officialUrl" } else { $officialUrl }

# Always obtain the checksum from the canonical release endpoint.
# 校验值始终从权威发布端点获取。
$temporaryDirectory = Join-Path ([System.IO.Path]::GetTempPath()) ('vasm-bootstrap-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $temporaryDirectory | Out-Null
try {
    $archivePath = Join-Path $temporaryDirectory $assetName
    $checksumPath = "$archivePath.sha256"
    Invoke-WebRequest -Uri "$officialUrl.sha256" -OutFile $checksumPath
    Invoke-WebRequest -Uri $archiveUrl -OutFile $archivePath
    $checksumLine = (Get-Content -LiteralPath $checksumPath -Raw).Trim()
    if ($checksumLine -notmatch '^([0-9a-f]{64})  vasm-windows-x64\.zip$') {
        throw 'Invalid manager checksum sidecar'
    }
    $actualHash = (Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actualHash -ne $Matches[1]) {
        throw 'Manager archive SHA-256 verification failed'
    }
    Expand-Archive -LiteralPath $archivePath -DestinationPath $temporaryDirectory
    $sourceBinary = Join-Path $temporaryDirectory 'vasm-windows-x64\vasm.exe'
    if (-not (Test-Path -LiteralPath $sourceBinary -PathType Leaf)) {
        throw 'Manager archive layout is invalid'
    }
    $sourceVersion = (& $sourceBinary --version | Out-String).Trim()
    if ($sourceVersion -notmatch '^vasm [0-9]+\.[0-9]+\.[0-9]+$') {
        throw 'Downloaded manager failed its version smoke test'
    }
    if ($managerVersion -ne 'latest' -and $sourceVersion -ne ('vasm ' + $managerVersion.Substring(1))) {
        throw 'Downloaded manager version does not match the selected tag'
    }

    # Copy the verified manager into a stable per-user command directory.
    # 将校验通过的管理器复制到稳定的用户命令目录。
    $commandDirectory = Join-Path $env:LOCALAPPDATA 'OpenVulcan\vasm\bin'
    New-Item -ItemType Directory -Path $commandDirectory -Force | Out-Null
    $destination = Join-Path $commandDirectory 'vasm.exe'
    $stagedDestination = Join-Path $commandDirectory ('.vasm-new-' + [guid]::NewGuid().ToString('N') + '.exe')
    Copy-Item -LiteralPath $sourceBinary -Destination $stagedDestination
    try {
        Move-Item -LiteralPath $stagedDestination -Destination $destination -Force
    } catch {
        if (-not (Test-Path -LiteralPath $destination -PathType Leaf)) {
            throw
        }
        Remove-Item -LiteralPath $stagedDestination -Force -ErrorAction SilentlyContinue
        Write-Warning 'The existing vasm executable is in use. Open it and choose Update vasm to complete the version change.'
    }
    Write-Host "vasm installed at $destination"
    & $destination
} finally {
    Remove-Item -LiteralPath $temporaryDirectory -Recurse -Force -ErrorAction SilentlyContinue
}
