param(
    [string]$Db = "data/poetry.db",
    [int]$Limit = 100,
    [string]$RunId = "",
    [string]$OutDir = "data/enrichment",
    [int]$Offset = 0,
    [ValidateSet("rules", "qanlo")]
    [string]$Provider = "rules",
    [string]$Model = "deepseek-v4-flash",
    [string]$BaseUrl = "",
    [string]$ApiKeyEnv = "QANLO_AGENT_KEY",
    [int]$BatchSize = 20,
    [switch]$SkipImport,
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
if ($Offset -lt 0) {
    throw "Offset cannot be negative."
}

if ([string]::IsNullOrWhiteSpace($RunId)) {
    $RunId = "enrich-" + (Get-Date -Format "yyyyMMdd-HHmmss") + "-sample$Limit"
}

if ([string]::IsNullOrWhiteSpace($BaseUrl)) {
    if ([string]::IsNullOrWhiteSpace($env:QANLO_OPENAI_BASE_URL)) {
        $BaseUrl = "https://qanlo.com/v1"
    } else {
        $BaseUrl = $env:QANLO_OPENAI_BASE_URL
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

$ResolvedRunner = Resolve-Runner

if ($Provider -eq "qanlo" -and [string]::IsNullOrWhiteSpace([Environment]::GetEnvironmentVariable($ApiKeyEnv))) {
    throw "$ApiKeyEnv is not set. Set the Qanlo Agent Key before running provider=qanlo."
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

    $rootForUri = $resolvedRoot
    if (-not $rootForUri.EndsWith([System.IO.Path]::DirectorySeparatorChar)) {
        $rootForUri = $rootForUri + [System.IO.Path]::DirectorySeparatorChar
    }
    $rootUri = New-Object System.Uri($rootForUri)
    $fileUri = New-Object System.Uri($resolvedFull)
    $relative = [System.Uri]::UnescapeDataString($rootUri.MakeRelativeUri($fileUri).ToString())
    return "/app/" + ($relative -replace "\\", "/")
}

$localOutDir = Get-LocalPath $OutDir
New-Item -ItemType Directory -Force -Path $localOutDir | Out-Null

$safeRunId = $RunId -replace '[^A-Za-z0-9_.-]', '-'
$samplePath = Join-Path $localOutDir "sample-$safeRunId.jsonl"
$candidatePath = Join-Path $localOutDir "candidates-$safeRunId.jsonl"
$reportPath = Join-Path $localOutDir "review-report-$safeRunId.json"

$commandDb = Convert-ToCommandPath $Db
$commandSamplePath = Convert-ToCommandPath $samplePath
$commandCandidatePath = Convert-ToCommandPath $candidatePath
$commandReportPath = Convert-ToCommandPath $reportPath

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
        )
        if ($Provider -eq "qanlo") {
            $dockerArgs = @("run", "--rm",
                "-v", "${RepoRoot}:/app",
                "-v", "poetry-go-mod-cache:/go/pkg/mod",
                "-v", "poetry-go-build-cache:/root/.cache/go-build",
                "-e", $ApiKeyEnv,
                "-w", "/app",
                $DockerImage,
                "go", "run", "./cmd/enrichment"
            )
            if ($ApiKeyEnv -eq "QANLO_AGENT_KEY" -and -not [string]::IsNullOrWhiteSpace($env:QANLO_API_KEY)) {
                $dockerArgs = @("run", "--rm",
                    "-v", "${RepoRoot}:/app",
                    "-v", "poetry-go-mod-cache:/go/pkg/mod",
                    "-v", "poetry-go-build-cache:/root/.cache/go-build",
                    "-e", $ApiKeyEnv,
                    "-e", "QANLO_API_KEY",
                    "-w", "/app",
                    $DockerImage,
                    "go", "run", "./cmd/enrichment"
                )
            }
        }
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

Invoke-Enrichment -CommandArgs @("--db", $commandDb, "export-sample", "--limit", "$Limit", "--offset", "$Offset", "--out", $commandSamplePath)

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

Invoke-Enrichment -CommandArgs @("--db", $commandDb, "validate", "--input", $commandCandidatePath)

if (-not $SkipImport) {
    Invoke-Enrichment -CommandArgs @("--db", $commandDb, "import-candidates", "--input", $commandCandidatePath, "--run-id", $RunId)
    Invoke-Enrichment -CommandArgs @("--db", $commandDb, "review-report", "--run-id", $RunId, "--out", $commandReportPath)
} else {
    Write-Host ""
    Write-Host "SkipImport is set; candidates were validated but not imported."
}

$result = [ordered]@{
    run_id = $RunId
    db = $Db
    provider = $Provider
    runner = $ResolvedRunner
    limit = $Limit
    offset = $Offset
    sample = $samplePath
    candidates = $candidatePath
    imported = (-not $SkipImport)
    report = $(if ($SkipImport) { $null } else { $reportPath })
}

Write-Host ""
$result | ConvertTo-Json -Depth 4
