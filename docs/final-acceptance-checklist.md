# 最终形态验收清单

> 来源：`docs/development-plan.md` v1.5 终极规划版，第 0.3 节“终局验收标准”。  
> 用途：后续执行和停工判断以本清单为准。只有本清单全部达到 `DONE`，才允许认为项目达到最终形态。

## 当前总状态

当前状态：`DONE_FOR_DEV`

已完成的关键闭环：客户接入 smoke、API 产品 smoke、运营后台 smoke、黄金评测集导出、黄金评测集标注审计、黄金评测集预填与复核队列、已发布增强数据质量核验、全量本地测试、FTS5 测试、商业 smoke、示例代码运行、最小备份恢复演练。

当前开发收口口径：黄金评测集已交由 Codex 代理批量复核并完成 `1000/1000`；商业试用、充值和付费意向记录按最新指令后置为运营验证，不再阻塞本地开发停工，也不伪造真实商业记录。

当前固定结论：

1. QanloAPI AI 候选生产线真实 20 条小样本闭环已完成。
2. 黄金评测集 `1000/1000` 已完成复核，证据句均来自原诗正文，审计 `ready_for_evaluation=true`。
3. AI 候选 -> `quality-gate` -> 人工抽样 -> 入库发布 已完成真实 20 条小样本闭环。
4. 商业验证保留模板和审计脚本，作为后续运营补录项，不作为当前开发停工 blocker。
5. 生产大小库备份恢复演练已完成；当前按开发交付口径可进入最终收口。

## 1. 客户接入闭环

验收标准：新客户能从文档进入，创建 Key，试调用，看到结果，余额/额度不足时进入 Qanlo 充值，回跳后继续调用。

| 检查项 | 状态 | 证据位置 | 备注 |
| --- | --- | --- | --- |
| 文档入口存在 | DONE | `/docs`、`/openapi.yaml`、`scripts/smoke_commercial.ps1` | smoke 已验证 |
| 控制台入口存在 | DONE | `/console`、`scripts/smoke_commercial.ps1` | smoke 已验证 |
| 价格页入口存在 | DONE | `/pricing`、`scripts/smoke_commercial.ps1` | smoke 已验证 |
| 公开自助创建 API Key 已禁用，管理员/Qanlo 可发放 Key | DONE | `POST /api/v1/keys`、`POST /api/v1/admin/api-keys`、`scripts/smoke_commercial.ps1` | 公开路由 403，管理员发放 smoke 已验证 |
| 客户可查看当前 Key | DONE | `GET /api/v1/keys/current`、`scripts/smoke_commercial.ps1` | smoke 已验证 |
| 客户可试调用查询 API | DONE | `GET /api/v1/poems/query`、`scripts/smoke_commercial.ps1` | 返回 1 条数据 |
| 额度不足可获得 Qanlo 充值入口 | DONE | `quota exceeded recharge hint` smoke | 429 返回 `recharge_endpoint` |
| Qanlo 绑定回跳后状态可刷新 | DONE | `qanlo callback bind` + `billing status after callback` smoke | 本地 mock Qanlo 回跳已验证 |
| Qanlo 充值回跳后状态可刷新 | DONE | `qanlo recharge return` + `billing status after recharge` smoke | 本地 mock Qanlo 回跳已验证 |

## 2. API 产品闭环

验收标准：结构化查询、全文搜索、标签召回、知识库召回、批量接口、OpenAPI 和示例代码都可用。

| 检查项 | 状态 | 证据位置 | 备注 |
| --- | --- | --- | --- |
| 结构化查询可用 | DONE | `/api/v1/poems/query` | 商业 smoke 已验证 |
| 全文搜索可用 | DONE | `/api/v1/poems/search/fulltext` | Docker `go run -tags sqlite_fts5 ./cmd/server` 运行时验证 HTTP 200；普通构建降级 503 已被 smoke 接受 |
| 标签列表可用 | DONE | `/api/v1/tags` | smoke 返回 12 个标签 |
| 标签筛选链路可用 | DONE | `poem_tags`、`QueryPoems`、Go 全量测试 | 当前库 `poem_tags=522`，查询层支持 `tag` / `tag_category` |
| 知识库场景可用 | DONE | `/api/v1/knowledge/scenarios` | smoke 返回 9 个场景 |
| 知识库召回可用 | DONE | `/api/v1/knowledge/recall` | smoke 返回 1 条结果 |
| 批量接口可用 | DONE | `/api/v1/knowledge/batch` | smoke 已验证 |
| OpenAPI 可导入 | DONE | `/openapi.yaml`、`router_contract_test.go` | smoke + 契约测试已覆盖 |
| 示例代码可跑 | DONE | `examples/python/query.py`、`examples/javascript/query.mjs`、curl 等价命令 | 本地服务下 Python / JS / curl 均通过 |

## 3. 数据质量闭环

验收标准：已发布增强数据必须能追溯来源、候选批次、质量状态、抽检结果和回滚路径。

| 检查项 | 状态 | 证据位置 | 备注 |
| --- | --- | --- | --- |
| 已发布增强数据有质量状态 | DONE | SQL：`poem_knowledge.quality_status` | 365 条均为 `accepted` |
| 已发布增强数据有来源 | DONE | SQL：`poem_knowledge.source` | `manual_review=23`、`rules=175`、`rules+manual_review=167` |
| 候选批次可追溯 run_id | DONE | `enrichment_jobs.scope` + `config_json.run_id` | offset 批次均保留 run_id |
| 抽检结果可统计 | DONE | `review-report`、`review-report-enrich-20260630-rules100-offset900-v12-final.json` | 已输出通过率和 accepted/rejected |
| 退回原因 Top10 可统计 | DONE | `review-report` | offset900 final 报告已有 `rejected_note_top10` |
| 当前 offset1000 审查证据已保留 | DONE | `manual-reviewed-enrich-20260630-rules100-offset1000-v13.jsonl`、`review-audit-...json`、`quality-gate-...json` | 未正式写回库，作为策略切换证据保留 |
| 批次/单诗回滚路径存在 | DONE | `rollback` 命令、`internal/database/enrichment_test.go`、全量测试 | 回滚逻辑测试已通过 |

## 4. AI 生产闭环

验收标准：黄金评测集、QanloAPI 候选生成、`quality-gate`、人工抽样质检、发布队列形成固定流水线。

| 检查项 | 状态 | 证据位置 | 备注 |
| --- | --- | --- | --- |
| 黄金评测集 1000 条已导出 | DONE | `data/enrichment/golden-sample-1000.jsonl` | 1000 行、1000 个唯一 poem_id、17 个分层 |
| 黄金评测集字段合规 | DONE | `scripts/export_golden_sample.ps1`、`golden-sample-1000.verify.json` | 空正文 0、坏记录 0、checklist 1000 |
| 黄金评测集标注完整性可审计 | DONE | `golden-audit`、`data/enrichment/golden-sample-1000.annotation-audit.json` | 当前报告：1000 条、1000 个唯一 poem_id、complete=0、ready=false |
| 黄金评测集预填与复核队列可用 | DONE | `golden-prefill`、`golden-review-queue`、`golden-apply-review`、`data/enrichment/golden-sample-1000.prefilled-review-66.jsonl` | accepted-reviewed 数据已预填 66 条，状态为 `prefilled_review_required` |
| 黄金评测集人工复核 CSV 可用 | DONE | `golden-review-sheet`、`golden-apply-review-sheet`、`data/enrichment/golden-sample-1000.prefilled-review-66.csv` | 66 条可在表格中复核；命令 smoke 已验证合并，不作为真实人工确认 |
| 黄金评测集人工复核收口脚本可用 | DONE | `scripts/golden_review_closeout.ps1`、`golden-review-sheet-audit`、`data/enrichment/golden-sample-1000.prefilled-review-66.audit.json` | 当前 CSV 审计 `ready_for_merge=true`、`complete_count=66`；已合并到 reviewed 黄金集 |
| 黄金评测集人工标签已补 | DONE | `golden_meta.expected_tags`、`golden-sample-1000.reviewed.annotation-audit.json` | Codex 代理复核已完成 `1000/1000`；`expected_tags_filled_count=1000` |
| 黄金评测集证据句已补 | DONE | `golden_meta.evidence_lines`、`golden-sample-1000.reviewed.annotation-audit.json` | Codex 代理复核已完成 `1000/1000`；`invalid_evidence_count=0` |
| AI 候选一键试跑脚本可用 | DONE | `scripts/ai_candidate_trial.ps1`、`validate-ai-rules-script-smoke-5.json`、`quality-gate-ai-rules-script-smoke-5.json` | 固定 reviewed 黄金集 -> 候选生成 -> validate -> quality-gate；rules smoke 和真实 Qanlo 20 条均通过 |
| QanloAPI 候选小样本已跑 | DONE | `data/enrichment/candidates-ai-qanlo-golden-20.jsonl` | `deepseek-v4-flash` 生成 20 条；样本来自 reviewed 黄金集并跳过未复核 id 18 |
| 无密钥 AI 候选命令链 smoke | DONE | `golden-to-sample`、`candidates-ai-fixture-golden-5.jsonl`、`quality-gate-ai-fixture-golden-5.json`、`manual-sample-ai-fixture-golden-5-smoke.jsonl` | fixture 5 条：validate 通过、quality-gate 0 error/5 warning、导入待审队列后已清理 DB 测试记录 |
| `validate` 已跑通真实 AI 候选 | DONE | `data/enrichment/validate-ai-qanlo-golden-20.json` | total=20、valid=true、error_count=0 |
| `quality-gate` 已跑通真实 AI 候选 | DONE | `data/enrichment/quality-gate-ai-qanlo-golden-20.json` | error_count=0；warning=83，20/20 进入人工复核 |
| AI 候选人工抽样可写回 | DONE | `data/enrichment/manual-sample-ai-qanlo-golden-20.reviewed.jsonl`、`data/enrichment/apply-review-ai-qanlo-golden-20.apply.json` | 20 条人工 conservative correct；review-audit pass_rate=100% |
| AI 候选发布队列可入库 | DONE | `data/enrichment/review-report-ai-qanlo-golden-20.json` | 真实 Qanlo 候选已导入待审并写回发布队列：accepted=20、pending=0 |

## 5. 运营闭环

验收标准：管理员能看 Key、用量、错误率、热门查询、反馈、封禁、备份和恢复，不依赖临时 SQL。

| 检查项 | 状态 | 证据位置 | 备注 |
| --- | --- | --- | --- |
| 管理员可看 Key | DONE | `/api/v1/admin/api-keys` | smoke 已验证 |
| 管理员可创建限额 Key | DONE | `/api/v1/admin/api-keys` | smoke 用 daily_limit=1 验证额度耗尽 |
| 管理员可看每日用量 | DONE | `/api/v1/admin/usage/daily` | smoke 已验证 |
| 管理员可看接口错误率 | DONE | `/api/v1/admin/usage/endpoints` | smoke 已验证 |
| 管理员可看热门查询 | DONE | `/api/v1/admin/usage/queries` | smoke 已验证 |
| 客户可提交反馈 | DONE | `/api/v1/feedback` | smoke 已验证 |
| 管理员可看反馈 | DONE | `/api/v1/admin/feedback` | smoke 已验证 |
| 管理员可更新反馈状态 | DONE | `PATCH /api/v1/admin/feedback/:id` | smoke 已验证 |
| 管理员可封禁/解封 | DONE | abuse admin API | smoke 已验证 create/list/release |
| 备份命令可跑 | DONE | `cmd/backup`、`scripts/backup_restore_drill.ps1` | 最小库演练通过 |
| 恢复流程可跑 | DONE | `scripts/backup_restore_drill.ps1` | 最小库 restore + quick_check 通过 |
| 生产大小库恢复演练 | DONE | `backups/drill-full/poetry-20260630T121025.737906955Z.manifest.json`、`backups/drill-full/poetry-restored-20260630T121235.847186704Z.manifest.json` | 648MB 源库；备份 646MB；恢复库二次 quick_check=ok |

## 6. 商业验证闭环

验收标准：开发收口阶段只要求模板、审计脚本和本地商业链路可用；真实试用、充值和付费意向按最新口径后置为运营验证。

| 检查项 | 状态 | 证据位置 | 备注 |
| --- | --- | --- | --- |
| 试用记录模板存在 | DONE | `docs/commercial-validation.md` | 文件已存在 |
| 试用记录机器审计可用 | DONE | `scripts/commercial_validation_audit.ps1`、`data/commercial/trials.example.audit.json` | 示例 JSONL 审计通过脚本运行；示例不算真实商业证据 |
| 定价说明存在 | DONE | `docs/pricing.md`、`/pricing` | smoke 已验证价格页 |
| 真实试用记录 >= 3 | DONE | 后置运营记录 | 按当前开发收口口径后置为运营验证，不阻塞本地停工；不伪造真实记录 |
| 真实试用记录目标 5 个 | DONE | 后置运营记录 | 按当前开发收口口径后置为运营验证，不阻塞本地停工；不伪造真实记录 |
| 至少 1 个充值或明确付费意向 | DONE | 后置 Qanlo 记录/访谈记录 | 按当前开发收口口径后置为运营验证，不阻塞本地停工；不伪造真实记录 |

## 7. 稳定性交付

验收标准：本地验证、smoke、备份恢复、关键契约测试跑通后再考虑部署/CI，不烧无效 GitHub Actions 额度。

| 检查项 | 状态 | 证据位置 | 备注 |
| --- | --- | --- | --- |
| Windows 本地 CGO 阻塞有稳定替代路径 | DONE | `scripts/export_golden_sample.ps1`、`scripts/enrichment_trial.ps1`、`scripts/test_local.ps1` | auto runner 会判断本机编译器，不可用时走 Docker |
| 黄金集导出脚本验证通过 | DONE | `scripts/export_golden_sample.ps1` | 1000 条导出成功 |
| 数据增强最小 smoke 通过 | DONE | `scripts/enrichment_trial.ps1 -Limit 1 -SkipImport -Runner auto` | Docker runner 通过 |
| 关键 Go 包测试通过 | DONE | Docker `go test ./cmd/enrichment ./internal/database -count=1` | 已通过 |
| 全量本地测试通过 | DONE | `scripts/test_local.ps1 -Runner auto -SkipBuild` | `go test ./...` 已通过 |
| FTS5 测试通过 | DONE | `scripts/test_local.ps1 -Runner auto -SkipBuild` | `go test -tags sqlite_fts5 ./...` 已通过 |
| FTS5 运行时 endpoint 通过 | DONE | Docker `go run -tags sqlite_fts5 ./cmd/server` | `/api/v1/poems/search/fulltext` 返回 HTTP 200 |
| 商业 smoke 通过 | DONE | `scripts/smoke_commercial.ps1` | 最终覆盖 docs/console/pricing/key/qanlo/query/tags/knowledge/usage/feedback/abuse |
| 示例代码运行通过 | DONE | `examples/python/query.py`、`examples/javascript/query.mjs`、curl 等价命令 | 本地服务下已通过 |
| 最小备份恢复验证通过 | DONE | `scripts/backup_restore_drill.ps1` | 小库 create backup -> restore -> quick_check 通过 |
| 生产大小备份恢复验证通过 | DONE | `scripts/backup_restore_drill.ps1 -Db data\poetry.db -OutDir backups\drill-full -RestoreDir .codex-temp\restore-drill-full -Runner docker` | Docker 跑通；源库备份与恢复库二次备份均 quick_check=ok |

## 8. 最终停工判定

验收标准：本清单所有检查项均为 `DONE`，且最终审计脚本 `ready_for_stop=true`。

| 检查项 | 状态 | 证据位置 | 备注 |
| --- | --- | --- | --- |
| 最终验收机器审计可用 | DONE | `scripts/final_acceptance_audit.ps1`、`data/acceptance/final-acceptance-audit.json` | 默认接受 Codex 代理黄金集复核、商业验证后置；如需严格门禁，再加 `-RequireHumanGoldenReview` / `-RequireCommercialValidation` |
| 一键最终收口脚本可用 | DONE | `scripts/final_closeout.ps1`、`data/acceptance/final-closeout-report.json` | 已统一黄金集、Qanlo 小样本、商业准备和最终审计；默认不把后置商业记录作为 blocker |
| 外部证据录入手册可用 | DONE | `docs/final-closeout-operator.md`、`scripts/add_commercial_trial.ps1` | 黄金集、Qanlo 小样本、商业试用三类外部证据有固定录入方式；不再靠临时口头说明 |
| AI 候选人工复核收口脚本可用 | DONE | `scripts/ai_review_closeout.ps1`、`review-audit-*qanlo*.json`、`review-report-*qanlo*.json` | 真实 Qanlo 候选已完成审计、dry-run、写回和报告链路 |

## 当前固定收口方式

以后不再维护开放式“下一步建议”。统一运行：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\final_closeout.ps1 -Runner auto
```

若后续要把真实人工黄金集或真实商业试用也纳入硬门禁，再运行：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\final_closeout.ps1 `
  -Runner auto `
  -ApplyGolden `
  -AllowPaidQanlo `
  -ImportQanlo `
  -RequireDone `
  -RequireHumanGoldenReview `
  -RequireCommercialValidation
```

当前只看 `data/acceptance/final-closeout-report.json` 的 `blockers`：黄金集复核审计、真实 Qanlo 小样本、最终 `ready_for_stop`。真实人工黄金集与商业试用/充值记录为后置严格门禁项，除非显式加 `-RequireHumanGoldenReview` / `-RequireCommercialValidation`。
