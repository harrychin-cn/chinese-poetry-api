param(
    [ValidateSet("auto", "local", "docker")]
    [string]$Runner = "auto",
    [string]$DockerImage = "golang:1.25",
    [switch]$SkipBuild,
    [switch]$SkipFTS,
    [switch]$DockerBuild,
    [string]$DockerBuildTag = "chinese-poetry-api:local-verify"
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

$ResolvedRunner = Resolve-Runner
Write-Host "Runner: $ResolvedRunner"

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

function Invoke-Go {
    param(
        [string]$Name,
        [string[]]$GoArgs
    )

    if ($ResolvedRunner -eq "docker") {
        $dockerArgs = @(
            "run", "--rm",
            "-v", "${RepoRoot}:/app",
            "-v", "poetry-go-mod-cache:/go/pkg/mod",
            "-v", "poetry-go-build-cache:/root/.cache/go-build",
            "-w", "/app",
            $DockerImage,
            "go"
        ) + $GoArgs
        Invoke-External -Name $Name -Exe "docker" -CommandArgs $dockerArgs
        return
    }

    Invoke-External -Name $Name -Exe "go" -CommandArgs $GoArgs
}

if (-not $SkipBuild) {
    Invoke-Go -Name "go build ./..." -GoArgs @("build", "./...")
}

Invoke-Go -Name "go test ./..." -GoArgs @("test", "./...")

if (-not $SkipFTS) {
    Invoke-Go -Name "go test -tags sqlite_fts5 ./..." -GoArgs @("test", "-tags", "sqlite_fts5", "./...")
}

if ($DockerBuild) {
    if (-not (Test-DockerReady)) {
        throw "Docker is not ready; cannot run docker build."
    }
    Invoke-External -Name "docker build" -Exe "docker" -CommandArgs @("build", "-t", $DockerBuildTag, ".")
}

Write-Host ""
Write-Host "Local verification passed."
