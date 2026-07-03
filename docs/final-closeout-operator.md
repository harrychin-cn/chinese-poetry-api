# 最终收口操作手册

> 这份手册只解决一个问题：还差哪些真实证据、怎么录入、怎么让机器验收通过。
>
> 不再追加新的产品方向。最终判断统一看 `scripts/final_closeout.ps1` 输出。

## 1. 先看当前缺口

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\final_closeout.ps1 -Runner auto
```

重点看两个输出文件：

- `data/acceptance/final-closeout-report.json`
- `data/acceptance/final-acceptance-audit.json`

只处理 `blockers`，不要临时扩新路线。

当前固定停工线：

- `ready_for_stop=true` 才能停；
- 黄金评测集可按当前口径使用 Codex 代理批量复核；如要强制真实人工门禁，加 `-RequireHumanGoldenReview`；
- 脚本不会伪造真实商业试用；
- 商业试用/充值记录默认后置为运营验证；如要强制商业门禁，加 `-RequireCommercialValidation`；
- 没有显式参数时，不会跑付费 Qanlo 或真实生图调用。

## 2. 没有真实外部客户时怎么做

如果暂时没有真实外部客户，不要把自己测试伪装成商业验证。

当前采用两层判断：

1. **创始人自测通过**：说明本地产品能用，可以继续演示和找用户。
2. **真实商业验证通过**：作为后续运营补录项；当前开发收口不再因缺少外部用户记录卡住。

创始人自测手册：

```text
docs/founder-self-test.md
```

记录创始人自测：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\record_founder_self_test.ps1 `
  -ApiKeyId "填控制台里看到的 key id" `
  -Result pass `
  -Notes "创建/查询 API Key、诗词查询、知识库召回、用量统计、反馈、作品级生图 dry_run 均完成；暂无真实外部用户。"
```

这会写入：

```text
data/commercial/founder-self-test.jsonl
```

但不会算作：

```text
data/commercial/trials.jsonl
```

## 3. 黄金评测集复核

黄金评测集的目的：固定一批有代表性的诗/词/曲样本，由人工确认“应该有哪些标签、证据句来自哪里”。后续 AI 或规则生成候选时，都拿这批样本当标尺，防止模型胡编、过度解读、标签漂移。

当前已由 Codex 代理批量完成 `1000/1000` 复核，证据句均来自原诗正文，审计文件为：

```text
data/enrichment/golden-sample-1000.reviewed.annotation-audit.json
```

如果后续要求真实人工逐行复核，再启用下面的 CSV 流程，并在最终收口时加 `-RequireHumanGoldenReview`。

当前复核 CSV：

```text
data/enrichment/golden-sample-1000.prefilled-review-66.csv
```

人工逐行确认：

1. `expected_tags_json`：标签是否符合原文；
2. `evidence_lines_json`：证据句是否真的来自原文；
3. `annotation_status`：确认无误后改成 `done`；
4. `review_notes`：有问题就写清楚；不确定不要标 `done`。

人工复核完成后运行：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\golden_review_closeout.ps1 `
  -Runner auto `
  -Apply `
  -Reviewer operator
```

最终门禁会检查：

- `data/enrichment/golden-sample-1000.prefilled-review-66.audit.json`
- `data/enrichment/golden-sample-1000.reviewed.annotation-audit.json`

## 4. 真实 Qanlo 小样本

当前 `ai-qanlo-golden-20` 已完成真实 20 条小样本：生成、validate、quality-gate、人工 conservative correct、写回发布队列。

只有需要重跑或扩大样本时，才设置真实密钥。不要打印密钥。

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

确认 `validate` 和 `quality-gate` 都没有 error 后，再导入待审队列：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\ai_candidate_trial.ps1 `
  -Provider qanlo `
  -Limit 20 `
  -Runner auto `
  -RunId ai-qanlo-golden-20-rerun `
  -Import
```

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

## 5. 真实商业试用记录

这部分按当前口径后置为运营验证，不阻塞开发收口；后续每个真实试用客户追加一条记录。推荐用脚本，避免 JSON 写错：

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

如果客户已充值或有明确付费意向：

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

如果后续显式加 `-RequireCommercialValidation`，最终门禁才要求：

- `data/commercial/trials.jsonl` 至少 5 条目标记录；
- 至少 3 条完成真实调用；
- 至少 1 条有 `recharge`、`paid`、`paid_intent` 或明确预算。

## 6. 最终门禁

默认开发收口运行：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\final_closeout.ps1 `
  -Runner auto `
  -RequireDone
```

如后续要把真实人工黄金集和真实商业记录也纳入硬门禁，再加：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\final_closeout.ps1 `
  -Runner auto `
  -RequireDone `
  -RequireHumanGoldenReview `
  -RequireCommercialValidation
```

只有 `ready_for_stop=true` 才表示达到最终停工线。

## 7. 如果暂时没有真实外部试用者

不要伪造客户记录，也不要把创始人自测、Codex 代测或脚本 smoke 写进 `data/commercial/trials.jsonl`。

可先生成“商业试用准备包”，把产品整理到可对外试用状态：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\export_commercial_trial_readiness.ps1 `
  -BaseUrl http://localhost:1279 `
  -RequireReady
```

输出目录：

```text
data/commercial/trial-readiness/
```

这一步会生成邀请话术、15 分钟试用步骤、反馈表、真实记录模板和机器可读报告。它只证明“产品可以发给外部人试用”；真实外部试用和至少 1 个充值或明确付费意向作为运营后置项，除非显式加 `-RequireCommercialValidation`。
