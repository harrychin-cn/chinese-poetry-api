# 黄金评测集机器辅助候选

用途：给剩余黄金评测集样本预填“候选标签 + 原文证据句”，降低人工复核成本。

边界：

- 机器候选不等于人工复核。
- 输出中的 `machine_suggested_review_required` 必须由人确认后才能改成 `done`。
- 如果历史文件里存在 `reviewed_by=codex_agent` 或类似机器代理 `done`，本脚本会在候选包里降级为待人工确认，避免误当真人复核。
- 本流程不调用外部大模型，不消耗生图或 LLM 额度。
- `counts_as_human_review=false`，`ready_for_final_gate=false`。

生成：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\export_golden_review_suggestions.ps1 `
  -Input data/enrichment/golden-sample-1000.reviewed.jsonl `
  -OutDir data/enrichment/golden-review-suggestions `
  -BatchSize 100 `
  -RequireSuggestions
```

主要产物：

- `data/enrichment/golden-review-suggestions/golden-sample-1000.machine-suggested.jsonl`
- `data/enrichment/golden-review-suggestions/suggestion-report.json`
- `data/enrichment/golden-review-suggestions/golden-suggestion-batch-*.csv`
- `data/enrichment/golden-review-suggestions/README.md`

人工复核时，逐行检查 `expected_tags_json` 与 `evidence_lines_json`。只有确认标签被原文直接支持后，才把 `annotation_status` 改成 `done` 并用 `scripts/golden_review_closeout.ps1` 合并。
