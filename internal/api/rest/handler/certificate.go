package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/palemoky/chinese-poetry-api/internal/database"
)

// CertificateHandler exposes stage-7 work certificates and anchor summaries.
type CertificateHandler struct {
	repo *database.Repository
}

func NewCertificateHandler(repo *database.Repository) *CertificateHandler {
	return &CertificateHandler{repo: repo}
}

// Issue creates or refreshes the current published work certificate.
func (h *CertificateHandler) Issue(c *gin.Context) {
	apiKeyID, ok := currentAPIKeyID(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}
	workID, ok := parseWorkID(c)
	if !ok {
		return
	}
	cert, err := h.repo.IssueWorkCertificate(apiKeyID, workID)
	h.respondCertificate(c, cert, err)
}

// Anchor returns the local blockchain-style anchor summary for the certificate.
func (h *CertificateHandler) Anchor(c *gin.Context) {
	apiKeyID, ok := currentAPIKeyID(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}
	workID, ok := parseWorkID(c)
	if !ok {
		return
	}
	cert, err := h.repo.AnchorWorkCertificate(apiKeyID, workID)
	h.respondCertificate(c, cert, err)
}

// Get returns an owned issued certificate, creating it lazily for eligible work.
func (h *CertificateHandler) Get(c *gin.Context) {
	apiKeyID, ok := currentAPIKeyID(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "api key required")
		return
	}
	workID, ok := parseWorkID(c)
	if !ok {
		return
	}
	cert, err := h.repo.GetWorkCertificate(apiKeyID, workID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		cert, err = h.repo.IssueWorkCertificate(apiKeyID, workID)
	}
	h.respondCertificate(c, cert, err)
}

// PublicGet returns a public certificate by work code, issuing it lazily.
func (h *CertificateHandler) PublicGet(c *gin.Context) {
	cert, err := h.repo.GetOrIssuePublicWorkCertificate(c.Param("code"))
	h.respondCertificate(c, cert, err)
}

func (h *CertificateHandler) respondCertificate(c *gin.Context, cert *database.WorkCertificate, err error) {
	if errors.Is(err, database.ErrInvalidQueryParam) {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respondError(c, http.StatusNotFound, "certificate work not found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to read certificate")
		return
	}
	respondOK(c, formatWorkCertificate(*cert))
}

func formatWorkCertificate(cert database.WorkCertificate) map[string]any {
	return map[string]any{
		"id":                     cert.ID,
		"work_id":                cert.WorkID,
		"api_key_id":             cert.APIKeyID,
		"certificate_code":       cert.CertificateCode,
		"work_code":              cert.WorkCode,
		"work_version":           cert.WorkVersion,
		"title":                  cert.Title,
		"work_type":              cert.WorkType,
		"content_hash":           cert.ContentHash,
		"license_type":           cert.LicenseType,
		"license_version":        cert.LicenseVersion,
		"certificate_hash":       cert.CertificateHash,
		"signature_algorithm":    cert.SignatureAlgorithm,
		"signature":              cert.Signature,
		"certificate_payload":    cert.CertificatePayload,
		"anchor_network":         cert.AnchorNetwork,
		"anchor_status":          cert.AnchorStatus,
		"anchor_hash":            cert.AnchorHash,
		"anchor_tx_id":           cert.AnchorTxID,
		"anchor_payload":         cert.AnchorPayload,
		"status":                 cert.Status,
		"issued_at":              cert.IssuedAt,
		"anchored_at":            cert.AnchoredAt,
		"public_certificate_url": "/certificates/" + cert.WorkCode,
	}
}

// CertificatePage renders the public certificate page shell.
func CertificatePage(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(certificatePageHTML))
}

const certificatePageHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Work Certificate - Qanlo Poetry</title>
  <style>
    :root{--ink:#211814;--muted:#766757;--paper:#fffaf0;--line:#dfcbb0;--red:#a8322a;--green:#1f9d68;--shadow:0 18px 55px rgba(83,48,18,.10);--song:"Noto Serif SC","Source Han Serif SC",STSong,serif;--sans:-apple-system,BlinkMacSystemFont,"PingFang SC","Microsoft YaHei",sans-serif}
    *{box-sizing:border-box}body{margin:0;background:radial-gradient(circle at 20% 0,#fffaf0 0,#f6ead7 42%,#efe1cd 100%);color:var(--ink);font-family:var(--sans);line-height:1.7}.wrap{max-width:980px;margin:0 auto;padding:42px 22px 76px}.card{background:rgba(255,250,240,.86);border:1px solid var(--line);border-radius:28px;box-shadow:var(--shadow);padding:26px;margin:18px 0}.hero{display:grid;grid-template-columns:1fr auto;gap:20px;align-items:start}.seal{width:116px;height:116px;border-radius:28px;background:var(--red);color:white;display:grid;place-items:center;font:900 42px var(--song);box-shadow:0 14px 40px rgba(168,50,42,.24)}h1,h2,p{margin:0}h1{font:900 34px var(--song)}h2{font-size:18px;margin-bottom:10px}.muted{color:var(--muted)}.pill{display:inline-flex;border:1px solid #a7dcb7;background:#e8f6ec;color:var(--green);border-radius:999px;padding:7px 12px;font-weight:900;margin-top:12px}.grid{display:grid;grid-template-columns:1fr 1fr;gap:14px}.field{border:1px solid #eadac5;border-radius:14px;background:rgba(255,255,255,.44);padding:12px;overflow:hidden}.field b{display:block;font-size:12px;color:var(--muted);text-transform:uppercase;letter-spacing:.04em}.mono{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;word-break:break-all}.actions{display:flex;gap:10px;flex-wrap:wrap;margin-top:16px}.btn{display:inline-flex;align-items:center;justify-content:center;border:1px solid var(--line);border-radius:999px;padding:9px 14px;text-decoration:none;color:var(--ink);background:#fffaf2;font-weight:900}.btn.primary{background:var(--red);color:white;border-color:transparent}@media(max-width:720px){.hero{display:block}.seal{margin-top:18px}.grid{grid-template-columns:1fr}h1{font-size:29px}}
  </style>
</head>
<body>
  <main class="wrap">
    <section class="card hero">
      <div>
        <p class="muted">Qanlo Poetry Work Certificate</p>
        <h1 id="title">Loading certificate...</h1>
        <p class="muted" id="subtitle">Verifying public work code.</p>
        <span class="pill" id="status">pending</span>
        <div class="actions"><a class="btn primary" id="jsonLink" href="#">Public JSON</a><a class="btn" id="workLink" href="#">Public work</a><a class="btn" href="/console">Console</a></div>
      </div>
      <div class="seal">CERT</div>
    </section>
    <section class="card"><h2>Certificate signature</h2><div class="grid" id="certGrid"></div></section>
    <section class="card"><h2>Blockchain-style anchor summary</h2><div class="grid" id="anchorGrid"></div></section>
  </main>
  <script>
  (function(){
    function $(id){return document.getElementById(id)}
    function esc(s){return String(s==null?"":s).replace(/[&<>"']/g,function(c){return {"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]})}
    function field(label,value){return '<div class="field"><b>'+esc(label)+'</b><div class="mono">'+esc(value||"-")+'</div></div>'}
    function base(){var p=location.pathname||"/";var m=p.match(/^(.*)\/certificates\/[^/]+\/?$/);return m?(m[1]||""):""}
    function code(){var parts=(location.pathname||"").split("/").filter(Boolean);return decodeURIComponent(parts[parts.length-1]||"").toUpperCase()}
    var b=base(), c=code();
    $("jsonLink").href=b+"/api/v1/public/works/"+encodeURIComponent(c)+"/certificate";
    $("workLink").href=b+"/api/v1/public/works/"+encodeURIComponent(c);
    fetch($("jsonLink").href).then(function(r){if(!r.ok)throw new Error("not found");return r.json()}).then(function(json){
      var d=json.data||json;
      $("title").textContent=d.title||"Untitled work";
      $("subtitle").textContent=(d.certificate_code||"")+" / "+(d.work_code||c);
      $("status").textContent=(d.status||"issued")+" / "+(d.anchor_status||"local_anchored");
      $("certGrid").innerHTML=field("certificate_code",d.certificate_code)+field("work_code",d.work_code)+field("content_hash",d.content_hash)+field("certificate_hash",d.certificate_hash)+field("signature_algorithm",d.signature_algorithm)+field("signature",d.signature)+field("issued_at",d.issued_at)+field("license",(d.license_type||"")+" "+(d.license_version||""));
      $("anchorGrid").innerHTML=field("anchor_network",d.anchor_network)+field("anchor_status",d.anchor_status)+field("anchor_hash",d.anchor_hash)+field("anchor_tx_id",d.anchor_tx_id)+field("anchored_at",d.anchored_at)+field("payload",d.anchor_payload);
    }).catch(function(){
      $("title").textContent="Certificate not found";
      $("subtitle").textContent="The work is not public, not published, or has not accepted the license.";
      $("status").textContent="not found";
      $("certGrid").innerHTML=field("work_code",c);
      $("anchorGrid").innerHTML=field("status","missing");
    });
  })();
  </script>
</body>
</html>`
