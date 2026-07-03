# 创始人自测使用手册

> 适用情况：暂时没有真实外部试用客户时，先由你自己验证产品能不能用、流程顺不顺、结果有没有价值。
>
> 结论先说清楚：创始人自测可以证明“本地产品可用”，但不能伪装成真实商业验证。真实商业验证以后仍需要外部用户记录。

## 1. 一键启动本地产品

先打开 Docker Desktop，然后在 PowerShell 里执行：

```powershell
Set-Location "G:\项目\唐诗宋词数据库API\chinese-poetry-api"
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\start_local_api.ps1 -Rebuild
```

看到输出里有这些地址，就说明启动成功：

```text
console:  http://localhost:1279/console
docs:     http://localhost:1279/docs
pricing:  http://localhost:1279/pricing
health:   http://localhost:1279/api/v1/health
```

如果只是重启，不想重新构建镜像，可以用：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\start_local_api.ps1 -SkipSmoke
```

停止本地服务：

```powershell
docker rm -f poetry-api-local
```

## 2. 打开页面

浏览器打开：

- 控制台：`http://localhost:1279/console`
- 文档站：`http://localhost:1279/docs`
- 价格页：`http://localhost:1279/pricing`

主要测试控制台。

## 3. 控制台手动测试顺序

### 第一步：确认或创建 API Key

本地冒烟脚本会通过管理员接口创建测试 Key。你也可以在控制台或管理员接口创建。拿到 Key 后，把它填进控制台左侧 `API Key 与额度` 输入框，点击保存/查询。

你要看到：

- Key 状态能查到；
- 今日用量有数字；
- 每日额度有数字；
- 没有报 `401` 或 `invalid api key`。

### 第二步：测试诗词查询

建议搜索：

```text
明月
思乡
送别
春天
中秋
```

重点看三件事：

1. 是否返回诗词；
2. 结果是否和关键词相关；
3. 展示是否能直接给内容工具、文旅场景或 AI 知识库使用。

### 第三步：测试自然语言知识库召回

建议输入：

```text
找适合中秋月亮的诗句
找毕业离别能用的诗句
找表达思乡的诗句
找春天景色的诗句
```

重点看：

- 是否返回可用诗句；
- 是否有推荐理由；
- 是否适合作为开发者接入示例。

### 第四步：测试诗画工坊 Prompt

在“自然语言搜索诗词”里输入一个场景，例如：

```text
找适合文旅山水宣传的诗
```

再点一个热门场景，例如 `山水文旅`。

当前逻辑口径：

- 如果你点了热门场景，热门场景会作为当前场景；
- 输入框仍可补充具体要求；
- 搜索诗词按“当前输入 + 当前场景”合并理解；
- 生成图片 Prompt 会强调“画中题诗”，不是把文字框后贴到背景上。

如果只是安全预览作品级 Prompt，不想调用真实生图，用作品接口的 `dry_run=true`。

### 第五步：测试作品级生图 dry_run

这一步不会调用真实生图，不消耗生图额度。先用冒烟脚本生成一个原创作品，或用已有作品 ID，然后执行：

```powershell
$apiKey = "填你的本地 API Key"
$workId = "填作品 ID"
Invoke-RestMethod -Method POST "http://localhost:1279/api/v1/works/$workId/images/generate" `
  -Headers @{ "X-API-Key" = $apiKey } `
  -ContentType "application/json" `
  -Body '{"style":"古风水墨","size":"1024x1024","dry_run":true}'
```

你要看到：

- `dry_run: true`；
- `job.status: prompt_ready`；
- Prompt 中出现“画中题诗”；
- 没有真实图片生成费用。

### 第六步：测试反馈链路

提交一条反馈，例如：

```text
类型：other
主题：创始人自测
内容：本地 API Key、诗词查询、知识库召回、用量统计和作品级生图 dry_run 已测试。
联系方式：local-test@example.invalid
```

这一步验证客户反馈能不能进入后台。

## 4. 命令行全链路 smoke

如果你不想全靠手点，可以直接跑：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\smoke_commercial.ps1 `
  -BaseUrl http://localhost:1279 `
  -AdminToken local-admin-token
```

看到最后一行：

```text
Commercial smoke passed.
```

就说明核心链路是通的。

## 5. 记录这次“创始人自测”

自测完成后执行：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\record_founder_self_test.ps1 `
  -ApiKeyId "填控制台里看到的 key id" `
  -Result pass `
  -Notes "创建/查询 API Key、诗词查询、知识库召回、用量统计、反馈、作品级生图 dry_run 均完成；暂无真实外部用户。"
```

记录会写入：

```text
data/commercial/founder-self-test.jsonl
```

注意：这个文件只证明你自己测过，不算真实商业用户证据，不会写进 `data/commercial/trials.jsonl`。

## 6. 通过标准

下面 6 项都完成，就算“创始人自测通过”：

- 能打开 `/console`、`/docs`、`/pricing`；
- 能查询当前 API Key；
- 带 API Key 能查诗词；
- 能用知识库召回；
- 能看到 usage 或提交 feedback；
- 能跑作品级生图 `dry_run` 并看到 `prompt_ready`。

## 7. 现在不需要做的事

- 不需要人工检查 40 万首；
- 不需要现在接真实支付；
- 不需要把 Codex 主模型改成 `gpt-image-2`；
- 不需要为测试 dry_run 购买生图额度；
- 不要把创始人自测伪装成真实商业客户试用。
