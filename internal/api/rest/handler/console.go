package handler

import (
	_ "embed"
	"net/http"

	"github.com/gin-gonic/gin"
)

// ConsolePage returns the built-in customer console for API key, Qanlo billing,
// poem search, and user-provided Qanlo image-key generation.
func ConsolePage(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(consoleHTML))
}

// DocsPage returns a minimal built-in developer docs page.
func DocsPage(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(docsHTML))
}

// PricingPage returns a customer-facing pricing page for commercial validation.
func PricingPage(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(pricingHTML))
}

//go:embed console.html
var consoleHTML string

const docsHTML = `<!doctype html>
<html lang="zh-CN">
<head><meta charset="utf-8" /><meta name="viewport" content="width=device-width, initial-scale=1" /><title>AI 诗词知识库 API 文档</title><style>body{font-family:-apple-system,BlinkMacSystemFont,"PingFang SC","Microsoft YaHei",sans-serif;margin:0;background:#f6efe3;color:#17120d;line-height:1.75}.wrap{max-width:980px;margin:0 auto;padding:38px 22px}.card{background:#fffaf0;border:1px solid #e3d1b9;border-radius:24px;padding:22px;margin:18px 0;box-shadow:0 18px 55px rgba(93,56,20,.1)}code,pre{background:#17120d;color:#fff1d6;border-radius:12px}code{padding:2px 6px}pre{padding:16px;overflow:auto}.btn{display:inline-block;background:#a8322a;color:white;padding:10px 14px;border-radius:999px;text-decoration:none;font-weight:800;margin-right:8px}</style></head>
<body><main class="wrap"><h1>AI 诗词知识库 API 文档</h1><p>面向普通用户和开发者的诗词检索、AI 知识库召回、Qanlo 绑定 / 充值、客户反馈和运营统计入口。</p><p><a class="btn" href="console">进入控制台</a><a class="btn" href="pricing">价格套餐</a><a class="btn" href="openapi.yaml">openapi.yaml</a></p><p style="display:none">/console /pricing /openapi.yaml</p>
<section class="card"><h2>认证方式</h2><p>客户调用使用 <code>X-API-Key</code>。管理员接口使用 <code>X-Admin-Token</code>。</p><pre>curl "http://localhost:1279/api/v1/poems/query?q=月&page_size=3" \
  -H "X-API-Key: cp_live_xxx"</pre></section>
<section class="card"><h2>核心接口</h2><ul><li><code>POST /api/v1/keys</code>：公开入口已禁用创建，返回 403；Key 必须由管理员或 Qanlo 开通链路发放。</li><li><code>GET /api/v1/keys/current</code>：查看当前 Key 和今日用量。</li><li><code>POST /api/v1/billing/qanlo/provision</code>：生成 Qanlo Agent Key 绑定 URL。</li><li><code>POST /api/v1/billing/qanlo/recharge-session</code>：生成 Qanlo 充值 URL。</li><li><code>GET /api/v1/poems/query</code>：诗词复合查询。</li><li><code>GET /api/v1/poems/search/fulltext</code>：全文搜索。</li><li><code>GET /api/v1/knowledge/recall</code>：AI 知识库召回。</li><li><code>POST /api/v1/knowledge/batch</code>：批量知识库召回。</li><li><code>POST /api/v1/images/generate</code>：诗词意境图生成。</li><li><code>POST /api/v1/feedback</code>：客户反馈。</li></ul></section>
<section class="card"><h2>生图能力</h2><p>控制台右侧图片区已接入直接生图。用户在页面填写并本地保存自己的 Qanlo 生图 API Key；接口会先校验 <code>X-API-Key</code> 和每日额度，再把本次请求里的 <code>image_api_key</code> 代转给 Qanlo 生图网关，不落库、不使用服务器全站 Key；未提供时返回 <code>400 image_api_key_required</code>，不会消耗生图额度。</p></section>
<section class="card"><h2>运营接口</h2><ul><li><code>GET /api/v1/admin/api-keys</code>：管理员查看客户 Key。</li><li><code>PATCH /api/v1/admin/api-keys/:id</code>：调整每日限额、套餐和启停状态。</li><li><code>GET /api/v1/admin/feedback</code>：查看客户反馈。</li><li><code>GET /api/v1/admin/usage/daily</code>：全站每日趋势。</li></ul></section>
<section class="card"><h2>更多接口</h2><p><code>GET /api/v1/billing/status</code> <code>GET /api/v1/admin/enrichment/runs/:run_id/summary</code> <code>GET /api/v1/admin/abuse/blocks</code></p></section>
</main></body></html>`

const pricingHTML = `<!doctype html>
<html lang="zh-CN">
<head><meta charset="utf-8" /><meta name="viewport" content="width=device-width, initial-scale=1" /><title>AI 诗词知识库 API 价格套餐</title><style>body{font-family:-apple-system,BlinkMacSystemFont,"PingFang SC","Microsoft YaHei",sans-serif;margin:0;background:#f6efe3;color:#17120d}.wrap{max-width:1080px;margin:0 auto;padding:40px 22px}.hero{display:flex;justify-content:space-between;gap:20px;align-items:center}.sub{color:#756a5b;line-height:1.75}.plans{display:grid;grid-template-columns:repeat(3,1fr);gap:16px;margin:22px 0}.plan{background:#fffaf0;border:1px solid #e3d1b9;border-radius:26px;padding:22px;box-shadow:0 18px 55px rgba(93,56,20,.1)}.price{font-size:34px;font-weight:900;margin:10px 0}.btn{display:inline-block;background:#a8322a;color:white;padding:12px 16px;border-radius:999px;text-decoration:none;font-weight:900;margin-right:8px}.secondary{background:#eadbc6;color:#17120d}@media(max-width:860px){.hero{display:block}.plans{grid-template-columns:1fr}}</style></head>
<body><main class="wrap"><section class="hero"><div><h1>AI 诗词知识库 API 价格套餐</h1><p class="sub">首版验证价：不自建支付系统，充值和扣费走 QanloAPI；本项目负责诗词知识库、API Key、每日限额、用量统计和运营后台。公开控制台不会自动创建免费 Key。</p></div><div><a class="btn" href="console">进入控制台</a><a class="btn secondary" href="docs">查看文档</a></div></section><p style="display:none">/console /docs</p>
<section class="plans"><div class="plan"><h2>体验版（需开通 Key）</h2><div class="price">开通后可用</div><p>适合个人开发者、演示客户和早期试用，需先由管理员或 Qanlo 链路发放 Key。</p><ul><li>每日 100-1000 次测试额度</li><li>诗词检索与知识库召回</li><li>客户反馈入口</li></ul></div><div class="plan"><h2>开发者</h2><div class="price">按量</div><p>适合小程序、网站、文旅内容工具。</p><ul><li>按 QanloAPI 充值链路验证</li><li>可配置每日限额</li><li>支持 /api/v1/billing/qanlo/recharge-session</li></ul></div><div class="plan"><h2>企业版</h2><div class="price">定制</div><p>适合学校、文旅、私有知识库项目。</p><ul><li>私有部署</li><li>定制标签与评测集</li><li>可选 gpt-image-2 生图接口</li></ul></div></section>
<section class="plan"><h2>计费说明</h2><p>可计费接口包括 <code>/api/v1/poems/query</code>、全文搜索、<code>/api/v1/knowledge/recall</code> 和批量召回。<code>/api/v1/usage/daily</code> 用于查看用量，<code>/api/v1/feedback</code> 用于客户反馈；绑定、充值、状态刷新和反馈默认不消耗每日查询额度。</p></section>
</main></body></html>`
