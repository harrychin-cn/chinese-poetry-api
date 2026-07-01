param(
    [string]$Db = "data/poetry.db",
    [string]$GoldenInput = "data/enrichment/golden-sample-1000.reviewed.jsonl",
    [int]$Limit = 20,
    [string]$RunId = "",
    [string]$OutDir = "data/enrichment",
    [ValidateSet("qanlo", "rules")]
    [string]$Provider = "qanlo",
    [string]$Model = "deepseek-v4-flash",
    [string]$BaseUrl = "",
    [string]$ApiKeyEnv = "QANLO_AGENT_KEY",
    [int]$BatchSize = 20,
    [double]$MinConfidence = 0.7,
    [int]$QualityMaxErrors = 0,
    [switch]$Import,
    [int]$ReviewLimit = 20,
    [ValidateSet("auto", "local", "docker")]
    [string]$Runner = "auto",
    [string]$DockerImage = "golang:1.25"
)

$ErrorActionPreference = "Stop"

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
Set-Location $RepoRoot

if ($Limit -lt 1) {
    throw "Limit must be positive."
}
if ($BatchSize -lt 1) {
    throw "BatchSize must be positive."
}
if ($MinConfidence -lt 0 -or $MinConfidence -gt 1) {
    throw "MinConfidence must be between 0 and 1."
}
if ($QualityMaxErrors -lt 0) {
    throw "QualityMaxErrors cannot be negative."
}
if ($ReviewLimit -lt 1) {
    throw "ReviewLimit must be positive."
}

if ([string]::IsNullOrWhiteSpace($RunId)) {
    $RunId = "ai-$Provider-golden-" + (Get-Date -Format "yyyyMMdd-HHmmss") + "-sample$Limit"
}

if ([string]::IsNullOrWhiteSpace($BaseUrl)) {
    if ([string]::IsNullOrWhiteSpace($env:QANLO_OPENAI_BASE_URL)) {
        $BaseUrl = "https://qanlo.com/v1"
    } else {
        $BaseUrl = $env:QANLO_OPENAI_BASE_URL
    }
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

function Test-QanloKeyPresent {
    if (-not [string]::IsNullOrWhiteSpace([Environment]::GetEnvironmentVariable($ApiKeyEnv))) {
        return $true
    }
    if ($ApiKeyEnv -eq "QANLO_AGENT_KEY" -and -not [string]::IsNullOrWhiteSpace($env:QANLO_API_KEY)) {
        return $true
    }
    return $false
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

if ($Provider -eq "qanlo" -and -not (Test-QanloKeyPresent)) {
    throw "$ApiKeyEnv is not set. Set Qanlo Agent Key first; if using QANLO_AGENT_KEY fallback, QANLO_API_KEY is also accepted."
}

$localGoldenInput = Get-LocalPath $GoldenInput
if (-not (Test-Path -LiteralPath $localGoldenInput)) {
    throw "GoldenInput not found: $localGoldenInput"
}

$localOutDir = Get-LocalPath $OutDir
New-Item -ItemType Directory -Force -Path $localOutDir | Out-Null

$safeRunId = $RunId -replace '[^A-Za-z0-9_.-]', '-'
$samplePath = Join-Path $localOutDir "sample-$safeRunId.jsonl"
$candidatePath = Join-Path $localOutDir "candidates-$safeRunId.jsonl"
$validatePath = Join-Path $localOutDir "validate-$safeRunId.json"
$qualityGatePath = Join-Path $localOutDir "quality-gate-$safeRunId.json"
$manualSamplePath = Join-Path $localOutDir "manual-sample-$safeRunId.jsonl"

$commandDb = Convert-ToCommandPath $Db
$commandGoldenInput = Convert-ToCommandPath $GoldenInput
$commandSamplePath = Convert-ToCommandPath $samplePath
$commandCandidatePath = Convert-ToCommandPath $candidatePath
$commandValidatePath = Convert-ToCommandPath $validatePath
$commandQualityGatePath = Convert-ToCommandPath $qualityGatePath
$commandManualSamplePath = Convert-ToCommandPath $manualSamplePath

Write-Host "Runner: $ResolvedRunner"
Write-Host "RunId: $RunId"

function Invoke-Enrichment {
    param([string[]]$CommandArgs)

    Write-Host ""
    if ($ResolvedRunner -eq "docker") {
        $dockerArgs = @(
            "run", "--rm",
            "-v", "${RepoRoot}:/app",
            "-v", "poetry-go-mod-cache:/go/pkg/mod",
            "-v", "poetry-go-build-cache:/root/.cache/go-build",
            "-w", "/app"
        )
        if ($Provider -eq "qanlo") {
            $dockerArgs += @("-e", $ApiKeyEnv)
            if ($ApiKeyEnv -eq "QANLO_AGENT_KEY" -and -not [string]::IsNullOrWhiteSpace($env:QANLO_API_KEY)) {
                $dockerArgs += @("-e", "QANLO_API_KEY")
            }
        }
        $dockerArgs += @(
            $DockerImage,
            "go", "run", "./cmd/enrichment"
        )
        $dockerArgs = $dockerArgs + $CommandArgs
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
    "golden-to-sample",
    "--input", $commandGoldenInput,
    "--output", $commandSamplePath,
    "--limit", "$Limit",
    "--require-done"
)

$generateArgs = @(
    "generate",
    "--provider", $Provider,
    "--input", $commandSamplePath,
    "--output", $commandCandidatePath,
    "--batch-size", "$BatchSize"
)
if ($Provider -eq "qanlo") {
    $generateArgs += @("--model", $Model, "--base-url", $BaseUrl, "--api-key-env", $ApiKeyEnv)
}
Invoke-Enrichment -CommandArgs $generateArgs

Invoke-Enrichment -CommandArgs @("--db", $commandDb, "validate", "--input", $commandCandidatePath, "--out", $commandValidatePath)

Invoke-Enrichment -CommandArgs @(
    "quality-gate",
    "--input", $commandCandidatePath,
    "--sample", $commandSamplePath,
    "--out", $commandQualityGatePath,
    "--min-confidence", "$MinConfidence",
    "--max-errors", "$QualityMaxErrors"
)

if ($Import) {
    Invoke-Enrichment -CommandArgs @("--db", $commandDb, "import-candidates", "--input", $commandCandidatePath, "--run-id", $RunId, "--scope", "ai-candidate-trial")
    Invoke-Enrichment -CommandArgs @("--db", $commandDb, "sample-review", "--run-id", $RunId, "--limit", "$ReviewLimit", "--out", $commandManualSamplePath)
} else {
    Write-Host ""
    Write-Host "Import switch is not set; candidates were validated and quality-gated but not written to the database."
}

$result = [ordered]@{
    run_id = $RunId
    db = $Db
    golden_input = $GoldenInput
    provider = $Provider
    model = $(if ($Provider -eq "qanlo") { $Model } else { "rules-v13" })
    runner = $ResolvedRunner
    limit = $Limit
    sample = $samplePath
    candidates = $candidatePath
    validate = $validatePath
    quality_gate = $qualityGatePath
    imported = [bool]$Import
    manual_sample = $(if ($Import) { $manualSamplePath } else { $null })
}

Write-Host ""
$result | ConvertTo-Json -Depth 4
