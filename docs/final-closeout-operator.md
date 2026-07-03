# 最终收口操作手册

> 这份手册只解决一个问题：还差哪些真实证据，怎么录入，怎么让机器验收通过。  
> 不再追加新的产品方向；最终判断统一看 `scripts/final_closeout.ps1` 输出。

## 1. 先看当前缺口

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\final_closeout.ps1 -Runner auto
```

看输出文件：

- `data/acceptance/final-closeout-report.json`
- `data/acceptance/final-acceptance-audit.json`

只处理 `blockers`，不要临时扩新路线。

## 1.1 没有真实用户时怎么办

如果暂时没有真实外部用户，不要把自己测试伪装成商业验证。

当前采用两层判断：

1. **创始人自测通过**：说明本地产品能用，可以继续演示和找用户。
2. **真实商业验证通过**：必须有外部用户试用记录，才能关闭最终商业验收。

创始人自测手册见：

```text
docs/founder-self-test.md
```

一键启动本地产品：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\start_local_api.ps1 -Rebuild
```

自测完成后记录：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\record_founder_self_test.ps1 `
  -ApiKeyId "填控制台里看到的 key id" `
  -Result pass `
  -Notes "创建 API Key、诗词查询、知识库召回、用量统计、反馈链路均完成；暂无真实外部用户。"
```

这会写入 `data/commercial/founder-self-test.jsonl`，但不会算作 `data/commercial/trials.jsonl` 的真实商业用户证据。

## 2. 黄金评测集人工复核

当前这批 66 条 CSV 已完成并合并。后续补齐全量黄金集时，继续按同一格式扩展 CSV/JSONL：

```text
data/enrichment/golden-sample-1000.prefilled-review-66.csv
```

人工逐行确认：

1. `expected_tags_json`：标签是否符合原文。
2. `evidence_lines_json`：证据句是否真的来自原文。
3. `annotation_status`：确认无误后改成 `done`。
4. `review_notes`：有问题就写清楚问题；不确定不要标 `done`。

复核完成后运行：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\golden_review_closeout.ps1 `
  -Runner auto `
  -Apply `
  -Reviewer operator
```

最终门禁会检查：

- `data/enrichment/golden-sample-1000.prefilled-review-66.audit.json`
- `data/enrichment/golden-sample-1000.reviewed.annotation-audit.json`

## 3. 真实 Qanlo 小样本

当前 `ai-qanlo-golden-20` 已完成真实 20 条小样本：生成、validate、quality-gate、人工 conservative correct、写回发布队列。以下命令仅在需要重新跑或扩大样本时使用。

设置真实密钥，不要打印密钥：

```powershell
$env:QANLO_AGENT_KEY="你的真实 Qanlo Agent Key"
```

先跑 20 条小样本：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\ai_candidate_trial.ps1 `
  -Provider qanlo `
  -Limit 20 `
  -Runner auto `
  -RunId ai-qanlo-golden-20-rerun
```

确认 `validate` 和 `quality-gate` 都没有 error 后，再导入待审队列。避免覆盖已收口证据，重跑时用新的 `RunId`：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\ai_candidate_trial.ps1 `
  -Provider qanlo `
  -Limit 20 `
  -Runner auto `
  -RunId ai-qanlo-golden-20-rerun `
  -Import
```

导入后会生成待人工抽样文件，例如：

```text
data/enrichment/manual-sample-ai-qanlo-golden-20-rerun.jsonl
```

人工编辑这份 JSONL：

1. 通过：`"review_decision":{"action":"accept","notes":"说明为什么通过"}`
2. 退回：`"review_decision":{"action":"reject","notes":"说明为什么退回"}`
3. 修正：先改 `proposed_tags` / `proposed_knowledge`，再填 `"action":"correct"`

人工确认后先审计，不写库：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\ai_review_closeout.ps1 `
  -RunId ai-qanlo-golden-20-rerun `
  -ReviewFile data\enrichment\manual-sample-ai-qanlo-golden-20-rerun.jsonl `
  -AuditOnly `
  -RequireReviewed `
  -Runner auto
```

审计通过后写回：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\ai_review_closeout.ps1 `
  -RunId ai-qanlo-golden-20-rerun `
  -ReviewFile data\enrichment\manual-sample-ai-qanlo-golden-20-rerun.jsonl `
  -RequireReviewed `
  -Apply `
  -Runner auto
```

最终门禁会检查：

- `data/enrichment/candidates-*qanlo*.jsonl`
- `data/enrichment/validate-*qanlo*.json`
- `data/enrichment/quality-gate-*qanlo*.json`
- `data/enrichment/review-audit-*qanlo*.json`
- `data/enrichment/review-report-*qanlo*.json`

## 4. 真实商业试用记录

每个真实试用客户追加一条记录。推荐用脚本，避免 JSON 写错：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\add_commercial_trial.ps1 `
  -CustomerProject "真实客户或项目名" `
  -CustomerType "教育 App / 内容工具 / AI 智能体" `
  -Scenario "真实接入场景" `
  -ApiKeyId "真实 API Key ID" `
  -SevenDayCalls 1 `
  -RealCallCompleted `
  -TopQueries "中秋 月亮","思乡" `
  -MissingData "客户反馈的缺失数据" `
  -PaidSignal none `
  -NextStep "继续跟进"
```

如果客户已充值或明确付费意向：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\add_commercial_trial.ps1 `
  -CustomerProject "真实客户或项目名" `
  -CustomerType "AI 智能体" `
  -Scenario "诗词知识库召回" `
  -ApiKeyId "真实 API Key ID" `
  -SevenDayCalls 10 `
  -RealCallCompleted `
  -TopQueries "送别","春天" `
  -PaidSignal paid_intent `
  -PaidIntentBudget "99 元/月可接受" `
  -NextStep "确认首充"
```

最终门禁要求：

- `data/commercial/trials.jsonl` 至少 5 条目标记录。
- 至少 3 条完成真实调用。
- 至少 1 条有 `recharge`、`paid`、`paid_intent` 或明确预算。

## 5. 最终门禁

补齐全量黄金集人工复核和真实商业记录后运行：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\final_closeout.ps1 `
  -Runner auto `
  -ApplyGolden `
  -AllowPaidQanlo `
  -ImportQanlo `
  -RequireDone
```

只有 `ready_for_stop=true` 才表示达到最终形态。
