[CmdletBinding()]
param(
    [string]$BaseUrl = "http://localhost:1279",
    [string]$OutDir = "data/commercial/trial-readiness",
    [string]$FounderSelfTestFile = "data/commercial/founder-self-test.jsonl",
    [string]$FinalCloseoutFile = "data/acceptance/final-closeout-report.json",
    [string]$AcceptanceAuditFile = "data/acceptance/final-acceptance-audit.json",
    [switch]$RequireReady
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

function Read-LastJsonLine {
    param([string]$PathValue)
    $local = Get-LocalPath $PathValue
    if (-not (Test-Path -LiteralPath $local)) {
        return $null
    }
    $last = Get-Content -LiteralPath $local -Encoding UTF8 | Where-Object { -not [string]::IsNullOrWhiteSpace($_) } | Select-Object -Last 1
    if ([string]::IsNullOrWhiteSpace($last)) {
        return $null
    }
    return $last | ConvertFrom-Json
}

function Test-HttpEndpoint {
    param(
        [string]$Url,
        [int]$TimeoutSec = 5
    )
    try {
        $resp = Invoke-WebRequest -Method GET -Uri $Url -TimeoutSec $TimeoutSec -UseBasicParsing
        return [ordered]@{
            ok = ($resp.StatusCode -ge 200 -and $resp.StatusCode -lt 400)
            status_code = [int]$resp.StatusCode
            error = ""
        }
    } catch {
        $status = 0
        if ($_.Exception.Response -and $_.Exception.Response.StatusCode) {
            $status = [int]$_.Exception.Response.StatusCode
        }
        return [ordered]@{
            ok = $false
            status_code = $status
            error = $_.Exception.Message
        }
    }
}

function Write-Utf8File {
    param(
        [string]$PathValue,
        [string]$Content
    )
    $local = Get-LocalPath $PathValue
    $dir = Split-Path -Parent $local
    if (-not [string]::IsNullOrWhiteSpace($dir)) {
        New-Item -ItemType Directory -Force -Path $dir | Out-Null
    }
    $Content | Set-Content -LiteralPath $local -Encoding UTF8
}

$outLocal = Get-LocalPath $OutDir
New-Item -ItemType Directory -Force -Path $outLocal | Out-Null

$base = $BaseUrl.TrimEnd("/")
$health = Test-HttpEndpoint -Url "$base/api/v1/health"
$console = Test-HttpEndpoint -Url "$base/console"
$docs = Test-HttpEndpoint -Url "$base/docs"
$pricing = Test-HttpEndpoint -Url "$base/pricing"

$founder = Read-LastJsonLine $FounderSelfTestFile
$finalCloseout = Read-JsonFile $FinalCloseoutFile
$acceptance = Read-JsonFile $AcceptanceAuditFile
$screenshots = @(Get-ChildItem -Path (Get-LocalPath "output/playwright") -Filter "*self-test.png" -File -ErrorAction SilentlyContinue |
    Sort-Object Name |
    ForEach-Object { $_.FullName })

$founderPass = ($null -ne $founder -and $founder.result -eq "pass" -and $founder.counts_as_real_commercial_validation -eq $false)
$pagesOk = ($health.ok -and $console.ok -and $docs.ok -and $pricing.ok)
$hasScreenshots = ($screenshots.Count -ge 1)
$readyForExternalTrial = ($pagesOk -and $founderPass)

$readmePath = Join-Path $OutDir "README.md"
$invitePath = Join-Path $OutDir "trial-invite.md"
$planPath = Join-Path $OutDir "trial-test-plan.md"
$feedbackPath = Join-Path $OutDir "trial-feedback-form.md"
$templatePath = Join-Path $OutDir "trial-record-template.example.jsonl"
$reportPath = Join-Path $OutDir "trial-readiness-report.json"

$readme = @"
# 商业试用准备包

这个目录用于解决“暂时没有真实外部试用客户”的卡点：先把产品整理成可以直接发给外部开发者/内容工具团队的试用包。

## 当前结论

- 本地服务健康：$($health.ok)
- 控制台 / 文档 / 价格页可访问：$pagesOk
- 创始人/代测记录通过：$founderPass
- 已有截图证据：$hasScreenshots
- 可以开始找外部试用：$readyForExternalTrial
- 是否算真实商业验证：false

真实商业验证仍必须来自外部用户。这个包只证明“可以拿去试用”，不会写入 `data/commercial/trials.jsonl`。

## 文件说明

- `trial-invite.md`：可复制给外部试用者的邀请说明。
- `trial-test-plan.md`：15 分钟试用步骤。
- `trial-feedback-form.md`：试用反馈记录模板。
- `trial-record-template.example.jsonl`：真实试用记录字段示例，不能直接当真实记录。
- `trial-readiness-report.json`：机器可读的准备状态。

## 外部真实试用回来后怎么记录

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\add_commercial_trial.ps1 `
  -CustomerProject "真实项目名" `
  -CustomerType "developer/content-tool/tourism/education" `
  -Scenario "真实试用场景" `
  -ApiKeyId "真实发放的 Key ID" `
  -SevenDayCalls 10 `
  -RealCallCompleted `
  -PaidSignal paid_intent `
  -PaidIntentBudget "例如 100-500 元/月" `
  -NextStep "继续跟进"
```
"@

$invite = @"
# 诗词知识库 API 试用邀请

我这边有一个面向内容工具、文旅产品、小程序和知识库应用的诗词 API，可以提供：

1. 诗词检索；
2. AI 知识库召回，例如“找适合中秋月亮的诗句”；
3. 标签、作者、朝代等结构化查询；
4. 用量统计；
5. 诗词意境 Prompt / 可选生图 dry_run。

试用地址：

- 控制台：$base/console
- 文档：$base/docs
- 价格说明：$base/pricing

试用目标：

- 15 分钟内完成一次 API Key 调用；
- 找到 3 条你项目里能用的诗词结果；
- 反馈缺少的数据、接口或价格接受度。

如果你愿意继续用，请回复：

- 你的项目类型；
- 主要使用场景；
- 预计每月调用量；
- 是否有付费意向或预算范围。
"@

$plan = @"
# 15 分钟试用步骤

## 1. 打开页面

- $base/console
- $base/docs
- $base/pricing

## 2. 填入已发放的 API Key

在控制台左侧填入 `cp_live_xxx`，确认能看到每日额度和今日用量。

## 3. 做三类查询

建议查询：

- `明月`
- `送别`
- `找适合中秋月亮的诗句`
- `找适合文旅山水宣传的诗`

## 4. 验证可用性

请判断：

- 返回内容是否相关；
- 是否能直接放进你的内容工具/小程序/知识库；
- 是否缺少你需要的字段；
- 是否愿意继续调用或付费。

## 5. 可选：试作品级生图 dry_run

如果你需要诗画场景，可以试 `POST /api/v1/works/:id/images/generate`，先用 `dry_run=true`，不会消耗真实生图额度。
"@

$feedback = @"
# 试用反馈表

- 试用者/项目：
- 项目类型：
- API Key ID：
- 试用日期：
- 是否完成真实接口调用：
- 7 天内调用次数：
- 主要查询词：
  -
  -
  -
- 结果是否可用：
- 缺少什么数据或接口：
- 是否有付费意向：
- 可接受预算：
- 下一步：
"@

$template = @"
{"customer_project":"真实项目名","customer_type":"developer/content-tool/tourism/education","scenario":"真实试用场景","api_key_id":"真实 Key ID","tier":"trial","start_date":"2026-07-03","seven_day_calls":10,"real_call_completed":true,"top_queries":["明月","送别","找适合中秋月亮的诗句"],"missing_data":"","paid_signal":"paid_intent","paid_amount":0,"paid_intent_budget":"100-500 元/月","next_step":"继续跟进"}
"@

Write-Utf8File -PathValue $readmePath -Content $readme
Write-Utf8File -PathValue $invitePath -Content $invite
Write-Utf8File -PathValue $planPath -Content $plan
Write-Utf8File -PathValue $feedbackPath -Content $feedback
Write-Utf8File -PathValue $templatePath -Content $template

$report = [ordered]@{
    created_at = (Get-Date).ToString("o")
    base_url = $base
    ready_for_external_trial = $readyForExternalTrial
    counts_as_real_commercial_validation = $false
    external_customer_required = $true
    health = $health
    pages = [ordered]@{
        console = $console
        docs = $docs
        pricing = $pricing
    }
    founder_self_test = [ordered]@{
        file = $FounderSelfTestFile
        present = ($null -ne $founder)
        result = $(if ($null -eq $founder) { "" } else { [string]$founder.result })
        api_key_id = $(if ($null -eq $founder) { "" } else { [string]$founder.api_key_id })
        counts_as_real_commercial_validation = $(if ($null -eq $founder) { $false } else { [bool]$founder.counts_as_real_commercial_validation })
    }
    screenshots = @($screenshots)
    final_closeout = [ordered]@{
        file = $FinalCloseoutFile
        present = ($null -ne $finalCloseout)
        ready_for_stop = $(if ($null -eq $finalCloseout) { $false } else { [bool]$finalCloseout.ready_for_stop })
        blockers = $(if ($null -eq $finalCloseout) { @() } else { @($finalCloseout.blockers) })
    }
    final_acceptance = [ordered]@{
        file = $AcceptanceAuditFile
        present = ($null -ne $acceptance)
        ready_for_stop = $(if ($null -eq $acceptance) { $false } else { [bool]$acceptance.ready_for_stop })
        todo_count = $(if ($null -eq $acceptance) { $null } else { $acceptance.todo_count })
    }
    kit_files = @($readmePath, $invitePath, $planPath, $feedbackPath, $templatePath)
    next_real_trial_record_command = 'powershell -NoProfile -ExecutionPolicy Bypass -File scripts\add_commercial_trial.ps1 -CustomerProject "真实项目名" -CustomerType "developer" -Scenario "真实试用场景" -ApiKeyId "真实 Key ID" -SevenDayCalls 10 -RealCallCompleted'
    stop_rule = "This readiness pack is not real customer evidence and must not flip ready_for_stop by itself."
}

$json = $report | ConvertTo-Json -Depth 12
Write-Utf8File -PathValue $reportPath -Content $json
$json

if ($RequireReady -and -not $readyForExternalTrial) {
    throw "Commercial trial readiness pack is not ready. See $reportPath"
}
