# AI 数据增强与人工抽检运行手册

> 阶段 4 已从“占位流程”升级为可执行 MVP：支持导出样本、生成候选、格式校验、导入待审队列、后台人工通过/退回/修正、按批次或单首回滚。

## 0. 生产路线修正

最新结论：`rules-v13 offset1000` 批次输入 100 首，只生成 32 条；人工审查 21 条可接受、4 条需修正、7 条错误拒绝，合格/可修正 25/32，通过率 78.13%。这说明规则继续扩批会变成无底洞，不能作为 40 万首主生产路线。

新的生产路线已经固化到 [development-plan.md](development-plan.md) 第 7 节“终极执行总路线”。本手册只负责执行数据增强流水线，不再单独提出开放式下一步建议。

固定生产路线：

1. 规则只做高置信种子数据，宁可少发，不乱发。
2. 先建立 1000-2000 条黄金评测集，按诗/词/曲、朝代、标签类型分层抽样。
3. 主生产改为 QanloAPI AI 候选生成。
4. AI 候选先跑自动校验：格式、重复标签、原文证据、过度解读、低置信。
5. 人工只做低置信、失败样本和每批 0.5%-2% 抽样质检。

当前 `offset1000` 人工审查只先保留证据；未正式写回数据库前，不计入已发布数据。

固定停止线：

- 规则批次抽检通过率低于 90%，停止扩批，不继续 rules-v14。
- AI/规则候选存在 `quality-gate error`，不导入数据库。
- warning 样本进入低置信人工队列，不直接发布。
- 没有 run_id、审查证据和回滚路径的数据，不发布。

## 1. 目标

阶段 4 不直接全库生成、全库发布。正确路径是：

1. 先导出 100 首小样本。
2. 生成增强候选数据：标签、解释、推荐理由。
3. 导入前做机器格式校验。
4. 候选数据只进入“待抽检队列”，不直接影响线上查询。
5. 人工通过后，才写入正式标签和知识字段。
6. 每个批次保留 `run_id`，可按批次或单首回滚。

## 2. 已实现能力

### 2.1 数据表

- `poem_knowledge`：保存已通过的摘要、译文、注释、推荐理由。
- `poem_embeddings`：预留后续向量召回数据，当前用 JSON 存储向量。
- `enrichment_jobs`：记录批量增强任务、总数、通过数、退回数。
- `enrichment_review_items`：记录待抽检、已通过、已退回、已回滚候选项。

### 2.2 命令行工具

```bash
go run ./cmd/enrichment --help
```

已支持：

- `export-sample`：导出样本 JSONL。
- `export-golden-sample`：分层导出黄金评测集 JSONL。
- `generate`：生成候选数据，支持 `rules` 和 `qanlo`。
- `validate`：导入前校验候选数据。
- `quality-gate`：导入前做自动质量闸门检查。
- `import-candidates`：导入到待抽检队列。
- `sample-review`：按 `run_id` 导出人工抽检 JSONL，方便运营逐条核对。
- `apply-review`：校验并写回人工抽检 JSONL 里的 `accept/reject/correct` 决策。
- `review-audit`：汇总人工审查 JSONL，不写数据库。
- `review-report`：按 `run_id` 输出抽检进度、通过率和退回原因 Top10。
- `rollback`：按 `run_id` 或 `poem_id` 回滚。

### 2.3 管理接口

需要 `X-Admin-Token`：

| 接口 | 用途 |
| --- | --- |
| `POST /api/v1/admin/enrichment/jobs` | 创建增强批次 |
| `GET /api/v1/admin/enrichment/jobs` | 查看增强批次 |
| `GET /api/v1/admin/enrichment/runs/:run_id/summary` | 查看批次抽检通过率和退回原因 Top10 |
| `POST /api/v1/admin/enrichment/review-items` | 导入单条候选 |
| `GET /api/v1/admin/enrichment/review-items?status=pending` | 查看待抽检队列 |
| `PATCH /api/v1/admin/enrichment/review-items/:id` | 人工修正候选 |
| `POST /api/v1/admin/enrichment/review-items/:id/accept` | 通过并发布 |
| `POST /api/v1/admin/enrichment/review-items/:id/reject` | 退回不发布 |

## 3. 建议增强字段

| 字段 | 说明 | 示例 |
| --- | --- | --- |
| `theme` | 主题标签 | 思乡、送别、边塞、怀古 |
| `mood` | 情绪标签 | 孤独、豪迈、惆怅、旷达 |
| `scenario` | 使用场景 | 中秋、毕业、文旅文案、课堂讲解 |
| `festival` | 节日标签 | 中秋、春节、重阳 |
| `season` | 季节标签 | 春天、秋天 |
| `image` | 意象标签 | 月亮、春风、江河、边关 |
| `grade` | 教育标签 | 小学、初中、高中、通识 |
| `keyword` | 召回关键词 | 明月、故乡、边塞 |

先不强制生成长篇赏析、逐句注释；这些成本更高，等短字段稳定后再扩展。

## 4. 新生产闭环：黄金评测集 + AI 候选 + 自动校验

### 4.1 收口当前人工审查证据（不写库）

```powershell
go run ./cmd/enrichment review-audit `
  --input data/enrichment/manual-reviewed-enrich-20260630-rules100-offset1000-v13.jsonl `
  --run-id enrich-20260630-rules100-offset1000-v13 `
  --out data/enrichment/review-audit-enrich-20260630-rules100-offset1000-v13.json
```

### 4.2 导出黄金评测集

推荐用脚本入口，它会自动判断本机 Go CGO/gcc 是否可用；不可用时自动切 Docker，并在导出后校验行数、唯一 `poem_id`、空正文和基础字段：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\export_golden_sample.ps1 `
  -Db data\poetry.db `
  -Total 1000 `
  -PerStratum 80 `
  -Out data\enrichment\golden-sample-1000.jsonl
```

底层命令等价于：

```powershell
go run ./cmd/enrichment --db data\poetry.db export-golden-sample `
  --total 1000 `
  --per-stratum 80 `
  --out data\enrichment\golden-sample-1000.jsonl
```

导出后先人工标注 `golden_meta.expected_tags` 和 `golden_meta.evidence_lines`，后续所有规则和 AI 生成都先跑这份评测集。

当前已补充黄金集标注辅助命令：

```powershell
# 审计黄金集标注完整性
go run ./cmd/enrichment golden-audit `
  --input data/enrichment/golden-sample-1000.jsonl `
  --out data/enrichment/golden-sample-1000.annotation-audit.json

# 从已 accepted 且带 manual_review 来源的数据预填候选标注；预填结果仍需人工复核
go run ./cmd/enrichment --db data/poetry.db golden-prefill `
  --input data/enrichment/golden-sample-1000.jsonl `
  --output data/enrichment/golden-sample-1000.prefilled.jsonl `
  --mode accepted-reviewed

# 导出待人工复核的小队列
go run ./cmd/enrichment golden-review-queue `
  --input data/enrichment/golden-sample-1000.prefilled.jsonl `
  --output data/enrichment/golden-sample-1000.prefilled-review-66.jsonl `
  --status prefilled_review_required

# 给人工复核导出更容易编辑的 CSV 表
go run ./cmd/enrichment golden-review-sheet `
  --input data/enrichment/golden-sample-1000.prefilled.jsonl `
  --output data/enrichment/golden-sample-1000.prefilled-review-66.csv `
  --status prefilled_review_required

# 人工复核 CSV 后，把 annotation_status 改成 done，再合并回黄金集
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\golden_review_closeout.ps1 `
  -Sheet data\enrichment\golden-sample-1000.prefilled-review-66.csv `
  -Apply `
  -Reviewer operator

# 单步命令参考：先审计 CSV，全部 done 才允许合并
go run ./cmd/enrichment golden-review-sheet-audit `
  --sheet data/enrichment/golden-sample-1000.prefilled-review-66.csv `
  --out data/enrichment/golden-sample-1000.prefilled-review-66.audit.json `
  --require-done

go run ./cmd/enrichment golden-apply-review-sheet `
  --base data/enrichment/golden-sample-1000.prefilled.jsonl `
  --sheet data/enrichment/golden-sample-1000.prefilled-review-66.csv `
  --output data/enrichment/golden-sample-1000.reviewed.jsonl `
  --reviewer operator

# 也可以直接编辑 JSONL 后合并
go run ./cmd/enrichment golden-apply-review `
  --base data/enrichment/golden-sample-1000.prefilled.jsonl `
  --review data/enrichment/golden-sample-1000.prefilled-review-66.jsonl `
  --output data/enrichment/golden-sample-1000.reviewed.jsonl `
  --reviewer operator
```

当前证据：`golden-sample-1000.prefilled-review-66.csv` 已人工确认 66 条；`golden-sample-1000.prefilled-review-66.audit.json` 显示 `ready_for_merge=true`、`complete_count=66`；`golden-sample-1000.reviewed.annotation-audit.json` 显示当前黄金集完成 `66/1000`。这 66 条可以作为小样本评测/生产种子，全量最终黄金闸门仍需补齐到 1000/1000。

### 4.3 QanloAPI 生成候选小样本

```powershell
# 推荐：一键跑黄金集 -> Qanlo 候选 -> validate -> quality-gate；不带 -Import 时不写库
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\ai_candidate_trial.ps1 `
  -Provider qanlo `
  -GoldenInput data\enrichment\golden-sample-1000.reviewed.jsonl `
  -Limit 20 `
  -RunId ai-qanlo-golden-20 `
  -Runner auto

# 单步命令参考
go run ./cmd/enrichment generate `
  --provider qanlo `
  --model deepseek-v4-flash `
  --input data/enrichment/sample-ai-qanlo-golden-20.jsonl `
  --output data/enrichment/candidates-qanlo-golden-20.jsonl `
  --batch-size 20
```

注意：只检查 `QANLO_AGENT_KEY` 是否存在，不打印密钥；`scripts/ai_candidate_trial.ps1` 没有密钥会直接停止，不会盲跑真实付费调用。批量调用前先确认成本。当前真实小样本证据为 `ai-qanlo-golden-20`：20 条已生成，validate 0 error，quality-gate 0 error，人工 conservative correct 后已写回发布队列。

### 4.4 自动校验和质检闸门

```powershell
go run ./cmd/enrichment validate `
  --input data/enrichment/candidates-qanlo-golden-20.jsonl `
  --out data/enrichment/validate-qanlo-golden-20.json `
  --skip-db-check

go run ./cmd/enrichment quality-gate `
  --input data/enrichment/candidates-qanlo-golden-20.jsonl `
  --sample data/enrichment/sample-ai-qanlo-golden-20.jsonl `
  --out data/enrichment/quality-gate-qanlo-golden-20.json
```

处理规则：`error` 不导入；`warning` 进入低置信人工队列；通过样本才进入待审/发布流程。

## 5. 100 首小样本闭环（旧规则流程，仅保留作回归参考）

### 5.0 一键试跑脚本（推荐）

PowerShell 环境可以直接跑完整闭环：

```powershell
.\scripts\enrichment_trial.ps1 `
  -Db data\poetry.db `
  -Limit 100 `
  -Provider rules
```

如果要扩大到 500-1000 首，可以分批跑，避免一次性导入太多待审项。例如第二批从第 101 首开始：

```powershell
.\scripts\enrichment_trial.ps1 `
  -Db data\poetry.db `
  -Limit 100 `
  -Offset 100 `
  -RunId enrich-20260630-rules-101-200 `
  -Provider rules
```

脚本会自动执行：

1. `export-sample`
2. `generate`
3. `validate`
4. `import-candidates`
5. `review-report`

脚本默认 `-Runner auto`：如果本机 Go 的 CGO 不可用，会自动改用 Docker 运行，避免 Windows 下 `go-sqlite3 requires cgo` 的问题。也可以手动指定 `-Runner docker`。

正式 QanloAPI 小样本试跑前，只检查环境变量是否存在，不要打印密钥：

```powershell
$env:QANLO_AGENT_KEY="你的 Qanlo Agent Key"
.\scripts\enrichment_trial.ps1 `
  -Db data\poetry.db `
  -Limit 100 `
  -Provider qanlo `
  -Model deepseek-v4-flash `
  -ApiKeyEnv QANLO_AGENT_KEY
```

### 5.1 导出样本

```bash
go run ./cmd/enrichment \
  --db data/poetry.db \
  export-sample \
  --limit 100 \
  --offset 0 \
  --out data/enrichment/sample-100.jsonl
```

### 5.2 生成候选数据

低成本规则草稿，适合先验证流程：

```bash
go run ./cmd/enrichment \
  generate \
  --provider rules \
  --input data/enrichment/sample-100.jsonl \
  --output data/enrichment/candidates-100.jsonl
```

`rules-v4` 会跳过未命中具体标签的低信号样本，不写入 candidates；`validate/import-candidates` 只处理实际写入的候选。若整批都被跳过，后续校验会因 `no candidates` 失败，需要扩大样本或改用 `qanlo` 生成。

QanloAPI 生成，适合正式小样本。当前 Qanlo 网关实测可用文本生成模型先用 `deepseek-v4-flash`，不要再把旧的 `gpt-4o-mini` 当默认模型。

正式 AI 试跑优先从 `data/enrichment/golden-sample-1000.reviewed.jsonl` 导出已复核样本，并加 `--require-done`，避免把未复核黄金集混入生产样本。

```bash
# 只检查环境变量是否存在，不要打印密钥
# PowerShell: $env:QANLO_AGENT_KEY="你的 Qanlo Agent Key"
# Bash: export QANLO_AGENT_KEY="你的 Qanlo Agent Key"

go run ./cmd/enrichment \
  generate \
  --provider qanlo \
  --model deepseek-v4-flash \
  --base-url https://qanlo.com/v1 \
  --api-key-env QANLO_AGENT_KEY \
  --input data/enrichment/sample-100.jsonl \
  --output data/enrichment/candidates-100.jsonl \
  --batch-size 20
```

### 5.3 校验候选数据

```bash
go run ./cmd/enrichment \
  validate \
  --input data/enrichment/candidates-100.jsonl \
  --db data/poetry.db
```

校验会检查：

- JSONL 可解析。
- `poem_id` 为正数且批次内不重复。
- 标签不为空、不过多、不是长句。
- `summary` 不为空，长度大致合理。
- 不包含“作为 AI”“根据提供的信息”等提示词残留。

### 5.4 导入待抽检队列

```bash
go run ./cmd/enrichment \
  --db data/poetry.db \
  import-candidates \
  --input data/enrichment/candidates-100.jsonl \
  --run-id enrich-20260629-sample100
```

导入后不会立刻影响公开查询。管理员需要在后台接口或控制台里人工通过。

### 5.5 导出人工抽检样本

候选导入后，可以先导出人工抽检 JSONL。这个命令只读数据库，不会发布数据：

```bash
go run ./cmd/enrichment \
  --db data/poetry.db \
  sample-review \
  --run-id enrich-20260629-sample100 \
  --limit 30 \
  --out data/enrichment/manual-sample-enrich-20260629-sample100.jsonl
```

每条 JSONL 会带上：

- 原诗标题、作者、朝代、正文。
- AI/规则生成的标签和知识字段。
- 人工检查清单。
- 待运营填写的 `review_decision.action` 和 `review_decision.notes`。

人工检查后，把每条的 `review_decision` 改成以下三种之一：

```json
{"action":"accept","notes":"抽检通过"}
{"action":"reject","notes":"标签与原文不符"}
{"action":"correct","notes":"人工修正后通过前复核"}
```

如果选择 `correct`，同时修改该行里的 `proposed_tags` 和 `proposed_knowledge`。

### 5.6 写回人工抽检结果

写回前先做 dry-run 校验，不会改数据库：

```bash
go run ./cmd/enrichment \
  --db data/poetry.db \
  apply-review \
  --input data/enrichment/manual-sample-enrich-20260629-sample100.jsonl \
  --reviewer operator
```

确认 `planned_accept`、`planned_reject`、`planned_correct` 和预期一致后，再追加 `--apply` 写回：

```bash
go run ./cmd/enrichment \
  --db data/poetry.db \
  apply-review \
  --input data/enrichment/manual-sample-enrich-20260629-sample100.jsonl \
  --reviewer operator \
  --apply
```

写回规则：

- `accept`：先按 JSONL 当前候选修正，再发布到正式标签和知识表。
- `reject`：只退回，不发布。
- `correct`：按 JSONL 里的人工修正版本发布为已通过，不再残留在 `pending`。
- `pending` 或未改动的样本会跳过。

### 5.7 输出抽检报告

人工通过/退回一批后，输出当前批次的通过率和退回原因：

```bash
go run ./cmd/enrichment \
  --db data/poetry.db \
  review-report \
  --run-id enrich-20260629-sample100 \
  --out data/enrichment/review-report-100.json
```

报告字段：

- `total_items`：本批次候选总数。
- `pending_count`：还没抽检的候选数。
- `accepted_count`：已通过并发布的候选数。
- `rejected_count`：已退回的候选数。
- `reviewed_count`：已处理候选数。
- `pass_rate`：已处理候选里的通过率。
- `rejected_note_top10`：退回原因 Top10。

## 6. 人工抽检标准

通过标准：

- 标签和解释基本符合原文。
- 推荐理由能说明为什么适合某个场景。
- 不编造作者经历、历史背景和人物关系。
- 标签有检索价值，不全是“人生”“自然”“情感”这类泛词。
- 语言简短稳定，不像营销文案或 AI 套话。

退回标准：

- 标签与原文明显不符。
- 情绪判断反了。
- 解释中出现无依据的历史故事。
- 标签过多、过泛、过长。
- 解释空洞、重复、过长或有提示词残留。

## 7. 管理接口示例

### 7.1 查看待抽检

```bash
curl "http://localhost:1279/api/v1/admin/enrichment/review-items?status=pending" \
  -H "X-Admin-Token: replace-with-random-secret"
```

### 7.2 通过并发布

```bash
curl -X POST "http://localhost:1279/api/v1/admin/enrichment/review-items/1/accept" \
  -H "X-Admin-Token: replace-with-random-secret" \
  -H "Content-Type: application/json" \
  -d '{"reviewer":"operator","notes":"抽检通过"}'
```

通过后会写入：

- `tags` / `poem_tags`
- `poem_knowledge`

随后 `/api/v1/knowledge/recall` 会返回这些增强字段。

### 7.3 退回

```bash
curl -X POST "http://localhost:1279/api/v1/admin/enrichment/review-items/1/reject" \
  -H "X-Admin-Token: replace-with-random-secret" \
  -H "Content-Type: application/json" \
  -d '{"reviewer":"operator","notes":"标签与原文不符"}'
```

### 7.4 人工修正

```bash
curl -X PATCH "http://localhost:1279/api/v1/admin/enrichment/review-items/1" \
  -H "X-Admin-Token: replace-with-random-secret" \
  -H "Content-Type: application/json" \
  -d '{
    "proposed_tags":[{"name":"思乡","category":"theme","source":"manual"}],
    "proposed_knowledge":{"summary":"人工修正后的简短解释。","source":"manual"},
    "reviewer":"operator",
    "notes":"人工修正后待通过"
  }'
```

### 7.5 查看批次抽检报告

```bash
curl "http://localhost:1279/api/v1/admin/enrichment/runs/enrich-20260629-sample100/summary" \
  -H "X-Admin-Token: replace-with-random-secret"
```

## 8. 500-1000 首试跑节奏

当前本地 `data/poetry.db` 已完成旧规则 1000 条候选导入，并基于旧规则退回原因把规则生成逐步收紧。旧规则不再继续发布；`rules-v3` 100 首复测批次通过率 91%。之后按 100 首小批持续迭代：`rules-v4` offset100 82.98%，`rules-v5` offset200 75.68%，`rules-v6` offset300 97.44%，`rules-v7` offset400 78.79%，`rules-v8` offset500 87.50%，`rules-v9` offset600 63.27%，`rules-v10` offset700 53.13%，`rules-v11` offset800 62.86%，`rules-v12` offset900 72.73%。`rules-v13 offset1000` 最新抽检通过率 78.13%，说明规则扩批不稳定；本节保留历史批次证据，但后续主生产改走黄金评测集 + AI 候选 + 自动校验。

| run_id | 数量 | 状态 |
| --- | ---: | --- |
| `enrich-20260630-rules100` | 100 | 旧规则批次；首批 20 条原为 10 通过、10 退回，通过项已回滚，当前 10 退回、10 已回滚、80 待审 |
| `enrich-20260630-rules100-v3` | 100 | `rules-v3` 复测批次；已全量检查并写回：91 通过、9 退回、0 待审，通过率 91% |
| `enrich-20260630-rules100-offset100-v4-preview` | 47/100 | `rules-v4` 预览批次；导出 100 首，生成 47 条候选，跳过低信号 53 条；已校验通过，未导入 |
| `enrich-20260630-rules100-offset100-v4` | 47/100 | `rules-v4` 正式候选批次；导出 100 首，生成并导入 47 条，跳过低信号 53 条；已全量检查：39 通过、8 退回、0 待审，通过率 82.98% |
| `enrich-20260630-rules100-offset100-v5-preview` | 43/100 | `rules-v5` 预览批次；基于 `rules-v4` 新误判继续收紧规则，回放 offset100 与已通过人工结果 39/39 对齐，未导入 |
| `enrich-20260630-rules100-offset200-v5` | 37/100 | `rules-v5` 正式候选批次；导出 100 首，生成并导入 37 条，跳过低信号 63 条；已全量检查：28 通过、9 退回、0 待审，通过率 75.68% |
| `enrich-20260630-rules100-offset200-v6-preview` | 28/100 | `rules-v6` 预览批次；基于 offset200 新退回原因继续收紧规则，生成 28 条、跳过 72 条，回放已与人工通过结果 28/28 对齐，未导入 |
| `enrich-20260630-rules100-offset300-v6` | 39/100 | `rules-v6` 正式候选批次；导出 100 首，生成并导入 39 条，跳过低信号 61 条；已全量检查：38 通过、1 退回、0 待审，通过率 97.44% |
| `enrich-20260630-rules100-offset300-v7-preview` | 38/100 | `rules-v7` 预览批次；基于 offset300 新误判继续收紧规则，生成 38 条、跳过 62 条；回放已与人工通过结果 38/38 对齐，未导入 |
| `enrich-20260630-rules100-offset400-v7` | 33/100 | `rules-v7` 正式候选批次；导出 100 首，生成并导入 33 条，跳过低信号 67 条；已全量检查：26 通过、7 退回、0 待审，通过率 78.79% |
| `enrich-20260630-rules100-offset400-v8-preview` | 26/100 | `rules-v8` 预览批次；基于 offset400 新误判继续收紧规则，生成 26 条、跳过 74 条；回放已与人工通过结果 26/26 对齐，未导入 |
| `enrich-20260630-rules100-offset500-v8` | 56/100 | `rules-v8` 正式候选批次；导出 100 首，生成并导入 56 条，跳过低信号 44 条；已全量检查：49 通过/修正、7 退回、0 待审，通过率 87.50% |
| `enrich-20260630-rules100-offset500-v9-preview` | 49/100 | `rules-v9` 预览批次；基于 offset500 新误判继续收紧规则，生成 49 条、跳过低信号 51 条；回放已与人工通过/修正结果 49/49 对齐，7 条退回样本未再生成，未导入 |
| `enrich-20260630-rules100-offset600-v9` | 49/100 | `rules-v9` 正式候选批次；导出 100 首，生成并导入 49 条，跳过低信号 51 条；已全量检查：31 通过/修正、18 退回、0 待审，通过率 63.27% |
| `enrich-20260630-rules100-offset600-v10-preview` | 31/100 | `rules-v10` 预览批次；基于 offset600 新误判继续收紧规则，生成 31 条、跳过低信号 69 条；回放已与人工通过/修正结果 31/31 对齐，18 条退回样本未再生成，未导入 |
| `enrich-20260630-rules100-offset700-v10` | 32/100 | `rules-v10` 正式候选批次；导出 100 首，生成并导入 32 条，跳过低信号 68 条；已全量检查：17 通过/修正、15 退回、0 待审，通过率 53.13% |
| `enrich-20260630-rules100-offset700-v11-preview` | 17/100 | `rules-v11` 预览批次；基于 offset700 新误判继续收紧规则，生成 17 条、跳过低信号 83 条；回放已与人工通过/修正结果 17/17 对齐，15 条退回样本未再生成，未导入 |
| `enrich-20260630-rules100-offset800-v11` | 35/100 | `rules-v11` 正式候选批次；导出 100 首，生成并导入 35 条，跳过低信号 65 条；已全量检查：22 通过/修正、13 退回、0 待审，通过率 62.86% |
| `enrich-20260630-rules100-offset800-v12-preview` | 22/100 | `rules-v12` 预览批次；基于 offset800 新误判继续收紧规则，生成 22 条、跳过低信号 78 条；回放已与人工通过/修正结果 22/22 对齐，13 条退回样本未再生成，未导入 |
| `enrich-20260630-rules100-offset900-v12` | 33/100 | `rules-v12` 正式候选批次；导出 100 首，生成并导入 33 条，跳过低信号 67 条；已全量检查：24 通过/修正、9 退回、0 待审，通过率 72.73% |
| `enrich-20260630-rules100-offset900-v13-preview` | 24/100 | `rules-v13` 预览批次；基于 offset900 新误判继续收紧规则，生成 24 条、跳过低信号 76 条；回放已与人工通过/修正结果 24/24 对齐，9 条退回样本未再生成，未导入 |
| `enrich-20260630-rules100-offset1000-v13` | 32/100 | 最新抽检证据批次；输入 100 首、规则生成 32 条，人工审查 21 可接受、4 需修正、7 错误拒绝，合格/可修正 25/32，通过率 78.13%；只保留证据，未正式写回数据库，不再继续规则扩批 |
| `enrich-20260630-rules500-offset100` | 500 | 旧规则待审；先不要继续发布，也不再按规则重建扩批，后续改走黄金评测集 + AI 候选 |
| `enrich-20260630-rules400-offset600` | 400 | 旧规则待审；先不要继续发布，也不再按规则重建扩批，后续改走黄金评测集 + AI 候选 |
| 当前可发布增强数据 | 365 | 来自 `rules-v3` 已通过 91 条 + `rules-v4` 已通过 39 条 + `rules-v5` offset200 已通过 28 条 + `rules-v6` offset300 已通过 38 条 + `rules-v7` offset400 已通过 26 条 + `rules-v8` offset500 已通过/修正 49 条 + `rules-v9` offset600 已通过/修正 31 条 + `rules-v10` offset700 已通过/修正 17 条 + `rules-v11` offset800 已通过/修正 22 条 + `rules-v12` offset900 已通过/修正 24 条；退回数据不进入发布数据 |

建议先导出人工抽检样本，不要直接全量通过：

```powershell
# rules-v3 100 首全检已完成，继续工作前先查看最终报告
go run ./cmd/enrichment --db data/poetry.db review-report `
  --run-id enrich-20260630-rules100-v3 `
  --out data/enrichment/review-report-enrich-20260630-rules100-v3-final.json

# rules-v4 offset100 批次已全量人工检查：39 通过、8 退回，通过率 82.98%，不直接扩批
go run ./cmd/enrichment --db data/poetry.db review-report `
  --run-id enrich-20260630-rules100-offset100-v4 `
  --out data/enrichment/review-report-enrich-20260630-rules100-offset100-v4.json

# 旧规则 500/400 首先暂停；rules-v13 offset1000 只有 78.13%，不再规则扩批，改走黄金评测集 + AI 候选 + quality-gate
```

首批 20 条抽检已经形成可复查文件和报告：

- 抽检写回文件：`data/enrichment/manual-reviewed-enrich-20260630-rules100-first20.jsonl`
- 报告文件：`data/enrichment/review-report-enrich-20260630-rules100-first20.json`
- 旧规则回滚后报告：`data/enrichment/review-report-enrich-20260630-rules100-after-rollback.json`
- 当前结果：旧规则通过项已回滚，保留 10 条退回原因用于规则回归；主要问题是 `山水`、`文旅`、`边塞`、`送别` 等标签过泛。

`rules-v3` 复测已经形成可复查文件和报告：

- 候选文件：`data/enrichment/candidates-enrich-20260630-rules100-v3.jsonl`
- 抽检样本：`data/enrichment/manual-sample-enrich-20260630-rules100-v3.jsonl`
- 首批抽检写回文件：`data/enrichment/manual-reviewed-enrich-20260630-rules100-v3-first30.jsonl`
- 剩余 70 条抽检写回文件：`data/enrichment/manual-reviewed-enrich-20260630-rules100-v3-remaining70.jsonl`
- 修正后二次通过文件：`data/enrichment/manual-reviewed-enrich-20260630-rules100-v3-corrected23-accept.jsonl`
- 最终报告文件：`data/enrichment/review-report-enrich-20260630-rules100-v3-final.json`
- 同步报告文件：`data/enrichment/review-report-enrich-20260630-rules100-v3.json`
- 当前结果：`reviewed_count=100`、`accepted_count=91`、`rejected_count=9`、`pending_count=0`、`pass_rate=0.91`
- 注意：`rules-v3` 只是规则候选质量提升，不代表可以直接全量发布；扩大到 500/1000 首前仍必须走候选入队、人工抽检、通过发布。

`rules-v4` 小批重建和人工检查已经形成可复查文件和报告：

- 预览样本：`data/enrichment/sample-enrich-20260630-rules100-offset100-v4-preview.jsonl`
- 预览候选：`data/enrichment/candidates-enrich-20260630-rules100-offset100-v4-preview.jsonl`
- 正式样本：`data/enrichment/sample-enrich-20260630-rules100-offset100-v4.jsonl`
- 正式候选：`data/enrichment/candidates-enrich-20260630-rules100-offset100-v4.jsonl`
- 待审抽检样本：`data/enrichment/manual-sample-enrich-20260630-rules100-offset100-v4.jsonl`
- 当前报告：`data/enrichment/review-report-enrich-20260630-rules100-offset100-v4.json`
- 最终报告：`data/enrichment/review-report-enrich-20260630-rules100-offset100-v4-final.json`
- 人工写回文件：`data/enrichment/manual-reviewed-enrich-20260630-rules100-offset100-v4.jsonl`
- 当前结果：导出 100 首，生成并导入 47 条候选，跳过低信号 53 条；`reviewed_count=47`、`accepted_count=39`、`rejected_count=8`、`pending_count=0`、`pass_rate=0.8297872340425532`
- 注意：`rules-v4` 低于 90% 扩批线，不能直接扩大到 500/1000；新误判已继续补进 `rules-v5` 回归规则。

`rules-v5` offset200 小批、`rules-v6` offset300 小批、`rules-v7` offset400 小批和最新回放已经形成可复查文件和报告：

- `rules-v5` 正式样本：`data/enrichment/sample-enrich-20260630-rules100-offset200-v5.jsonl`
- `rules-v5` 正式候选：`data/enrichment/candidates-enrich-20260630-rules100-offset200-v5.jsonl`
- `rules-v5` 抽检样本：`data/enrichment/manual-sample-enrich-20260630-rules100-offset200-v5.jsonl`
- `rules-v5` 人工写回文件：`data/enrichment/manual-reviewed-enrich-20260630-rules100-offset200-v5.jsonl`
- `rules-v5` 最终报告：`data/enrichment/review-report-enrich-20260630-rules100-offset200-v5-final.json`
- `rules-v5` 同步报告：`data/enrichment/review-report-enrich-20260630-rules100-offset200-v5.json`
- `rules-v6` 预览样本：`data/enrichment/sample-enrich-20260630-rules100-offset200-v6-preview.jsonl`
- `rules-v6` 预览候选：`data/enrichment/candidates-enrich-20260630-rules100-offset200-v6-preview.jsonl`
- `rules-v5` 当前结果：导出 100 首，生成并导入 37 条候选，跳过低信号 63 条；`reviewed_count=37`、`accepted_count=28`、`rejected_count=9`、`pending_count=0`、`pass_rate=0.7567567567567568`
- `rules-v6` 回放结果：导出 100 首，生成 28 条候选，跳过低信号 72 条；9 条退回样本均未再生成，28 条已通过样本均保留，标签与人工结果 28/28 对齐。
- `rules-v6` offset300 人工写回文件：`data/enrichment/manual-reviewed-enrich-20260630-rules100-offset300-v6.jsonl`
- `rules-v6` offset300 最终报告：`data/enrichment/review-report-enrich-20260630-rules100-offset300-v6-final.json`
- `rules-v6` offset300 当前结果：导出 100 首，生成并导入 39 条候选，跳过低信号 61 条；`reviewed_count=39`、`accepted_count=38`、`rejected_count=1`、`pending_count=0`、`pass_rate=0.9743589743589743`
- `rules-v7` offset300 预览样本：`data/enrichment/sample-enrich-20260630-rules100-offset300-v7-preview.jsonl`
- `rules-v7` offset300 预览候选：`data/enrichment/candidates-enrich-20260630-rules100-offset300-v7-preview.jsonl`
- `rules-v7` offset300 回放结果：导出 100 首，生成 38 条候选，跳过低信号 62 条；1 条退回样本未再生成，38 条已通过样本均保留，标签与人工结果 38/38 对齐。
- `rules-v7` offset400 人工写回文件：`data/enrichment/manual-reviewed-enrich-20260630-rules100-offset400-v7.jsonl`
- `rules-v7` offset400 最终报告：`data/enrichment/review-report-enrich-20260630-rules100-offset400-v7-final.json`
- `rules-v7` offset400 当前结果：导出 100 首，生成并导入 33 条候选，跳过低信号 67 条；`reviewed_count=33`、`accepted_count=26`、`rejected_count=7`、`pending_count=0`、`pass_rate=0.7878787878787878`
- `rules-v8` offset400 预览样本：`data/enrichment/sample-enrich-20260630-rules100-offset400-v8-preview.jsonl`
- `rules-v8` offset400 预览候选：`data/enrichment/candidates-enrich-20260630-rules100-offset400-v8-preview.jsonl`
- `rules-v8` offset400 回放结果：导出 100 首，生成 26 条候选，跳过低信号 74 条；7 条退回样本均未再生成，26 条已通过样本均保留，标签与人工结果 26/26 对齐。
- `rules-v8` offset500 人工写回文件：`data/enrichment/manual-reviewed-enrich-20260630-rules100-offset500-v8.jsonl`
- `rules-v8` offset500 修正项二次写回文件：`data/enrichment/manual-reviewed-enrich-20260630-rules100-offset500-v8-correct-fix.jsonl`
- `rules-v8` offset500 最终报告：`data/enrichment/review-report-enrich-20260630-rules100-offset500-v8-final.json`
- `rules-v8` offset500 当前结果：导出 100 首，生成并导入 56 条候选，跳过低信号 44 条；`reviewed_count=56`、`accepted_count=49`、`rejected_count=7`、`pending_count=0`、`pass_rate=0.875`
- `rules-v9` offset500 预览样本：`data/enrichment/sample-enrich-20260630-rules100-offset500-v9-preview.jsonl`
- `rules-v9` offset500 预览候选：`data/enrichment/candidates-enrich-20260630-rules100-offset500-v9-preview.jsonl`
- `rules-v9` offset500 回放结果：导出 100 首，生成 49 条候选，跳过低信号 51 条；7 条退回样本均未再生成，49 条已通过/修正样本均保留，标签与人工结果 49/49 对齐。
- `rules-v9` offset600 人工写回文件：`data/enrichment/manual-reviewed-enrich-20260630-rules100-offset600-v9.jsonl`
- `rules-v9` offset600 最终报告：`data/enrichment/review-report-enrich-20260630-rules100-offset600-v9-final.json`
- `rules-v9` offset600 当前结果：导出 100 首，生成并导入 49 条候选，跳过低信号 51 条；`reviewed_count=49`、`accepted_count=31`、`rejected_count=18`、`pending_count=0`、`pass_rate=0.6326530612244898`
- `rules-v10` offset600 预览样本：`data/enrichment/sample-enrich-20260630-rules100-offset600-v10-preview.jsonl`
- `rules-v10` offset600 预览候选：`data/enrichment/candidates-enrich-20260630-rules100-offset600-v10-preview.jsonl`
- `rules-v10` offset600 回放结果：导出 100 首，生成 31 条候选，跳过低信号 69 条；18 条退回样本均未再生成，31 条已通过/修正样本均保留，标签与人工结果 31/31 对齐。
- `rules-v10` offset700 人工写回文件：`data/enrichment/manual-reviewed-enrich-20260630-rules100-offset700-v10.jsonl`
- `rules-v10` offset700 最终报告：`data/enrichment/review-report-enrich-20260630-rules100-offset700-v10-final.json`
- `rules-v10` offset700 当前结果：导出 100 首，生成并导入 32 条候选，跳过低信号 68 条；`reviewed_count=32`、`accepted_count=17`、`rejected_count=15`、`pending_count=0`、`pass_rate=0.53125`
- `rules-v11` offset700 预览样本：`data/enrichment/sample-enrich-20260630-rules100-offset700-v11-preview.jsonl`
- `rules-v11` offset700 预览候选：`data/enrichment/candidates-enrich-20260630-rules100-offset700-v11-preview.jsonl`
- `rules-v11` offset700 回放结果：导出 100 首，生成 17 条候选，跳过低信号 83 条；15 条退回样本均未再生成，17 条已通过/修正样本均保留，标签与人工结果 17/17 对齐。
- `rules-v11` offset800 人工写回文件：`data/enrichment/manual-reviewed-enrich-20260630-rules100-offset800-v11.jsonl`
- `rules-v11` offset800 最终报告：`data/enrichment/review-report-enrich-20260630-rules100-offset800-v11-final.json`
- `rules-v11` offset800 当前结果：导出 100 首，生成并导入 35 条候选，跳过低信号 65 条；`reviewed_count=35`、`accepted_count=22`、`rejected_count=13`、`pending_count=0`、`pass_rate=0.6285714285714286`
- `rules-v12` offset800 预览样本：`data/enrichment/sample-enrich-20260630-rules100-offset800-v12-preview.jsonl`
- `rules-v12` offset800 预览候选：`data/enrichment/candidates-enrich-20260630-rules100-offset800-v12-preview.jsonl`
- `rules-v12` offset800 回放结果：导出 100 首，生成 22 条候选，跳过低信号 78 条；13 条退回样本均未再生成，22 条已通过/修正样本均保留，标签与人工结果 22/22 对齐。
- `rules-v12` offset900 人工写回文件：`data/enrichment/manual-reviewed-enrich-20260630-rules100-offset900-v12.jsonl`
- `rules-v12` offset900 最终报告：`data/enrichment/review-report-enrich-20260630-rules100-offset900-v12-final.json`
- `rules-v12` offset900 当前结果：导出 100 首，生成并导入 33 条候选，跳过低信号 67 条；`reviewed_count=33`、`accepted_count=24`、`rejected_count=9`、`pending_count=0`、`pass_rate=0.7272727272727273`
- `rules-v13` offset900 预览样本：`data/enrichment/sample-enrich-20260630-rules100-offset900-v13-preview.jsonl`
- `rules-v13` offset900 预览候选：`data/enrichment/candidates-enrich-20260630-rules100-offset900-v13-preview.jsonl`
- `rules-v13` offset900 回放结果：导出 100 首，生成 24 条候选，跳过低信号 76 条；9 条退回样本均未再生成，24 条已通过/修正样本均保留，标签与人工结果 24/24 对齐。
- 注意：`rules-v13 offset1000` 通过率 78.13%，仍低于 90% 扩批线；停止规则扩批，本节旧批次记录只作回归证据，后续走黄金评测集 + AI 候选 + 自动校验。

### 第一步：100 首提示词验证

- 目标是 100% 人工检查。
- 旧规则首批 20 条通过率只有 50%，已停止继续发布旧规则并回滚旧通过项。
- `rules-v3` 100 首复测已完成全量人工检查，通过率 91%，主要退回集中在泛化 `经典引用` 和戏曲对白/动作场景无安全标签。
- `rules-v4` 已把这类泛化 `经典引用` 误判改为低信号跳过，并完成 offset100 小批重建和 47 条全量人工检查：39 通过、8 退回，通过率 82.98%。
- `rules-v5` offset200 新小批 37 条全量人工检查后通过率 75.68%，仍低于 90%；已升级到 `rules-v6`，回放 offset200 与人工通过结果 28/28 对齐。
- `rules-v6` offset300 新小批 39 条全量人工检查后通过率 97.44%，但 `rules-v7` offset400 新小批 33 条全量人工检查后通过率 78.79%，仍需继续收紧。
- `rules-v8` offset500 新小批 56 条全量人工检查后通过率 87.50%，仍低于 90%；已升级到 `rules-v9`，回放 offset500 与人工通过/修正结果 49/49 对齐，7 条退回样本未再生成。
- `rules-v9` offset600 新小批 49 条全量人工检查后通过率 63.27%，明显低于 90%；已升级到 `rules-v10`，回放 offset600 与人工通过/修正结果 31/31 对齐，18 条退回样本未再生成。
- `rules-v10` offset700 新小批 32 条全量人工检查后通过率 53.13%，明显低于 90%；已升级到 `rules-v11`，回放 offset700 与人工通过/修正结果 17/17 对齐，15 条退回样本未再生成。
- `rules-v11` offset800 新小批 35 条全量人工检查后通过率 62.86%，明显低于 90%；已升级到 `rules-v12`，回放 offset800 与人工通过/修正结果 22/22 对齐，13 条退回样本未再生成。
- `rules-v12` offset900 新小批 33 条全量人工检查后通过率 72.73%，仍低于 90%；已升级到 `rules-v13`，回放 offset900 与人工通过/修正结果 24/24 对齐，9 条退回样本未再生成。
- 不再用规则小批继续硬磨到 500/1000 首；`rules-v13 offset1000` 已证明规则扩批不稳定，后续改走黄金评测集 + AI 候选 + 自动校验 + 抽样质检。

### 第二步：500 首质量闭环

- 至少抽检 30%。
- 通过率低于 90% 不扩大。
- 记录退回原因 Top 10。

### 第三步：1000 首运营试跑

- 至少抽检 20%。
- 按作者、主题、体裁分层抽检。
- 测试“中秋月亮”“毕业送别”“文旅山水”等热门召回场景。

## 9. 回滚

### 9.1 按批次回滚

```bash
go run ./cmd/enrichment \
  --db data/poetry.db \
  rollback \
  --run-id enrich-20260629-sample500 \
  --reviewer operator \
  --notes "批次质量不稳定，整批回滚"
```

### 9.2 按单首回滚

```bash
go run ./cmd/enrichment \
  --db data/poetry.db \
  rollback \
  --poem-id 123 \
  --reviewer operator \
  --notes "单首标签错误"
```

回滚会：

- 将已通过候选标记为 `rolled_back`。
- 删除对应 `poem_knowledge`。
- 删除该候选发布的诗词标签关联。
- 重新统计批次通过/退回数量。

## 10. 上线前检查清单

- [ ] 候选数据有唯一 `run_id`。
- [ ] `validate` 校验通过。
- [ ] 抽检比例达到当前批次要求。
- [ ] 已执行 `review-report` 或调用 summary 接口统计抽检通过率。
- [ ] 退回原因已通过 `review_notes` 归类，并进入 Top10 报告。
- [ ] 通过数据和退回数据状态分离。
- [ ] 回滚命令已测试。
- [ ] 测试环境知识库召回正常。
- [ ] 原始诗词接口不受影响。

## 11. 固定收口命令

数据增强后续不再输出开放式“当前建议”。统一由最终收口脚本判断状态：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\final_closeout.ps1 -Runner auto
```

补齐全量黄金集人工复核和真实商业记录后，运行最终门禁：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\final_closeout.ps1 `
  -Runner auto `
  -ApplyGolden `
  -AllowPaidQanlo `
  -ImportQanlo `
  -RequireDone
```

脚本规则：

1. 黄金集 CSV 未全部 `done`，不合并为最终黄金集。
2. 不带 `-AllowPaidQanlo`，不发起真实 Qanlo 付费调用。
3. `quality-gate` 有 error，不进入发布队列。
4. 真实 Qanlo 候选导入后，用 `scripts\ai_review_closeout.ps1` 固定完成人工抽样审计、dry-run 和写回。
5. 没有真实商业试用记录，不把示例数据当商业验证。
6. 只有最终 `ready_for_stop=true`，才算数据增强和商业化闭环达到终局验收。
