param(
    [string]$Db = "data/poetry.db",
    [int]$Total = 1000,
    [int]$PerStratum = 80,
    [string]$Out = "data/enrichment/golden-sample-1000.jsonl",
    [ValidateSet("auto", "local", "docker")]
    [string]$Runner = "auto",
    [string]$DockerImage = "golang:1.25"
)

$ErrorActionPreference = "Stop"

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
Set-Location $RepoRoot

if ($Total -lt 1) {
    throw "Total must be positive."
}
if ($PerStratum -lt 1) {
    throw "PerStratum must be positive."
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

function Get-LocalPath {
    param([string]$PathValue)
    if ([System.IO.Path]::IsPathRooted($PathValue)) {
        return $PathValue
    }
    return (Join-Path $RepoRoot $PathValue)
}

function Convert-ToCommandPath {
    param(
        [string]$PathValue,
        [string]$ResolvedRunner
    )

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

function Invoke-External {
    param(
        [string]$Name,
        [string]$Exe,
        [string[]]$CommandArgs
    )

    Write-Host ""
    Write-Host "==> $Name"
    Write-Host ("> $Exe " + ($CommandArgs -join " "))
    & $Exe @CommandArgs
    if ($LASTEXITCODE -ne 0) {
        throw "$Name failed with exit code $LASTEXITCODE"
    }
}

function Test-GoldenSampleFile {
    param(
        [string]$PathValue,
        [int]$ExpectedTotal
    )

    $localPath = Get-LocalPath $PathValue
    if (-not (Test-Path $localPath)) {
        throw "Golden sample was not created: $localPath"
    }

    $ids = @{}
    $lineCount = 0
    $badCount = 0
    $emptyContent = 0
    $strata = @{}

    Get-Content -LiteralPath $localPath -Encoding UTF8 | ForEach-Object {
        $lineCount++
        try {
            $record = $_ | ConvertFrom-Json
            if ($null -eq $record.poem_id -or [int64]$record.poem_id -le 0) {
                $badCount++
            } else {
                $ids[[string]$record.poem_id] = $true
            }
            if ($null -eq $record.content -or $record.content.Count -lt 1) {
                $emptyContent++
            }
            $stratum = [string]$record.golden_meta.stratum
            if ([string]::IsNullOrWhiteSpace($stratum)) {
                $badCount++
            } else {
                if (-not $strata.ContainsKey($stratum)) {
                    $strata[$stratum] = 0
                }
                $strata[$stratum]++
            }
        } catch {
            $badCount++
        }
    }

    if ($lineCount -ne $ExpectedTotal) {
        throw "Golden sample line count mismatch: expected $ExpectedTotal, got $lineCount"
    }
    if ($ids.Count -ne $lineCount) {
        throw "Golden sample has duplicated or missing poem_id values: lines=$lineCount unique_ids=$($ids.Count)"
    }
    if ($badCount -ne 0) {
        throw "Golden sample contains invalid records: bad_count=$badCount"
    }
    if ($emptyContent -ne 0) {
        throw "Golden sample contains empty content: empty_content=$emptyContent"
    }

    return [ordered]@{
        file = $localPath
        total = $lineCount
        unique_poem_ids = $ids.Count
        strata_count = $strata.Count
        empty_content = $emptyContent
        bad_count = $badCount
    }
}

$ResolvedRunner = Resolve-Runner
Write-Host "Runner: $ResolvedRunner"

$localOut = Get-LocalPath $Out
$localOutDir = Split-Path -Parent $localOut
if (-not [string]::IsNullOrWhiteSpace($localOutDir)) {
    New-Item -ItemType Directory -Force -Path $localOutDir | Out-Null
}

$commandDb = Convert-ToCommandPath -PathValue $Db -ResolvedRunner $ResolvedRunner
$commandOut = Convert-ToCommandPath -PathValue $Out -ResolvedRunner $ResolvedRunner

$enrichmentArgs = @(
    "--db", $commandDb,
    "export-golden-sample",
    "--total", "$Total",
    "--per-stratum", "$PerStratum",
    "--out", $commandOut
)

if ($ResolvedRunner -eq "docker") {
    $dockerArgs = @(
        "run", "--rm",
        "-v", "${RepoRoot}:/app",
        "-v", "poetry-go-mod-cache:/go/pkg/mod",
        "-v", "poetry-go-build-cache:/root/.cache/go-build",
        "-w", "/app",
        $DockerImage,
        "go", "run", "./cmd/enrichment"
    ) + $enrichmentArgs
    Invoke-External -Name "export golden sample" -Exe "docker" -CommandArgs $dockerArgs
} else {
    Invoke-External -Name "export golden sample" -Exe "go" -CommandArgs (@("run", "./cmd/enrichment") + $enrichmentArgs)
}

$summary = Test-GoldenSampleFile -PathValue $Out -ExpectedTotal $Total
Write-Host ""
$summary | ConvertTo-Json -Depth 4
