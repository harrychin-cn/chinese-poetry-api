[CmdletBinding()]
param(
    [string]$BaseUrl = "http://localhost:1279",
    [string]$ApiKeyId = "",
    [string[]]$Scenarios = @(
        "create api key",
        "poem query",
        "knowledge recall",
        "tags and scenarios",
        "usage statistics",
        "feedback"
    ),
    [ValidateSet("pass", "partial", "fail")]
    [string]$Result = "pass",
    [string]$Notes = "",
    [string]$Out = "data/commercial/founder-self-test.jsonl"
)

$ErrorActionPreference = "Stop"

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
Set-Location $RepoRoot

$outPath = if ([System.IO.Path]::IsPathRooted($Out)) { $Out } else { Join-Path $RepoRoot $Out }
$outDir = Split-Path -Parent $outPath
if (-not [string]::IsNullOrWhiteSpace($outDir)) {
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
}

$record = [ordered]@{
    record_type = "founder_self_test"
    created_at = (Get-Date).ToString("o")
    tester = "owner"
    base_url = $BaseUrl.TrimEnd("/")
    api_key_id = $ApiKeyId.Trim()
    scenarios = @($Scenarios | Where-Object { -not [string]::IsNullOrWhiteSpace($_) } | ForEach-Object { $_.Trim() })
    result = $Result
    notes = $Notes.Trim()
    counts_as_real_commercial_validation = $false
    explanation = "Founder self-test proves local product usability, but it is not external customer evidence."
}

($record | ConvertTo-Json -Compress -Depth 8) | Add-Content -LiteralPath $outPath -Encoding UTF8

[ordered]@{
    appended = $Out
    result = $Result
    counts_as_real_commercial_validation = $false
    next_gate = "external customer trials are still needed before final commercial validation"
} | ConvertTo-Json -Depth 4
