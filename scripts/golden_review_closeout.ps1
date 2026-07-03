param(
    [string]$Base = "data/enrichment/golden-sample-1000.prefilled.jsonl",
    [string]$Sheet = "data/enrichment/golden-sample-1000.prefilled-review-66.csv",
    [string]$Out = "data/enrichment/golden-sample-1000.reviewed.jsonl",
    [string]$AuditOut = "data/enrichment/golden-sample-1000.reviewed.annotation-audit.json",
    [string]$SheetAuditOut = "data/enrichment/golden-sample-1000.prefilled-review-66.audit.json",
    [string]$Reviewer = "operator",
    [switch]$Apply,
    [ValidateSet("auto", "local", "docker", "python")]
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
    if (-not (Test-CommandExists "docker")) {
        return $false
    }
    try {
        & docker version --format "{{.Server.Version}}" *> $null
        return $LASTEXITCODE -eq 0
    } catch {
        return $false
    }
}

function Test-LocalGoReady {
    if (-not (Test-CommandExists "go")) {
        return $false
    }

    try {
        $cgo = (& go env CGO_ENABLED 2>$null).Trim()
        if ($LASTEXITCODE -ne 0 -or $cgo -ne "1") {
            return $false
        }

        $cc = (& go env CC 2>$null).Trim()
        if ([string]::IsNullOrWhiteSpace($cc)) {
            $cc = "gcc"
        }

        if (Test-CommandExists $cc) {
            return $true
        }
        if (Test-CommandExists "gcc") {
            return $true
        }
        if (Test-CommandExists "clang") {
            return $true
        }
        if (Test-CommandExists "zig") {
            return $true
        }
    } catch {
        return $false
    }

    return $false
}

function Test-PythonReady {
    try {
        & python --version *> $null
        return $LASTEXITCODE -eq 0
    } catch {
        return $false
    }
}

function Resolve-Runner {
    if ($Runner -ne "auto") {
        return $Runner
    }
    if (Test-LocalGoReady) {
        return "local"
    }
    if (Test-PythonReady) {
        return "python"
    }
    if (Test-DockerReady) {
        return "docker"
    }
    throw "Local Go CGO/C compiler is not ready, Docker is not ready, and Python fallback is unavailable. Start Docker Desktop, install a local CGO toolchain, or install Python."
}

function Get-LocalPath {
    param([string]$PathValue)
    if ([System.IO.Path]::IsPathRooted($PathValue)) {
        return $PathValue
    }
    return (Join-Path $RepoRoot $PathValue)
}

function Convert-ToCommandPath {
    param([string]$PathValue)

    if ($ResolvedRunner -ne "docker") {
        return $PathValue
    }

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

$ResolvedRunner = Resolve-Runner
if ($ResolvedRunner -eq "python") {
    $fallback = Join-Path $PSScriptRoot "golden_review_closeout_fallback.py"
    if (-not (Test-Path -LiteralPath $fallback)) {
        throw "Python fallback not found: $fallback"
    }
    Write-Host "Runner: python"
    $fallbackArgs = @(
        $fallback,
        "--base", $Base,
        "--sheet", $Sheet,
        "--out", $Out,
        "--audit-out", $AuditOut,
        "--sheet-audit-out", $SheetAuditOut,
        "--reviewer", $Reviewer,
        "--require-done"
    )
    if ($Apply) {
        $fallbackArgs += "--apply"
    }
    Write-Host ""
    Write-Host ("> python " + ($fallbackArgs -join " "))
    & python @fallbackArgs
    if ($LASTEXITCODE -ne 0) {
        exit $LASTEXITCODE
    }
    exit 0
}

$localBase = Get-LocalPath $Base
$localSheet = Get-LocalPath $Sheet
if (-not (Test-Path -LiteralPath $localBase)) {
    throw "Base golden file not found: $localBase"
}
if (-not (Test-Path -LiteralPath $localSheet)) {
    throw "Review sheet not found: $localSheet"
}

foreach ($pathValue in @($Out, $AuditOut, $SheetAuditOut)) {
    $dir = Split-Path -Parent (Get-LocalPath $pathValue)
    if (-not [string]::IsNullOrWhiteSpace($dir)) {
        New-Item -ItemType Directory -Force -Path $dir | Out-Null
    }
}

$commandBase = Convert-ToCommandPath $Base
$commandSheet = Convert-ToCommandPath $Sheet
$commandOut = Convert-ToCommandPath $Out
$commandAuditOut = Convert-ToCommandPath $AuditOut
$commandSheetAuditOut = Convert-ToCommandPath $SheetAuditOut

Write-Host "Runner: $ResolvedRunner"

function Invoke-Enrichment {
    param([string[]]$CommandArgs)

    Write-Host ""
    if ($ResolvedRunner -eq "docker") {
        $dockerArgs = @(
            "run", "--rm",
            "-v", "${RepoRoot}:/app",
            "-v", "poetry-go-mod-cache:/go/pkg/mod",
            "-v", "poetry-go-build-cache:/root/.cache/go-build",
            "-w", "/app",
            $DockerImage,
            "go", "run", "./cmd/enrichment"
        ) + $CommandArgs
        Write-Host ("> docker " + ($dockerArgs -join " "))
        & docker @dockerArgs
    } else {
        Write-Host ("> go run ./cmd/enrichment " + ($CommandArgs -join " "))
        & go run ./cmd/enrichment @CommandArgs
    }
    if ($LASTEXITCODE -ne 0) {
        throw "enrichment command failed with exit code $LASTEXITCODE"
    }
}

Invoke-Enrichment -CommandArgs @(
    "golden-review-sheet-audit",
    "--sheet", $commandSheet,
    "--out", $commandSheetAuditOut,
    "--require-done"
)

if (-not $Apply) {
    Write-Host ""
    Write-Host "Apply switch is not set; sheet is complete enough to merge, but no merged golden file was written."
    [ordered]@{
        mode = "dry_run"
        base = $Base
        sheet = $Sheet
        sheet_audit = $SheetAuditOut
        output = $Out
        final_audit = $AuditOut
        next_step = "rerun with -Apply to write merged golden JSONL and final audit"
    } | ConvertTo-Json -Depth 4
    exit 0
}

Invoke-Enrichment -CommandArgs @(
    "golden-apply-review-sheet",
    "--base", $commandBase,
    "--sheet", $commandSheet,
    "--output", $commandOut,
    "--reviewer", $Reviewer
)

Invoke-Enrichment -CommandArgs @(
    "golden-audit",
    "--input", $commandOut,
    "--out", $commandAuditOut
)

[ordered]@{
    mode = "apply"
    base = $Base
    sheet = $Sheet
    sheet_audit = $SheetAuditOut
    output = $Out
    final_audit = $AuditOut
    reviewer = $Reviewer
} | ConvertTo-Json -Depth 4
