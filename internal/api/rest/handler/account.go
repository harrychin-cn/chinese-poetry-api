package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/palemoky/chinese-poetry-api/internal/database"
)

// AccountHandler manages the MVP user account/profile layer.
type AccountHandler struct {
	repo *database.Repository
}

// NewAccountHandler creates an account handler.
func NewAccountHandler(repo *database.Repository) *AccountHandler {
	return &AccountHandler{repo: repo}
}

type updateAccountRequest struct {
	Handle      *string `json:"handle"`
	DisplayName *string `json:"display_name"`
	Email       *string `json:"email"`
	Bio         *string `json:"bio"`
	AvatarURL   *string `json:"avatar_url"`
	WebsiteURL  *string `json:"website_url"`
}

// Current returns the current API key's public author profile and lazily creates one if needed.
func (h *AccountHandler) Current(c *gin.Context) {
	apiKeyID, ok := currentAPIKeyID(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}
	account, err := h.repo.GetOrCreateUserAccountForAPIKey(apiKeyID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to read account")
		return
	}
	respondOK(c, formatUserAccount(*account))
}

// Update edits the current API key's public author profile.
func (h *AccountHandler) Update(c *gin.Context) {
	apiKeyID, ok := currentAPIKeyID(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}

	var req updateAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid json body")
		return
	}

	account, err := h.repo.UpdateUserAccountForAPIKey(apiKeyID, database.UpdateUserAccountParams{
		Handle:      req.Handle,
		DisplayName: req.DisplayName,
		Email:       req.Email,
		Bio:         req.Bio,
		AvatarURL:   req.AvatarURL,
		WebsiteURL:  req.WebsiteURL,
	})
	if errors.Is(err, database.ErrInvalidQueryParam) {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	if errors.Is(err, database.ErrUserHandleTaken) {
		respondError(c, http.StatusConflict, "handle already exists")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to update account")
		return
	}

	respondOK(c, formatUserAccount(*account))
}

// PublicProfile returns a public account profile plus recent published works.
func (h *AccountHandler) PublicProfile(c *gin.Context) {
	account, err := h.repo.GetPublicUserAccountByHandle(c.Param("handle"))
	if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, database.ErrInvalidQueryParam) {
		respondError(c, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to get user")
		return
	}

	count, err := h.repo.CountPublicOriginalWorksByAccount(account.ID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to count works")
		return
	}
	works, err := h.repo.ListPublicOriginalWorksByAccount(account.ID, queryInt(c, "limit", 20))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to list works")
		return
	}

	items := make([]map[string]any, len(works))
	for i, work := range works {
		items[i] = formatWork(work)
	}
	data := formatPublicUserAccount(*account)
	data["public_work_count"] = count
	data["works"] = items
	respondOK(c, data)
}

// PublicWorks returns only the public works for one account.
func (h *AccountHandler) PublicWorks(c *gin.Context) {
	account, err := h.repo.GetPublicUserAccountByHandle(c.Param("handle"))
	if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, database.ErrInvalidQueryParam) {
		respondError(c, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to get user")
		return
	}
	works, err := h.repo.ListPublicOriginalWorksByAccount(account.ID, queryInt(c, "limit", 20))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to list works")
		return
	}
	items := make([]map[string]any, len(works))
	for i, work := range works {
		items[i] = formatWork(work)
	}
	respondOK(c, gin.H{"items": items})
}

func formatUserAccount(account database.UserAccount) map[string]any {
	return map[string]any{
		"id":           account.ID,
		"handle":       account.Handle,
		"display_name": account.DisplayName,
		"email":        account.Email,
		"bio":          account.Bio,
		"avatar_url":   account.AvatarURL,
		"website_url":  account.WebsiteURL,
		"status":       account.Status,
		"profile_path": "/u/" + account.Handle,
		"created_at":   account.CreatedAt,
		"updated_at":   account.UpdatedAt,
	}
}

func formatPublicUserAccount(account database.UserAccount) map[string]any {
	data := formatUserAccount(account)
	delete(data, "email")
	return data
}

// UserPage renders the public personal works page shell.
func UserPage(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(userPageHTML))
}

const userPageHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>&#20010;&#20154;&#20316;&#21697;&#39029; - &#35799;&#35789;&#26354;&#36171;</title>
  <style>
    :root{--ink:#211814;--muted:#766757;--paper:#fffaf0;--line:#dfcbb0;--red:#a8322a;--shadow:0 18px 55px rgba(83,48,18,.10);--song:"Noto Serif SC","Source Han Serif SC","Songti SC",STSong,serif;--sans:-apple-system,BlinkMacSystemFont,"PingFang SC","Microsoft YaHei",sans-serif}
    *{box-sizing:border-box}body{margin:0;background:radial-gradient(circle at 18% 0,#fffaf0 0,#f6ead7 42%,#efe1cd 100%);color:var(--ink);font-family:var(--sans);line-height:1.72}.wrap{max-width:1040px;margin:0 auto;padding:38px 22px 72px}.hero,.work{background:rgba(255,250,240,.82);border:1px solid var(--line);border-radius:28px;box-shadow:var(--shadow)}.hero{padding:28px;margin-bottom:18px;display:flex;gap:20px;align-items:flex-start;justify-content:space-between}.avatar{width:82px;height:82px;border-radius:26px;background:var(--red);color:white;display:grid;place-items:center;font:900 38px var(--song);overflow:hidden;flex:0 0 auto}.avatar img{width:100%;height:100%;object-fit:cover}.meta{flex:1}h1,h2,p{margin:0}.handle{color:var(--muted);font-weight:800}.bio{margin-top:10px;color:#4d4035}.actions{display:flex;gap:10px;flex-wrap:wrap;margin-top:14px}.btn{display:inline-flex;align-items:center;justify-content:center;border:1px solid var(--line);border-radius:999px;padding:9px 14px;text-decoration:none;color:var(--ink);background:#fffaf2;font-weight:900}.btn.primary{background:var(--red);color:white;border-color:transparent}.grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:16px}.work{padding:22px}.work h2{font:800 24px var(--song)}.content{white-space:pre-line;font:21px/1.95 var(--song);letter-spacing:.05em;margin:14px 0;color:#2f241e}.tiny{color:var(--muted);font-size:13px}.empty{padding:34px;text-align:center;color:var(--muted)}@media(max-width:760px){.hero{display:block}.avatar{margin-bottom:14px}.grid{grid-template-columns:1fr}.content{font-size:19px}}
  </style>
</head>
<body>
  <main class="wrap">
    <section class="hero">
      <div class="avatar" id="avatar">&#35799;</div>
      <div class="meta">
        <h1 id="name">&#20010;&#20154;&#20316;&#21697;&#39029;</h1>
        <p class="handle" id="handle">@loading</p>
        <p class="bio" id="bio">&#27491;&#22312;&#35835;&#21462;&#20844;&#24320;&#20316;&#21697;...</p>
        <div class="actions">
          <a class="btn primary" id="consoleLink" href="/console">&#36827;&#20837;&#25511;&#21046;&#21488;</a>
          <a class="btn" id="apiLink" href="#">&#20844;&#24320; API</a>
          <a class="btn" id="siteLink" href="#" target="_blank" rel="noopener" style="display:none">&#20010;&#20154;&#32593;&#31449;</a>
        </div>
      </div>
    </section>
    <div class="grid" id="works"></div>
  </main>
  <script>
  (function(){
    function $(id){return document.getElementById(id)}
    function esc(s){return String(s==null?"":s).replace(/[&<>"']/g,function(c){return {"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]})}
    function apiBase(){var p=location.pathname||"/";var m=p.match(/^(.*)\/(?:u|users)\/[^/]+\/?$/);return m?(m[1]||""):""}
    function handle(){var parts=(location.pathname||"").split("/").filter(Boolean);return decodeURIComponent(parts[parts.length-1]||"")}
    function lines(content){return String(content||"").split(/\n+/).filter(Boolean).slice(0,8).join("\n")}
    function fmtDate(s){if(!s)return "";try{return new Date(s).toLocaleDateString()}catch(e){return ""}}
    var h=handle(), base=apiBase();
    $("consoleLink").href=base+"/console";
    $("apiLink").href=base+"/api/v1/public/users/"+encodeURIComponent(h);
    fetch(base+"/api/v1/public/users/"+encodeURIComponent(h)+"?limit=50").then(function(r){if(!r.ok)throw new Error("not found");return r.json()}).then(function(json){
      var data=json.data||json, works=data.works||[];
      $("name").textContent=data.display_name||"\u672a\u547d\u540d\u4f5c\u8005";
      $("handle").textContent="@"+(data.handle||h)+" \u00b7 \u516c\u5f00\u4f5c\u54c1 "+(data.public_work_count||works.length)+" \u7bc7";
      $("bio").textContent=data.bio||"\u8fd9\u4e2a\u4f5c\u8005\u8fd8\u6ca1\u6709\u586b\u5199\u7b80\u4ecb\u3002";
      if(data.avatar_url){$("avatar").innerHTML='<img alt="avatar" src="'+esc(data.avatar_url)+'">'}else{$("avatar").textContent=(data.display_name||"\u8bd7").slice(0,1)}
      if(data.website_url){$("siteLink").href=data.website_url;$("siteLink").style.display=""}
      $("works").innerHTML=works.length?works.map(function(w){
        return '<article class="work"><p class="tiny">'+esc(w.work_type||"poem")+' \u00b7 '+esc(fmtDate(w.published_at||w.created_at))+'</p><h2>'+esc(w.title||"\u672a\u547d\u540d\u4f5c\u54c1")+'</h2><div class="content">'+esc(lines(w.content||""))+'</div><p class="tiny">'+esc(w.description||"")+'</p><div class="actions"><a class="btn" href="'+base+'/api/v1/public/works/'+encodeURIComponent(w.work_code)+'">\u67e5\u770b JSON</a></div></article>'
      }).join(""):'<section class="work empty">\u6682\u65e0\u516c\u5f00\u4f5c\u54c1\u3002</section>';
    }).catch(function(){
      $("handle").textContent="@"+h;
      $("bio").textContent="\u6ca1\u6709\u627e\u5230\u8fd9\u4e2a\u4f5c\u8005\uff0c\u6216\u4f5c\u8005\u8fd8\u6ca1\u6709\u516c\u5f00\u4f5c\u54c1\u3002";
      $("works").innerHTML='<section class="work empty">\u4e2a\u4eba\u4f5c\u54c1\u9875\u6682\u65f6\u4e0d\u53ef\u7528\u3002</section>';
    });
  })();
  </script>
</body>
</html>`
