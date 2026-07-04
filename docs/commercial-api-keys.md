# 商业化 API Key 与额度控制

本项目已经具备最小可售卖底座：API Key、每日额度、QanloAPI 精简充值、请求审计、每日趋势、接口错误率、热门查询和后台密钥管理。

## 开启方式

生产环境建议通过环境变量开启：

```bash
API_AUTH_ENABLED=true
API_ADMIN_TOKEN=replace-with-a-long-random-secret
API_DEFAULT_DAILY_LIMIT=1000
ABUSE_PROTECTION_ENABLED=true
ABUSE_AUTO_BLOCK_ENABLED=true
ABUSE_FAILURE_THRESHOLD=20
ABUSE_WINDOW_SECONDS=60
ABUSE_BLOCK_MINUTES=60
```

也可以在 `config.yaml` 里配置：

```yaml
api_auth:
  enabled: true
  admin_token: "replace-with-a-long-random-secret"
  default_daily_limit: 1000
```

## 客户侧商业链路接口

客户从 0 到可调用的主链路：

```http
POST /api/v1/keys
GET /api/v1/keys/current
POST /api/v1/billing/qanlo/provision
POST /api/v1/billing/qanlo/recharge-session
GET /api/v1/billing/qanlo/callback
GET /api/v1/billing/status
```

其中 `POST /api/v1/keys` 仅保留路由兼容，公开环境已禁止自助创建 API Key，会返回 403，避免未充值用户直接生成可用 Key。Key 必须由管理员接口 `POST /api/v1/admin/api-keys` 或受信任的 Qanlo 开通链路发放；返回里的完整密钥只出现一次。其余客户侧状态、充值和用量接口需要 `X-API-Key`。

## OpenAPI 规格

内置开发者规格文件：

```http
GET /openapi.yaml
```

可直接导入 Apifox、Postman、Swagger UI，或用于生成轻量 SDK：

```bash
curl "http://localhost:1279/openapi.yaml" -o openapi.yaml
```

## 受保护接口

当前保护商业增强接口和客户运营接口：

```http
GET /api/v1/poems/query
GET /api/v1/poems/search/fulltext
GET /api/v1/knowledge/recall
POST /api/v1/knowledge/batch
POST /api/v1/images/generate
GET /api/v1/usage/daily
GET /api/v1/usage/endpoints
GET /api/v1/usage/queries
POST /api/v1/feedback
GET /api/v1/public/works/:code
POST /api/v1/works
GET /api/v1/works
GET /api/v1/works/:id
PATCH /api/v1/works/:id
POST /api/v1/works/:id/publish
GET /api/v1/works/:id/versions
GET /api/v1/works/:id/license-acceptances
GET /api/v1/works/:id/plagiarism-report
GET /api/v1/works/:id/media-assets
GET /api/v1/works/:id/image-jobs
POST /api/v1/works/:id/images/generate
```

管理员后台接口需要 `X-Admin-Token`：

```http
GET /api/v1/admin/api-keys
POST /api/v1/admin/api-keys
PATCH /api/v1/admin/api-keys/:id
DELETE /api/v1/admin/api-keys/:id
GET /api/v1/admin/abuse/blocks
POST /api/v1/admin/abuse/blocks
PATCH /api/v1/admin/abuse/blocks/:id
POST /api/v1/admin/search/rebuild
POST /api/v1/admin/tags
POST /api/v1/admin/poems/:id/tags
GET /api/v1/admin/usage/daily
GET /api/v1/admin/usage/endpoints
GET /api/v1/admin/usage/queries
GET /api/v1/admin/feedback
PATCH /api/v1/admin/feedback/:id
GET /api/v1/admin/plagiarism/review-queue
POST /api/v1/admin/plagiarism/review-queue/:id/approve
POST /api/v1/admin/plagiarism/review-queue/:id/reject
GET /api/v1/admin/plagiarism/corpus-sources
POST /api/v1/admin/plagiarism/corpus-sources
GET /api/v1/admin/enrichment/jobs
POST /api/v1/admin/enrichment/jobs
GET /api/v1/admin/enrichment/runs/:run_id/summary
GET /api/v1/admin/enrichment/review-items
POST /api/v1/admin/enrichment/review-items
PATCH /api/v1/admin/enrichment/review-items/:id
POST /api/v1/admin/enrichment/review-items/:id/accept
POST /api/v1/admin/enrichment/review-items/:id/reject
```

普通免费接口仍保持兼容，不强制 API Key：

- `/api/v1/poems`
- `/api/v1/poems/search`
- `/api/v1/poems/random`
- `/api/v1/authors`
- `/api/v1/dynasties`
- `/api/v1/types`
- `/api/v1/tags`
- `/api/v1/knowledge/scenarios`

## 创建 API Key

公开客户自助创建已禁用：

```bash
curl -X POST "http://localhost:1279/api/v1/keys" \
  -H "Content-Type: application/json" \
  -d '{"name":"demo customer","tier":"trial"}'
# HTTP 403: public api key creation is disabled
```

查看当前 Key：

```bash
curl "http://localhost:1279/api/v1/keys/current" \
  -H "X-API-Key: cp_live_xxx"
```

管理员创建：

```bash
curl -X POST "http://localhost:1279/api/v1/admin/api-keys" \
  -H "X-Admin-Token: replace-with-a-long-random-secret" \
  -H "Content-Type: application/json" \
  -d '{"name":"demo customer","tier":"developer","daily_limit":1000}'
```

返回里的 `api_key` 只出现一次，客户需要马上保存。

## QanloAPI 绑定、充值和状态

创建或刷新 Qanlo Agent Key 绑定：

```bash
curl -X POST "http://localhost:1279/api/v1/billing/qanlo/provision" \
  -H "X-API-Key: cp_live_xxx"
```

生成 Qanlo 精简充值页：

```bash
curl -X POST "http://localhost:1279/api/v1/billing/qanlo/recharge-session" \
  -H "X-API-Key: cp_live_xxx"
```

查询本地 Key、额度和 Qanlo 绑定状态：

```bash
curl "http://localhost:1279/api/v1/billing/status" \
  -H "X-API-Key: cp_live_xxx"
```

Qanlo 回跳入口：

```http
GET /api/v1/billing/qanlo/callback
```

## 使用 API Key 调用增强查询

```bash
curl "http://localhost:1279/api/v1/poems/query?author=李白&q=月&search_in=content" \
  -H "X-API-Key: cp_live_xxx"
```

也支持：

```bash
curl "http://localhost:1279/api/v1/poems/query?author=李白&q=月" \
  -H "Authorization: Bearer cp_live_xxx"
```

调用全文搜索：

```bash
curl "http://localhost:1279/api/v1/poems/search/fulltext?author=李白&q=明月&search_in=content" \
  -H "X-API-Key: cp_live_xxx"
```

调用 AI 知识库召回：

```bash
curl "http://localhost:1279/api/v1/knowledge/recall?q=找中秋月亮诗句&page_size=5" \
  -H "X-API-Key: cp_live_xxx"
```

批量召回：

```bash
curl -X POST "http://localhost:1279/api/v1/knowledge/batch" \
  -H "X-API-Key: cp_live_xxx" \
  -H "Content-Type: application/json" \
  -d '{"queries":[{"id":"moon","q":"中秋月亮"},{"id":"farewell","q":"毕业离别"}],"page_size":3}'
```

## 诗词意境图生成

控制台右侧图片区可直接调用：

```bash
curl -X POST "http://localhost:1279/api/v1/images/generate" \
  -H "X-API-Key: cp_live_xxx" \
  -H "X-Image-API-Key: sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{"prompt":"古风水墨，江南春景，轻舟远山，留白构图","size":"1024x1024","image_api_key":"你的 Qanlo 生图 API Key"}'
```

这个接口必须先校验本地 `X-API-Key`，不会公开创建免费 Key。用户在控制台填写自己的 Qanlo 生图 API Key 后，前端会随本次请求传入 `image_api_key`；服务器只代转，不落库、不使用全站后台环境变量。未提供时返回 `400 image_api_key_required`，不消耗生图额度。

可选服务端网关默认配置：

```bash
IMAGE_BASE_URL=https://qanlo.com/openai/v1
IMAGE_MODEL=gpt-image-2
IMAGE_QUALITY=high
IMAGE_OUTPUT_FORMAT=png
IMAGE_TIMEOUT_SECONDS=180
IMAGE_STORAGE_DIR=data/media-assets
IMAGE_PUBLIC_BASE_PATH=/media-assets
IMAGE_CREDIT_COST=1
IMAGE_INITIAL_CREDITS=20
```

## 原创作品库 MVP

用户可以先用 API Key 保存自己的原创诗词曲赋，系统会生成平台作品编号、保存版本，并记录原创承诺和开放授权确认。公开发布必须同时传：

- `original_commitment: true`
- `license_accepted: true`

发布时会自动做基础查重：先按规范化文本 hash 查完全重复，再用 n-gram overlap 比对古代诗词库和平台原创库。`exact_duplicate` / `high_risk` 不会公开发布，会进入 `review_required`；低风险和中风险会保留查重报告。

创建并发布：

```bash
curl -X POST "http://localhost:1279/api/v1/works" \
  -H "X-API-Key: cp_live_xxx" \
  -H "Content-Type: application/json" \
  -d '{"title":"山窗夜坐","work_type":"poem","content":"山窗灯影薄\n一盏照清风","original_commitment":true,"license_accepted":true,"publish":true}'
```

常用接口：

```bash
curl "http://localhost:1279/api/v1/works" -H "X-API-Key: cp_live_xxx"
curl "http://localhost:1279/api/v1/works/1" -H "X-API-Key: cp_live_xxx"
curl -X PATCH "http://localhost:1279/api/v1/works/1" \
  -H "X-API-Key: cp_live_xxx" \
  -H "Content-Type: application/json" \
  -d '{"content":"山窗灯影薄\n一盏照清风\n松声入梦来","change_note":"补第三句"}'
curl -X POST "http://localhost:1279/api/v1/works/1/publish" -H "X-API-Key: cp_live_xxx"
curl "http://localhost:1279/api/v1/works/1/versions" -H "X-API-Key: cp_live_xxx"
curl "http://localhost:1279/api/v1/works/1/license-acceptances" -H "X-API-Key: cp_live_xxx"
curl "http://localhost:1279/api/v1/works/1/plagiarism-report" -H "X-API-Key: cp_live_xxx"
curl "http://localhost:1279/api/v1/public/works/PCQF-2026-00000001"
```


Stage 2 plagiarism admin MVP:

```bash
# Add an operator-curated network/dispute corpus source. The service stores a local semantic embedding for later checks.
curl -X POST "http://localhost:1279/api/v1/admin/plagiarism/corpus-sources" \
  -H "X-Admin-Token: replace-with-a-long-random-secret" \
  -H "Content-Type: application/json" \
  -d '{"source_type":"dispute_case","title":"Known disputed source","source_url":"https://example.com/source","content":"source text for comparison","created_by":"operator"}'

curl "http://localhost:1279/api/v1/admin/plagiarism/corpus-sources?status=enabled" \
  -H "X-Admin-Token: replace-with-a-long-random-secret"

curl "http://localhost:1279/api/v1/admin/plagiarism/review-queue?status=pending" \
  -H "X-Admin-Token: replace-with-a-long-random-secret"

curl -X POST "http://localhost:1279/api/v1/admin/plagiarism/review-queue/1/approve" \
  -H "X-Admin-Token: replace-with-a-long-random-secret" \
  -H "Content-Type: application/json" \
  -d '{"reviewer":"operator","notes":"authorized quotation"}'

curl -X POST "http://localhost:1279/api/v1/admin/plagiarism/review-queue/1/reject" \
  -H "X-Admin-Token: replace-with-a-long-random-secret" \
  -H "Content-Type: application/json" \
  -d '{"reviewer":"operator","notes":"too close to known source"}'
```

作品级生图资产：

```bash
# 只生成并保存作品专属 Prompt / 任务，不调用生图网关、不消耗额度，适合前端预览和冒烟测试
curl -X POST "http://localhost:1279/api/v1/works/1/images/generate" \
  -H "X-API-Key: cp_live_xxx" \
  -H "Content-Type: application/json" \
  -d '{"style":"古风水墨","size":"1024x1024","dry_run":true}'

# 真实生成：可继续复用服务端 IMAGE_API_KEY，也可按次传 X-Image-API-Key
curl -X POST "http://localhost:1279/api/v1/works/1/images/generate" \
  -H "X-API-Key: cp_live_xxx" \
  -H "X-Image-API-Key: sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{"style":"古风水墨","size":"1024x1024","prompt":"题诗自然融入画中留白"}'

  # 同样参数再次调用默认命中缓存；如需重新生成，把请求体加上 "force_regenerate": true
  curl "http://localhost:1279/api/v1/works/1/media-assets?asset_type=image" -H "X-API-Key: cp_live_xxx"
curl "http://localhost:1279/api/v1/works/1/image-jobs" -H "X-API-Key: cp_live_xxx"
```

这个入口会把原创作品正文、作品 `image_prompt`、本次补充要求合并成“画中题诗”的完整生图提示词。`dry_run=true` 只落库任务和 Prompt；真实生成成功后会把图片文件保存到 `IMAGE_STORAGE_DIR`，用 `/media-assets/...` 形式返回正式资产 URL，同时保存 `media_assets`、`image_generation_jobs`，并记录一次本地 API Key 用量。

积分规则（阶段 3 MVP）：每个 API Key 首次使用会初始化 `IMAGE_INITIAL_CREDITS` 积分，真实生图成功扣 `IMAGE_CREDIT_COST` 分并写入 `credit_transactions`；`dry_run` 和缓存命中不扣分。余额不足时返回 `402 insufficient image credits` 和充值入口。相同 work/prompt/model/size/quality/output_format 默认复用最近一次图片资产，返回 `cached=true`、新增成功任务并关联同一资产，不调用生图网关、不增加本地用量、不扣积分；需要强制重生时传 `"force_regenerate": true`。

## 查看 API Key 与今日用量

```bash
curl "http://localhost:1279/api/v1/admin/api-keys" \
  -H "X-Admin-Token: replace-with-a-long-random-secret"
```

返回字段包含：

- `id`
- `name`
- `tier`
- `daily_limit`
- `today_usage`
- `enabled`
- `notes`
- `updated_at`
- `key_prefix`

不会返回完整密钥。

## 管理员调整 Key 状态、额度和备注

```bash
curl -X PATCH "http://localhost:1279/api/v1/admin/api-keys/1" \
  -H "X-Admin-Token: replace-with-a-long-random-secret" \
  -H "Content-Type: application/json" \
  -d '{"daily_limit":2000,"tier":"developer","enabled":true,"notes":"已人工调整限额"}'
```

支持字段：

- `name`：客户名称。
- `tier`：套餐标记，例如 `free`、`developer`、`enterprise`。
- `daily_limit`：每日调用限额；`0` 表示不限量。
- `enabled`：`true` 启用，`false` 禁用。
- `notes`：运营备注，不返回完整密钥。

## Usage 运营统计接口

阶段 5/7 的运营后台已经从“只看今日用量”升级到“能看趋势、错误率和热门查询”的 MVP。审计表会记录 API Key、接口、状态码、耗时、是否计费和 URL 查询摘要，不记录完整请求体或密钥。

### 客户侧用量

```bash
# 当前 Key 的每日调用趋势
curl "http://localhost:1279/api/v1/usage/daily?days=30" \
  -H "X-API-Key: cp_live_xxx"

# 当前 Key 的接口调用量、错误数、错误率
curl "http://localhost:1279/api/v1/usage/endpoints?days=30&limit=20" \
  -H "X-API-Key: cp_live_xxx"

# 当前 Key 的热门查询摘要
curl "http://localhost:1279/api/v1/usage/queries?days=30&limit=20" \
  -H "X-API-Key: cp_live_xxx"
```

主要返回字段：

- `usage_date`：日期，格式 `YYYY-MM-DD`。
- `endpoint`：接口路径或接口分组。
- `total_requests`：总调用次数。
- `billable_requests`：计费调用次数。
- `success_requests`：成功次数。
- `client_error_requests` / `server_error_requests` / `error_requests`：错误次数。
- `error_rate`：错误率，0-1 小数。
- `avg_latency_ms` / `max_latency_ms`：平均/最高耗时，单位毫秒。
- `query_text` / `query_signature`：热门查询摘要，只记录 URL 查询摘要，不记录请求体和密钥。

### 管理员聚合

```bash
# 全站或指定 Key 的每日调用聚合
curl "http://localhost:1279/api/v1/admin/usage/daily?days=30&api_key_id=1" \
  -H "X-Admin-Token: replace-with-a-long-random-secret"

# 全站或指定 Key 的接口错误率聚合
curl "http://localhost:1279/api/v1/admin/usage/endpoints?days=30&api_key_id=1&limit=20" \
  -H "X-Admin-Token: replace-with-a-long-random-secret"

# 全站或指定 Key 的热门查询聚合
curl "http://localhost:1279/api/v1/admin/usage/queries?days=30&api_key_id=1&limit=20" \
  -H "X-Admin-Token: replace-with-a-long-random-secret"
```

运营后台现在至少能回答这几个问题：

- 今天、近 7 天、近 30 天各有多少调用。
- 哪个接口调用最多、错误率最高、平均耗时最高。
- 哪些搜索词、标签、知识库场景最热门。
- 指定客户/Key 的调用是否异常、是否接近每日限额。
- 是否存在连续失败调用或明显滥用。

## 防滥用封禁

生产环境有三层保护：

1. 全局 IP 限流：由 `RATE_LIMIT_RPS` / `RATE_LIMIT_BURST` 控制。
2. API Key 短周期限流：由 `API_KEY_RATE_LIMIT_RPS` / `API_KEY_RATE_LIMIT_BURST` 控制，429 不消耗每日额度。
3. 持久化封禁：管理员可封禁 IP 或 API Key ID；系统也可按 `ABUSE_FAILURE_THRESHOLD` 自动封禁连续 401/429。

查看有效封禁：

```bash
curl "http://localhost:1279/api/v1/admin/abuse/blocks?active_only=true" \
  -H "X-Admin-Token: replace-with-a-long-random-secret"
```

手动封禁 IP 60 分钟：

```bash
curl -X POST "http://localhost:1279/api/v1/admin/abuse/blocks" \
  -H "X-Admin-Token: replace-with-a-long-random-secret" \
  -H "Content-Type: application/json" \
  -d '{"target_type":"ip","target_value":"203.0.113.10","reason":"刷接口","ttl_minutes":60}'
```

手动封禁 API Key ID：

```bash
curl -X POST "http://localhost:1279/api/v1/admin/abuse/blocks" \
  -H "X-Admin-Token: replace-with-a-long-random-secret" \
  -H "Content-Type: application/json" \
  -d '{"target_type":"api_key","target_value":"1","reason":"退款争议","enabled":true}'
```

解封：

```bash
curl -X PATCH "http://localhost:1279/api/v1/admin/abuse/blocks/1" \
  -H "X-Admin-Token: replace-with-a-long-random-secret" \
  -H "Content-Type: application/json" \
  -d '{"enabled":false,"notes":"误封，已解封"}'
```

封禁效果：

- `target_type=ip`：请求进入业务路由前返回 `403 request blocked`。
- `target_type=api_key`：鉴权时返回 `403 api key blocked`，不消耗每日额度。

## 吊销 API Key

```bash
curl -X DELETE "http://localhost:1279/api/v1/admin/api-keys/1" \
  -H "X-Admin-Token: replace-with-a-long-random-secret"
```

吊销后该 Key 不能再调用商业接口。

## 命令行管理

如果是私有部署，也可以不用 HTTP 管理接口，直接操作数据库：

```bash
go run ./cmd/apikey --db data/poetry.db create --name "demo customer" --tier developer --daily-limit 1000 --notes "首批试用客户"
go run ./cmd/apikey --db data/poetry.db update 1 --daily-limit 2000 --tier developer --enabled=true --notes "已人工调整限额"
```

查看 Key：

```bash
go run ./cmd/apikey --db data/poetry.db list
```

吊销 Key：

```bash
go run ./cmd/apikey --db data/poetry.db revoke 1
```

如果使用全文搜索，重建搜索索引：

```bash
go run -tags sqlite_fts5 ./cmd/apikey --db data/poetry.db rebuild-search
```

## AI 数据增强抽检后台

阶段 4 的批量生成和人工抽检走 `cmd/enrichment` 与管理员接口：

```bash
go run ./cmd/enrichment --db data/poetry.db export-sample --limit 100 --out data/enrichment/sample-100.jsonl
go run ./cmd/enrichment generate --provider rules --input data/enrichment/sample-100.jsonl --output data/enrichment/candidates-100.jsonl
go run ./cmd/enrichment validate --input data/enrichment/candidates-100.jsonl
go run ./cmd/enrichment --db data/poetry.db import-candidates --input data/enrichment/candidates-100.jsonl --run-id enrich-20260629-sample100
```

查看待抽检：

```bash
curl "http://localhost:1279/api/v1/admin/enrichment/review-items?status=pending" \
  -H "X-Admin-Token: replace-with-a-long-random-secret"
```

通过并发布：

```bash
curl -X POST "http://localhost:1279/api/v1/admin/enrichment/review-items/1/accept" \
  -H "X-Admin-Token: replace-with-a-long-random-secret" \
  -H "Content-Type: application/json" \
  -d '{"reviewer":"operator","notes":"抽检通过"}'
```

回滚批次：

```bash
go run ./cmd/enrichment --db data/poetry.db rollback --run-id enrich-20260629-sample100 --reviewer operator --notes "质量不稳定，整批回滚"
```


## 客户反馈入口

客户可用自己的 API Key 提交数据缺失、接口问题、功能建议或充值问题；该接口只校验 Key，不消耗每日诗词查询额度。

```bash
curl -X POST "http://localhost:1279/api/v1/feedback" \
  -H "X-API-Key: cp_live_xxx" \
  -H "Content-Type: application/json" \
  -d '{"type":"data","subject":"缺少中秋诗句","message":"希望补充更多中秋月亮相关诗句","contact":"wechat"}'
```

管理员查看和处理反馈：

```bash
curl "http://localhost:1279/api/v1/admin/feedback?status=open&limit=50" \
  -H "X-Admin-Token: replace-with-a-long-random-secret"

curl -X PATCH "http://localhost:1279/api/v1/admin/feedback/1" \
  -H "X-Admin-Token: replace-with-a-long-random-secret" \
  -H "Content-Type: application/json" \
  -d '{"status":"resolved","admin_notes":"已加入增强队列"}'
```

支持反馈类型：`bug`、`data`、`feature`、`billing`、`other`。支持状态：`open`、`reviewing`、`resolved`、`closed`。

## 额度规则

额度和风控分三层：

1. 每日额度：由 `daily_limit` 控制，主要用于套餐和成本上限。
2. 短周期防滥用：由 `API_KEY_RATE_LIMIT_RPS` / `API_KEY_RATE_LIMIT_BURST` 控制，主要防止同一 Key 瞬时刷接口；这类 429 会在鉴权计费前拦截，不消耗每日额度。
3. 封禁：由 `abuse_blocks` 表持久化，适合临时拉黑恶意 IP、异常 Key、退款争议 Key。

- `daily_limit > 0`：每天最多调用对应次数。
- `daily_limit = 0`：不限量，适合企业私有部署或内部测试。
- 超额后返回：

```http
429 Too Many Requests
```

```json
{"error":"daily api quota exceeded"}
```

## 安全设计

- 数据库只保存 API Key 的 SHA-256 哈希，不保存明文。
- 管理接口需要 `X-Admin-Token` 或 `Authorization: Bearer <token>`。
- 商业接口支持 `X-API-Key` 或 `Authorization: Bearer <api_key>`。
- API Key 短周期限流使用哈希后的 Key 作为内存限流键，不在限流器里保存明文 Key。
- 封禁 API Key 使用本地 Key ID，不保存或展示完整密钥。
- 当前阶段先做最小管理接口，后续可接入用户系统和更完整的风控策略。
