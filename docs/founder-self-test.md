# 创始人自测使用手册

> 适用情况：暂时没有真实外部用户，先由你自己验证产品能不能用、流程顺不顺、结果有没有价值。
>
> 结论先说清楚：创始人自测可以证明“本地产品可用”，但不能冒充真实商业验证。真实商业验证以后仍然要靠外部用户记录。

## 1. 一键启动本地产品

先打开 Docker Desktop，然后在 PowerShell 里执行：

```powershell
Set-Location "G:\项目\唐诗宋词数据库API\chinese-poetry-api"

powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\start_local_api.ps1 -Rebuild
```

看到最后输出类似下面内容就说明启动成功：

```json
{
  "status": "running",
  "console": "http://localhost:1279/console",
  "docs": "http://localhost:1279/docs",
  "pricing": "http://localhost:1279/pricing",
  "smoke_passed": true
}
```

如果只是重新启动，不想重新构建镜像，可以用：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\start_local_api.ps1
```

停止本地服务：

```powershell
docker rm -f poetry-api-local
```

## 2. 打开这三个页面

浏览器打开：

- 控制台：`http://localhost:1279/console`
- 文档站：`http://localhost:1279/docs`
- 价格页：`http://localhost:1279/pricing`

你主要用控制台。

## 3. 你在控制台里按这个顺序测

### 第一步：创建 API Key

在控制台里找到“创建 API Key”。

建议填：

```text
name: founder-local-test
tier: trial
notes: 创始人本地自测
```

创建后会显示一串 `api_key`。这串 key 只显示一次，先复制到本地临时记事本。

### 第二步：查当前 Key

把刚才复制的 `api_key` 填到控制台的 API Key 输入框里，然后点“查看当前 Key / Billing Status”一类按钮。

你要看到：

- key id 存在
- tier 是 trial
- daily limit 有值
- today usage 有值或 0

### 第三步：测诗词查询

测这些关键词：

```text
明月
思乡
送别
春天
中秋
```

你重点看三件事：

1. 有没有返回诗词；
2. 返回结果是否和关键词相关；
3. 如果是增强查询，标签、知识库解释有没有明显乱说。

### 第四步：测知识库召回

建议输入：

```text
找适合中秋月亮的诗句
找毕业离别能用的诗句
找表达思乡的诗句
找春天景色的诗句
```

你重点看：

- 是否能返回可用诗句；
- 是否有推荐理由；
- 是否适合拿去做内容工具/API 示例。

### 第五步：测反馈

在控制台提交一条反馈，例如：

```text
类型：other
主题：创始人自测
内容：本地 API 创建 key、查询、知识库召回、用量统计均已测试。
联系方式：local-test@example.invalid
```

这一步是验证客户反馈链路。

## 4. 用命令跑一次全链路 smoke

如果你不想手点，也可以直接跑：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\smoke_commercial.ps1 `
  -BaseUrl http://localhost:1279 `
  -AdminToken local-admin-token
```

看到最后：

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
  -Notes "创建 API Key、诗词查询、知识库召回、用量统计、反馈链路均完成；暂无真实外部用户。"
```

记录会写到：

```text
data/commercial/founder-self-test.jsonl
```

注意：这个文件只证明你自己测过，不算真实商业用户证据。

## 6. 你怎么判断测过了

只要下面 5 项都完成，就算“创始人自测通过”：

- 能打开 `/console`、`/docs`、`/pricing`
- 能创建 API Key
- 带 API Key 能查诗词
- 能用知识库召回
- 能看到 usage 或提交 feedback

## 7. 现在不需要你做的事

- 不需要你找 40 万首人工检查。
- 不需要你改代码。
- 不需要你现在接真实支付。
- 不需要你把 Codex 主模型改成 `gpt-image-2`。
- 不需要你为了生图功能先买模型；生图只是后续可选扩展。
