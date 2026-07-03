[CmdletBinding()]
param(
    [Alias("Input")]
    [string]$GoldenInput = "data/enrichment/golden-sample-1000.reviewed.jsonl",
    [string]$OutDir = "data/enrichment/golden-review-batches",
    [int]$BatchSize = 100,
    [ValidateSet("todo", "prefilled_review_required", "machine_suggested_review_required", "incomplete")]
    [string]$Status = "todo",
    [switch]$RequireRemaining
)

$ErrorActionPreference = "Stop"

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
Set-Location $RepoRoot

$scriptPath = Join-Path $PSScriptRoot "golden_review_batches.py"
if (-not (Test-Path -LiteralPath $scriptPath)) {
    throw "golden_review_batches.py not found: $scriptPath"
}

$argsList = @(
    $scriptPath,
    "--input", $GoldenInput,
    "--out-dir", $OutDir,
    "--batch-size", "$BatchSize",
    "--status", $Status
)

Write-Host ("> python " + ($argsList -join " "))
& python @argsList
if ($LASTEXITCODE -ne 0) {
    throw "golden review batch export failed with exit code $LASTEXITCODE"
}

$reportPath = Join-Path $RepoRoot (Join-Path $OutDir "batch-report.json")
if ($RequireRemaining) {
    $report = Get-Content -LiteralPath $reportPath -Raw -Encoding UTF8 | ConvertFrom-Json
    if ([int]$report.exported_count -lt 1) {
        throw "No remaining golden review rows were exported."
    }
}
