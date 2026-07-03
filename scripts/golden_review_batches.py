#!/usr/bin/env python3
"""Export unfinished golden-set records into reviewable CSV batches.

This tool does not mark any row as human-reviewed. It only packages the
remaining rows so they can be reviewed in small batches and later merged with
golden_review_closeout.ps1 after a real reviewer sets annotation_status=done.
"""

from __future__ import annotations

import argparse
import csv
import json
import math
from collections import Counter
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Iterable


CSV_HEADER = [
    "poem_id",
    "title",
    "author",
    "dynasty",
    "type",
    "content",
    "expected_tags_json",
    "evidence_lines_json",
    "annotation_status",
    "review_notes",
    "prefill_source",
    "stratum",
]


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--input",
        default="data/enrichment/golden-sample-1000.reviewed.jsonl",
        help="Current golden JSONL file.",
    )
    parser.add_argument(
        "--out-dir",
        default="data/enrichment/golden-review-batches",
        help="Directory for generated review batches.",
    )
    parser.add_argument("--batch-size", type=int, default=100, help="Rows per batch.")
    parser.add_argument(
        "--status",
        default="todo",
        help="Annotation status to export. Use 'incomplete' for every non-done row.",
    )
    parser.add_argument("--prefix", default="golden-review-batch", help="Batch filename prefix.")
    parser.add_argument("--report", default="batch-report.json", help="Report filename under out-dir.")
    return parser.parse_args()


def read_jsonl(path: Path) -> list[dict[str, Any]]:
    records: list[dict[str, Any]] = []
    with path.open("r", encoding="utf-8-sig") as f:
        for line_no, line in enumerate(f, start=1):
            text = line.strip()
            if not text:
                continue
            try:
                records.append(json.loads(text))
            except json.JSONDecodeError as exc:
                raise SystemExit(f"{path}:{line_no}: invalid JSON: {exc}") from exc
    return records


def meta(record: dict[str, Any]) -> dict[str, Any]:
    value = record.get("golden_meta")
    return value if isinstance(value, dict) else {}


def status_of(record: dict[str, Any]) -> str:
    return str(meta(record).get("annotation_status") or "").strip() or "todo"


def should_export(record: dict[str, Any], status: str) -> bool:
    current = status_of(record)
    if status == "incomplete":
        return current != "done"
    return current == status


def text_lines(value: Any) -> list[str]:
    if isinstance(value, list):
        return [str(item).strip() for item in value if str(item).strip()]
    text = str(value or "").strip()
    return [text] if text else []


def json_cell(value: Any) -> str:
    if value is None:
        value = []
    return json.dumps(value, ensure_ascii=False, separators=(",", ":"))


def row_for(record: dict[str, Any]) -> list[str]:
    m = meta(record)
    return [
        str(record.get("poem_id") or ""),
        str(record.get("title") or ""),
        str(record.get("author") or ""),
        str(record.get("dynasty") or ""),
        str(record.get("type") or ""),
        "\n".join(text_lines(record.get("content"))),
        json_cell(m.get("expected_tags") or []),
        json_cell(m.get("evidence_lines") or []),
        status_of(record),
        str(m.get("review_notes") or ""),
        str(m.get("prefill_source") or ""),
        str(m.get("stratum") or ""),
    ]


def write_csv(path: Path, records: Iterable[dict[str, Any]]) -> int:
    count = 0
    with path.open("w", encoding="utf-8-sig", newline="") as f:
        writer = csv.writer(f)
        writer.writerow(CSV_HEADER)
        for record in records:
            writer.writerow(row_for(record))
            count += 1
    return count


def write_readme(out_dir: Path, report: dict[str, Any]) -> None:
    next_file = report["batches"][0]["file"] if report["batches"] else ""
    content = f"""# 黄金评测集分批复核包

这个目录把还没有完成人工复核的黄金评测集拆成小批 CSV。

## 当前进度

- 总样本：{report["total_records"]}
- 已完成：{report["done_count"]}
- 待复核：{report["exported_count"]}
- 批次数：{report["batch_count"]}
- 每批大小：{report["batch_size"]}

## 边界

这些 CSV 只是“待复核包”，不会自动变成人工复核结果。

审核者需要打开 CSV，检查或填写：

- `expected_tags_json`
- `evidence_lines_json`
- `review_notes`
- `annotation_status`

只有真实审核后把 `annotation_status` 改成 `done`，才可以合并回黄金集。

## 下一批

```text
{next_file}
```

## 单批合并示例

先把该批 CSV 全部审完并把每行 `annotation_status` 改为 `done`，再运行：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\\golden_review_closeout.ps1 `
  -Runner auto `
  -Base data/enrichment/golden-sample-1000.reviewed.jsonl `
  -Sheet {next_file} `
  -Out data/enrichment/golden-sample-1000.reviewed.next.jsonl `
  -AuditOut data/enrichment/golden-sample-1000.reviewed.next.annotation-audit.json `
  -SheetAuditOut {next_file}.audit.json `
  -Apply
```

确认 `*.next.jsonl` 无误后，再替换正式 reviewed 文件。
"""
    (out_dir / "README.md").write_text(content, encoding="utf-8")


def main() -> int:
    args = parse_args()
    if args.batch_size < 1:
        raise SystemExit("--batch-size must be positive")
    if args.status not in {"todo", "prefilled_review_required", "machine_suggested_review_required", "incomplete"}:
        raise SystemExit("--status must be todo, prefilled_review_required, machine_suggested_review_required, or incomplete")

    input_path = Path(args.input)
    out_dir = Path(args.out_dir)
    out_dir.mkdir(parents=True, exist_ok=True)

    records = read_jsonl(input_path)
    status_counts = Counter(status_of(record) for record in records)
    done_count = status_counts.get("done", 0)
    export_records = [record for record in records if should_export(record, args.status)]
    stratum_counts = Counter(str(meta(record).get("stratum") or "未分层") for record in export_records)
    batch_count = math.ceil(len(export_records) / args.batch_size) if export_records else 0

    batches: list[dict[str, Any]] = []
    for idx in range(batch_count):
        start = idx * args.batch_size
        end = min(start + args.batch_size, len(export_records))
        batch_records = export_records[start:end]
        batch_path = out_dir / f"{args.prefix}-{idx + 1:03d}.csv"
        count = write_csv(batch_path, batch_records)
        poem_ids = [int(record.get("poem_id") or 0) for record in batch_records]
        batches.append(
            {
                "index": idx + 1,
                "file": str(batch_path).replace("\\", "/"),
                "count": count,
                "poem_id_min": min(poem_ids) if poem_ids else None,
                "poem_id_max": max(poem_ids) if poem_ids else None,
            }
        )

    report: dict[str, Any] = {
        "created_at": datetime.now(timezone.utc).isoformat(),
        "input": str(input_path).replace("\\", "/"),
        "out_dir": str(out_dir).replace("\\", "/"),
        "batch_size": args.batch_size,
        "export_status": args.status,
        "total_records": len(records),
        "done_count": done_count,
        "exported_count": len(export_records),
        "remaining_to_done": len(records) - done_count,
        "batch_count": batch_count,
        "status_counts": dict(sorted(status_counts.items())),
        "stratum_counts_top20": dict(stratum_counts.most_common(20)),
        "counts_as_human_review": False,
        "ready_for_final_gate": done_count == len(records),
        "batches": batches,
        "next_step": "Review each CSV, set annotation_status=done only after real review, then merge with golden_review_closeout.ps1.",
    }

    report_path = out_dir / args.report
    report_path.write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
    write_readme(out_dir, report)
    print(json.dumps(report, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
