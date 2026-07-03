param(
    [string]$Checklist = "docs/final-acceptance-checklist.md",
    [string]$Out = "data/acceptance/final-acceptance-audit.json",
    [switch]$RequireDone,
    [switch]$RequireCommercialValidation
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

function Read-JsonFile {
    param([string]$PathValue)
    $local = Get-LocalPath $PathValue
    if (-not (Test-Path -LiteralPath $local)) {
        return $null
    }
    return (Get-Content -LiteralPath $local -Raw -Encoding UTF8 | ConvertFrom-Json)
}

function Get-QuickCheckStatus {
    param([object]$Manifest)
    if ($null -eq $Manifest) {
        return ""
    }
    $quick = $Manifest.quick_check
    if ($null -eq $quick) {
        return ""
    }
    if ($quick -is [string]) {
        return $quick
    }
    if ($null -ne $quick.status) {
        return [string]$quick.status
    }
    return [string]$quick
}

function Get-NewestFile {
    param([string]$Pattern)
    $files = Get-ChildItem -Path $Pattern -File -ErrorAction SilentlyContinue | Sort-Object LastWriteTimeUtc -Descending
    if ($files.Count -lt 1) {
        return $null
    }
    return $files[0]
}

$checklistPath = Get-LocalPath $Checklist
$issues = [System.Collections.Generic.List[string]]::new()
$todoRows = [System.Collections.Generic.List[object]]::new()
$doneRows = [System.Collections.Generic.List[object]]::new()

if (-not (Test-Path -LiteralPath $checklistPath)) {
    $issues.Add("checklist not found: $Checklist") | Out-Null
    $lines = @()
} else {
    $lines = Get-Content -LiteralPath $checklistPath -Encoding UTF8
}

for ($i = 0; $i -lt $lines.Count; $i++) {
    $line = [string]$lines[$i]
    if ($line -match '^\|\s*(?<item>[^|]+?)\s*\|\s*(?<status>DONE|TODO)\s*\|(?<rest>.*)\|$') {
        $row = [ordered]@{
            line = $i + 1
            item = $Matches.item.Trim()
            status = $Matches.status.Trim()
            raw = $line
        }
        if ($row.status -eq "TODO") {
            $todoRows.Add($row) | Out-Null
        } else {
            $doneRows.Add($row) | Out-Null
        }
    }
}

if ($todoRows.Count -gt 0) {
    $issues.Add("checklist still has TODO rows: $($todoRows.Count)") | Out-Null
}

$sourceBackupManifestPath = "backups/drill-full/poetry-20260630T121025.737906955Z.manifest.json"
$restoredBackupManifestPath = "backups/drill-full/poetry-restored-20260630T121235.847186704Z.manifest.json"
$sourceBackupManifest = Read-JsonFile $sourceBackupManifestPath
$restoredBackupManifest = Read-JsonFile $restoredBackupManifestPath
$sourceQuickCheck = Get-QuickCheckStatus $sourceBackupManifest
$restoredQuickCheck = Get-QuickCheckStatus $restoredBackupManifest
if ($sourceQuickCheck -ne "ok") {
    $issues.Add("source backup quick_check is not ok") | Out-Null
}
if ($restoredQuickCheck -ne "ok") {
    $issues.Add("restored backup quick_check is not ok") | Out-Null
}

$goldenCsvPath = Get-LocalPath "data/enrichment/golden-sample-1000.prefilled-review-66.csv"
$goldenCsvRows = 0
if (Test-Path -LiteralPath $goldenCsvPath) {
    $goldenCsvRows = @((Import-Csv -LiteralPath $goldenCsvPath -Encoding UTF8)).Count
} else {
    $issues.Add("golden review CSV is missing") | Out-Null
}

$currentGoldenAuditPath = "data/enrichment/golden-sample-1000.reviewed.annotation-audit.json"
$currentGoldenAudit = Read-JsonFile $currentGoldenAuditPath
if ($null -eq $currentGoldenAudit) {
    $currentGoldenAuditPath = "data/enrichment/golden-sample-1000.prefilled.annotation-audit.json"
    $currentGoldenAudit = Read-JsonFile $currentGoldenAuditPath
}
$finalGoldenAuditFile = Get-NewestFile "data/enrichment/golden-sample-1000*.annotation-audit.json"
$finalGoldenAudit = $null
if ($null -ne $finalGoldenAuditFile) {
    $finalGoldenAudit = Read-JsonFile $finalGoldenAuditFile.FullName
}

$validateSmoke = Read-JsonFile "data/enrichment/validate-ai-rules-script-smoke-5.json"
$qualityGateSmoke = Read-JsonFile "data/enrichment/quality-gate-ai-rules-script-smoke-5.json"
if ($null -eq $validateSmoke -or $validateSmoke.valid -ne $true) {
    $issues.Add("local AI candidate validate smoke is missing or invalid") | Out-Null
}
if ($null -eq $qualityGateSmoke -or [int]$qualityGateSmoke.error_count -ne 0) {
    $issues.Add("local AI candidate quality gate smoke is missing or has errors") | Out-Null
}

$realQanloValidateFile = Get-NewestFile "data/enrichment/validate-*qanlo*.json"
$realQanloQualityGateFile = Get-NewestFile "data/enrichment/quality-gate-*qanlo*.json"
$realQanloReviewAuditFile = Get-NewestFile "data/enrichment/review-audit-*qanlo*.json"
$realQanloReviewReportFile = Get-NewestFile "data/enrichment/review-report-*qanlo*.json"
$realQanloValidate = $null
$realQanloQualityGate = $null
$realQanloReviewAudit = $null
$realQanloReviewReport = $null
if ($null -ne $realQanloValidateFile) {
    $realQanloValidate = Read-JsonFile $realQanloValidateFile.FullName
}
if ($null -ne $realQanloQualityGateFile) {
    $realQanloQualityGate = Read-JsonFile $realQanloQualityGateFile.FullName
}
if ($null -ne $realQanloReviewAuditFile) {
    $realQanloReviewAudit = Read-JsonFile $realQanloReviewAuditFile.FullName
}
if ($null -ne $realQanloReviewReportFile) {
    $realQanloReviewReport = Read-JsonFile $realQanloReviewReportFile.FullName
}

$realQanloReviewUnsupportedCount = 0
if ($null -ne $realQanloReviewAudit -and $null -ne $realQanloReviewAudit.PSObject.Properties["unsupported_actions"]) {
    $realQanloReviewUnsupportedCount = @($realQanloReviewAudit.unsupported_actions).Count
}
$realQanloReviewAuditReady = $null -ne $realQanloReviewAudit -and
    [int]$realQanloReviewAudit.reviewed_count -gt 0 -and
    [int]$realQanloReviewAudit.pending_count -eq 0 -and
    $realQanloReviewUnsupportedCount -eq 0 -and
    [double]$realQanloReviewAudit.pass_rate -ge 0.9
$realQanloReviewReportReady = $null -ne $realQanloReviewReport -and
    [int]$realQanloReviewReport.reviewed_count -gt 0 -and
    [int]$realQanloReviewReport.pending_count -eq 0 -and
    [int]$realQanloReviewReport.accepted_count -gt 0

$commercialAudit = Read-JsonFile "data/commercial/trials.audit.json"
$commercialExampleAudit = Read-JsonFile "data/commercial/trials.example.audit.json"

if ($todoRows.Count -eq 0) {
    if ($null -eq $finalGoldenAudit -or $finalGoldenAudit.ready_for_evaluation -ne $true) {
        $issues.Add("all checklist rows are DONE but final golden audit is not ready") | Out-Null
    }
    if ($null -eq $realQanloValidate -or $realQanloValidate.valid -ne $true) {
        $issues.Add("all checklist rows are DONE but real Qanlo validate report is missing or invalid") | Out-Null
    }
    if ($null -eq $realQanloQualityGate -or [int]$realQanloQualityGate.error_count -ne 0) {
        $issues.Add("all checklist rows are DONE but real Qanlo quality gate report is missing or has errors") | Out-Null
    }
    if (-not $realQanloReviewAuditReady) {
        $issues.Add("all checklist rows are DONE but real Qanlo manual review audit is missing or not ready") | Out-Null
    }
    if (-not $realQanloReviewReportReady) {
        $issues.Add("all checklist rows are DONE but real Qanlo review report is missing or not published") | Out-Null
    }
    if ($RequireCommercialValidation -and ($null -eq $commercialAudit -or $commercialAudit.ready_for_final_acceptance -ne $true)) {
        $issues.Add("all checklist rows are DONE but real commercial validation audit is not ready") | Out-Null
    }
}

$readyForStop = $todoRows.Count -eq 0 -and $issues.Count -eq 0

$report = [ordered]@{
    checklist = $Checklist
    done_count = $doneRows.Count
    todo_count = $todoRows.Count
    ready_for_stop = $readyForStop
    todo_rows = @($todoRows)
    issue_count = $issues.Count
    issues = @($issues)
    checks = [ordered]@{
        source_backup_manifest = $sourceBackupManifestPath
        source_backup_quick_check = $sourceQuickCheck
        restored_backup_manifest = $restoredBackupManifestPath
        restored_backup_quick_check = $restoredQuickCheck
        golden_review_csv = "data/enrichment/golden-sample-1000.prefilled-review-66.csv"
        golden_review_csv_rows = $goldenCsvRows
        current_golden_audit = $currentGoldenAuditPath
        current_golden_audit_complete_count = $(if ($null -eq $currentGoldenAudit) { $null } else { $currentGoldenAudit.complete_count })
        current_golden_audit_ready = $(if ($null -eq $currentGoldenAudit) { $null } else { $currentGoldenAudit.ready_for_evaluation })
        local_validate_smoke = "data/enrichment/validate-ai-rules-script-smoke-5.json"
        local_validate_smoke_valid = $(if ($null -eq $validateSmoke) { $null } else { $validateSmoke.valid })
        local_quality_gate_smoke = "data/enrichment/quality-gate-ai-rules-script-smoke-5.json"
        local_quality_gate_smoke_errors = $(if ($null -eq $qualityGateSmoke) { $null } else { $qualityGateSmoke.error_count })
        real_qanlo_validate_report = $(if ($null -eq $realQanloValidateFile) { $null } else { $realQanloValidateFile.FullName })
        real_qanlo_quality_gate_report = $(if ($null -eq $realQanloQualityGateFile) { $null } else { $realQanloQualityGateFile.FullName })
        real_qanlo_review_audit = $(if ($null -eq $realQanloReviewAuditFile) { $null } else { $realQanloReviewAuditFile.FullName })
        real_qanlo_review_audit_ready = $realQanloReviewAuditReady
        real_qanlo_review_report = $(if ($null -eq $realQanloReviewReportFile) { $null } else { $realQanloReviewReportFile.FullName })
        real_qanlo_review_report_ready = $realQanloReviewReportReady
        commercial_audit = "data/commercial/trials.audit.json"
        commercial_required = [bool]$RequireCommercialValidation
        commercial_ready = $(if ($null -eq $commercialAudit) { $null } else { $commercialAudit.ready_for_final_acceptance })
        commercial_example_ready = $(if ($null -eq $commercialExampleAudit) { $null } else { $commercialExampleAudit.ready_for_final_acceptance })
    }
    stop_rule = "Only stop when ready_for_stop is true. With -RequireDone this script exits non-zero until all checklist rows are DONE and final evidence is ready. Real commercial validation is optional unless -RequireCommercialValidation is set."
}

$outPath = Get-LocalPath $Out
$outDir = Split-Path -Parent $outPath
if (-not [string]::IsNullOrWhiteSpace($outDir)) {
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
}
$json = $report | ConvertTo-Json -Depth 12
$json | Set-Content -LiteralPath $outPath -Encoding UTF8
$json

if ($RequireDone -and -not $readyForStop) {
    throw "Final acceptance is not ready. See $Out"
}
