# 黄金评测集分批复核包

黄金评测集不能直接用机器结果冒充人工复核。当前做法是把剩余待复核样本拆成小批 CSV，降低后续人工或 AI 辅助复核成本。

## 一键导出剩余待复核批次

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\export_golden_review_batches.ps1 `
  -Input data/enrichment/golden-sample-1000.reviewed.jsonl `
  -OutDir data/enrichment/golden-review-batches `
  -BatchSize 100 `
  -Status todo `
  -RequireRemaining
```

输出：

```text
data/enrichment/golden-review-batches/
```

## 审核者要改什么

每个 CSV 里主要看这几列：

- `expected_tags_json`：该诗词应该有哪些标签。
- `evidence_lines_json`：标签对应的原文证据句。
- `review_notes`：审核备注。
- `annotation_status`：只有真实审核完成后才能改成 `done`。

## 合并单批

先审完一批 CSV，并确认每行 `annotation_status=done`，再用 `scripts\golden_review_closeout.ps1` 合并到临时 reviewed 文件。

不要直接把机器导出的待审 CSV 当作已审结果。
