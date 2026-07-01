param(
    [Parameter(Mandatory = $true)]
    [string]$RunId,
    [string]$Database = "data/poetry.db",
    [string]$ReviewFile = "",
    [string]$OutDir = "data/enrichment",
    [int]$ReviewLimit = 20,
    [double]$MinPassRate = 0.9,
    [string]$Reviewer = "operator",
    [switch]$ExportSample,
    [switch]$AuditOnly,
    [switch]$RequireReviewed,
    [switch]$Apply,
    [ValidateSet("auto", "local", "docker")]
    [string]$Runner = "auto",
    [string]$DockerImage = "golang:1.25"
)

$ErrorActionPreference = "Stop"

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
Set-Location $RepoRoot

if ([string]::IsNullOrWhiteSpace($RunId)) {
    throw "RunId is required."
}
if ($ReviewLimit -lt 1) {
    throw "ReviewLimit must be positive."
}
if ($MinPassRate -lt 0 -or $MinPassRate -gt 1) {
    throw "MinPassRate must be between 0 and 1."
}

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
        return (Test-CommandExists $cc) -or (Test-CommandExists "gcc") -or (Test-CommandExists "clang") -or (Test-CommandExists "zig")
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
    if (Test-DockerReady) {
        return "docker"
    }
    throw "Local Go CGO/C compiler is not ready and Docker is not ready. Start Docker Desktop, or install a local CGO toolchain and set CGO_ENABLED=1."
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

function Invoke-Enrichment {
    param(
        [string[]]$CommandArgs,
        [string]$CaptureTo = ""
    )

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
        $output = & docker @dockerArgs 2>&1
    } else {
        Write-Host ("> go run ./cmd/enrichment " + ($CommandArgs -join " "))
        $output = & go run ./cmd/enrichment @CommandArgs 2>&1
    }
    $exitCode = $LASTEXITCODE
    if (-not [string]::IsNullOrWhiteSpace($CaptureTo)) {
        $capturePath = Get-LocalPath $CaptureTo
        $captureDir = Split-Path -Parent $capturePath
        if (-not [string]::IsNullOrWhiteSpace($captureDir)) {
            New-Item -ItemType Directory -Force -Path $captureDir | Out-Null
        }
        ($output | Out-String).Trim() | Set-Content -LiteralPath $capturePath -Encoding UTF8
    }
    if ($exitCode -ne 0) {
        $message = (($output | Out-String).Trim())
        if ($message.Length -gt 600) {
            $message = $message.Substring(0, 600) + "..."
        }
        throw "enrichment command failed with exit code $exitCode. $message"
    }
    return $output
}

function Read-JsonFile {
    param([string]$PathValue)
    $local = Get-LocalPath $PathValue
    if (-not (Test-Path -LiteralPath $local)) {
        return $null
    }
    return (Get-Content -LiteralPath $local -Raw -Encoding UTF8 | ConvertFrom-Json)
}

$ResolvedRunner = Resolve-Runner
$safeRunId = $RunId -replace '[^A-Za-z0-9_.-]', '-'

if ([string]::IsNullOrWhiteSpace($ReviewFile)) {
    $ReviewFile = Join-Path $OutDir "manual-sample-$safeRunId.jsonl"
}

$auditOut = Join-Path $OutDir "review-audit-$safeRunId.json"
$applyDryRunOut = Join-Path $OutDir "apply-review-$safeRunId.dry-run.json"
$applyOut = Join-Path $OutDir "apply-review-$safeRunId.apply.json"
$reviewReportOut = Join-Path $OutDir "review-report-$safeRunId.json"

foreach ($pathValue in @($ReviewFile, $auditOut, $applyDryRunOut, $applyOut, $reviewReportOut)) {
    $dir = Split-Path -Parent (Get-LocalPath $pathValue)
    if (-not [string]::IsNullOrWhiteSpace($dir)) {
        New-Item -ItemType Directory -Force -Path $dir | Out-Null
    }
}

$commandDb = Convert-ToCommandPath $Database
$commandReviewFile = Convert-ToCommandPath $ReviewFile
$commandAuditOut = Convert-ToCommandPath $auditOut
$commandReviewReportOut = Convert-ToCommandPath $reviewReportOut

Write-Host "Runner: $ResolvedRunner"
Write-Host "RunId: $RunId"

if ($ExportSample) {
    Invoke-Enrichment -CommandArgs @(
        "--db", $commandDb,
        "sample-review",
        "--run-id", $RunId,
        "--limit", "$ReviewLimit",
        "--out", $commandReviewFile
    ) | Out-Null
}

if (-not (Test-Path -LiteralPath (Get-LocalPath $ReviewFile))) {
    throw "ReviewFile not found: $ReviewFile. Run ai_candidate_trial.ps1 with -Import, or rerun this script with -ExportSample after candidates are imported."
}

Invoke-Enrichment -CommandArgs @(
    "review-audit",
    "--input", $commandReviewFile,
    "--run-id", $RunId,
    "--out", $commandAuditOut
) | Out-Null

$audit = Read-JsonFile $auditOut
if ($null -eq $audit) {
    throw "failed to read review audit: $auditOut"
}

$auditReady = $audit.reviewed_count -gt 0 -and
    $audit.pending_count -eq 0 -and
    (($null -eq $audit.unsupported_actions) -or $audit.unsupported_actions.Count -eq 0) -and
    ([double]$audit.pass_rate -ge $MinPassRate)

if ($RequireReviewed -and -not $auditReady) {
    throw "AI review is not ready. reviewed=$($audit.reviewed_count), pending=$($audit.pending_count), pass_rate=$($audit.pass_rate_percent), min_pass_rate=$MinPassRate. Edit the review JSONL decisions first."
}

if ($AuditOnly) {
    [ordered]@{
        mode = "audit_only"
        run_id = $RunId
        review_file = $ReviewFile
        audit = $auditOut
        ready_for_apply = $auditReady
        reviewed_count = $audit.reviewed_count
        pending_count = $audit.pending_count
        pass_rate = $audit.pass_rate
        pass_rate_percent = $audit.pass_rate_percent
        next_step = $(if ($auditReady) { "rerun without -AuditOnly to dry-run/apply review decisions" } else { "edit review_decision.action and notes in the review JSONL" })
    } | ConvertTo-Json -Depth 6
    exit 0
}

Invoke-Enrichment -CommandArgs @(
    "--db", $commandDb,
    "apply-review",
    "--input", $commandReviewFile,
    "--reviewer", $Reviewer
) -CaptureTo $applyDryRunOut | Out-Null

if ($Apply) {
    if (-not $auditReady) {
        throw "Refusing to apply incomplete/low-pass review. Use -RequireReviewed during validation and fix the review JSONL first."
    }
    Invoke-Enrichment -CommandArgs @(
        "--db", $commandDb,
        "apply-review",
        "--input", $commandReviewFile,
        "--reviewer", $Reviewer,
        "--apply"
    ) -CaptureTo $applyOut | Out-Null

    Invoke-Enrichment -CommandArgs @(
        "--db", $commandDb,
        "review-report",
        "--run-id", $RunId,
        "--out", $commandReviewReportOut
    ) | Out-Null
}

[ordered]@{
    mode = $(if ($Apply) { "apply" } else { "dry_run" })
    run_id = $RunId
    review_file = $ReviewFile
    audit = $auditOut
    apply_dry_run = $applyDryRunOut
    apply_result = $(if ($Apply) { $applyOut } else { $null })
    review_report = $(if ($Apply) { $reviewReportOut } else { $null })
    ready_for_apply = $auditReady
    reviewed_count = $audit.reviewed_count
    pending_count = $audit.pending_count
    pass_rate = $audit.pass_rate
    pass_rate_percent = $audit.pass_rate_percent
    min_pass_rate = $MinPassRate
} | ConvertTo-Json -Depth 6
