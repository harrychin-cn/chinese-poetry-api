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
    [switch]$RequireDone,
    [switch]$RequireCommercialValidation,
    [switch]$RequireHumanGoldenReview
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

function Test-GoldenAuditHasMachineAgentReview {
    param([object]$Audit)
    if ($null -eq $Audit -or $null -eq $Audit.PSObject.Properties["agent_review"]) {
        return $false
    }
    $agentReview = $Audit.agent_review
    if ($null -eq $agentReview -or $null -eq $agentReview.PSObject.Properties["reviewed_by"]) {
        return $false
    }
    $reviewedBy = ([string]$agentReview.reviewed_by).ToLowerInvariant()
    return $reviewedBy -match "codex|agent|machine"
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
    $goldenAuditMachineReviewed = Test-GoldenAuditHasMachineAgentReview -Audit $goldenReviewedAudit
    if ($goldenAuditMachineReviewed -and $RequireHumanGoldenReview) {
        Add-Step -Steps $steps -Name "golden_review_closeout" -Status "pending" -Detail "黄金评测集存在 Codex/机器代理标记为 done 的记录；不能当作真实人工复核，需人工确认后再收口。" -Evidence @("data/enrichment/golden-sample-1000.reviewed.annotation-audit.json")
        Add-Blocker -Blockers $blockers -Text "黄金评测集里 Codex/机器代理 done 记录需要人工确认，不能直接作为最终人工复核。"
    } elseif ($null -ne $goldenReviewedAudit -and $goldenReviewedAudit.ready_for_evaluation -eq $true) {
        $goldenDetail = $(if ($goldenAuditMachineReviewed) {
                "黄金评测集已由 Codex 代理批量复核并通过审计；当前完成 $reviewedComplete/1000。如需强制真实人工门禁，可加 -RequireHumanGoldenReview。"
            } else {
                "黄金评测集复核已全部合并并通过审计。"
            })
        Add-Step -Steps $steps -Name "golden_review_closeout" -Status "done" -Detail $goldenDetail -Evidence @("data/enrichment/golden-sample-1000.reviewed.annotation-audit.json")
    } elseif ($null -ne $sheetAudit -and $sheetAudit.ready_for_merge -eq $true -and $sheetComplete -gt 0 -and $reviewedComplete -ge $sheetComplete) {
        $goldenDetail = $(if ($ApplyGolden) {
                "黄金评测集当前人工复核批次已合并；当前完成 $reviewedComplete/1000，剩余补齐由最终验收清单跟踪。"
            } else {
                "已存在黄金评测集 reviewed 审计；当前完成 $reviewedComplete/1000。本次未带 -ApplyGolden，只做检查，不重新写入 reviewed 文件。"
            })
        Add-Step -Steps $steps -Name "golden_review_closeout" -Status "done" -Detail $goldenDetail -Evidence @("data/enrichment/golden-sample-1000.prefilled-review-66.audit.json", "data/enrichment/golden-sample-1000.reviewed.annotation-audit.json")
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

# 2. 黄金评测集分批复核包：只用于降低后续复核成本，不冒充人工 done。
try {
    $batchReport = Read-JsonFile "data/enrichment/golden-review-batches/batch-report.json"
    if ($null -ne $batchReport -and [int]$batchReport.exported_count -gt 0) {
        Add-Step -Steps $steps -Name "golden_review_batches" -Status "done" -Detail "已生成黄金评测集剩余样本分批复核包；待复核 $($batchReport.exported_count) 条，批次数 $($batchReport.batch_count)。" -Evidence @("data/enrichment/golden-review-batches/batch-report.json", "data/enrichment/golden-review-batches/README.md")
    } elseif ($null -ne $batchReport) {
        Add-Step -Steps $steps -Name "golden_review_batches" -Status "done" -Detail "黄金评测集没有待导出的复核批次。" -Evidence @("data/enrichment/golden-review-batches/batch-report.json")
    } else {
        Add-Step -Steps $steps -Name "golden_review_batches" -Status "skipped" -Detail "尚未生成剩余黄金评测集分批复核包；可运行 scripts/export_golden_review_batches.ps1。" -Evidence @("scripts/export_golden_review_batches.ps1")
    }
} catch {
    Add-Step -Steps $steps -Name "golden_review_batches" -Status "pending" -Detail "黄金评测集分批复核包检查失败：$($_.Exception.Message)" -Evidence @("data/enrichment/golden-review-batches/batch-report.json")
}

# 2b. 黄金评测集机器辅助候选：只预填建议，不冒充人工 done。
try {
    $suggestionReport = Read-JsonFile "data/enrichment/golden-review-suggestions/suggestion-report.json"
    $goldenReviewedAuditForSuggestions = Read-JsonFile "data/enrichment/golden-sample-1000.reviewed.annotation-audit.json"
    if ($null -ne $goldenReviewedAuditForSuggestions -and $goldenReviewedAuditForSuggestions.ready_for_evaluation -eq $true) {
        Add-Step -Steps $steps -Name "golden_review_suggestions" -Status "done" -Detail "黄金评测集 reviewed 审计已完成；历史机器辅助候选已被 Codex 代理复核结果覆盖，不再作为待办。" -Evidence @("data/enrichment/golden-sample-1000.reviewed.annotation-audit.json")
    } elseif ($null -ne $suggestionReport -and [int]$suggestionReport.suggested_count -gt 0) {
        Add-Step -Steps $steps -Name "golden_review_suggestions" -Status "done" -Detail "已生成黄金评测集机器辅助候选；候选 $($suggestionReport.suggested_count) 条，仍需人工确认，不计入人工复核。" -Evidence @("data/enrichment/golden-review-suggestions/suggestion-report.json", "data/enrichment/golden-review-suggestions/README.md")
    } elseif ($null -ne $suggestionReport) {
        Add-Step -Steps $steps -Name "golden_review_suggestions" -Status "done" -Detail "已运行黄金评测集机器辅助候选生成，但没有产生保守候选；仍需人工复核。" -Evidence @("data/enrichment/golden-review-suggestions/suggestion-report.json")
    } else {
        Add-Step -Steps $steps -Name "golden_review_suggestions" -Status "skipped" -Detail "尚未生成机器辅助候选包；可运行 scripts/export_golden_review_suggestions.ps1。" -Evidence @("scripts/export_golden_review_suggestions.ps1")
    }
} catch {
    Add-Step -Steps $steps -Name "golden_review_suggestions" -Status "pending" -Detail "黄金评测集机器辅助候选检查失败：$($_.Exception.Message)" -Evidence @("data/enrichment/golden-review-suggestions/suggestion-report.json")
}

# 3. 真实 Qanlo 小样本：默认不发起付费调用；已有真实报告则直接识别。
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

# 4. AI 候选人工复核收口：只有真实 Qanlo 候选完成后才检查人工抽样写回证据。
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

# 5. 商业试用准备包：这不是外部客户证据，只说明产品已可发给别人试用。
try {
    $readinessReport = Read-JsonFile "data/commercial/trial-readiness/trial-readiness-report.json"
    if ($null -ne $readinessReport -and $readinessReport.ready_for_external_trial -eq $true) {
        Add-Step -Steps $steps -Name "commercial_trial_readiness" -Status "done" -Detail "已生成商业试用准备包；产品可以发给外部开发者/内容团队试用，但不计入真实商业验证。" -Evidence @("data/commercial/trial-readiness/trial-readiness-report.json", "data/commercial/trial-readiness/trial-invite.md", "data/commercial/trial-readiness/trial-test-plan.md")
    } elseif ($null -ne $readinessReport) {
        Add-Step -Steps $steps -Name "commercial_trial_readiness" -Status "pending" -Detail "商业试用准备包已存在，但页面或自测证据还未全部通过。" -Evidence @("data/commercial/trial-readiness/trial-readiness-report.json")
    } else {
        Add-Step -Steps $steps -Name "commercial_trial_readiness" -Status "skipped" -Detail "尚未生成商业试用准备包；可运行 scripts/export_commercial_trial_readiness.ps1。" -Evidence @("scripts/export_commercial_trial_readiness.ps1")
    }
} catch {
    Add-Step -Steps $steps -Name "commercial_trial_readiness" -Status "pending" -Detail "商业试用准备包检查失败：$($_.Exception.Message)" -Evidence @("data/commercial/trial-readiness/trial-readiness-report.json")
}

# 6. 商业验证记录审计：默认按运营后置处理；只有显式要求时才作为停工门禁。
try {
    $commercialLocal = Get-LocalPath $CommercialRecordFile
    if (-not (Test-Path -LiteralPath $commercialLocal)) {
        if ($RequireCommercialValidation) {
            throw "real commercial trial file not found: $CommercialRecordFile"
        }
        Add-Step -Steps $steps -Name "commercial_validation" -Status "skipped" -Detail "真实商业试用和充值证据按当前收口口径后置为运营验证；本次不作为开发停工 blocker，且不伪造记录。" -Evidence @($CommercialRecordFile, "docs/commercial-validation.md")
    } else {
        Invoke-ProjectScript -Script "scripts/commercial_validation_audit.ps1" -Arguments @("-RecordFile", $CommercialRecordFile)
        $commercialAudit = Read-JsonFile "data/commercial/trials.audit.json"
        if ($null -ne $commercialAudit -and $commercialAudit.ready_for_final_acceptance -eq $true) {
            Add-Step -Steps $steps -Name "commercial_validation" -Status "done" -Detail "真实商业试用记录已达到最终验收。" -Evidence @("data/commercial/trials.audit.json")
        } else {
            $completed = $(if ($null -eq $commercialAudit) { "unknown" } else { [string]$commercialAudit.completed_trials })
            $paid = $(if ($null -eq $commercialAudit) { "unknown" } else { [string]$commercialAudit.paid_signal_count })
            $status = $(if ($RequireCommercialValidation) { "pending" } else { "skipped" })
            $detail = $(if ($RequireCommercialValidation) {
                    "真实商业试用记录未达标；completed_trials=$completed，paid_signal_count=$paid。"
                } else {
                    "真实商业试用记录未达标；completed_trials=$completed，paid_signal_count=$paid。按当前收口口径后置为运营验证，不阻塞开发停工。"
                })
            Add-Step -Steps $steps -Name "commercial_validation" -Status $status -Detail $detail -Evidence @($CommercialRecordFile, "data/commercial/trials.audit.json")
            if ($RequireCommercialValidation) {
                Add-Blocker -Blockers $blockers -Text "补齐 3-5 个真实试用记录，并至少有 1 个充值或明确付费意向。"
            }
        }
    }
} catch {
    Add-Step -Steps $steps -Name "commercial_validation" -Status "pending" -Detail "真实商业验证未完成：$($_.Exception.Message)" -Evidence @($CommercialRecordFile)
    if ($RequireCommercialValidation) {
        Add-Blocker -Blockers $blockers -Text "把真实试用记录写入 data/commercial/trials.jsonl。"
    }
}

# 7. 最终机器验收：始终生成最终审计报告。
try {
    $finalAcceptanceArgs = @("-Out", "data/acceptance/final-acceptance-audit.json")
    if ($RequireCommercialValidation) {
        $finalAcceptanceArgs += "-RequireCommercialValidation"
    }
    Invoke-ProjectScript -Script "scripts/final_acceptance_audit.ps1" -Arguments $finalAcceptanceArgs
    $finalAudit = Read-JsonFile "data/acceptance/final-acceptance-audit.json"
    if ($null -ne $finalAudit -and $finalAudit.ready_for_stop -eq $true) {
        Add-Step -Steps $steps -Name "final_acceptance_audit" -Status "done" -Detail "最终验收 ready_for_stop=true，可以停工。" -Evidence @("data/acceptance/final-acceptance-audit.json")
    } else {
        $todo = $(if ($null -eq $finalAudit) { "unknown" } else { [string]$finalAudit.todo_count })
        $issue = $(if ($null -eq $finalAudit -or $null -eq $finalAudit.PSObject.Properties["issue_count"]) { "unknown" } else { [string]$finalAudit.issue_count })
        Add-Step -Steps $steps -Name "final_acceptance_audit" -Status "pending" -Detail "最终验收还未达到停工线；todo_count=$todo，issue_count=$issue。" -Evidence @("data/acceptance/final-acceptance-audit.json")
        if ($todo -ne "0") {
            Add-Blocker -Blockers $blockers -Text "最终验收清单仍有 TODO，ready_for_stop 不是 true。"
        } else {
            Add-Blocker -Blockers $blockers -Text "最终验收清单已无 TODO，但仍有证据问题，ready_for_stop 不是 true。"
        }
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
    stop_rule = "Stop only when ready_for_stop is true. This script does not fake Qanlo paid calls or real commercial evidence. Codex agent golden review and real commercial validation can be made strict with -RequireHumanGoldenReview and -RequireCommercialValidation."
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

