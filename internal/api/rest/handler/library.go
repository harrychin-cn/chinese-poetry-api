package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/palemoky/chinese-poetry-api/internal/database"
)

// LibraryHandler exposes the Stage-8 global original works library MVP.
type LibraryHandler struct {
	repo *database.Repository
}

// NewLibraryHandler creates a global library handler.
func NewLibraryHandler(repo *database.Repository) *LibraryHandler {
	return &LibraryHandler{repo: repo}
}

// ListPublicWorks returns searchable global public works.
func (h *LibraryHandler) ListPublicWorks(c *gin.Context) {
	items, total, err := h.repo.ListPublicOriginalWorkSummaries(publicWorkListParamsFromQuery(c, "latest"))
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	respondOK(c, gin.H{
		"items":       formatPublicWorkSummaries(items, libraryLang(c)),
		"total":       total,
		"locale":      libraryLocale(c),
		"library_url": "/library",
	})
}

// WorkRankings returns a public work leaderboard.
func (h *LibraryHandler) WorkRankings(c *gin.Context) {
	metric := c.DefaultQuery("metric", "tips")
	params := publicWorkListParamsFromQuery(c, metric)
	params.Sort = metric
	items, total, err := h.repo.ListPublicOriginalWorkSummaries(params)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	respondOK(c, gin.H{
		"metric": metric,
		"items":  formatPublicWorkSummaries(items, libraryLang(c)),
		"total":  total,
		"locale": libraryLocale(c),
	})
}

// AuthorRankings returns a public author leaderboard.
func (h *LibraryHandler) AuthorRankings(c *gin.Context) {
	metric := c.DefaultQuery("metric", "works")
	items, total, err := h.repo.ListPublicAuthorSummaries(database.PublicAuthorListParams{
		Query:  c.Query("q"),
		Sort:   metric,
		Limit:  queryInt(c, "limit", 20),
		Offset: queryOffset(c),
	})
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	respondOK(c, gin.H{
		"metric": metric,
		"items":  formatPublicAuthorSummaries(items),
		"total":  total,
		"locale": libraryLocale(c),
	})
}

// PartnerExport returns public works in a stable partner-friendly export shape.
func (h *LibraryHandler) PartnerExport(c *gin.Context) {
	apiKeyID, ok := currentAPIKeyID(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}
	items, total, err := h.repo.ListPublicOriginalWorkSummaries(publicWorkListParamsFromQuery(c, "latest"))
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	respondOK(c, gin.H{
		"partner_api_version": "stage8-global-library-v1",
		"api_key_id":          apiKeyID,
		"generated_at":        time.Now().UTC(),
		"total":               total,
		"items":               formatPartnerWorkExport(items, libraryLang(c)),
		"license_notice":      libraryLicenseNotice(libraryLang(c)),
	})
}

func publicWorkListParamsFromQuery(c *gin.Context, defaultSort string) database.PublicWorkListParams {
	sort := strings.TrimSpace(c.Query("sort"))
	if sort == "" {
		sort = defaultSort
	}
	return database.PublicWorkListParams{
		Query:        c.Query("q"),
		WorkType:     c.Query("work_type"),
		AuthorHandle: c.Query("author"),
		Sort:         sort,
		Limit:        queryInt(c, "limit", 20),
		Offset:       queryOffset(c),
	}
}

func queryOffset(c *gin.Context) int {
	limit := queryInt(c, "limit", 20)
	page := queryInt(c, "page", 1)
	if page > 1 && limit > 0 {
		return (page - 1) * limit
	}
	offset := queryInt(c, "offset", 0)
	if offset < 0 {
		return 0
	}
	return offset
}

func formatPublicWorkSummaries(items []database.PublicWorkSummary, lang string) []map[string]any {
	data := make([]map[string]any, len(items))
	for i, item := range items {
		data[i] = formatPublicWorkSummary(item, lang)
		data[i]["rank"] = i + 1
	}
	return data
}

func formatPublicWorkSummary(item database.PublicWorkSummary, lang string) map[string]any {
	data := formatWork(item.Work)
	delete(data, "api_key_id")
	data["author"] = formatPublicUserAccount(item.Author)
	data["author_url"] = "/u/" + item.Author.Handle
	data["public_url"] = "/api/v1/public/works/" + item.Work.WorkCode
	data["tip_summary"] = gin.H{
		"tip_count":    item.TipCount,
		"total_amount": item.TotalTipAmount,
	}
	data["localized"] = gin.H{
		"lang":            lang,
		"work_type_label": localizedWorkType(item.Work.WorkType, lang),
		"license_summary": localizedLicenseSummary(item.Work.LicenseType, item.Work.LicenseVersion, lang),
	}
	if item.LatestActivityAt != nil {
		data["latest_activity_at"] = item.LatestActivityAt
	}
	if item.CertificateCode != "" {
		data["certificate"] = gin.H{
			"certificate_code": item.CertificateCode,
			"certificate_hash": item.CertificateHash,
			"anchor_status":    item.AnchorStatus,
			"anchor_network":   item.AnchorNetwork,
			"anchor_tx_id":     item.AnchorTxID,
			"public_url":       "/certificates/" + item.Work.WorkCode,
			"api_url":          "/api/v1/public/works/" + item.Work.WorkCode + "/certificate",
		}
	}
	return data
}

func formatPublicAuthorSummaries(items []database.PublicAuthorSummary) []map[string]any {
	data := make([]map[string]any, len(items))
	for i, item := range items {
		author := formatPublicUserAccount(item.Author)
		author["rank"] = i + 1
		author["public_work_count"] = item.PublicWorkCount
		author["tip_summary"] = gin.H{
			"tip_count":    item.TipCount,
			"total_amount": item.TotalTipAmount,
		}
		author["profile_url"] = "/u/" + item.Author.Handle
		if item.LatestPublishedAt != nil {
			author["latest_published_at"] = item.LatestPublishedAt
		}
		data[i] = author
	}
	return data
}

func formatPartnerWorkExport(items []database.PublicWorkSummary, lang string) []map[string]any {
	data := make([]map[string]any, len(items))
	for i, item := range items {
		data[i] = gin.H{
			"work_code":       item.Work.WorkCode,
			"title":           item.Work.Title,
			"work_type":       item.Work.WorkType,
			"work_type_label": localizedWorkType(item.Work.WorkType, lang),
			"content":         item.Work.Content,
			"content_hash":    item.Work.ContentHash,
			"description":     item.Work.Description,
			"license": gin.H{
				"type":    item.Work.LicenseType,
				"version": item.Work.LicenseVersion,
				"summary": localizedLicenseSummary(item.Work.LicenseType, item.Work.LicenseVersion, lang),
			},
			"author":          formatPublicUserAccount(item.Author),
			"public_url":      "/api/v1/public/works/" + item.Work.WorkCode,
			"author_url":      "/u/" + item.Author.Handle,
			"certificate_url": "/api/v1/public/works/" + item.Work.WorkCode + "/certificate",
			"published_at":    item.Work.PublishedAt,
			"version":         item.Work.Version,
			"tip_summary": gin.H{
				"tip_count":    item.TipCount,
				"total_amount": item.TotalTipAmount,
			},
		}
	}
	return data
}

func libraryLang(c *gin.Context) string {
	switch strings.ToLower(strings.TrimSpace(c.DefaultQuery("lang", "zh-CN"))) {
	case "en", "en-us", "en_us":
		return "en"
	default:
		return "zh-CN"
	}
}

func libraryLocale(c *gin.Context) gin.H {
	lang := libraryLang(c)
	if lang == "en" {
		return gin.H{
			"lang":        "en",
			"title":       "Global Original Works Library",
			"description": "Search public original poems, author profiles, leaderboards, certificates, and partner export APIs.",
		}
	}
	return gin.H{
		"lang":        "zh-CN",
		"title":       "全球原创作品库",
		"description": "检索公开原创诗词曲赋、作者主页、榜单、证书和合作 API。",
	}
}

func localizedWorkType(workType, lang string) string {
	if lang == "en" {
		switch workType {
		case "ci":
			return "Ci lyric"
		case "qu":
			return "Qu song"
		case "fu":
			return "Fu prose-poem"
		case "modern_poem":
			return "Modern poem"
		case "lyric":
			return "Lyric"
		default:
			return "Poem"
		}
	}
	switch workType {
	case "ci":
		return "词"
	case "qu":
		return "曲"
	case "fu":
		return "赋"
	case "modern_poem":
		return "现代诗"
	case "lyric":
		return "歌词"
	default:
		return "诗"
	}
}

func localizedLicenseSummary(licenseType, version, lang string) string {
	if lang == "en" {
		return "Open license " + licenseType + " " + version + "; keep attribution and dispute takedown metadata when reusing."
	}
	return "开放授权 " + licenseType + " " + version + "；复用时请保留作者署名、作品编号和争议下架信息。"
}

func libraryLicenseNotice(lang string) string {
	if lang == "en" {
		return "Partner export contains only public published works. Reusers should preserve author attribution, work_code, license metadata, and certificate links."
	}
	return "合作导出仅包含公开发布作品；复用方应保留作者署名、work_code、授权元数据和证书链接。"
}

// LibraryPage renders the public global works library shell.
func LibraryPage(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", renderProductHTML(c, libraryPageHTML))
}

const libraryPageHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>全球原创作品库 - 诗词曲赋</title>
  <style>
    :root{--ink:#211814;--muted:#756657;--paper:#fffaf0;--line:#dfcbb0;--red:#a8322a;--gold:#c1904a;--shadow:0 18px 55px rgba(83,48,18,.10);--song:"Noto Serif SC","Source Han Serif SC","Songti SC",STSong,serif;--sans:-apple-system,BlinkMacSystemFont,"PingFang SC","Microsoft YaHei",sans-serif}
    *{box-sizing:border-box}body{margin:0;background:radial-gradient(circle at 18% 0,#fffaf0 0,#f6ead7 42%,#efe1cd 100%);color:var(--ink);font-family:var(--sans);line-height:1.72}.wrap{max-width:1180px;margin:0 auto;padding:38px 22px 72px}.hero,.panel,.work{background:rgba(255,250,240,.86);border:1px solid var(--line);border-radius:28px;box-shadow:var(--shadow)}.hero{padding:30px;margin-bottom:18px}.eyebrow{color:var(--red);font-weight:900;letter-spacing:.15em}.hero h1{font:900 48px/1.08 var(--song);margin:8px 0}.sub{color:var(--muted);max-width:760px}.tools{display:grid;grid-template-columns:1.2fr .6fr .6fr auto;gap:10px;margin-top:18px}input,select,button{border:1px solid var(--line);border-radius:16px;padding:12px 13px;font:inherit;background:#fffaf2;color:var(--ink)}button,.btn{background:var(--red);color:white;border-color:transparent;font-weight:900;cursor:pointer;text-decoration:none}.grid{display:grid;grid-template-columns:1.5fr .8fr;gap:16px}.panel{padding:18px}.work{padding:20px;margin-bottom:14px}.work h2{font:800 24px var(--song);margin:0}.content{white-space:pre-line;font:20px/1.9 var(--song);letter-spacing:.04em;margin:12px 0}.tiny{color:var(--muted);font-size:13px}.actions{display:flex;gap:8px;flex-wrap:wrap}.btn{display:inline-flex;border-radius:999px;padding:8px 12px}.rank{display:flex;gap:12px;align-items:flex-start;border-bottom:1px dashed var(--line);padding:12px 0}.badge{width:30px;height:30px;border-radius:50%;display:grid;place-items:center;background:var(--gold);color:white;font-weight:900;flex:0 0 auto}@media(max-width:880px){.hero h1{font-size:36px}.tools,.grid{grid-template-columns:1fr}}
  </style>
</head>
<body>
  <main class="wrap">
    <section class="hero">
      <div class="eyebrow" id="eyebrow">GLOBAL LIBRARY</div>
      <h1 id="title">全球原创作品库</h1>
      <p class="sub" id="desc">检索公开原创诗词曲赋、作者主页、榜单、证书和合作 API。</p>
      <div class="tools">
        <input id="q" placeholder="搜索标题、正文、作者" />
        <select id="sort"><option value="latest">最新发布</option><option value="tips">打赏榜</option><option value="activity">最新活跃</option></select>
        <select id="lang"><option value="zh-CN">中文</option><option value="en">English</option></select>
        <button id="search">搜索</button>
      </div>
    </section>
    <section class="grid">
      <div id="works"></div>
      <aside class="panel">
        <h2 id="rankTitle">作品榜单</h2>
        <div id="rankings"></div>
      </aside>
    </section>
  </main>
  <script>
  (function(){
    function $(id){return document.getElementById(id)}
    function esc(s){return String(s==null?"":s).replace(/[&<>"']/g,function(c){return {"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]})}
    function lines(content){return String(content||"").split(/\n+/).filter(Boolean).slice(0,8).join("\n")}
    function qs(){var p=new URLSearchParams();p.set("limit","30");p.set("sort",$("sort").value);p.set("lang",$("lang").value);if($("q").value.trim())p.set("q",$("q").value.trim());return p.toString()}
    function base(){var p=location.pathname||"/";var m=p.match(/^(.*)\/library\/?$/);return m?(m[1]||""):""}
    function external(path){return typeof path==="string"&&path.charAt(0)==="/"?base()+path:path}
    function item(w){var a=w.author||{}, cert=w.certificate?'<a class="btn" href="'+esc(external(w.certificate.public_url))+'">证书</a>':"";return '<article class="work"><p class="tiny">'+esc((w.localized&&w.localized.work_type_label)||w.work_type)+' · '+esc(a.display_name||a.handle||"作者")+' · '+esc(w.work_code||"")+'</p><h2>'+esc(w.title||"未命名作品")+'</h2><div class="content">'+esc(lines(w.content||""))+'</div><p class="tiny">'+esc(w.description||"")+'</p><div class="actions"><a class="btn" href="/u/'+encodeURIComponent(a.handle||"")+'">作者主页</a><a class="btn" href="'+esc(external(w.public_url||"#"))+'">JSON</a>'+cert+'</div></article>'}
    function render(json){var data=json.data||json, meta=data.locale||{};$("title").textContent=meta.title||"全球原创作品库";$("desc").textContent=meta.description||"";var items=data.items||[];$("works").innerHTML=items.length?items.map(item).join(""):'<article class="work">暂无公开作品。</article>'}
    function renderRank(json){var data=json.data||json, items=data.items||[];$("rankings").innerHTML=items.map(function(w,i){return '<div class="rank"><span class="badge">'+(i+1)+'</span><div><b>'+esc(w.title||"作品")+'</b><p class="tiny">'+esc(((w.author||{}).display_name)||"作者")+' · '+esc(((w.tip_summary||{}).total_amount)||0)+' credits</p></div></div>'}).join("")||'<p class="tiny">暂无榜单。</p>'}
    function load(){fetch("/api/v1/public/works?"+qs()).then(function(r){return r.json()}).then(render);fetch("/api/v1/public/rankings/works?metric=tips&limit=10&lang="+encodeURIComponent($("lang").value)).then(function(r){return r.json()}).then(renderRank)}
    $("search").onclick=load;$("lang").onchange=load;$("sort").onchange=load;load();
  })();
  </script>
</body>
</html>`
