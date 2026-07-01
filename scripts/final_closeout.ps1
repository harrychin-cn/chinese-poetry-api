param(
    [ValidateSet("auto", "local", "docker")]
    [string]$Runner = "auto",
    [string]$Out = "data/acceptance/final-closeout-report.json",
    [string]$GoldenSheet = "data/enrichment/golden-sample-1000.prefilled-review-66.csv",
    [switch]$ApplyGolden,
    [int]$QanloLimit = 20,
    [string]$QanloRunId = "",
    [switch]$AllowPaidQanlo,
    [switch]$ImportQanlo,
    [string]$CommercialRecordFile = "data/commercial/trials.jsonl",
    [switch]$RequireDone
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

function Get-NewestFile {
    param([string]$Pattern)
    $files = Get-ChildItem -Path $Pattern -File -ErrorAction SilentlyContinue | Sort-Object LastWriteTimeUtc -Descending
    if ($files.Count -lt 1) {
        return $null
    }
    return $files[0]
}

function Test-QanloKeyPresent {
    return (-not [string]::IsNullOrWhiteSpace($env:QANLO_AGENT_KEY)) -or
        (-not [string]::IsNullOrWhiteSpace($env:QANLO_API_KEY))
}

function Add-Step {
    param(
        [System.Collections.Generic.List[object]]$Steps,
        [string]$Name,
        [ValidateSet("done", "pending", "skipped", "failed")]
        [string]$Status,
        [string]$Detail,
        [string[]]$Evidence = @()
    )
    $Steps.Add([ordered]@{
        name = $Name
        status = $Status
        detail = $Detail
        evidence = @($Evidence)
    }) | Out-Null
}

function Add-Blocker {
    param(
        [System.Collections.Generic.List[string]]$Blockers,
        [string]$Text
    )
    if (-not [string]::IsNullOrWhiteSpace($Text) -and -not $Blockers.Contains($Text)) {
        $Blockers.Add($Text) | Out-Null
    }
}

function Invoke-ProjectScript {
    param(
        [string]$Script,
        [string[]]$Arguments = @()
    )
    $scriptPath = Get-LocalPath $Script
    if (-not (Test-Path -LiteralPath $scriptPath)) {
        throw "script not found: $Script"
    }
    Write-Host ""
    Write-Host ("> powershell -NoProfile -ExecutionPolicy Bypass -File " + $Script + " " + ($Arguments -join " "))
    $output = & powershell -NoProfile -ExecutionPolicy Bypass -File $scriptPath @Arguments 2>&1
    $exitCode = $LASTEXITCODE
    if ($exitCode -ne 0) {
        $message = (($output | Out-String).Trim())
        if ($message.Length -gt 600) {
            $message = $message.Substring(0, 600) + "..."
        }
        throw "$Script failed with exit code $exitCode. $message"
    }
    return $output
}

$startedAt = (Get-Date).ToString("o")
$steps = [System.Collections.Generic.List[object]]::new()
$blockers = [System.Collections.Generic.List[string]]::new()

# 1. 黄金评测集人工复核收口：只在 CSV 全部 done 后才允许合并。
try {
    $goldenArgs = @("-Runner", $Runner, "-Sheet", $GoldenSheet)
    if ($ApplyGolden) {
        $goldenArgs += "-Apply"
    }
    Invoke-ProjectScript -Script "scripts/golden_review_closeout.ps1" -Arguments $goldenArgs
    $sheetAudit = Read-JsonFile "data/enrichment/golden-sample-1000.prefilled-review-66.audit.json"
    $goldenReviewedAudit = Read-JsonFile "data/enrichment/golden-sample-1000.reviewed.annotation-audit.json"
    $sheetComplete = $(if ($null -eq $sheetAudit) { 0 } else { [int]$sheetAudit.complete_count })
    $reviewedComplete = $(if ($null -eq $goldenReviewedAudit) { 0 } else { [int]$goldenReviewedAudit.complete_count })
    if ($null -ne $goldenReviewedAudit -and $goldenReviewedAudit.ready_for_evaluation -eq $true) {
        Add-Step -Steps $steps -Name "golden_review_closeout" -Status "done" -Detail "黄金评测集人工复核已全部合并并通过审计。" -Evidence @("data/enrichment/golden-sample-1000.reviewed.annotation-audit.json")
    } elseif ($null -ne $sheetAudit -and $sheetAudit.ready_for_merge -eq $true -and $sheetComplete -gt 0 -and $reviewedComplete -ge $sheetComplete) {
        Add-Step -Steps $steps -Name "golden_review_closeout" -Status "done" -Detail "黄金评测集当前人工复核批次已合并；当前完成 $reviewedComplete/1000，剩余补齐由最终验收清单跟踪。" -Evidence @("data/enrichment/golden-sample-1000.prefilled-review-66.audit.json", "data/enrichment/golden-sample-1000.reviewed.annotation-audit.json")
    } else {
        Add-Step -Steps $steps -Name "golden_review_closeout" -Status "pending" -Detail "复核表已达到可合并条件，但还没有生成最终 reviewed 审计；需要带 -ApplyGolden 写出结果。" -Evidence @("data/enrichment/golden-sample-1000.prefilled-review-66.audit.json")
        Add-Blocker -Blockers $blockers -Text "黄金评测集复核表需要合并成 reviewed 版本。"
    }
} catch {
    $sheetAudit = Read-JsonFile "data/enrichment/golden-sample-1000.prefilled-review-66.audit.json"
    $complete = $(if ($null -eq $sheetAudit) { "unknown" } else { "$($sheetAudit.complete_count)/$($sheetAudit.total)" })
    Add-Step -Steps $steps -Name "golden_review_closeout" -Status "pending" -Detail "黄金评测集人工复核未完成；当前完成数 $complete。" -Evidence @($GoldenSheet, "data/enrichment/golden-sample-1000.prefilled-review-66.audit.json")
    Add-Blocker -Blockers $blockers -Text "人工复核黄金评测集 CSV，并把确认通过的 annotation_status 改为 done。"
}

# 2. 真实 Qanlo 小样本：默认不发起付费调用；已有真实报告则直接识别。
try {
    $qanloValidateFile = Get-NewestFile "data/enrichment/validate-*qanlo*.json"
    $qanloQualityGateFile = Get-NewestFile "data/enrichment/quality-gate-*qanlo*.json"
    $qanloValidate = $(if ($null -eq $qanloValidateFile) { $null } else { Read-JsonFile $qanloValidateFile.FullName })
    $qanloQualityGate = $(if ($null -eq $qanloQualityGateFile) { $null } else { Read-JsonFile $qanloQualityGateFile.FullName })
    $qanloReady = ($null -ne $qanloValidate -and $qanloValidate.valid -eq $true -and
        $null -ne $qanloQualityGate -and [int]$qanloQualityGate.error_count -eq 0)

    if ($qanloReady) {
        Add-Step -Steps $steps -Name "qanlo_ai_candidate_trial" -Status "done" -Detail "已发现真实 Qanlo validate / quality-gate 报告且无 error。" -Evidence @($qanloValidateFile.FullName, $qanloQualityGateFile.FullName)
    } elseif ($AllowPaidQanlo) {
        if (-not (Test-QanloKeyPresent)) {
            throw "QANLO_AGENT_KEY/QANLO_API_KEY is missing"
        }
        $qanloArgs = @("-Provider", "qanlo", "-Limit", "$QanloLimit", "-Runner", $Runner)
        if (-not [string]::IsNullOrWhiteSpace($QanloRunId)) {
            $qanloArgs += @("-RunId", $QanloRunId)
        }
        if ($ImportQanlo) {
            $qanloArgs += "-Import"
        }
        Invoke-ProjectScript -Script "scripts/ai_candidate_trial.ps1" -Arguments $qanloArgs
        Add-Step -Steps $steps -Name "qanlo_ai_candidate_trial" -Status "done" -Detail "真实 Qanlo 小样本链路已执行。" -Evidence @("data/enrichment/candidates-*qanlo*.jsonl", "data/enrichment/validate-*qanlo*.json", "data/enrichment/quality-gate-*qanlo*.json")
    } else {
        Add-Step -Steps $steps -Name "qanlo_ai_candidate_trial" -Status "pending" -Detail "未发现真实 Qanlo 报告；本次未带 -AllowPaidQanlo，所以没有发起可能计费的外部调用。" -Evidence @("data/enrichment/validate-*qanlo*.json", "data/enrichment/quality-gate-*qanlo*.json")
        Add-Blocker -Blockers $blockers -Text "设置 QANLO_AGENT_KEY 或 QANLO_API_KEY，并用 -AllowPaidQanlo 跑真实 Qanlo 小样本。"
    }
} catch {
    Add-Step -Steps $steps -Name "qanlo_ai_candidate_trial" -Status "pending" -Detail "真实 Qanlo 小样本未完成：$($_.Exception.Message)" -Evidence @("scripts/ai_candidate_trial.ps1")
    Add-Blocker -Blockers $blockers -Text "补齐 Qanlo 密钥和成本确认后，再跑真实 Qanlo 小样本。"
}

# 3. AI 候选人工复核收口：只有真实 Qanlo 候选完成后才检查人工抽样写回证据。
try {
    $qanloValidateFileForReview = Get-NewestFile "data/enrichment/validate-*qanlo*.json"
    $qanloQualityGateFileForReview = Get-NewestFile "data/enrichment/quality-gate-*qanlo*.json"
    $hasRealQanloCandidate = $null -ne $qanloValidateFileForReview -and $null -ne $qanloQualityGateFileForReview

    $aiReviewAuditFile = Get-NewestFile "data/enrichment/review-audit-*qanlo*.json"
    $aiReviewReportFile = Get-NewestFile "data/enrichment/review-report-*qanlo*.json"
    $aiReviewAudit = $(if ($null -eq $aiReviewAuditFile) { $null } else { Read-JsonFile $aiReviewAuditFile.FullName })
    $aiReviewReport = $(if ($null -eq $aiReviewReportFile) { $null } else { Read-JsonFile $aiReviewReportFile.FullName })

    $unsupportedCount = 0
    if ($null -ne $aiReviewAudit -and $null -ne $aiReviewAudit.PSObject.Properties["unsupported_actions"]) {
        $unsupportedCount = @($aiReviewAudit.unsupported_actions).Count
    }

    $reviewAuditReady = $null -ne $aiReviewAudit -and
        [int]$aiReviewAudit.reviewed_count -gt 0 -and
        [int]$aiReviewAudit.pending_count -eq 0 -and
        $unsupportedCount -eq 0 -and
        [double]$aiReviewAudit.pass_rate -ge 0.9

    $reviewReportReady = $null -ne $aiReviewReport -and
        [int]$aiReviewReport.reviewed_count -gt 0 -and
        [int]$aiReviewReport.pending_count -eq 0 -and
        [int]$aiReviewReport.accepted_count -gt 0

    if ($reviewAuditReady -and $reviewReportReady) {
        Add-Step -Steps $steps -Name "ai_review_closeout" -Status "done" -Detail "真实 AI 候选人工抽样已审计并写回发布队列。" -Evidence @($aiReviewAuditFile.FullName, $aiReviewReportFile.FullName)
    } elseif ($hasRealQanloCandidate) {
        Add-Step -Steps $steps -Name "ai_review_closeout" -Status "pending" -Detail "真实 Qanlo 候选已存在，但人工抽样写回证据还不完整。" -Evidence @("scripts/ai_review_closeout.ps1", "data/enrichment/review-audit-*qanlo*.json", "data/enrichment/review-report-*qanlo*.json")
        Add-Blocker -Blockers $blockers -Text "真实 Qanlo 候选通过后，用 scripts/ai_review_closeout.ps1 完成人工抽样审计和写回。"
    } else {
        Add-Step -Steps $steps -Name "ai_review_closeout" -Status "pending" -Detail "等待真实 Qanlo 候选小样本完成后再做人工抽样写回。" -Evidence @("scripts/ai_review_closeout.ps1")
    }
} catch {
    Add-Step -Steps $steps -Name "ai_review_closeout" -Status "pending" -Detail "AI 候选人工复核收口检查未完成：$($_.Exception.Message)" -Evidence @("scripts/ai_review_closeout.ps1")
    Add-Blocker -Blockers $blockers -Text "修复 AI 候选人工复核收口检查。"
}

# 4. 商业验证记录审计：真实 trials.jsonl 不存在或未达标时只记录缺口。
try {
    $commercialLocal = Get-LocalPath $CommercialRecordFile
    if (-not (Test-Path -LiteralPath $commercialLocal)) {
        throw "real commercial trial file not found: $CommercialRecordFile"
    }
    Invoke-ProjectScript -Script "scripts/commercial_validation_audit.ps1" -Arguments @("-RecordFile", $CommercialRecordFile)
    $commercialAudit = Read-JsonFile "data/commercial/trials.audit.json"
    if ($null -ne $commercialAudit -and $commercialAudit.ready_for_final_acceptance -eq $true) {
        Add-Step -Steps $steps -Name "commercial_validation" -Status "done" -Detail "真实商业试用记录已达到最终验收。" -Evidence @("data/commercial/trials.audit.json")
    } else {
        $completed = $(if ($null -eq $commercialAudit) { "unknown" } else { [string]$commercialAudit.completed_trials })
        $paid = $(if ($null -eq $commercialAudit) { "unknown" } else { [string]$commercialAudit.paid_signal_count })
        Add-Step -Steps $steps -Name "commercial_validation" -Status "pending" -Detail "真实商业试用记录未达标；completed_trials=$completed，paid_signal_count=$paid。" -Evidence @($CommercialRecordFile, "data/commercial/trials.audit.json")
        Add-Blocker -Blockers $blockers -Text "补齐 3-5 个真实试用记录，并至少有 1 个充值或明确付费意向。"
    }
} catch {
    Add-Step -Steps $steps -Name "commercial_validation" -Status "pending" -Detail "真实商业验证未完成：$($_.Exception.Message)" -Evidence @($CommercialRecordFile)
    Add-Blocker -Blockers $blockers -Text "把真实试用记录写入 data/commercial/trials.jsonl。"
}

# 5. 最终机器验收：始终生成最终审计报告。
try {
    Invoke-ProjectScript -Script "scripts/final_acceptance_audit.ps1" -Arguments @("-Out", "data/acceptance/final-acceptance-audit.json")
    $finalAudit = Read-JsonFile "data/acceptance/final-acceptance-audit.json"
    if ($null -ne $finalAudit -and $finalAudit.ready_for_stop -eq $true) {
        Add-Step -Steps $steps -Name "final_acceptance_audit" -Status "done" -Detail "最终验收 ready_for_stop=true，可以停工。" -Evidence @("data/acceptance/final-acceptance-audit.json")
    } else {
        $todo = $(if ($null -eq $finalAudit) { "unknown" } else { [string]$finalAudit.todo_count })
        Add-Step -Steps $steps -Name "final_acceptance_audit" -Status "pending" -Detail "最终验收还未达到停工线；todo_count=$todo。" -Evidence @("data/acceptance/final-acceptance-audit.json")
        Add-Blocker -Blockers $blockers -Text "最终验收清单仍有 TODO，ready_for_stop 不是 true。"
    }
} catch {
    Add-Step -Steps $steps -Name "final_acceptance_audit" -Status "failed" -Detail "最终验收脚本执行失败：$($_.Exception.Message)" -Evidence @("scripts/final_acceptance_audit.ps1")
    Add-Blocker -Blockers $blockers -Text "修复 final_acceptance_audit.ps1 执行失败。"
}

$finalAuditForReport = Read-JsonFile "data/acceptance/final-acceptance-audit.json"
$readyForStop = ($null -ne $finalAuditForReport -and $finalAuditForReport.ready_for_stop -eq $true -and $blockers.Count -eq 0)

$report = [ordered]@{
    started_at = $startedAt
    finished_at = (Get-Date).ToString("o")
    ready_for_stop = $readyForStop
    steps = @($steps)
    blocker_count = $blockers.Count
    blockers = @($blockers)
    operator_guide = "docs/final-closeout-operator.md"
    final_acceptance_audit = "data/acceptance/final-acceptance-audit.json"
    stop_rule = "Stop only when ready_for_stop is true. This script does not fake human review, Qanlo paid calls, or real commercial evidence."
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
    throw "Final closeout is not ready. See $Out"
}

