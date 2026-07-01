[CmdletBinding()]
param(
    [string]$BaseUrl = "http://localhost:1279",
    [string]$AdminToken = "",
    [ValidateRange(1, 300)]
    [int]$TimeoutSec = 60
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

$BaseUrl = $BaseUrl.Trim().TrimEnd("/")
if ([string]::IsNullOrWhiteSpace($BaseUrl)) {
    throw "BaseUrl is required. Example: -BaseUrl http://localhost:1279"
}

$script:BaseUrlIsApiRoot = $BaseUrl.EndsWith("/api/v1", [System.StringComparison]::OrdinalIgnoreCase)
$script:RawApiKey = ""
$script:AdminTokenForRedaction = $AdminToken
$script:InvokeWebRequestHasUseBasicParsing = (Get-Command Invoke-WebRequest).Parameters.ContainsKey("UseBasicParsing")
$script:InvokeWebRequestHasSkipHttpErrorCheck = (Get-Command Invoke-WebRequest).Parameters.ContainsKey("SkipHttpErrorCheck")

function Mask-Secret {
    param([AllowNull()][string]$Value)

    if ([string]::IsNullOrWhiteSpace($Value)) {
        return ""
    }

    $clean = $Value.Trim()
    if ($clean.Length -le 4) {
        return "****"
    }
    if ($clean.Length -le 12) {
        return ($clean.Substring(0, 2) + "***" + $clean.Substring($clean.Length - 2))
    }

    $prefixLength = [Math]::Min(10, $clean.Length - 6)
    return ($clean.Substring(0, $prefixLength) + "..." + $clean.Substring($clean.Length - 6))
}

function Protect-Text {
    param([AllowNull()][string]$Text)

    if ([string]::IsNullOrEmpty($Text)) {
        return $Text
    }

    $safe = $Text
    foreach ($secret in @($script:RawApiKey, $script:AdminTokenForRedaction)) {
        if (-not [string]::IsNullOrWhiteSpace($secret)) {
            $safe = $safe.Replace($secret, (Mask-Secret $secret))
        }
    }

    $safe = $safe -replace '(?i)("api_key"\s*:\s*")[^"]+(")', '$1[REDACTED]$2'
    $safe = $safe -replace '(?i)("x-api-key"\s*:\s*")[^"]+(")', '$1[REDACTED]$2'
    $safe = $safe -replace '(?i)("x-admin-token"\s*:\s*")[^"]+(")', '$1[REDACTED]$2'
    return $safe
}

function Join-SmokeUrl {
    param([Parameter(Mandatory = $true)][string]$Path)

    $cleanPath = $Path.TrimStart("/")
    if ($script:BaseUrlIsApiRoot -and $cleanPath.StartsWith("api/v1", [System.StringComparison]::OrdinalIgnoreCase)) {
        $cleanPath = $cleanPath.Substring("api/v1".Length).TrimStart("/")
    }
    return ($BaseUrl + "/" + $cleanPath)
}

function Join-SiteUrl {
    param([Parameter(Mandatory = $true)][string]$Path)

    $root = $BaseUrl
    if ($script:BaseUrlIsApiRoot) {
        $root = $BaseUrl.Substring(0, $BaseUrl.Length - "/api/v1".Length).TrimEnd("/")
    }
    return ($root + "/" + $Path.TrimStart("/"))
}

function Read-ErrorResponseBody {
    param($Response)

    if ($null -eq $Response) {
        return ""
    }

    try {
        if ($Response.Content -and ($Response.Content | Get-Member -Name ReadAsStringAsync -MemberType Method -ErrorAction SilentlyContinue)) {
            return $Response.Content.ReadAsStringAsync().GetAwaiter().GetResult()
        }
    } catch {
        return ""
    }

    try {
        $stream = $Response.GetResponseStream()
        if ($null -ne $stream) {
            $reader = New-Object System.IO.StreamReader($stream)
            return $reader.ReadToEnd()
        }
    } catch {
        return ""
    }

    return ""
}

function Invoke-RawHttpRequest {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory = $true)][string]$Method,
        [Parameter(Mandatory = $true)][string]$Uri,
        [hashtable]$Headers = @{},
        [AllowNull()][string]$BodyText = $null,
        [int]$TimeoutSec = 10,
        [string]$ContentType = "application/json"
    )

    $request = [System.Net.HttpWebRequest]::Create($Uri)
    $request.Method = $Method
    $request.Timeout = [Math]::Max(1, $TimeoutSec) * 1000
    $request.ReadWriteTimeout = [Math]::Max(1, $TimeoutSec) * 1000
    $request.Accept = "*/*"

    foreach ($key in $Headers.Keys) {
        $request.Headers[$key] = [string]$Headers[$key]
    }

    if ($null -ne $BodyText -and $Method -notin @("GET", "HEAD")) {
        $bytes = [System.Text.Encoding]::UTF8.GetBytes($BodyText)
        $request.ContentType = $ContentType
        $request.ContentLength = $bytes.Length
        $stream = $request.GetRequestStream()
        try {
            $stream.Write($bytes, 0, $bytes.Length)
        } finally {
            $stream.Close()
        }
    }

    $response = $null
    try {
        $response = $request.GetResponse()
    } catch [System.Net.WebException] {
        $response = $_.Exception.Response
        if ($null -eq $response) {
            throw
        }
    }

    try {
        $status = [int]$response.StatusCode
        $reader = New-Object System.IO.StreamReader($response.GetResponseStream(), [System.Text.Encoding]::UTF8)
        try {
            $content = $reader.ReadToEnd()
        } finally {
            $reader.Close()
        }

        return [pscustomobject]@{
            StatusCode = $status
            Content    = $content
            Headers    = $response.Headers
        }
    } finally {
        if ($null -ne $response) {
            $response.Close()
        }
    }
}

function Convert-ResponseJson {
    param(
        [Parameter(Mandatory = $true)][string]$Step,
        [AllowNull()][string]$Content,
        [switch]$SensitiveResponse
    )

    if ([string]::IsNullOrWhiteSpace($Content)) {
        return $null
    }

    try {
        return ($Content | ConvertFrom-Json)
    } catch {
        $body = if ($SensitiveResponse) { "[redacted]" } else { Protect-Text $Content }
        throw "[$Step] response is not valid JSON. Body: $body"
    }
}

function Invoke-SmokeRequest {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory = $true)]
        [ValidateSet("GET", "POST", "PATCH")]
        [string]$Method,

        [Parameter(Mandatory = $true)]
        [string]$Path,

        [hashtable]$Headers = @{},
        [AllowNull()][object]$Body = $null,
        [int[]]$ExpectedStatus = @(200),

        [Parameter(Mandatory = $true)]
        [string]$Step,

        [switch]$SensitiveResponse
    )

    $uri = Join-SmokeUrl $Path
    $bodyText = $null
    if ($null -ne $Body) {
        $bodyText = ($Body | ConvertTo-Json -Depth 8 -Compress)
    }

    try {
        $response = Invoke-RawHttpRequest -Method $Method -Uri $uri -TimeoutSec $TimeoutSec -Headers $Headers -BodyText $bodyText
    } catch {
        $message = Protect-Text $_.Exception.Message
        throw "[$Step] $Method $Path failed (status=no response): $message"
    }

    $status = [int]$response.StatusCode
    if ($ExpectedStatus -notcontains $status) {
        $safeBody = if ($SensitiveResponse) { "[redacted]" } else { Protect-Text ([string]$response.Content) }
        throw "[$Step] expected HTTP $($ExpectedStatus -join '/') but got $status. Body: $safeBody"
    }

    $json = Convert-ResponseJson -Step $Step -Content ([string]$response.Content) -SensitiveResponse:$SensitiveResponse
    Write-Host ("PASS {0} {1} -> HTTP {2}" -f $Step, $Path, $status)

    return [pscustomobject]@{
        StatusCode = $status
        Json       = $json
        Headers    = $response.Headers
    }
}

function Invoke-SmokeTextRequest {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory = $true)]
        [ValidateSet("GET")]
        [string]$Method,

        [Parameter(Mandatory = $true)]
        [string]$Path,

        [int[]]$ExpectedStatus = @(200),

        [Parameter(Mandatory = $true)]
        [string]$Step,

        [string[]]$Contains = @()
    )

    $uri = Join-SiteUrl $Path

    try {
        $response = Invoke-RawHttpRequest -Method $Method -Uri $uri -TimeoutSec $TimeoutSec
    } catch {
        $message = Protect-Text $_.Exception.Message
        throw "[$Step] $Method $Path failed (status=no response): $message"
    }

    $status = [int]$response.StatusCode
    $content = [string]$response.Content
    if ($ExpectedStatus -notcontains $status) {
        throw "[$Step] expected HTTP $($ExpectedStatus -join '/') but got $status. Body: $(Protect-Text $content)"
    }

    foreach ($expected in $Contains) {
        if (-not $content.Contains($expected)) {
            throw "[$Step] response does not contain expected text: $expected"
        }
    }

    Write-Host ("PASS {0} {1} -> HTTP {2}" -f $Step, $Path, $status)

    return [pscustomobject]@{
        StatusCode = $status
        Content    = $content
        Headers    = $response.Headers
    }
}

function Assert-Field {
    param(
        [Parameter(Mandatory = $true)][string]$Step,
        [AllowNull()][object]$Value,
        [Parameter(Mandatory = $true)][string]$Field
    )

    if ($null -eq $Value -or [string]::IsNullOrWhiteSpace([string]$Value)) {
        throw "[$Step] missing response field: $Field"
    }
}

function Get-UrlQueryParam {
    param(
        [Parameter(Mandatory = $true)][string]$Url,
        [Parameter(Mandatory = $true)][string]$Name
    )

    $uri = [System.Uri]$Url
    $query = $uri.Query
    if ([string]::IsNullOrWhiteSpace($query)) {
        return ""
    }
    $query = $query.TrimStart("?")
    foreach ($pair in $query.Split("&", [System.StringSplitOptions]::RemoveEmptyEntries)) {
        $parts = $pair.Split("=", 2)
        $key = [System.Net.WebUtility]::UrlDecode($parts[0])
        if ($key -eq $Name) {
            if ($parts.Count -lt 2) {
                return ""
            }
            return [System.Net.WebUtility]::UrlDecode($parts[1])
        }
    }

    return ""
}

function Get-QanloStateFromConnectUrl {
    param([Parameter(Mandatory = $true)][string]$ConnectUrl)

    $returnUrl = Get-UrlQueryParam -Url $ConnectUrl -Name "return_url"
    if ([string]::IsNullOrWhiteSpace($returnUrl)) {
        throw "[qanlo state] connect_url missing return_url"
    }

    $state = Get-UrlQueryParam -Url $returnUrl -Name "state"
    if ([string]::IsNullOrWhiteSpace($state)) {
        throw "[qanlo state] return_url missing state"
    }

    return $state
}

function Get-ItemCount {
    param([AllowNull()][object]$Value)

    if ($null -eq $Value) {
        return 0
    }
    return @($Value).Count
}

function Wait-APIKeyRefill {
    # Default API-Key short-cycle limit is intentionally strict (2 rps / burst 5).
    # Keep the smoke deterministic when it checks several authenticated endpoints.
    Start-Sleep -Milliseconds 700
}

Write-Host "Commercial smoke target: $BaseUrl"
Write-Host "This script does not start the server, follow Qanlo URLs, or call external paid services."
Write-Host ""

$health = Invoke-SmokeRequest -Method GET -Path "/api/v1/health" -Step "health" -ExpectedStatus @(200)
Assert-Field -Step "health" -Value $health.Json.status -Field "status"
if ($health.Json.status -ne "healthy") {
    throw "[health] expected status=healthy but got $($health.Json.status)"
}

$docsPage = Invoke-SmokeTextRequest -Method GET -Path "/docs" -Step "docs page" -Contains @(
    'href="console"',
    'href="pricing"',
    'href="openapi.yaml"',
    "POST /api/v1/keys",
    "GET /api/v1/knowledge/recall"
)
$openAPI = Invoke-SmokeTextRequest -Method GET -Path "/openapi.yaml" -Step "openapi yaml" -Contains @(
    "openapi: 3.0.3",
    "paths:",
    "/api/v1/keys:",
    "/api/v1/billing/status:",
    "/api/v1/knowledge/recall:",
    "X-API-Key",
    "X-Admin-Token"
)
$consolePage = Invoke-SmokeTextRequest -Method GET -Path "/console" -Step "console page" -Contains @(
    "AI 诗词知识库 API 控制台",
    "创建 API Key",
    "Qanlo 绑定 / 充值",
    "AI 知识库召回",
    "客户反馈"
)
$pricingPage = Invoke-SmokeTextRequest -Method GET -Path "/pricing" -Step "pricing page" -Contains @(
    "AI 诗词知识库 API 价格套餐",
    "QanloAPI",
    "立即试用",
    "/api/v1/billing/qanlo/recharge-session"
)
Write-Host ("      docs bytes={0}, openapi bytes={1}, console bytes={2}, pricing bytes={3}" -f $docsPage.Content.Length, $openAPI.Content.Length, $consolePage.Content.Length, $pricingPage.Content.Length)

$keyName = "commercial-smoke-" + (Get-Date -Format "yyyyMMdd-HHmmss")
$createKeyBody = [ordered]@{
    name  = $keyName
    tier  = "trial"
    notes = "local commercial smoke only; no external payment"
}
$createKey = Invoke-SmokeRequest -Method POST -Path "/api/v1/keys" -Step "create key" -Body $createKeyBody -ExpectedStatus @(201) -SensitiveResponse
$script:RawApiKey = [string]$createKey.Json.data.api_key
Assert-Field -Step "create key" -Value $script:RawApiKey -Field "data.api_key"
Assert-Field -Step "create key" -Value $createKey.Json.data.id -Field "data.id"
Write-Host ("      created key id={0}, key={1}, prefix={2}" -f $createKey.Json.data.id, (Mask-Secret $script:RawApiKey), $createKey.Json.data.key_prefix)

$apiHeaders = @{ "X-API-Key" = $script:RawApiKey }

$currentKey = Invoke-SmokeRequest -Method GET -Path "/api/v1/keys/current" -Step "current key" -Headers $apiHeaders -ExpectedStatus @(200)
Assert-Field -Step "current key" -Value $currentKey.Json.data.id -Field "data.id"
Write-Host ("      current key id={0}, tier={1}, today_usage={2}" -f $currentKey.Json.data.id, $currentKey.Json.data.tier, $currentKey.Json.data.today_usage)

$billingStatus = Invoke-SmokeRequest -Method GET -Path "/api/v1/billing/status" -Step "billing status" -Headers $apiHeaders -ExpectedStatus @(200)
Assert-Field -Step "billing status" -Value $billingStatus.Json.data.api_key.id -Field "data.api_key.id"
if ($null -eq $billingStatus.Json.data.qanlo) {
    throw "[billing status] missing response field: data.qanlo"
}
$qanloConfigured = [bool]$billingStatus.Json.data.qanlo.configured
Write-Host ("      qanlo status={0}, configured={1}" -f $billingStatus.Json.data.qanlo.status, $qanloConfigured)

if ($qanloConfigured) {
    $provision = Invoke-SmokeRequest -Method POST -Path "/api/v1/billing/qanlo/provision" -Step "qanlo provision" -Headers $apiHeaders -ExpectedStatus @(200)
    Assert-Field -Step "qanlo provision" -Value $provision.Json.data.connect_url -Field "data.connect_url"
    Write-Host "      provision URL generated locally; not opening it."

    $provisionState = Get-QanloStateFromConnectUrl -ConnectUrl ([string]$provision.Json.data.connect_url)
    $mockQanloKey = "sk-qanlo-smoke-" + (Get-Date -Format "yyyyMMddHHmmss")
    $callbackPath = "/api/v1/billing/qanlo/callback?state=$([System.Uri]::EscapeDataString($provisionState))&qanlo_key=$([System.Uri]::EscapeDataString($mockQanloKey))&base_url=$([System.Uri]::EscapeDataString('http://qanlo.local/v1'))&intent=provision"
    $null = Invoke-SmokeTextRequest -Method GET -Path $callbackPath -Step "qanlo callback bind" -Contains @("Qanlo")

    Wait-APIKeyRefill
    $billingAfterCallback = Invoke-SmokeRequest -Method GET -Path "/api/v1/billing/status" -Step "billing status after callback" -Headers $apiHeaders -ExpectedStatus @(200)
    if ($billingAfterCallback.Json.data.qanlo.status -ne "linked" -or $billingAfterCallback.Json.data.qanlo.has_qanlo_key -ne $true) {
        throw "[billing status after callback] expected linked Qanlo binding"
    }
    Write-Host "      qanlo callback linked local API key."
} else {
    Write-Host "SKIP qanlo provision: QANLO_AGENT_APP_ID is not configured."
}

$recharge = Invoke-SmokeRequest -Method POST -Path "/api/v1/billing/qanlo/recharge-session" -Step "qanlo recharge" -Headers $apiHeaders -ExpectedStatus @(200)
Assert-Field -Step "qanlo recharge" -Value $recharge.Json.data.recharge_url -Field "data.recharge_url"
Write-Host "      recharge URL generated locally; not opening it."

if ($qanloConfigured) {
    $rechargeState = Get-QanloStateFromConnectUrl -ConnectUrl ([string]$recharge.Json.data.recharge_url)
    $rechargeReturnPath = "/api/v1/billing/qanlo/callback?state=$([System.Uri]::EscapeDataString($rechargeState))&intent=recharge"
    $null = Invoke-SmokeTextRequest -Method GET -Path $rechargeReturnPath -Step "qanlo recharge return" -Contains @("Qanlo")

    Wait-APIKeyRefill
    $billingAfterRecharge = Invoke-SmokeRequest -Method GET -Path "/api/v1/billing/status" -Step "billing status after recharge" -Headers $apiHeaders -ExpectedStatus @(200)
    if ($billingAfterRecharge.Json.data.qanlo.status -ne "linked") {
        throw "[billing status after recharge] expected linked Qanlo status"
    }
    Write-Host "      qanlo recharge return recorded."
}

$poems = Invoke-SmokeRequest -Method GET -Path "/api/v1/poems/query?page_size=1" -Step "poems query" -Headers $apiHeaders -ExpectedStatus @(200)
if ($null -eq $poems.Json.data) {
    throw "[poems query] missing response field: data"
}
$poemCount = Get-ItemCount $poems.Json.data
if ($poemCount -gt 1) {
    throw "[poems query] expected at most 1 poem but got $poemCount"
}
Write-Host ("      poems returned={0}" -f $poemCount)

Wait-APIKeyRefill
$fulltext = Invoke-SmokeRequest -Method GET -Path "/api/v1/poems/search/fulltext?q=明月&page_size=1" -Step "poems fulltext" -Headers $apiHeaders -ExpectedStatus @(200, 503)
Write-Host ("      fulltext status={0} (200=FTS enabled, 503=expected fallback when built without sqlite_fts5)" -f $fulltext.StatusCode)

$tags = Invoke-SmokeRequest -Method GET -Path "/api/v1/tags" -Step "tags list" -ExpectedStatus @(200)
if ($null -eq $tags.Json.data) {
    throw "[tags list] missing response field: data"
}
Write-Host ("      tags returned={0}" -f (Get-ItemCount $tags.Json.data))

$scenarios = Invoke-SmokeRequest -Method GET -Path "/api/v1/knowledge/scenarios" -Step "knowledge scenarios" -ExpectedStatus @(200)
if ($null -eq $scenarios.Json.data) {
    throw "[knowledge scenarios] missing response field: data"
}
Write-Host ("      scenarios returned={0}" -f (Get-ItemCount $scenarios.Json.data))

Wait-APIKeyRefill
$knowledge = Invoke-SmokeRequest -Method GET -Path "/api/v1/knowledge/recall?q=找中秋月亮诗句&page_size=1" -Step "knowledge recall" -Headers $apiHeaders -ExpectedStatus @(200)
if ($null -eq $knowledge.Json.data) {
    throw "[knowledge recall] missing response field: data"
}
Write-Host ("      knowledge recall returned={0}" -f (Get-ItemCount $knowledge.Json.data))

Wait-APIKeyRefill
$knowledgeBatchBody = [ordered]@{
    page_size = 1
    queries   = @(
        [ordered]@{ id = "moon"; q = "中秋月亮" },
        [ordered]@{ id = "farewell"; q = "毕业离别" }
    )
}
$knowledgeBatch = Invoke-SmokeRequest -Method POST -Path "/api/v1/knowledge/batch" -Step "knowledge batch" -Headers $apiHeaders -Body $knowledgeBatchBody -ExpectedStatus @(200)
if ($null -eq $knowledgeBatch.Json.data) {
    throw "[knowledge batch] missing response field: data"
}
Write-Host ("      knowledge batch groups={0}" -f (Get-ItemCount $knowledgeBatch.Json.data))

Wait-APIKeyRefill
$usage = Invoke-SmokeRequest -Method GET -Path "/api/v1/usage/daily" -Step "usage daily" -Headers $apiHeaders -ExpectedStatus @(200)
if ($null -eq $usage.Json.data) {
    throw "[usage daily] missing response field: data"
}
Write-Host ("      daily usage rows={0}" -f (Get-ItemCount $usage.Json.data.items))

Wait-APIKeyRefill
$usageEndpoints = Invoke-SmokeRequest -Method GET -Path "/api/v1/usage/endpoints" -Step "usage endpoints" -Headers $apiHeaders -ExpectedStatus @(200)
if ($null -eq $usageEndpoints.Json.data) {
    throw "[usage endpoints] missing response field: data"
}
Write-Host ("      endpoint usage rows={0}" -f (Get-ItemCount $usageEndpoints.Json.data.items))

Wait-APIKeyRefill
$usageQueries = Invoke-SmokeRequest -Method GET -Path "/api/v1/usage/queries" -Step "usage queries" -Headers $apiHeaders -ExpectedStatus @(200)
if ($null -eq $usageQueries.Json.data) {
    throw "[usage queries] missing response field: data"
}
Write-Host ("      query usage rows={0}" -f (Get-ItemCount $usageQueries.Json.data.items))

Wait-APIKeyRefill

$feedbackBody = [ordered]@{
    type    = "other"
    subject = "commercial smoke"
    message = "Local commercial smoke feedback created by scripts/smoke_commercial.ps1."
    contact = "smoke-local@example.invalid"
}
$feedback = Invoke-SmokeRequest -Method POST -Path "/api/v1/feedback" -Step "feedback" -Headers $apiHeaders -Body $feedbackBody -ExpectedStatus @(201)
Assert-Field -Step "feedback" -Value $feedback.Json.data.id -Field "data.id"
Write-Host ("      feedback id={0}, status={1}" -f $feedback.Json.data.id, $feedback.Json.data.status)

if (-not [string]::IsNullOrWhiteSpace($AdminToken)) {
    $adminHeaders = @{ "X-Admin-Token" = $AdminToken }
    $adminKeys = Invoke-SmokeRequest -Method GET -Path "/api/v1/admin/api-keys" -Step "admin api keys" -Headers $adminHeaders -ExpectedStatus @(200)
    if ($null -eq $adminKeys.Json.data) {
        throw "[admin api keys] missing response field: data"
    }
    Write-Host ("      admin key rows={0}" -f (Get-ItemCount $adminKeys.Json.data))

    $quotaKeyBody = [ordered]@{
        name        = "commercial-smoke-quota-" + (Get-Date -Format "yyyyMMdd-HHmmss")
        tier        = "trial"
        daily_limit = 1
        notes       = "local commercial smoke quota-exhaustion check only"
    }
    $quotaKey = Invoke-SmokeRequest -Method POST -Path "/api/v1/admin/api-keys" -Step "admin create quota key" -Headers $adminHeaders -Body $quotaKeyBody -ExpectedStatus @(201) -SensitiveResponse
    $quotaRawKey = [string]$quotaKey.Json.data.api_key
    Assert-Field -Step "admin create quota key" -Value $quotaRawKey -Field "data.api_key"
    $quotaHeaders = @{ "X-API-Key" = $quotaRawKey }
    $null = Invoke-SmokeRequest -Method GET -Path "/api/v1/poems/query?page_size=1" -Step "quota key first call" -Headers $quotaHeaders -ExpectedStatus @(200)
    Wait-APIKeyRefill
    $quotaExceeded = Invoke-SmokeRequest -Method GET -Path "/api/v1/poems/query?page_size=1" -Step "quota exceeded recharge hint" -Headers $quotaHeaders -ExpectedStatus @(429)
    if ($quotaExceeded.Json.error -ne "daily api quota exceeded") {
        throw "[quota exceeded recharge hint] expected quota error but got: $($quotaExceeded.Json.error)"
    }
    Assert-Field -Step "quota exceeded recharge hint" -Value $quotaExceeded.Json.recharge_endpoint -Field "recharge_endpoint"
    Write-Host "      quota exceeded response includes recharge endpoint."

    $adminUsageDaily = Invoke-SmokeRequest -Method GET -Path "/api/v1/admin/usage/daily" -Step "admin usage daily" -Headers $adminHeaders -ExpectedStatus @(200)
    if ($null -eq $adminUsageDaily.Json.data) {
        throw "[admin usage daily] missing response field: data"
    }

    $adminUsageEndpoints = Invoke-SmokeRequest -Method GET -Path "/api/v1/admin/usage/endpoints" -Step "admin usage endpoints" -Headers $adminHeaders -ExpectedStatus @(200)
    if ($null -eq $adminUsageEndpoints.Json.data) {
        throw "[admin usage endpoints] missing response field: data"
    }

    $adminUsageQueries = Invoke-SmokeRequest -Method GET -Path "/api/v1/admin/usage/queries" -Step "admin usage queries" -Headers $adminHeaders -ExpectedStatus @(200)
    if ($null -eq $adminUsageQueries.Json.data) {
        throw "[admin usage queries] missing response field: data"
    }
    Write-Host ("      admin usage rows daily={0}, endpoints={1}, queries={2}" -f (Get-ItemCount $adminUsageDaily.Json.data.items), (Get-ItemCount $adminUsageEndpoints.Json.data.items), (Get-ItemCount $adminUsageQueries.Json.data.items))

    $adminFeedback = Invoke-SmokeRequest -Method GET -Path "/api/v1/admin/feedback?limit=20" -Step "admin feedback list" -Headers $adminHeaders -ExpectedStatus @(200)
    if ($null -eq $adminFeedback.Json.data.items) {
        throw "[admin feedback list] missing response field: data.items"
    }
    Write-Host ("      admin feedback rows={0}" -f (Get-ItemCount $adminFeedback.Json.data.items))

    $feedbackUpdateBody = [ordered]@{
        status      = "reviewing"
        admin_notes = "checked by commercial smoke"
    }
    $feedbackUpdate = Invoke-SmokeRequest -Method PATCH -Path ("/api/v1/admin/feedback/" + $feedback.Json.data.id) -Step "admin feedback update" -Headers $adminHeaders -Body $feedbackUpdateBody -ExpectedStatus @(200)
    if ($feedbackUpdate.Json.data.status -ne "reviewing") {
        throw "[admin feedback update] expected status=reviewing"
    }

    $blockTarget = "198.51.100." + (Get-Random -Minimum 1 -Maximum 250)
    $blockBody = [ordered]@{
        target_type  = "ip"
        target_value = $blockTarget
        reason       = "commercial smoke temporary block"
        ttl_minutes  = 5
        notes        = "created by scripts/smoke_commercial.ps1"
    }
    $block = Invoke-SmokeRequest -Method POST -Path "/api/v1/admin/abuse/blocks" -Step "admin abuse block create" -Headers $adminHeaders -Body $blockBody -ExpectedStatus @(201)
    Assert-Field -Step "admin abuse block create" -Value $block.Json.data.id -Field "data.id"
    Write-Host ("      temporary abuse block id={0}, target={1}" -f $block.Json.data.id, $blockTarget)

    $blocks = Invoke-SmokeRequest -Method GET -Path "/api/v1/admin/abuse/blocks?active_only=true&limit=20" -Step "admin abuse block list" -Headers $adminHeaders -ExpectedStatus @(200)
    if ($null -eq $blocks.Json.data.items) {
        throw "[admin abuse block list] missing response field: data.items"
    }

    $releaseBody = [ordered]@{
        enabled = $false
        notes   = "released by commercial smoke"
    }
    $released = Invoke-SmokeRequest -Method PATCH -Path ("/api/v1/admin/abuse/blocks/" + $block.Json.data.id) -Step "admin abuse block release" -Headers $adminHeaders -Body $releaseBody -ExpectedStatus @(200)
    if ($released.Json.data.enabled -ne $false) {
        throw "[admin abuse block release] expected enabled=false"
    }
} else {
    Write-Host "SKIP admin api keys: -AdminToken not provided."
}

Write-Host ""
Write-Host "Commercial smoke passed."
Write-Host ("Masked API key used: {0}" -f (Mask-Secret $script:RawApiKey))
