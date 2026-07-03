[CmdletBinding()]
param(
    [Alias("Input")]
    [string]$GoldenInput = "data/enrichment/golden-sample-1000.reviewed.jsonl",
    [string]$OutDir = "data/enrichment/golden-review-suggestions",
    [int]$BatchSize = 100,
    [switch]$RequireSuggestions
)

$ErrorActionPreference = "Stop"

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
Set-Location $RepoRoot

$scriptPath = Join-Path $PSScriptRoot "golden_review_suggest.py"
if (-not (Test-Path -LiteralPath $scriptPath)) {
    throw "golden_review_suggest.py not found: $scriptPath"
}

$argsList = @(
    $scriptPath,
    "--input", $GoldenInput,
    "--out-dir", $OutDir,
    "--batch-size", "$BatchSize"
)

Write-Host ("> python " + ($argsList -join " "))
& python @argsList
if ($LASTEXITCODE -ne 0) {
    throw "golden review suggestion export failed with exit code $LASTEXITCODE"
}

$reportPath = Join-Path $RepoRoot (Join-Path $OutDir "suggestion-report.json")
if ($RequireSuggestions) {
    $report = Get-Content -LiteralPath $reportPath -Raw -Encoding UTF8 | ConvertFrom-Json
    if ([int]$report.suggested_count -lt 1) {
        throw "No machine suggestions were generated."
    }
}
