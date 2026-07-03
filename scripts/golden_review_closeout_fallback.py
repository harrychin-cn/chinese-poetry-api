#!/usr/bin/env python3
"""Pure-Python fallback for scripts/golden_review_closeout.ps1.

The normal path shells out to `go run ./cmd/enrichment`, which needs either a
local CGO toolchain or Docker because that command package imports the SQLite
layer. Golden review closeout only needs CSV/JSONL processing, so this fallback
keeps the final closeout path usable on Windows machines without gcc/Docker.
"""

from __future__ import annotations

import argparse
import csv
import json
import sys
from collections import Counter
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8")
if hasattr(sys.stderr, "reconfigure"):
    sys.stderr.reconfigure(encoding="utf-8")

REVIEWED_STATUSES = {"done", "reviewed", "accepted", "complete", "completed"}


def non_empty_lines(lines: list[str]) -> list[str]:
    return [line.strip() for line in lines if line and line.strip()]


def reviewed_status(status: Any) -> bool:
    return str(status or "").strip().lower() in REVIEWED_STATUSES


def parse_tags(value: str, line: int, poem_id: int) -> list[Any]:
    value = (value or "").strip()
    if not value:
        return []
    try:
        parsed = json.loads(value)
    except json.JSONDecodeError as exc:
        raise ValueError(f"line {line} poem_id={poem_id} expected_tags_json: {exc}") from exc
    if isinstance(parsed, list) and all(isinstance(item, str) for item in parsed):
        return [
            {"name": item.strip(), "category": "theme", "source": "human_review"}
            for item in parsed
            if item.strip()
        ]
    if isinstance(parsed, list):
        return parsed
    raise ValueError(f"line {line} poem_id={poem_id} expected_tags_json: must be a JSON array")


def parse_string_array(value: str, line: int, poem_id: int) -> list[str]:
    value = (value or "").strip()
    if not value:
        return []
    try:
        parsed = json.loads(value)
    except json.JSONDecodeError as exc:
        raise ValueError(f"line {line} poem_id={poem_id} evidence_lines_json: {exc}") from exc
    if not isinstance(parsed, list):
        raise ValueError(f"line {line} poem_id={poem_id} evidence_lines_json: must be a JSON array")
    return non_empty_lines([str(item) for item in parsed])


def tags_len(value: Any) -> int:
    if value is None:
        return 0
    if isinstance(value, list):
        count = 0
        for item in value:
            if isinstance(item, dict):
                if str(item.get("name", "")).strip() or str(item.get("category", "")).strip():
                    count += 1
            elif str(item).strip():
                count += 1
        return count
    return 0


def string_slice(value: Any) -> list[str]:
    if value is None:
        return []
    if isinstance(value, list):
        return non_empty_lines([str(item) for item in value])
    return []


def evidence_in_content(evidence: str, content: list[str]) -> bool:
    evidence = (evidence or "").strip()
    if not evidence:
        return False
    for line in content:
        line = (line or "").strip()
        if not line:
            continue
        if line == evidence or evidence in line or line in evidence:
            return True
    return evidence in "\n".join(content)


def load_jsonl(path: Path) -> list[dict[str, Any]]:
    records: list[dict[str, Any]] = []
    with path.open("r", encoding="utf-8") as file:
        for index, line in enumerate(file, start=1):
            line = line.strip()
            if not line:
                continue
            try:
                records.append(json.loads(line))
            except json.JSONDecodeError as exc:
                raise ValueError(f"{path}:{index}: invalid JSONL: {exc}") from exc
    return records


def write_jsonl(path: Path, records: list[dict[str, Any]]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8", newline="\n") as file:
        for record in records:
            file.write(json.dumps(record, ensure_ascii=False, separators=(",", ":")))
            file.write("\n")


def load_review_sheet(path: Path) -> list[dict[str, Any]]:
    with path.open("r", encoding="utf-8-sig", newline="") as file:
        reader = csv.DictReader(file)
        if not reader.fieldnames:
            raise ValueError("review sheet is empty")
        headers = {name.strip() for name in reader.fieldnames}
        required = {"poem_id", "content", "expected_tags_json", "evidence_lines_json", "annotation_status"}
        missing = sorted(required - headers)
        if missing:
            raise ValueError(f"review sheet missing required column {missing[0]!r}")

        records: list[dict[str, Any]] = []
        for line, row in enumerate(reader, start=2):
            if not any((value or "").strip() for value in row.values()):
                continue
            try:
                poem_id = int((row.get("poem_id") or "").strip())
            except ValueError as exc:
                raise ValueError(f"line {line}: poem_id must be positive") from exc
            if poem_id < 1:
                raise ValueError(f"line {line}: poem_id must be positive")

            tags = parse_tags(row.get("expected_tags_json", ""), line, poem_id)
            evidence_lines = parse_string_array(row.get("evidence_lines_json", ""), line, poem_id)
            meta: dict[str, Any] = {
                "expected_tags": tags,
                "evidence_lines": evidence_lines,
                "annotation_status": (row.get("annotation_status") or "").strip(),
            }
            for key in ("review_notes", "prefill_source", "stratum"):
                value = (row.get(key) or "").strip()
                if value:
                    meta[key] = value

            records.append(
                {
                    "poem_id": poem_id,
                    "title": (row.get("title") or "").strip(),
                    "author": (row.get("author") or "").strip(),
                    "dynasty": (row.get("dynasty") or "").strip(),
                    "type": (row.get("type") or "").strip(),
                    "content": non_empty_lines((row.get("content") or "").splitlines()),
                    "golden_meta": meta,
                }
            )
    return records


def build_audit(input_path: str, records: list[dict[str, Any]], min_complete_rate: float = 1.0) -> dict[str, Any]:
    seen: Counter[int] = Counter()
    status_counts: Counter[str] = Counter()
    stratum_counts: Counter[str] = Counter()
    issue_counts: Counter[str] = Counter()
    issues: list[dict[str, Any]] = []

    def add_issue(poem_id: int, reason: str, evidence: str = "") -> None:
        issue_counts[reason] += 1
        if len(issues) < 20:
            issue: dict[str, Any] = {"reason": reason}
            if poem_id:
                issue["poem_id"] = poem_id
            if evidence:
                issue["evidence"] = evidence
            issues.append(issue)

    missing_content_count = 0
    missing_meta_count = 0
    expected_tags_filled_count = 0
    evidence_lines_filled_count = 0
    reviewed_status_count = 0
    complete_count = 0
    invalid_evidence_count = 0
    duplicate_poem_ids: list[int] = []

    for record in records:
        poem_id = int(record.get("poem_id") or 0)
        seen[poem_id] += 1
        if seen[poem_id] == 2:
            duplicate_poem_ids.append(poem_id)
            add_issue(poem_id, "duplicate_poem_id", str(poem_id))

        content = non_empty_lines([str(item) for item in record.get("content") or []])
        title = str(record.get("title") or "")
        if not content:
            missing_content_count += 1
            add_issue(poem_id, "missing_content", title)

        meta = record.get("golden_meta")
        if not isinstance(meta, dict):
            missing_meta_count += 1
            add_issue(poem_id, "missing_golden_meta", title)
            continue

        status = str(meta.get("annotation_status") or "").strip()
        if not status:
            status = "(empty)"
            add_issue(poem_id, "missing_annotation_status", title)
        status_counts[status] += 1
        if reviewed_status(status):
            reviewed_status_count += 1

        stratum = str(meta.get("stratum") or "").strip()
        if not stratum:
            stratum = "(empty)"
            add_issue(poem_id, "missing_stratum", title)
        stratum_counts[stratum] += 1

        expected_count = tags_len(meta.get("expected_tags"))
        evidence_lines = string_slice(meta.get("evidence_lines"))
        if expected_count > 0:
            expected_tags_filled_count += 1
        else:
            add_issue(poem_id, "missing_expected_tags", title)
        if evidence_lines:
            evidence_lines_filled_count += 1
        else:
            add_issue(poem_id, "missing_evidence_lines", title)

        invalid = False
        for evidence in evidence_lines:
            if not evidence_in_content(evidence, content):
                invalid_evidence_count += 1
                invalid = True
                add_issue(poem_id, "invalid_evidence_line", evidence)

        if expected_count > 0 and evidence_lines and not invalid and reviewed_status(status):
            complete_count += 1

    total = len(records)
    complete_rate = (complete_count / total) if total else 0.0
    ready = (
        total > 0
        and complete_rate >= min_complete_rate
        and missing_content_count == 0
        and missing_meta_count == 0
        and invalid_evidence_count == 0
        and not duplicate_poem_ids
    )
    report: dict[str, Any] = {
        "input": input_path,
        "total": total,
        "unique_poem_ids": len(seen),
        "missing_content_count": missing_content_count,
        "missing_meta_count": missing_meta_count,
        "expected_tags_filled_count": expected_tags_filled_count,
        "evidence_lines_filled_count": evidence_lines_filled_count,
        "reviewed_status_count": reviewed_status_count,
        "complete_count": complete_count,
        "complete_rate": complete_rate,
        "complete_rate_percent": f"{complete_rate * 100:.2f}%",
        "invalid_evidence_count": invalid_evidence_count,
        "min_complete_rate": min_complete_rate,
        "ready_for_evaluation": ready,
        "status_counts": dict(status_counts),
        "stratum_counts": [
            {"stratum": key, "count": count}
            for key, count in sorted(stratum_counts.items(), key=lambda item: (-item[1], item[0]))
        ],
        "issue_top10": [
            {"reason": key, "count": count}
            for key, count in sorted(issue_counts.items(), key=lambda item: (-item[1], item[0]))[:10]
        ],
        "required_action": (
            "golden set is ready for AI evaluation"
            if ready
            else "fill expected_tags, evidence_lines, and reviewed annotation_status before using this golden set as a gate"
        ),
    }
    if duplicate_poem_ids:
        report["duplicate_poem_ids"] = duplicate_poem_ids
    if issues:
        report["issue_examples"] = issues
    return report


def write_json(path: Path, value: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def apply_review(
    base_records: list[dict[str, Any]],
    review_records: list[dict[str, Any]],
    reviewer: str,
) -> tuple[list[dict[str, Any]], dict[str, Any]]:
    reviewer = reviewer.strip() or "operator"
    review_by_id: dict[int, dict[str, Any]] = {}
    for record in review_records:
        poem_id = int(record.get("poem_id") or 0)
        if poem_id < 1:
            raise ValueError("review record has invalid poem_id")
        if poem_id in review_by_id:
            raise ValueError(f"duplicate review poem_id={poem_id}")
        meta = record.get("golden_meta") or {}
        if not reviewed_status(meta.get("annotation_status")):
            raise ValueError(f"poem_id={poem_id} annotation_status must be done/reviewed/accepted/complete")
        if tags_len(meta.get("expected_tags")) == 0:
            raise ValueError(f"poem_id={poem_id} expected_tags is required")
        evidence_lines = string_slice(meta.get("evidence_lines"))
        if not evidence_lines:
            raise ValueError(f"poem_id={poem_id} evidence_lines is required")
        content = non_empty_lines([str(item) for item in record.get("content") or []])
        for evidence in evidence_lines:
            if not evidence_in_content(evidence, content):
                raise ValueError(f"poem_id={poem_id} evidence line is not in content: {evidence}")
        review_by_id[poem_id] = record

    out = list(base_records)
    applied = 0
    reviewed_at = datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")
    for index, record in enumerate(out):
        poem_id = int(record.get("poem_id") or 0)
        reviewed = review_by_id.pop(poem_id, None)
        if reviewed is None:
            continue
        reviewed = json.loads(json.dumps(reviewed, ensure_ascii=False))
        meta = dict(reviewed.get("golden_meta") or {})
        meta["reviewed_by"] = reviewer
        meta["reviewed_at"] = reviewed_at
        reviewed["golden_meta"] = meta
        out[index] = reviewed
        applied += 1

    if review_by_id:
        missing = sorted(review_by_id)
        raise ValueError(f"review poem_id not found in base: {missing}")

    report = {
        "base_total": len(base_records),
        "review_total": len(review_records),
        "applied": applied,
        "reviewer": reviewer,
        "require_done": True,
        "next_step": "run golden-audit on output and use it as the next golden sample if ready",
        "remaining_review": len(base_records) - applied,
    }
    return out, report


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--base", required=True)
    parser.add_argument("--sheet", required=True)
    parser.add_argument("--out", required=True)
    parser.add_argument("--audit-out", required=True)
    parser.add_argument("--sheet-audit-out", required=True)
    parser.add_argument("--reviewer", default="operator")
    parser.add_argument("--apply", action="store_true")
    parser.add_argument("--require-done", action="store_true")
    args = parser.parse_args()

    try:
        sheet_records = load_review_sheet(Path(args.sheet))
        sheet_audit = build_audit(args.sheet, sheet_records, 1.0)
        sheet_report = {
            "sheet": args.sheet,
            "total": sheet_audit["total"],
            "complete_count": sheet_audit["complete_count"],
            "ready_for_merge": sheet_audit["ready_for_evaluation"],
            "issue_count": len(sheet_audit.get("issue_examples", [])),
            "audit": sheet_audit,
            "next_step": "when ready_for_merge is true, run golden-apply-review-sheet and then golden-audit on the merged golden JSONL",
        }
        write_json(Path(args.sheet_audit_out), sheet_report)
        print(json.dumps(sheet_report, ensure_ascii=False, indent=2))
        if args.require_done and not sheet_audit["ready_for_evaluation"]:
            raise ValueError(f"golden review sheet is not ready: {sheet_audit['required_action']}")

        if not args.apply:
            print()
            print("Apply switch is not set; sheet is complete enough to merge, but no merged golden file was written.")
            print(
                json.dumps(
                    {
                        "mode": "dry_run",
                        "base": args.base,
                        "sheet": args.sheet,
                        "sheet_audit": args.sheet_audit_out,
                        "output": args.out,
                        "final_audit": args.audit_out,
                        "next_step": "rerun with -Apply to write merged golden JSONL and final audit",
                    },
                    ensure_ascii=False,
                    indent=2,
                )
            )
            return 0

        base_records = load_jsonl(Path(args.base))
        merged_records, apply_report = apply_review(base_records, sheet_records, args.reviewer)
        write_jsonl(Path(args.out), merged_records)
        apply_report.update({"base": args.base, "sheet": args.sheet, "output": args.out})
        print(json.dumps(apply_report, ensure_ascii=False, indent=2))

        final_audit = build_audit(args.out, merged_records, 1.0)
        write_json(Path(args.audit_out), final_audit)
        print(json.dumps(final_audit, ensure_ascii=False, indent=2))
        print(
            json.dumps(
                {
                    "mode": "apply",
                    "base": args.base,
                    "sheet": args.sheet,
                    "sheet_audit": args.sheet_audit_out,
                    "output": args.out,
                    "final_audit": args.audit_out,
                    "reviewer": args.reviewer,
                },
                ensure_ascii=False,
                indent=2,
            )
        )
        return 0
    except Exception as exc:  # noqa: BLE001
        print(str(exc), file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
