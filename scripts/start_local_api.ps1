[CmdletBinding()]
param(
    [int]$Port = 1279,
    [string]$AdminToken = "local-admin-token",
    [string]$ImageName = "chinese-poetry-api:local",
    [string]$ContainerName = "poetry-api-local",
    [switch]$Rebuild,
    [switch]$SkipSmoke
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
Set-Location $RepoRoot

if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    throw "Docker is required. Start Docker Desktop first, then rerun this script."
}

$oldErrorActionPreference = $ErrorActionPreference
$ErrorActionPreference = "Continue"
try {
    & docker info 1>$null 2>$null
    $dockerInfoExitCode = $LASTEXITCODE
} finally {
    $ErrorActionPreference = $oldErrorActionPreference
}
if ($dockerInfoExitCode -ne 0) {
    throw "Docker is installed but not running. Open Docker Desktop, wait until it says 'Docker Desktop is running', then rerun this script."
}

if (-not (Test-Path -LiteralPath (Join-Path $RepoRoot "data\poetry.db"))) {
    throw "Missing data\poetry.db. The local API cannot start without the database."
}

if ([string]::IsNullOrWhiteSpace($AdminToken)) {
    throw "AdminToken cannot be empty. For local testing you can use: -AdminToken local-admin-token"
}

$imageExists = $false
$oldErrorActionPreference = $ErrorActionPreference
$ErrorActionPreference = "Continue"
try {
    & docker image inspect $ImageName 1>$null 2>$null
    $imageInspectExitCode = $LASTEXITCODE
} finally {
    $ErrorActionPreference = $oldErrorActionPreference
}
if ($imageInspectExitCode -eq 0) {
    $imageExists = $true
}

if ($Rebuild -or -not $imageExists) {
    Write-Host "Building local Docker image: $ImageName"
    & docker build -t $ImageName .
    if ($LASTEXITCODE -ne 0) {
        throw "docker build failed with exit code $LASTEXITCODE"
    }
}

$existing = ((@(& docker ps -aq -f "name=^/$ContainerName$")) -join "").Trim()
if (-not [string]::IsNullOrWhiteSpace($existing)) {
    Write-Host "Removing existing container: $ContainerName"
    & docker rm -f $ContainerName | Out-Null
}

$dataPath = (Resolve-Path (Join-Path $RepoRoot "data")).Path
$portMap = "$Port`:$Port"
$volumeMap = "${dataPath}:/app/data"
$qanloEnvArgs = @()
@(
    "QANLO_AGENT_BASE_URL",
    "QANLO_OPENAI_BASE_URL",
    "QANLO_RECHARGE_URL",
    "QANLO_AGENT_APP_ID",
    "QANLO_AGENT_NAME",
    "QANLO_AGENT_MODEL",
    "QANLO_RETURN_URL",
    "QANLO_AGENT_RETURN_URL",
    "QANLO_CALLBACK_SECRET"
) | ForEach-Object {
    $value = [Environment]::GetEnvironmentVariable($_)
    if (-not [string]::IsNullOrWhiteSpace($value)) {
        $qanloEnvArgs += @("-e", "$_=$value")
    }
}

Write-Host "Starting local API container: $ContainerName"
$containerId = ((@(& docker run `
    -d `
    --name $ContainerName `
    -p $portMap `
    -v $volumeMap `
    -e "PORT=$Port" `
    -e "API_AUTH_ENABLED=true" `
    -e "API_ADMIN_TOKEN=$AdminToken" `
    -e "RATE_LIMIT_ENABLED=false" `
    -e "API_KEY_RATE_LIMIT_RPS=20" `
    -e "API_KEY_RATE_LIMIT_BURST=50" `
    @qanloEnvArgs `
    --entrypoint ./server `
    $ImageName)) -join "").Trim()
if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($containerId)) {
    throw "docker run failed with exit code $LASTEXITCODE"
}

$baseUrl = "http://localhost:$Port"
$healthUrl = "$baseUrl/api/v1/health"
$ready = $false
for ($i = 1; $i -le 60; $i++) {
    try {
        $resp = Invoke-WebRequest -Uri $healthUrl -UseBasicParsing -TimeoutSec 2
        if ($resp.StatusCode -eq 200 -and $resp.Content -match "healthy") {
            $ready = $true
            break
        }
    } catch {
        Start-Sleep -Seconds 1
    }
}

if (-not $ready) {
    Write-Host "Container logs:"
    & docker logs $ContainerName
    throw "local API did not become healthy in 60 seconds"
}

$smokePassed = $null
if (-not $SkipSmoke) {
    Write-Host ""
    Write-Host "Running commercial smoke test..."
    & powershell -NoProfile -ExecutionPolicy Bypass -File (Join-Path $RepoRoot "scripts\smoke_commercial.ps1") -BaseUrl $baseUrl -AdminToken $AdminToken -TimeoutSec 60
    if ($LASTEXITCODE -ne 0) {
        $smokePassed = $false
        throw "commercial smoke failed with exit code $LASTEXITCODE"
    }
    $smokePassed = $true
}

[ordered]@{
    status = "running"
    base_url = $baseUrl
    console = "$baseUrl/console"
    docs = "$baseUrl/docs"
    pricing = "$baseUrl/pricing"
    health = $healthUrl
    admin_token = $AdminToken
    container = $ContainerName
    image = $ImageName
    smoke_passed = $smokePassed
    stop_command = "docker rm -f $ContainerName"
} | ConvertTo-Json -Depth 4
