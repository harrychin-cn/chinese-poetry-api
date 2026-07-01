package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ConsolePage returns the built-in customer console for API key, Qanlo billing,
// poem search, and prompt-based image workflow.
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

const consoleHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>AI 诗词知识库 · 诗词画曲赋</title>
  <style>
    :root{--font:-apple-system,BlinkMacSystemFont,"SF Pro Text","PingFang SC","Microsoft YaHei",sans-serif;--serif:"Noto Serif SC","Songti SC","STSong",serif;--bg:#f4eadb;--panel:#fffaf0;--paper:#fffdf7;--ink:#17120d;--muted:#756a5b;--line:#e3d1b9;--soft:#f7eddd;--red:#a8322a;--red2:#7f211c;--green:#0b7a4f;--gold:#bd8a37;--shadow:0 24px 70px rgba(93,56,20,.14);--radius:28px}
    body[data-theme="jade"]{--bg:#edf7f1;--panel:#f7fffb;--paper:#fff;--ink:#102019;--muted:#607169;--line:#cde1d7;--soft:#e3f1ea;--red:#0d7a4e;--red2:#075a39;--green:#0d7a4e;--gold:#89a85d;--shadow:0 24px 70px rgba(25,91,58,.13)}
    body[data-theme="night"]{--bg:#11100d;--panel:#191612;--paper:#201d17;--ink:#f7eed9;--muted:#bbad90;--line:#40372a;--soft:#2b251a;--red:#d45a48;--red2:#9d362d;--green:#67c58f;--gold:#e0b66b;--shadow:0 24px 90px rgba(0,0,0,.45);color-scheme:dark}
    *{box-sizing:border-box}html,body{height:100%;margin:0}body{font-family:var(--font);color:var(--ink);background:radial-gradient(circle at 78% 4%,rgba(189,138,55,.2),transparent 32%),linear-gradient(135deg,var(--bg),color-mix(in srgb,var(--paper) 58%,var(--bg)));overflow:hidden}button,input,select,textarea{font:inherit}button{cursor:pointer;border:0}a{text-decoration:none;color:inherit}.app{height:100vh;display:grid;grid-template-columns:430px minmax(0,1fr)}.left{height:100vh;overflow:auto;padding:24px 22px;background:color-mix(in srgb,var(--panel) 90%,transparent);border-right:1px solid var(--line)}.right{height:100vh;overflow:auto;padding:26px 30px 32px}.brand{display:flex;align-items:center;gap:14px;margin-bottom:20px}.seal{width:56px;height:56px;border-radius:18px;background:linear-gradient(145deg,var(--red),var(--red2));color:white;display:grid;place-items:center;font:900 28px var(--serif);box-shadow:var(--shadow)}h1{margin:0;font-size:26px;letter-spacing:-.04em}.brand p{margin:4px 0 0;color:var(--muted);font-size:13px}.themes{display:grid;grid-template-columns:repeat(3,1fr);gap:8px;margin:0 0 16px}.themes button{border:1px solid var(--line);border-radius:999px;background:var(--paper);color:var(--muted);padding:9px 10px;font-weight:850}.themes button.active{background:var(--ink);color:var(--paper);border-color:var(--ink)}.card{background:color-mix(in srgb,var(--paper) 92%,transparent);border:1px solid var(--line);border-radius:var(--radius);box-shadow:var(--shadow);padding:18px;margin-bottom:14px}.title{display:flex;align-items:center;justify-content:space-between;gap:10px;margin-bottom:13px}.title h2{font-size:18px;margin:0;letter-spacing:-.02em}.tag{font-size:12px;color:var(--green);background:color-mix(in srgb,var(--green) 12%,transparent);border:1px solid color-mix(in srgb,var(--green) 26%,transparent);border-radius:999px;padding:6px 9px;font-weight:900}.muted{font-size:13px;color:var(--muted);line-height:1.7}.steps{display:grid;gap:9px}.step{display:grid;grid-template-columns:34px 1fr;gap:10px;align-items:center;border:1px dashed var(--line);border-radius:18px;background:color-mix(in srgb,var(--soft) 35%,transparent);padding:12px}.num{width:34px;height:34px;border-radius:50%;display:grid;place-items:center;background:var(--red);color:#fff;font-weight:950}.step b{display:block}.field{display:grid;gap:7px;margin:0 0 11px}label{font-size:12px;color:var(--muted);font-weight:900}input,textarea,select{width:100%;border:1px solid var(--line);border-radius:15px;background:color-mix(in srgb,var(--paper) 88%,transparent);color:var(--ink);padding:12px 13px;outline:none}textarea{min-height:96px;resize:vertical;line-height:1.62}input:focus,textarea:focus,select:focus{border-color:var(--red);box-shadow:0 0 0 4px color-mix(in srgb,var(--red) 12%,transparent)}.row{display:flex;gap:9px;align-items:center}.grid2{display:grid;grid-template-columns:1fr 1fr;gap:10px}.btn{display:inline-flex;align-items:center;justify-content:center;gap:8px;border-radius:15px;padding:12px 14px;background:linear-gradient(145deg,var(--red),var(--red2));color:white;font-weight:950;box-shadow:0 14px 28px color-mix(in srgb,var(--red) 21%,transparent)}.btn.secondary{background:var(--soft);color:var(--ink);border:1px solid var(--line);box-shadow:none}.btn.ghost{background:transparent;color:var(--red);border:1px solid var(--line);box-shadow:none}.btn.green{background:linear-gradient(145deg,var(--green),color-mix(in srgb,var(--green) 70%,black));box-shadow:0 14px 28px color-mix(in srgb,var(--green) 18%,transparent)}.btn.full{width:100%}.metrics{display:grid;grid-template-columns:1fr 1fr;gap:10px;margin-top:12px}.metric{border:1px solid var(--line);border-radius:18px;background:color-mix(in srgb,var(--soft) 42%,transparent);padding:12px}.metric span{display:block;color:var(--muted);font-size:12px}.metric strong{font-size:24px;letter-spacing:-.04em}.chips{display:flex;flex-wrap:wrap;gap:8px;margin:10px 0}.chip{border:1px solid var(--line);border-radius:999px;background:var(--paper);color:var(--ink);padding:9px 12px;font-weight:850}.chip.active{background:var(--red);border-color:var(--red);color:#fff}.switchline{display:flex;align-items:center;justify-content:space-between;gap:12px;border:1px solid var(--line);border-radius:18px;background:color-mix(in srgb,var(--soft) 38%,transparent);padding:12px;margin:0 0 10px}.switchline input{display:none}.toggle{width:54px;height:30px;border-radius:999px;background:#c7b9a5;position:relative;flex:0 0 auto}.toggle:before{content:"";position:absolute;width:24px;height:24px;border-radius:50%;background:white;left:3px;top:3px;transition:.2s;box-shadow:0 3px 10px rgba(0,0,0,.18)}.switchline input:checked+.toggle{background:var(--red)}.switchline input:checked+.toggle:before{transform:translateX(24px)}details{border:1px solid var(--line);border-radius:20px;background:color-mix(in srgb,var(--paper) 82%,transparent);margin-top:12px;overflow:hidden}summary{padding:14px 15px;cursor:pointer;font-weight:950}.detail{padding:0 15px 15px}.topbar{display:flex;align-items:center;justify-content:space-between;gap:16px;margin-bottom:18px}.topbar h2{font-size:28px;margin:0;letter-spacing:-.04em}.tools{display:flex;gap:10px;flex-wrap:wrap}.tool{border:1px solid var(--line);background:color-mix(in srgb,var(--paper) 86%,transparent);border-radius:999px;padding:10px 13px;font-weight:850}.stage{display:grid;grid-template-columns:minmax(320px,38%) minmax(0,1fr);gap:16px;min-height:690px}.sheet{border:1px solid var(--line);border-radius:30px;background:linear-gradient(145deg,color-mix(in srgb,var(--paper) 94%,transparent),color-mix(in srgb,var(--soft) 42%,transparent));box-shadow:var(--shadow);overflow:hidden}.poem{padding:30px;display:flex;flex-direction:column;position:relative}.poem:after{content:"";position:absolute;right:19px;bottom:19px;width:58px;height:58px;border:2px solid color-mix(in srgb,var(--red) 44%,transparent);border-radius:8px;opacity:.36}.badge{align-self:flex-start;border:1px solid var(--line);border-radius:999px;padding:7px 10px;color:var(--red);font-size:12px;font-weight:950;background:color-mix(in srgb,var(--paper) 80%,transparent)}.poem-title{font:900 clamp(34px,4vw,64px)/1.1 var(--serif);letter-spacing:-.07em;margin:24px 0 12px;white-space:pre-line}.poem-meta{color:var(--muted);font-weight:900}.poem-lines{margin-top:28px;font:500 clamp(22px,1.75vw,34px)/2.05 var(--serif);letter-spacing:.06em;white-space:pre-line}.actions{margin-top:auto;display:flex;gap:10px;flex-wrap:wrap;padding-top:24px}.image{display:grid;grid-template-rows:auto 1fr auto}.image-head{display:flex;align-items:center;justify-content:space-between;gap:12px;border-bottom:1px solid var(--line);padding:18px 20px}.image-head h3{font-size:19px;margin:0}.canvas{min-height:460px;display:grid;place-items:center;position:relative;overflow:hidden;padding:24px;background:radial-gradient(circle at 70% 18%,color-mix(in srgb,var(--gold) 22%,transparent),transparent 34%),linear-gradient(145deg,color-mix(in srgb,var(--paper) 92%,transparent),color-mix(in srgb,var(--soft) 35%,transparent))}.mountain{position:absolute;inset:auto 0 0;height:62%;opacity:.42;background:linear-gradient(to top,color-mix(in srgb,var(--ink) 8%,transparent),transparent 72%);clip-path:polygon(0 76%,12% 55%,24% 69%,37% 38%,52% 63%,67% 33%,83% 66%,100% 42%,100% 100%,0 100%)}.empty{text-align:center;max-width:520px;position:relative;z-index:1}.icon{width:94px;height:94px;margin:0 auto 18px;border:2px solid color-mix(in srgb,var(--gold) 70%,transparent);border-radius:28px;display:grid;place-items:center;color:var(--gold);font-size:42px;background:color-mix(in srgb,var(--paper) 72%,transparent)}.empty h3{font:800 28px var(--serif);margin:0 0 10px}.prompt{border-top:1px solid var(--line);padding:18px 20px;background:color-mix(in srgb,var(--paper) 86%,transparent)}.prompt textarea{min-height:116px;font-size:14px}.recommend{margin-top:16px}.recommend-head{display:flex;align-items:center;justify-content:space-between;margin-bottom:10px}.result-list{display:grid;grid-template-columns:repeat(5,minmax(150px,1fr));gap:10px}.rec{text-align:left;min-height:72px;border:1px solid var(--line);border-radius:17px;background:color-mix(in srgb,var(--paper) 84%,transparent);padding:12px;color:var(--ink)}.rec b{display:block;margin-bottom:5px}.raw{margin-top:16px}pre{margin:0;white-space:pre-wrap;word-break:break-word;max-height:360px;overflow:auto;background:#15110d;color:#f8ecd8;border-radius:16px;padding:16px;font-size:12px;line-height:1.55}.toast{position:fixed;right:24px;bottom:24px;z-index:99;background:var(--paper);border:1px solid var(--line);box-shadow:var(--shadow);border-radius:18px;padding:14px 16px;max-width:430px;transform:translateY(18px);opacity:0;pointer-events:none;transition:.22s}.toast.show{transform:translateY(0);opacity:1}.hidden{display:none}@media(max-width:1180px){body{overflow:auto}.app{height:auto;min-height:100vh;grid-template-columns:1fr}.left{height:auto;overflow:visible;border-right:0;border-bottom:1px solid var(--line)}.right{height:auto;overflow:visible}.stage{grid-template-columns:1fr}.result-list{grid-template-columns:repeat(2,minmax(0,1fr))}}@media(max-width:680px){.left,.right{padding:16px}.topbar{align-items:flex-start;flex-direction:column}.stage{min-height:0}.grid2,.metrics{grid-template-columns:1fr}.result-list{grid-template-columns:1fr}.poem-title{font-size:38px}.poem-lines{font-size:24px}.row{flex-wrap:wrap}.btn{width:100%}}
  </style>
</head>
<body data-theme="paper">
  <div class="hidden" aria-hidden="true">POST /api/v1/keys /api/v1/billing/status /api/v1/knowledge/recall /api/v1/feedback</div>
  <div class="app">
    <aside class="left">
      <div class="brand"><div class="seal">诗</div><div><h1>诗词画曲赋</h1><p>AI 诗词知识库 API 控制台</p></div></div>
      <div class="themes"><button data-theme="paper" onclick="setTheme('paper')">宣纸</button><button data-theme="jade" onclick="setTheme('jade')">青玉</button><button data-theme="night" onclick="setTheme('night')">夜色</button></div>

      <section class="card">
        <div class="title"><h2>搜索与生成</h2><span id="serviceState" class="tag">AI 知识库召回</span></div>
        <div class="field"><label>直接说你想找什么</label><textarea id="searchInput" placeholder="例如：找适合文旅山水宣传的诗">找适合文旅山水宣传的诗</textarea></div>
        <div class="chips" id="scenarioChips"></div>
        <div class="switchline"><div><b>同时准备意境图 Prompt</b><div class="muted">按诗句意象、氛围、时令和色彩生成可复制提示词。</div></div><label><input id="imageEnabled" type="checkbox" checked onchange="syncImageMode()" /><span class="toggle"></span></label></div>
        <div class="grid2"><div class="field"><label>画风</label><select id="imageStyle" onchange="refreshPrompt()"><option>古风水墨</option><option>宋画工笔</option><option>宣纸淡彩</option><option>国风插画</option></select></div><div class="field"><label>比例</label><select id="imageRatio" onchange="refreshPrompt()"><option>1:1 方图</option><option>16:9 横图</option><option>3:4 竖图</option></select></div></div>
        <div class="row"><button class="btn full" onclick="searchPoems(false)">搜索诗词</button><button class="btn secondary full" onclick="searchPoems(true)">生成意境图</button></div>
        <p class="muted">可不填 Key：首次搜索会自动创建试用 Key。当前是方案 A：返回诗词和 Prompt；真图需要配置 IMAGE_MODEL=gpt-image-2。</p>
      </section>

      <section class="card">
        <div class="title"><h2>访问 Key 与额度</h2><span id="bindState" class="tag">本地额度</span></div>
        <div class="field"><label>客户名称</label><input id="customerName" value="demo customer" /></div>
        <div class="field"><label>当前 API Key</label><input id="apiKey" placeholder="可不填：首次搜索会自动创建试用 Key" /></div>
        <div class="row"><button class="btn" onclick="createKey(false)">创建 API Key</button><button class="btn secondary" onclick="saveKey()">保存</button><button class="btn ghost" onclick="clearKey()">清空</button></div>
        <div class="metrics"><div class="metric"><span>今日用量</span><strong id="todayUsage">--</strong></div><div class="metric"><span>每日额度</span><strong id="dailyLimit">--</strong></div></div>
        <p class="muted">这里的用量是本服务 API Key 调用次数，不等于 Qanlo 大模型消耗。</p>
      </section>

      <details>
        <summary>Qanlo 绑定 / 充值</summary>
        <div class="detail">
          <p id="qanloNotice" class="muted">创建 Key 后可跳转绑定或充值。</p>
          <div class="row"><button class="btn" onclick="provisionQanlo()">创建 / 绑定 Qanlo</button><button class="btn green" onclick="openRecharge()">打开充值页</button></div>
          <button class="btn secondary full" style="margin-top:10px" onclick="refreshStatus()">刷新状态</button>
        </div>
      </details>

      <details>
        <summary>高级查询 / 客户反馈</summary>
        <div class="detail">
          <div class="grid2"><div class="field"><label>作者</label><input id="exactAuthor" placeholder="李白" /></div><div class="field"><label>关键词</label><input id="exactKeyword" value="月" /></div></div>
          <div class="field"><label>每页数量</label><input id="pageSize" value="3" /></div>
          <div class="row"><button class="btn secondary" onclick="exactSearch()">精确查询</button><button class="btn secondary" onclick="loadMyUsage()">查看用量</button></div>
          <div class="field"><label>反馈标题</label><input id="feedbackTitle" placeholder="例如：缺少某首诗" /></div>
          <div class="field"><label>反馈内容</label><textarea id="feedbackContent" placeholder="描述缺失数据、接口问题或想要的功能"></textarea></div>
          <button class="btn secondary full" onclick="submitFeedback()">提交客户反馈</button>
        </div>
      </details>
    </aside>

    <main class="right">
      <div class="topbar">
        <div><h2>诗词与图像结果</h2><p class="muted">左边负责输入、额度、充值和开关；右边整块空间展示诗词、候选结果和生图 Prompt。</p></div>
        <div class="tools"><a class="tool" href="/pricing" target="_blank">价格套餐</a><a class="tool" href="/docs" target="_blank">开发文档</a><span class="tool" id="statusPill">等待查询</span></div>
      </div>

      <section class="stage">
        <article class="sheet poem">
          <div class="badge" id="poemBadge">等待查询</div>
          <div class="poem-title" id="poemTitle">选择场景
或输入一句需求</div>
          <div class="poem-meta" id="poemMeta">诗词结果会显示在这里</div>
          <div class="poem-lines" id="poemContent">例如：找中秋月亮诗句</div>
          <div class="actions"><button class="btn secondary" onclick="copyPoem()">复制诗词</button><button class="btn ghost" onclick="clearResult()">清空结果</button></div>
        </article>
        <article class="sheet image">
          <div class="image-head"><div><h3>意境图预览 / Prompt</h3><p class="muted">可选择生图，也可只查诗。</p></div><span class="tag" id="imageModeTag">Prompt 模式</span></div>
          <div class="canvas" id="imageCanvas"><div class="mountain"></div><div class="empty"><div class="icon">画</div><h3>尚未生成图片</h3><p class="muted">点击“生成意境图”后，会根据诗词正文提取意象和氛围，不再随意配不相关图片。</p></div></div>
          <div class="prompt"><label>图片提示词 Prompt</label><textarea id="imagePrompt" readonly>等待诗词结果生成 Prompt。</textarea><div class="row" style="margin-top:10px"><button class="btn secondary" onclick="copyPrompt()">复制提示词</button><button class="btn ghost" onclick="prepareImage(true)">刷新 Prompt</button></div></div>
        </article>
      </section>

      <section class="recommend">
        <div class="recommend-head"><h3>候选结果</h3><span class="muted">点任意候选可切换展示</span></div>
        <div class="result-list" id="resultList"></div>
      </section>

      <details class="raw">
        <summary>接口原始返回</summary>
        <pre id="rawOutput">暂无返回。</pre>
      </details>
    </main>
  </div>
  <div class="toast" id="toast"></div>

  <script>
    var rawData=null,currentPoem=null,currentScenario='山水文旅';
    var scenarios=[['中秋月亮','中秋、月亮、团圆、望月怀人'],['毕业送别','毕业、离别、送别、同窗分别'],['山水文旅','山水、旅途、风景、城市宣传'],['思乡怀人','故乡、乡愁、旅居、怀人'],['春日海报','春天、春风、花鸟、海报文案']];
    function $(id){return document.getElementById(id)}
    function apiKey(){return ($('apiKey').value||'').trim()}
    function setRaw(d){rawData=d;$('rawOutput').textContent=typeof d==='string'?d:JSON.stringify(d,null,2)}
    function toast(msg){var el=$('toast');el.textContent=msg;el.classList.add('show');clearTimeout(window.__toastTimer);window.__toastTimer=setTimeout(function(){el.classList.remove('show')},2800)}
    function setTheme(t){document.body.dataset.theme=t;localStorage.setItem('poetry_console_theme',t);document.querySelectorAll('.themes button').forEach(function(b){b.classList.toggle('active',b.dataset.theme===t)})}
    async function request(path,opt){opt=opt||{};opt.headers=Object.assign({'Content-Type':'application/json'},opt.headers||{});var r=await fetch(path,opt);var text=await r.text();var data=text;try{data=text?JSON.parse(text):{}}catch(e){}setRaw(data);if(!r.ok){throw new Error((data&&data.error)||text||('HTTP '+r.status))}return data}
    function payload(d){return d&&typeof d==='object'&&Object.prototype.hasOwnProperty.call(d,'data')?d.data:d}
    function saveKey(){localStorage.setItem('poetry_api_key',apiKey());toast('已保存当前 Key');refreshStatus()}
    function clearKey(){$('apiKey').value='';localStorage.removeItem('poetry_api_key');$('todayUsage').textContent='--';$('dailyLimit').textContent='--';toast('已清空本地 Key')}
    async function createKey(silent){var req={name:($('customerName').value||'demo customer'),tier:'trial',daily_limit:1000,notes:'console trial key'};var d=await request('/api/v1/keys',{method:'POST',body:JSON.stringify(req)});var body=payload(d)||{};var key=body.key||body.api_key||body.token;if(!key)throw new Error('创建 Key 成功但响应里没有 key 字段');$('apiKey').value=key;localStorage.setItem('poetry_api_key',key);if(!silent)toast('API Key 已创建');await refreshStatus();return key}
    async function ensureKey(){var k=apiKey();if(k){localStorage.setItem('poetry_api_key',k);return k}toast('未填写 Key，正在自动创建试用 Key');return await createKey(true)}
    async function refreshStatus(){var k=apiKey();if(!k)return;try{var d=await request('/api/v1/keys/current',{headers:{'X-API-Key':k}});var body=payload(d)||{};var item=body.key||body;$('todayUsage').textContent=item.today_usage!=null?item.today_usage:'0';$('dailyLimit').textContent=item.daily_limit!=null?item.daily_limit:'--';$('bindState').textContent=(item.enabled===false?'已停用':'配置可用');var b=payload(await request('/api/v1/billing/status',{headers:{'X-API-Key':k}}))||{};$('qanloNotice').textContent='Qanlo 状态：'+((b.qanlo&&b.qanlo.status)||b.status||'可用')+'；configured='+(b.configured!=null?b.configured:'--')}catch(e){toast(e.message)}}
    async function provisionQanlo(){try{var k=await ensureKey();var d=payload(await request('/api/v1/billing/qanlo/provision',{method:'POST',headers:{'X-API-Key':k},body:'{}'}))||{};var url=d.url||d.connect_url||d.bind_url||d.recharge_url||d.compact_recharge_url;if(url){window.open(url,'_blank');toast('已打开 Qanlo 绑定页')}else{toast('已生成绑定信息，查看右侧原始返回')}}catch(e){toast(e.message)}}
    async function openRecharge(){try{var k=await ensureKey();var d=payload(await request('/api/v1/billing/qanlo/recharge-session',{method:'POST',headers:{'X-API-Key':k},body:'{}'}))||{};var url=d.url||d.recharge_url||d.compact_recharge_url;if(url){window.open(url,'_blank');toast('已打开 Qanlo 充值页')}else{toast('已生成充值信息，查看右侧原始返回')}}catch(e){toast(e.message)}}
    function renderChips(){var box=$('scenarioChips');box.innerHTML='';scenarios.forEach(function(s){var b=document.createElement('button');b.className='chip'+(s[0]===currentScenario?' active':'');b.textContent=s[0];b.onclick=function(){currentScenario=s[0];$('searchInput').value='找'+s[0]+'相关诗句';renderChips()};box.appendChild(b)})}
    function listFrom(d){if(!d)return[];if(Array.isArray(d))return d;if(Array.isArray(d.items))return d.items;if(Array.isArray(d.results))return d.results;if(Array.isArray(d.poems))return d.poems;if(Array.isArray(d.data))return d.data;if(d.poem)return[d.poem];return[]}
    function poemOf(x){return x&&x.poem?x.poem:x}
    function cleanTitle(t){t=String(t||'').trim().replace(/\s+/g,' ');if(!t)return'';if(t.length>24||/[，。；！？]/.test(t)){t=t.split(/[，。；！？\n]/)[0].slice(0,16)}return t||'无题'}
    function displayTitle(p){p=poemOf(p)||{};return cleanTitle(p.title||p.title_zh||p.name)||'无题'}
    function poemLines(p){p=poemOf(p)||{};var c=p.content||p.paragraphs||p.text||p.body||'';if(Array.isArray(c))return c.join('\n');if(typeof c==='string')return c.replace(/\\n/g,'\n').split(/\n|。|！|？/).filter(Boolean).slice(0,8).join('\n');return''}
    function dynastyName(p){p=poemOf(p)||{};var d=p.dynasty||p.dynasty_name||p.dynastyName||{};return typeof d==='string'?d:(d.name||'')}
    function authorName(p){p=poemOf(p)||{};var a=p.author||p.author_name||p.authorName||{};return typeof a==='string'?a:(a.name||'')}
    function renderPoem(x){var p=poemOf(x)||{};currentPoem=p;$('poemTitle').textContent=displayTitle(p);$('poemMeta').textContent=[dynastyName(p),authorName(p)].filter(Boolean).join(' · ')||'作者待补充';$('poemContent').textContent=poemLines(p)||'暂无正文';$('poemBadge').textContent=(x&&x.category)||currentScenario||'诗词';$('statusPill').textContent='已返回结果';refreshPrompt();syncImageMode()}
    function renderResults(d,wantImage){var arr=listFrom(d);var box=$('resultList');box.innerHTML='';if(!arr.length){$('statusPill').textContent='未匹配到结果';toast('未匹配到结果，换个说法再试');return}arr.slice(0,10).forEach(function(x,i){var p=poemOf(x)||{};var b=document.createElement('button');b.className='rec';b.innerHTML='<b>'+(i+1)+'. '+escapeHtml(displayTitle(p))+'</b><span class="muted">'+escapeHtml([dynastyName(p),authorName(p)].filter(Boolean).join(' · ')||'诗词候选')+'</span>';b.onclick=function(){renderPoem(x)};box.appendChild(b)});renderPoem(arr[0]);if(wantImage){$('imageEnabled').checked=true;prepareImage(true)}refreshStatus()}
    function escapeHtml(s){return String(s||'').replace(/[&<>"]/g,function(c){return {'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]})}
    async function searchPoems(wantImage){try{var k=await ensureKey();var q=($('searchInput').value||'').trim()||'找中秋月亮诗句';$('statusPill').textContent='查询中';var path='/api/v1/knowledge/recall?q='+encodeURIComponent(q)+'&scenario='+encodeURIComponent(currentScenario)+'&page_size=5';var d=await request(path,{headers:{'X-API-Key':k}});renderResults(d,wantImage)}catch(e){$('statusPill').textContent='查询失败';toast(e.message)}}
    async function exactSearch(){try{var k=await ensureKey();var a=$('exactAuthor').value.trim(),q=$('exactKeyword').value.trim(),s=$('pageSize').value.trim()||'3',p='/api/v1/poems/query?page_size='+encodeURIComponent(s);if(a)p+='&author='+encodeURIComponent(a);if(q)p+='&q='+encodeURIComponent(q)+'&search_in=content';var d=await request(p,{headers:{'X-API-Key':k}});renderResults(d,false)}catch(e){toast(e.message)}}
    async function loadMyUsage(){try{var k=await ensureKey();await request('/api/v1/usage/daily',{headers:{'X-API-Key':k}});toast('用量已加载')}catch(e){toast(e.message)}}
    async function submitFeedback(){try{var k=await ensureKey();await request('/api/v1/feedback',{method:'POST',headers:{'X-API-Key':k},body:JSON.stringify({type:'data',title:$('feedbackTitle').value||'客户反馈',content:$('feedbackContent').value||'',contact:''})});toast('客户反馈已提交')}catch(e){toast(e.message)}}
    function hasAny(text,words){return words.filter(function(w){return text.indexOf(w)>=0})}
    function inferMood(text){var quiet=hasAny(text,['月','夜','清','寒','霜','孤','寂','空','梦','愁','别','归','远']);var bright=hasAny(text,['春','花','日','晴','香','红','绿','新','酒','欢']);var vast=hasAny(text,['山','江','水','海','天','云','舟','风','雪','边','塞']);if(quiet.length>=2)return'清冷、含蓄、安静、带一点思念';if(bright.length>=2)return'明亮、温润、有生机';if(vast.length>=2)return'辽阔、疏朗、有行旅感';return'贴合原诗情绪，克制含蓄，不夸张'}
    function extractImagery(text){var map=[['月','明月/夜色'],['夜','夜景'],['江','江水'],['水','流水'],['山','远山'],['云','云气'],['风','微风'],['雨','细雨'],['雪','落雪'],['花','花枝'],['柳','柳岸'],['竹','竹影'],['梅','梅花'],['荷','荷花'],['舟','小舟'],['楼','楼阁'],['酒','酒盏'],['灯','灯火'],['人','人物剪影'],['马','行旅马匹'],['鸟','飞鸟']];var out=[];map.forEach(function(x){if(text.indexOf(x[0])>=0&&out.indexOf(x[1])<0)out.push(x[1])});return out.slice(0,6)}
    function buildPrompt(){if(!currentPoem)return'等待诗词结果生成 Prompt。';var style=$('imageStyle').value,ratio=$('imageRatio').value,title=displayTitle(currentPoem),raw=poemLines(currentPoem),lines=raw.replace(/\n/g,'，'),imagery=extractImagery(raw),mood=inferMood(raw);var scene=imagery.length?imagery.join('、'):'从诗句中可见的自然景物和人物关系';return'请严格根据《'+title+'》的诗词意境创作一幅'+style+'图像，画面比例 '+ratio+'。\\n原诗依据：'+lines+'。\\n核心意象：'+scene+'。\\n情绪氛围：'+mood+'。\\n构图要求：不要凭空加入与诗词无关的现代城市、科幻元素、欧美建筑、卡通角色；优先表现诗句中的时间、地点、景物和人物动作。\\n画面风格：中国古典审美、留白、宣纸或绢本质感、色彩克制、光影柔和；如需文字，只保留少量诗句题款，不要水印和乱码。'}
    function refreshPrompt(){$('imagePrompt').value=buildPrompt()}
    function syncImageMode(){if($('imageEnabled').checked){prepareImage(false)}else{prepareTextOnly()}}
    function prepareImage(active){refreshPrompt();$('imageModeTag').textContent='意境 Prompt';$('imageCanvas').innerHTML='<div class="mountain"></div><div class="empty"><div class="icon">画</div><h3>'+(active?'已准备意境图 Prompt':'等待生成')+'</h3><p class="muted">当前不消耗生图额度。Prompt 会绑定原诗正文、核心意象和情绪氛围；接入 IMAGE_API_KEY 后这里可直接显示图片。</p></div>';if(active)toast('已准备意境图 Prompt')}
    function prepareTextOnly(){$('imageModeTag').textContent='仅查诗';$('imageCanvas').innerHTML='<div class="mountain"></div><div class="empty"><div class="icon">诗</div><h3>仅查询诗词</h3><p class="muted">已关闭生图选项，本次只返回诗词内容。</p></div>'}
    function copyText(t){if(navigator.clipboard){navigator.clipboard.writeText(t||'');toast('已复制')}}
    function copyPrompt(){copyText($('imagePrompt').value)}
    function copyPoem(){copyText($('poemTitle').textContent+'\n'+$('poemMeta').textContent+'\n'+$('poemContent').textContent)}
    function clearResult(){rawData=null;currentPoem=null;$('poemBadge').textContent='等待查询';$('poemTitle').textContent='选择场景\n或输入一句需求';$('poemMeta').textContent='诗词结果会显示在这里';$('poemContent').textContent='例如：找中秋月亮诗句';$('resultList').innerHTML='';$('imagePrompt').value='等待诗词结果生成 Prompt。';setRaw('暂无返回。');prepareImage(false)}
    (function init(){setTheme(localStorage.getItem('poetry_console_theme')||'paper');$('apiKey').value=localStorage.getItem('poetry_api_key')||'';renderChips();prepareImage(false);if(apiKey())refreshStatus()})();
  </script>
</body>
</html>`

const docsHTML = `<!doctype html>
<html lang="zh-CN">
<head><meta charset="utf-8" /><meta name="viewport" content="width=device-width, initial-scale=1" /><title>AI 诗词知识库 API 文档</title><style>body{font-family:-apple-system,BlinkMacSystemFont,"PingFang SC","Microsoft YaHei",sans-serif;margin:0;background:#f6efe3;color:#17120d;line-height:1.75}.wrap{max-width:980px;margin:0 auto;padding:38px 22px}.card{background:#fffaf0;border:1px solid #e3d1b9;border-radius:24px;padding:22px;margin:18px 0;box-shadow:0 18px 55px rgba(93,56,20,.1)}code,pre{background:#17120d;color:#fff1d6;border-radius:12px}code{padding:2px 6px}pre{padding:16px;overflow:auto}.btn{display:inline-block;background:#a8322a;color:white;padding:10px 14px;border-radius:999px;text-decoration:none;font-weight:800;margin-right:8px}</style></head>
<body><main class="wrap"><h1>AI 诗词知识库 API 文档</h1><p>面向普通用户和开发者的诗词检索、AI 知识库召回、Qanlo 绑定 / 充值、客户反馈和运营统计入口。</p><p><a class="btn" href="/console">进入控制台</a><a class="btn" href="/pricing">价格套餐</a><a class="btn" href="/openapi.yaml">openapi.yaml</a></p>
<section class="card"><h2>认证方式</h2><p>客户调用使用 <code>X-API-Key</code>。管理员接口使用 <code>X-Admin-Token</code>。</p><pre>curl "http://localhost:1279/api/v1/poems/query?q=月&page_size=3" \
  -H "X-API-Key: cp_live_xxx"</pre></section>
<section class="card"><h2>核心接口</h2><ul><li><code>POST /api/v1/keys</code>：创建 API Key。</li><li><code>GET /api/v1/keys/current</code>：查看当前 Key 和今日用量。</li><li><code>POST /api/v1/billing/qanlo/provision</code>：生成 Qanlo Agent Key 绑定 URL。</li><li><code>POST /api/v1/billing/qanlo/recharge-session</code>：生成 Qanlo 充值 URL。</li><li><code>GET /api/v1/poems/query</code>：诗词复合查询。</li><li><code>GET /api/v1/poems/search/fulltext</code>：全文搜索。</li><li><code>GET /api/v1/knowledge/recall</code>：AI 知识库召回。</li><li><code>POST /api/v1/knowledge/batch</code>：批量知识库召回。</li><li><code>POST /api/v1/feedback</code>：客户反馈。</li></ul></section>
<section class="card"><h2>生图能力规划</h2><p>当前控制台先落地方案 A：诗词 API 返回诗词和 Prompt。方案 B 需要服务器配置 <code>IMAGE_API_KEY</code>、<code>IMAGE_BASE_URL</code>、<code>IMAGE_MODEL=gpt-image-2</code> 后，由 API 直接返回图片。</p></section>
<section class="card"><h2>运营接口</h2><ul><li><code>GET /api/v1/admin/api-keys</code>：管理员查看客户 Key。</li><li><code>PATCH /api/v1/admin/api-keys/:id</code>：调整每日限额、套餐和启停状态。</li><li><code>GET /api/v1/admin/feedback</code>：查看客户反馈。</li><li><code>GET /api/v1/admin/usage/daily</code>：全站每日趋势。</li></ul></section>
<section class="card"><h2>更多接口</h2><p><code>GET /api/v1/billing/status</code> <code>GET /api/v1/admin/enrichment/runs/:run_id/summary</code> <code>GET /api/v1/admin/abuse/blocks</code></p></section>
</main></body></html>`

const pricingHTML = `<!doctype html>
<html lang="zh-CN">
<head><meta charset="utf-8" /><meta name="viewport" content="width=device-width, initial-scale=1" /><title>AI 诗词知识库 API 价格套餐</title><style>body{font-family:-apple-system,BlinkMacSystemFont,"PingFang SC","Microsoft YaHei",sans-serif;margin:0;background:#f6efe3;color:#17120d}.wrap{max-width:1080px;margin:0 auto;padding:40px 22px}.hero{display:flex;justify-content:space-between;gap:20px;align-items:center}.sub{color:#756a5b;line-height:1.75}.plans{display:grid;grid-template-columns:repeat(3,1fr);gap:16px;margin:22px 0}.plan{background:#fffaf0;border:1px solid #e3d1b9;border-radius:26px;padding:22px;box-shadow:0 18px 55px rgba(93,56,20,.1)}.price{font-size:34px;font-weight:900;margin:10px 0}.btn{display:inline-block;background:#a8322a;color:white;padding:12px 16px;border-radius:999px;text-decoration:none;font-weight:900;margin-right:8px}.secondary{background:#eadbc6;color:#17120d}@media(max-width:860px){.hero{display:block}.plans{grid-template-columns:1fr}}</style></head>
<body><main class="wrap"><section class="hero"><div><h1>AI 诗词知识库 API 价格套餐</h1><p class="sub">首版验证价：不自建支付系统，充值和扣费走 QanloAPI；本项目负责诗词知识库、API Key、每日限额、用量统计和运营后台。</p></div><div><a class="btn" href="/console">立即试用</a><a class="btn secondary" href="/docs">查看文档</a></div></section>
<section class="plans"><div class="plan"><h2>免费试用</h2><div class="price">free</div><p>适合个人开发者、演示客户和早期试用。</p><ul><li>每日 100-1000 次测试额度</li><li>诗词检索与知识库召回</li><li>客户反馈入口</li></ul></div><div class="plan"><h2>开发者</h2><div class="price">按量</div><p>适合小程序、网站、文旅内容工具。</p><ul><li>按 QanloAPI 充值链路验证</li><li>可配置每日限额</li><li>支持 /api/v1/billing/qanlo/recharge-session</li></ul></div><div class="plan"><h2>企业版</h2><div class="price">定制</div><p>适合学校、文旅、私有知识库项目。</p><ul><li>私有部署</li><li>定制标签与评测集</li><li>可选 gpt-image-2 生图接口</li></ul></div></section>
<section class="plan"><h2>计费说明</h2><p>可计费接口包括 <code>/api/v1/poems/query</code>、全文搜索、<code>/api/v1/knowledge/recall</code> 和批量召回。<code>/api/v1/usage/daily</code> 用于查看用量，<code>/api/v1/feedback</code> 用于客户反馈；绑定、充值、状态刷新和反馈默认不消耗每日查询额度。</p></section>
</main></body></html>`
