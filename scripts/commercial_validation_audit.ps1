param(
    [Alias("Input")]
    [string]$RecordFile = "data/commercial/trials.jsonl",
    [string]$Out = "data/commercial/trials.audit.json",
    [int]$MinTrials = 3,
    [int]$TargetTrials = 5,
    [switch]$RequireFinal
)

$ErrorActionPreference = "Stop"

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
Set-Location $RepoRoot

if ($MinTrials -lt 1) {
    throw "MinTrials must be positive."
}
if ($TargetTrials -lt $MinTrials) {
    throw "TargetTrials must be greater than or equal to MinTrials."
}

function Get-LocalPath {
    param([string]$PathValue)
    if ([System.IO.Path]::IsPathRooted($PathValue)) {
        return $PathValue
    }
    return (Join-Path $RepoRoot $PathValue)
}

function Get-Field {
    param(
        [object]$Record,
        [string]$Name
    )
    if ($null -eq $Record) {
        return $null
    }
    $property = $Record.PSObject.Properties[$Name]
    if ($null -eq $property) {
        return $null
    }
    return $property.Value
}

function Get-StringField {
    param(
        [object]$Record,
        [string]$Name
    )
    $value = Get-Field -Record $Record -Name $Name
    if ($null -eq $value) {
        return ""
    }
    return ([string]$value).Trim()
}

function Get-IntField {
    param(
        [object]$Record,
        [string]$Name
    )
    $value = Get-Field -Record $Record -Name $Name
    if ($null -eq $value) {
        return 0
    }
    $parsed = 0
    if ([int]::TryParse(([string]$value).Trim(), [ref]$parsed)) {
        return $parsed
    }
    return 0
}

function Get-DecimalField {
    param(
        [object]$Record,
        [string]$Name
    )
    $value = Get-Field -Record $Record -Name $Name
    if ($null -eq $value) {
        return [decimal]0
    }
    $parsed = [decimal]0
    if ([decimal]::TryParse(([string]$value).Trim(), [ref]$parsed)) {
        return $parsed
    }
    return [decimal]0
}

function Get-BoolField {
    param(
        [object]$Record,
        [string]$Name
    )
    $value = Get-Field -Record $Record -Name $Name
    if ($null -eq $value) {
        return $false
    }
    if ($value -is [bool]) {
        return $value
    }
    $text = ([string]$value).Trim().ToLowerInvariant()
    return $text -in @("1", "true", "yes", "y", "done", "completed")
}

$inputPath = Get-LocalPath $RecordFile
if (-not (Test-Path -LiteralPath $inputPath)) {
    throw "Commercial trial record file not found: $inputPath"
}

$trialRecords = [System.Collections.Generic.List[object]]::new()
$problems = [System.Collections.Generic.List[string]]::new()
$lineNo = 0
foreach ($line in Get-Content -LiteralPath $inputPath -Encoding UTF8) {
    $lineNo++
    $trimmed = $line.Trim()
    if ([string]::IsNullOrWhiteSpace($trimmed)) {
        continue
    }
    try {
        $record = $trimmed | ConvertFrom-Json -ErrorAction Stop
        $trialRecords.Add([pscustomobject]@{
            line = $lineNo
            value = $record
        }) | Out-Null
    } catch {
        $problems.Add("line $lineNo invalid JSON: $($_.Exception.Message)") | Out-Null
    }
}

$completedTrials = 0
$targetTrialRecords = [System.Collections.Generic.List[object]]::new()
$paidSignalCount = 0
$paidSignalRecords = [System.Collections.Generic.List[object]]::new()
$missingRequiredCount = 0

foreach ($entry in $trialRecords) {
    $record = $entry.value
    $line = $entry.line
    $customer = Get-StringField -Record $record -Name "customer_project"
    $scenario = Get-StringField -Record $record -Name "scenario"
    $apiKeyID = Get-StringField -Record $record -Name "api_key_id"
    $sevenDayCalls = Get-IntField -Record $record -Name "seven_day_calls"
    $realCallCompleted = Get-BoolField -Record $record -Name "real_call_completed"
    $paidSignal = (Get-StringField -Record $record -Name "paid_signal").ToLowerInvariant()
    $paidAmount = Get-DecimalField -Record $record -Name "paid_amount"
    $paidIntentBudget = Get-StringField -Record $record -Name "paid_intent_budget"

    if ([string]::IsNullOrWhiteSpace($customer) -or [string]::IsNullOrWhiteSpace($scenario) -or [string]::IsNullOrWhiteSpace($apiKeyID)) {
        $missingRequiredCount++
        if ($problems.Count -lt 20) {
            $problems.Add("line $line missing customer_project/scenario/api_key_id") | Out-Null
        }
    }

    $completed = (-not [string]::IsNullOrWhiteSpace($customer)) -and
        (-not [string]::IsNullOrWhiteSpace($apiKeyID)) -and
        ($realCallCompleted -or $sevenDayCalls -gt 0)
    if ($completed) {
        $completedTrials++
        $targetTrialRecords.Add([ordered]@{
            line = $line
            customer_project = $customer
            api_key_id = $apiKeyID
            seven_day_calls = $sevenDayCalls
            real_call_completed = $realCallCompleted
        }) | Out-Null
    }

    $hasPaidSignal = ($paidSignal -in @("recharge", "paid", "paid_intent", "intent", "budget")) -or
        ($paidAmount -gt 0) -or
        (-not [string]::IsNullOrWhiteSpace($paidIntentBudget))
    if ($hasPaidSignal) {
        $paidSignalCount++
        $paidSignalRecords.Add([ordered]@{
            line = $line
            customer_project = $customer
            paid_signal = $paidSignal
            paid_amount = $paidAmount
            paid_intent_budget = $paidIntentBudget
        }) | Out-Null
    }
}

$readyMin = $completedTrials -ge $MinTrials
$readyTarget = $completedTrials -ge $TargetTrials
$readyPaid = $paidSignalCount -ge 1
$readyFinal = $readyTarget -and $readyPaid -and ($problems.Count -eq 0)

$report = [ordered]@{
    input = $RecordFile
    total_records = $trialRecords.Count
    invalid_or_problem_count = $problems.Count
    missing_required_count = $missingRequiredCount
    completed_trials = $completedTrials
    min_trials_required = $MinTrials
    min_trials_ready = $readyMin
    target_trials_required = $TargetTrials
    target_trials_ready = $readyTarget
    paid_signal_count = $paidSignalCount
    paid_signal_ready = $readyPaid
    ready_for_final_acceptance = $readyFinal
    completed_trial_examples = @($targetTrialRecords | Select-Object -First 10)
    paid_signal_examples = @($paidSignalRecords | Select-Object -First 10)
    problem_examples = @($problems | Select-Object -First 20)
    required_action = $(if ($readyFinal) { "commercial validation evidence is ready" } else { "fill real trial records until target_trials_ready and paid_signal_ready are true" })
}

$outPath = Get-LocalPath $Out
$outDir = Split-Path -Parent $outPath
if (-not [string]::IsNullOrWhiteSpace($outDir)) {
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
}
$json = $report | ConvertTo-Json -Depth 8
$json | Set-Content -LiteralPath $outPath -Encoding UTF8
$json

if ($RequireFinal -and -not $readyFinal) {
    throw "Commercial validation is not ready for final acceptance."
}
