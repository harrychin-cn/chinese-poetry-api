param(
    [Parameter(Mandatory = $true)]
    [string]$CustomerProject,
    [Parameter(Mandatory = $true)]
    [string]$CustomerType,
    [Parameter(Mandatory = $true)]
    [string]$Scenario,
    [Parameter(Mandatory = $true)]
    [string]$ApiKeyId,
    [string]$Tier = "free",
    [string]$StartDate = (Get-Date -Format "yyyy-MM-dd"),
    [int]$SevenDayCalls = 0,
    [switch]$RealCallCompleted,
    [string[]]$TopQueries = @(),
    [string]$MissingData = "",
    [ValidateSet("none", "paid_intent", "recharge", "paid", "intent", "budget")]
    [string]$PaidSignal = "none",
    [decimal]$PaidAmount = 0,
    [string]$PaidIntentBudget = "",
    [string]$NextStep = "",
    [string]$Out = "data/commercial/trials.jsonl",
    [string]$AuditOut = "data/commercial/trials.audit.json",
    [switch]$RequireFinal,
    [switch]$AllowPlaceholder
)

$ErrorActionPreference = "Stop"

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
Set-Location $RepoRoot

function Get-LocalPath {
    param([string]$PathValue)
    if ([System.IO.Path]::IsPathRooted($PathValue)) {
        return $PathValue
    }
    return (Join-Path $RepoRoot $PathValue)
}

function Assert-RealValue {
    param(
        [string]$Name,
        [string]$Value
    )
    $trimmed = ([string]$Value).Trim()
    if ([string]::IsNullOrWhiteSpace($trimmed)) {
        throw "$Name cannot be empty."
    }
    if (-not $AllowPlaceholder) {
        $lower = $trimmed.ToLowerInvariant()
        if ($lower -match '^(待填|todo|placeholder|example|示例|test)$') {
            throw "$Name looks like a placeholder. Replace it with real trial evidence, or pass -AllowPlaceholder only for local smoke tests."
        }
    }
}

Assert-RealValue -Name "CustomerProject" -Value $CustomerProject
Assert-RealValue -Name "CustomerType" -Value $CustomerType
Assert-RealValue -Name "Scenario" -Value $Scenario
Assert-RealValue -Name "ApiKeyId" -Value $ApiKeyId

if ($SevenDayCalls -lt 0) {
    throw "SevenDayCalls cannot be negative."
}
if ($PaidAmount -lt 0) {
    throw "PaidAmount cannot be negative."
}

$record = [ordered]@{
    customer_project = $CustomerProject.Trim()
    customer_type = $CustomerType.Trim()
    scenario = $Scenario.Trim()
    api_key_id = $ApiKeyId.Trim()
    tier = $Tier.Trim()
    start_date = $StartDate.Trim()
    seven_day_calls = $SevenDayCalls
    real_call_completed = [bool]$RealCallCompleted
    top_queries = @($TopQueries | Where-Object { -not [string]::IsNullOrWhiteSpace($_) } | ForEach-Object { $_.Trim() })
    missing_data = $MissingData.Trim()
    paid_signal = $PaidSignal
    paid_amount = $PaidAmount
    paid_intent_budget = $PaidIntentBudget.Trim()
    next_step = $NextStep.Trim()
}

$outPath = Get-LocalPath $Out
$outDir = Split-Path -Parent $outPath
if (-not [string]::IsNullOrWhiteSpace($outDir)) {
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
}

($record | ConvertTo-Json -Compress -Depth 8) | Add-Content -LiteralPath $outPath -Encoding UTF8

$auditArgs = @(
    "-RecordFile", $Out,
    "-Out", $AuditOut
)
if ($RequireFinal) {
    $auditArgs += "-RequireFinal"
}

Write-Host ""
Write-Host ("> powershell -NoProfile -ExecutionPolicy Bypass -File scripts/commercial_validation_audit.ps1 " + ($auditArgs -join " "))
& powershell -NoProfile -ExecutionPolicy Bypass -File (Get-LocalPath "scripts/commercial_validation_audit.ps1") @auditArgs
if ($LASTEXITCODE -ne 0) {
    throw "commercial validation audit failed with exit code $LASTEXITCODE"
}

[ordered]@{
    appended = $Out
    audit = $AuditOut
    customer_project = $record.customer_project
    api_key_id = $record.api_key_id
    paid_signal = $record.paid_signal
} | ConvertTo-Json -Depth 4
