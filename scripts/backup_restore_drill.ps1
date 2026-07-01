param(
    [string]$Db = "data/poetry.db",
    [string]$OutDir = "backups/drill",
    [string]$RestoreDir = ".codex-temp/restore-drill",
    [int]$Keep = 2,
    [ValidateSet("auto", "local", "docker")]
    [string]$Runner = "auto",
    [string]$DockerImage = "golang:1.25"
)

$ErrorActionPreference = "Stop"

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
Set-Location $RepoRoot

function Test-CommandExists {
    param([string]$Name)
    return $null -ne (Get-Command $Name -ErrorAction SilentlyContinue)
}

function Test-DockerReady {
    if (-not (Test-CommandExists "docker")) { return $false }
    try {
        & docker version --format "{{.Server.Version}}" *> $null
        return $LASTEXITCODE -eq 0
    } catch {
        return $false
    }
}

function Test-LocalGoReady {
    if (-not (Test-CommandExists "go")) { return $false }
    try {
        $cgo = (& go env CGO_ENABLED 2>$null).Trim()
        if ($LASTEXITCODE -ne 0 -or $cgo -ne "1") { return $false }
        $cc = (& go env CC 2>$null).Trim()
        if ([string]::IsNullOrWhiteSpace($cc)) { $cc = "gcc" }
        foreach ($candidate in @($cc, "gcc", "clang", "zig")) {
            if (Test-CommandExists $candidate) { return $true }
        }
    } catch {
        return $false
    }
    return $false
}

function Resolve-Runner {
    if ($Runner -ne "auto") { return $Runner }
    if (Test-LocalGoReady) { return "local" }
    if (Test-DockerReady) { return "docker" }
    throw "Local Go CGO/C compiler is not ready and Docker is not ready. Start Docker Desktop, or install a local CGO toolchain and set CGO_ENABLED=1."
}

function Get-LocalPath {
    param([string]$PathValue)
    if ([System.IO.Path]::IsPathRooted($PathValue)) { return $PathValue }
    return (Join-Path $RepoRoot $PathValue)
}

function Convert-ToCommandPath {
    param(
        [string]$PathValue,
        [string]$ResolvedRunner
    )
    if ($ResolvedRunner -ne "docker") { return $PathValue }

    $fullPath = Get-LocalPath $PathValue
    $resolvedFull = [System.IO.Path]::GetFullPath($fullPath)
    $resolvedRoot = [System.IO.Path]::GetFullPath($RepoRoot)
    if (-not $resolvedRoot.EndsWith([System.IO.Path]::DirectorySeparatorChar)) {
        $resolvedRoot = $resolvedRoot + [System.IO.Path]::DirectorySeparatorChar
    }
    if (-not $resolvedFull.StartsWith($resolvedRoot, [System.StringComparison]::OrdinalIgnoreCase)) {
        throw "Docker runner only supports paths inside repo: $PathValue"
    }

    $rootUri = New-Object System.Uri($resolvedRoot)
    $fileUri = New-Object System.Uri($resolvedFull)
    $relative = [System.Uri]::UnescapeDataString($rootUri.MakeRelativeUri($fileUri).ToString())
    return "/app/" + ($relative -replace "\\", "/")
}

function Invoke-Backup {
    param(
        [string]$Name,
        [string]$DbPath,
        [string]$OutPath,
        [int]$KeepValue,
        [string]$ResolvedRunner
    )

    $commandDb = Convert-ToCommandPath -PathValue $DbPath -ResolvedRunner $ResolvedRunner
    $commandOut = Convert-ToCommandPath -PathValue $OutPath -ResolvedRunner $ResolvedRunner
    $backupArgs = @("--db", $commandDb, "--out", $commandOut, "--keep", "$KeepValue")

    Write-Host ""
    Write-Host "==> $Name"
    if ($ResolvedRunner -eq "docker") {
        $dockerArgs = @(
            "run", "--rm",
            "-v", "${RepoRoot}:/app",
            "-v", "poetry-go-mod-cache:/go/pkg/mod",
            "-v", "poetry-go-build-cache:/root/.cache/go-build",
            "-w", "/app",
            $DockerImage,
            "go", "run", "./cmd/backup"
        ) + $backupArgs
        Write-Host ("> docker " + ($dockerArgs -join " "))
        & docker @dockerArgs
    } else {
        $goArgs = @("run", "./cmd/backup") + $backupArgs
        Write-Host ("> go " + ($goArgs -join " "))
        & go @goArgs
    }
    if ($LASTEXITCODE -ne 0) {
        throw "$Name failed with exit code $LASTEXITCODE"
    }
}

$ResolvedRunner = Resolve-Runner
Write-Host "Runner: $ResolvedRunner"

$localOut = Get-LocalPath $OutDir
$localRestore = Get-LocalPath $RestoreDir
New-Item -ItemType Directory -Force -Path $localOut | Out-Null
New-Item -ItemType Directory -Force -Path $localRestore | Out-Null

Invoke-Backup -Name "create source backup" -DbPath $Db -OutPath $OutDir -KeepValue $Keep -ResolvedRunner $ResolvedRunner

$manifest = Get-ChildItem -LiteralPath $localOut -Filter "*.manifest.json" |
    Where-Object { $_.Name -ne "manifest.json" } |
    Sort-Object LastWriteTime -Descending |
    Select-Object -First 1
if ($null -eq $manifest) {
    throw "No backup manifest found in $localOut"
}

$manifestJson = Get-Content -LiteralPath $manifest.FullName -Raw -Encoding UTF8 | ConvertFrom-Json
if ($manifestJson.quick_check -ne "ok") {
    throw "Backup manifest quick_check is not ok: $($manifestJson.quick_check)"
}

$backupPath = Join-Path $localOut $manifestJson.backup_file
if (-not (Test-Path -LiteralPath $backupPath)) {
    throw "Backup file from manifest does not exist: $backupPath"
}

$restoredDb = Join-Path $localRestore "poetry-restored.db"
Copy-Item -LiteralPath $backupPath -Destination $restoredDb -Force

$checkDir = Join-Path $RestoreDir "check"
Invoke-Backup -Name "verify restored database" -DbPath $restoredDb -OutPath $checkDir -KeepValue 1 -ResolvedRunner $ResolvedRunner

$checkLocalDir = Get-LocalPath $checkDir
$checkManifest = Get-ChildItem -LiteralPath $checkLocalDir -Filter "*.manifest.json" |
    Where-Object { $_.Name -ne "manifest.json" } |
    Sort-Object LastWriteTime -Descending |
    Select-Object -First 1
if ($null -eq $checkManifest) {
    throw "No restore-check manifest found in $checkLocalDir"
}
$checkJson = Get-Content -LiteralPath $checkManifest.FullName -Raw -Encoding UTF8 | ConvertFrom-Json
if ($checkJson.quick_check -ne "ok") {
    throw "Restored database quick_check is not ok: $($checkJson.quick_check)"
}

$summary = [ordered]@{
    runner = $ResolvedRunner
    source_db = (Get-LocalPath $Db)
    backup_file = $backupPath
    backup_manifest = $manifest.FullName
    restored_db = $restoredDb
    restore_check_manifest = $checkManifest.FullName
    quick_check = $checkJson.quick_check
    backup_size_bytes = $manifestJson.backup_size_bytes
    restore_check_size_bytes = $checkJson.backup_size_bytes
}

Write-Host ""
$summary | ConvertTo-Json -Depth 4
