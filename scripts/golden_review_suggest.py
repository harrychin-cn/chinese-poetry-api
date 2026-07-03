#!/usr/bin/env python3
"""Create machine-assisted golden review suggestions without marking rows done.

The output is a helper package for human review:
- existing `done` rows are preserved as-is;
- unfinished rows may be prefilled with conservative rule-based tags/evidence;
- machine-prefilled rows use `annotation_status=machine_suggested_review_required`;
- `counts_as_human_review` and `ready_for_final_gate` are always false.
"""

from __future__ import annotations

import argparse
import csv
import copy
import json
import math
from collections import Counter
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Iterable


RULE_VERSION = "golden-review-suggest-v1"
MACHINE_STATUS = "machine_suggested_review_required"
MACHINE_SOURCE = "deterministic_rule_suggestion"

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

CATEGORY_ORDER = {
    "mood": 10,
    "theme": 20,
    "season": 30,
    "scenario": 40,
}

ALLOWED_TAG_NAMES = {
    "愁绪",
    "月亮",
    "春天",
    "相思",
    "宴乐歌舞",
    "山水",
    "送别",
    "家国",
    "思乡",
    "文旅",
    "边塞",
}


@dataclass(frozen=True)
class TagRule:
    category: str
    name: str
    strong: tuple[str, ...]
    weak: tuple[str, ...]
    negative: tuple[str, ...] = ()
    min_score: int = 2
    max_evidence: int = 2


@dataclass
class RuleHit:
    category: str
    name: str
    score: int
    confidence: float
    evidence_lines: list[str]
    matched_terms: list[str]


RULES: tuple[TagRule, ...] = (
    TagRule(
        category="mood",
        name="愁绪",
        strong=("愁", "恨", "泪", "断肠", "惆怅", "凄凉", "寂寞", "孤灯", "悲", "伤心", "离恨", "无奈", "憔悴", "病", "奈何"),
        weak=("冷", "残", "夜长", "难眠", "空", "怨", "苦", "不见", "梦断"),
        negative=("无愁", "不愁"),
        min_score=2,
    ),
    TagRule(
        category="mood",
        name="相思",
        strong=("相思", "思君", "忆君", "梦君", "郎", "妾", "鸳鸯", "红豆", "心期", "佳人", "人不见", "离恨"),
        weak=("思", "梦", "情", "恨", "断肠"),
        negative=("思量国事", "思政"),
        min_score=3,
    ),
    TagRule(
        category="theme",
        name="月亮",
        strong=("明月", "初月", "新月", "残月", "皓月", "孤月", "秋月", "月色", "月明", "月华", "月影", "蟾", "桂魄", "婵娟", "嫦娥", "玉兔"),
        weak=("月",),
        negative=("风月", "岁月", "年月", "日月", "花月约", "月俸"),
        min_score=2,
    ),
    TagRule(
        category="season",
        name="春天",
        strong=("春风", "春色", "春愁", "春雨", "春草", "春花", "春水", "春日", "春光", "芳春", "暮春", "早春", "东风"),
        weak=("春", "柳", "桃花", "莺", "燕", "芳草", "落花", "花枝", "梨花", "杏花"),
        negative=("春梦", "春心", "青春", "临春", "春衫", "春宫", "一场春梦"),
        min_score=2,
    ),
    TagRule(
        category="theme",
        name="思乡",
        strong=("故乡", "故园", "乡关", "家山", "归乡", "归梦", "客舍", "旅馆", "羁旅", "天涯", "归去", "归来"),
        weak=("客", "乡", "归", "旅", "家", "故人", "故国"),
        negative=("故国", "国家", "家国"),
        min_score=3,
    ),
    TagRule(
        category="scenario",
        name="送别",
        strong=("送别", "别离", "离亭", "长亭", "南浦", "折柳", "送君", "别君", "送客", "饯"),
        weak=("送", "别", "行人", "离", "渡头", "归棹"),
        negative=("别有", "别院", "别殿", "别巷", "离愁", "离恨", "离魂"),
        min_score=3,
    ),
    TagRule(
        category="theme",
        name="山水",
        strong=("山水", "青山", "远山", "江山", "江水", "溪水", "湖山", "烟波", "云山", "峰峦", "流水", "沧浪", "渔舟", "孤舟", "扁舟", "江南", "江上"),
        weak=("山", "水", "江", "河", "溪", "湖", "峰", "岭", "舟", "波", "云", "烟", "泉", "林", "松", "竹", "渔", "汀", "岸", "滩"),
        min_score=4,
    ),
    TagRule(
        category="scenario",
        name="宴乐歌舞",
        strong=("歌舞", "管弦", "笙歌", "清歌", "舞榭", "宴", "觞", "画堂", "玉楼", "红楼", "罗幕", "锦筵", "尊前", "樽前"),
        weak=("歌", "舞", "酒", "琴", "弦", "箫", "笛", "乐", "杯", "席", "筵", "醉"),
        min_score=3,
    ),
    TagRule(
        category="theme",
        name="家国",
        strong=("故国", "亡国", "家国", "社稷", "君王", "宫阙", "金陵", "长安", "汴京", "帝王", "龙楼", "凤阁", "宗庙", "黍离"),
        weak=("国", "君", "王", "宫", "帝", "朝", "京", "城", "都"),
        min_score=3,
    ),
    TagRule(
        category="theme",
        name="边塞",
        strong=("边塞", "沙场", "烽火", "玉门关", "楼兰", "阴山", "边城", "胡马", "羌笛", "塞上", "塞下", "征人", "戍鼓", "戍楼", "陇头"),
        weak=("塞", "胡", "羌", "兵", "战", "戍", "关", "马", "旗", "鼓"),
        negative=("鸡塞远",),
        min_score=4,
    ),
)

TRAVEL_STRONG_TERMS = ("游", "登", "寻", "过", "宿", "寺", "楼", "亭", "舟", "江南", "山水", "烟波")
TRAVEL_NEGATIVE_TERMS = ("故国", "亡国", "愁", "恨", "泪", "惆怅", "断肠")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--input",
        default="data/enrichment/golden-sample-1000.reviewed.jsonl",
        help="Current golden JSONL file.",
    )
    parser.add_argument(
        "--out-dir",
        default="data/enrichment/golden-review-suggestions",
        help="Directory for suggestion package.",
    )
    parser.add_argument(
        "--jsonl-name",
        default="golden-sample-1000.machine-suggested.jsonl",
        help="Output JSONL filename under out-dir.",
    )
    parser.add_argument("--batch-size", type=int, default=100, help="Rows per CSV batch.")
    parser.add_argument("--prefix", default="golden-suggestion-batch", help="CSV batch filename prefix.")
    parser.add_argument("--report", default="suggestion-report.json", help="Report filename under out-dir.")
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


def write_jsonl(path: Path, records: Iterable[dict[str, Any]]) -> None:
    with path.open("w", encoding="utf-8", newline="\n") as f:
        for record in records:
            f.write(json.dumps(record, ensure_ascii=False, separators=(",", ":")))
            f.write("\n")


def meta(record: dict[str, Any]) -> dict[str, Any]:
    value = record.get("golden_meta")
    return value if isinstance(value, dict) else {}


def status_of(record: dict[str, Any]) -> str:
    return str(meta(record).get("annotation_status") or "").strip() or "todo"


def is_machine_reviewed_done(record: dict[str, Any]) -> bool:
    """Detect rows that were marked done by an agent/machine, not a human reviewer."""

    m = meta(record)
    if status_of(record) != "done":
        return False
    reviewer = str(m.get("reviewed_by") or "").strip().lower()
    prefill_source = str(m.get("prefill_source") or "").strip().lower()
    if reviewer in {"codex_agent", "agent", "machine"}:
        return True
    if prefill_source.startswith("codex_agent") or "machine" in prefill_source:
        return True
    tags = m.get("expected_tags") or []
    if isinstance(tags, list) and tags:
        sources = {str(tag.get("source") or "").strip().lower() for tag in tags if isinstance(tag, dict)}
        if sources and all(source.startswith("codex_agent") or "machine" in source for source in sources):
            return True
    return False


def is_human_done(record: dict[str, Any]) -> bool:
    return status_of(record) == "done" and not is_machine_reviewed_done(record)


def content_lines(record: dict[str, Any]) -> list[str]:
    value = record.get("content")
    if isinstance(value, list):
        return [str(item).strip() for item in value if str(item).strip()]
    text = str(value or "").strip()
    return [line.strip() for line in text.splitlines() if line.strip()] if text else []


def has_any(text: str, terms: Iterable[str]) -> bool:
    return any(term and term in text for term in terms)


def line_score(line: str, rule: TagRule) -> tuple[int, list[str]]:
    if has_any(line, rule.negative):
        return 0, []

    matched: list[str] = []
    score = 0
    for term in rule.strong:
        if term in line:
            matched.append(term)
            score += 2
    for term in rule.weak:
        if term in line:
            matched.append(term)
            score += 1
    return score, matched


def apply_rule(lines: list[str], rule: TagRule) -> RuleHit | None:
    total = 0
    evidence: list[str] = []
    matched_terms: list[str] = []
    for line in lines:
        score, matched = line_score(line, rule)
        if score <= 0:
            continue
        total += score
        matched_terms.extend(matched)
        if line not in evidence:
            evidence.append(line)

    if total < rule.min_score:
        return None

    distinct_terms = sorted(set(matched_terms), key=matched_terms.index)
    # Avoid broad landscape tags from only one weak token repeated.
    if rule.name == "山水" and len(distinct_terms) < 2 and not any(term in rule.strong for term in distinct_terms):
        return None

    confidence = min(0.95, round(0.48 + total * 0.07 + len(distinct_terms) * 0.02, 2))
    return RuleHit(
        category=rule.category,
        name=rule.name,
        score=total,
        confidence=confidence,
        evidence_lines=evidence[: rule.max_evidence],
        matched_terms=distinct_terms[:8],
    )


def maybe_add_travel_hit(lines: list[str], hits: list[RuleHit]) -> None:
    has_landscape = any(hit.name == "山水" for hit in hits)
    if not has_landscape:
        return
    joined = "\n".join(lines)
    if has_any(joined, TRAVEL_NEGATIVE_TERMS):
        return
    if not has_any(joined, TRAVEL_STRONG_TERMS):
        return
    evidence = [line for line in lines if has_any(line, TRAVEL_STRONG_TERMS)]
    if not evidence:
        evidence = next((hit.evidence_lines for hit in hits if hit.name == "山水"), [])
    hits.append(
        RuleHit(
            category="scenario",
            name="文旅",
            score=3,
            confidence=0.62,
            evidence_lines=evidence[:1],
            matched_terms=[term for term in TRAVEL_STRONG_TERMS if term in joined][:6],
        )
    )


def suggest(record: dict[str, Any]) -> list[RuleHit]:
    lines = content_lines(record)
    hits: list[RuleHit] = []
    for rule in RULES:
        hit = apply_rule(lines, rule)
        if hit is not None:
            hits.append(hit)
    maybe_add_travel_hit(lines, hits)

    by_name: dict[str, RuleHit] = {}
    for hit in hits:
        old = by_name.get(hit.name)
        if old is None or (hit.confidence, hit.score) > (old.confidence, old.score):
            by_name[hit.name] = hit

    return sorted(
        by_name.values(),
        key=lambda hit: (-hit.confidence, CATEGORY_ORDER.get(hit.category, 99), hit.name),
    )[:5]


def tag_for(hit: RuleHit) -> dict[str, Any]:
    return {
        "category": hit.category,
        "name": hit.name,
        "source": MACHINE_SOURCE,
        "description": f"machine suggestion; confidence={hit.confidence}; evidence must be confirmed by reviewer",
    }


def unique_lines(lines: Iterable[str], limit: int = 6) -> list[str]:
    result: list[str] = []
    for line in lines:
        text = str(line).strip()
        if text and text not in result:
            result.append(text)
        if len(result) >= limit:
            break
    return result


def apply_suggestions(records: list[dict[str, Any]], created_at: str) -> tuple[list[dict[str, Any]], dict[str, Any]]:
    output: list[dict[str, Any]] = []
    status_counts_in = Counter(status_of(record) for record in records)
    status_counts_out: Counter[str] = Counter()
    tag_counts: Counter[str] = Counter()
    human_done_count = 0
    machine_done_reclassified_count = 0
    suggested_count = 0
    unsuggested_count = 0
    generated_tag_count = 0

    for record in records:
        current_status = status_of(record)
        new_record = copy.deepcopy(record)
        new_meta = copy.deepcopy(meta(new_record))

        machine_reviewed_done = is_machine_reviewed_done(record)
        if is_human_done(record):
            human_done_count += 1
            output.append(new_record)
            status_counts_out[status_of(new_record)] += 1
            continue
        if machine_reviewed_done:
            machine_done_reclassified_count += 1

        existing_machine_tags = new_meta.get("expected_tags") if machine_reviewed_done else []
        existing_machine_evidence = new_meta.get("evidence_lines") if machine_reviewed_done else []
        if not isinstance(existing_machine_tags, list):
            existing_machine_tags = []
        existing_machine_tags = [
            tag
            for tag in existing_machine_tags
            if isinstance(tag, dict) and str(tag.get("name") or "") in ALLOWED_TAG_NAMES
        ]
        if not isinstance(existing_machine_evidence, list):
            existing_machine_evidence = []

        hits = suggest(record)
        if machine_reviewed_done and existing_machine_tags and existing_machine_evidence:
            suggested_count += 1
            tags = copy.deepcopy(existing_machine_tags)
            evidence = unique_lines(existing_machine_evidence)
            generated_tag_count += len(tags)
            tag_counts.update(str(tag.get("name") or "") for tag in tags if isinstance(tag, dict) and tag.get("name"))
            note = str(new_meta.get("review_notes") or "").strip()
            machine_note = (
                "codex_agent result reclassified as machine suggestion; reviewer must verify before setting annotation_status=done"
            )
            new_meta["expected_tags"] = tags
            new_meta["evidence_lines"] = evidence
            new_meta["annotation_status"] = MACHINE_STATUS
            new_meta["review_notes"] = f"{note}; {machine_note}".strip("; ")
            new_meta["prefill_source"] = "codex_agent_batch_review_reclassified"
            new_meta.pop("reviewed_by", None)
            new_meta.pop("reviewed_at", None)
            new_meta["machine_suggestion"] = {
                "rule_version": RULE_VERSION,
                "created_at": created_at,
                "counts_as_human_review": False,
                "review_required": True,
                "reclassified_from_status": current_status,
                "reclassified_reason": "codex_agent review is machine assistance, not human review",
                "matched_rules": [],
            }
        elif hits:
            suggested_count += 1
            tags = [tag_for(hit) for hit in hits]
            evidence = unique_lines(line for hit in hits for line in hit.evidence_lines)
            generated_tag_count += len(tags)
            tag_counts.update(tag["name"] for tag in tags)
            note = str(new_meta.get("review_notes") or "").strip()
            machine_note = (
                "machine suggestion only; reviewer must verify tags/evidence before setting annotation_status=done"
            )
            new_meta["expected_tags"] = tags
            new_meta["evidence_lines"] = evidence
            new_meta["annotation_status"] = MACHINE_STATUS
            new_meta["review_notes"] = f"{note}; {machine_note}".strip("; ")
            new_meta["prefill_source"] = MACHINE_SOURCE
            if machine_reviewed_done:
                new_meta.pop("reviewed_by", None)
                new_meta.pop("reviewed_at", None)
            new_meta["machine_suggestion"] = {
                "rule_version": RULE_VERSION,
                "created_at": created_at,
                "counts_as_human_review": False,
                "review_required": True,
                "matched_rules": [
                    {
                        "category": hit.category,
                        "name": hit.name,
                        "score": hit.score,
                        "confidence": hit.confidence,
                        "matched_terms": hit.matched_terms,
                        "evidence_lines": hit.evidence_lines,
                    }
                    for hit in hits
                ],
            }
        else:
            unsuggested_count += 1
            if machine_reviewed_done:
                new_meta["expected_tags"] = []
                new_meta["evidence_lines"] = []
            else:
                new_meta.setdefault("expected_tags", [])
                new_meta.setdefault("evidence_lines", [])
            new_meta["annotation_status"] = "todo"
            if machine_reviewed_done:
                new_meta["prefill_source"] = "codex_agent_batch_review_reclassified_no_allowed_candidate"
                new_meta.pop("reviewed_by", None)
                new_meta.pop("reviewed_at", None)
            new_meta["machine_suggestion"] = {
                "rule_version": RULE_VERSION,
                "created_at": created_at,
                "counts_as_human_review": False,
                "review_required": True,
                "reclassified_from_status": current_status if machine_reviewed_done else None,
                "matched_rules": [],
            }

        new_record["golden_meta"] = new_meta
        output.append(new_record)
        status_counts_out[status_of(new_record)] += 1

    report = {
        "created_at": created_at,
        "rule_version": RULE_VERSION,
        "total_records": len(records),
        "done_preserved_count": human_done_count,
        "machine_done_reclassified_count": machine_done_reclassified_count,
        "candidate_input_count": len(records) - human_done_count,
        "suggested_count": suggested_count,
        "unsuggested_count": unsuggested_count,
        "generated_tag_count": generated_tag_count,
        "status_counts_input": dict(sorted(status_counts_in.items())),
        "status_counts_output": dict(sorted(status_counts_out.items())),
        "tag_counts": dict(tag_counts.most_common()),
        "counts_as_human_review": False,
        "ready_for_final_gate": False,
        "review_required": True,
    }
    return output, report


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
        "\n".join(content_lines(record)),
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


def write_batches(out_dir: Path, records: list[dict[str, Any]], prefix: str, batch_size: int) -> list[dict[str, Any]]:
    export_records = [record for record in records if status_of(record) != "done"]
    batch_count = math.ceil(len(export_records) / batch_size) if export_records else 0
    batches: list[dict[str, Any]] = []
    for idx in range(batch_count):
        start = idx * batch_size
        end = min(start + batch_size, len(export_records))
        batch_records = export_records[start:end]
        batch_path = out_dir / f"{prefix}-{idx + 1:03d}.csv"
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
    return batches


def write_readme(out_dir: Path, report: dict[str, Any]) -> None:
    next_file = report["batches"][0]["file"] if report.get("batches") else ""
    content = f"""# 黄金评测集机器辅助候选包

这个目录是给人工复核提速用的，不是最终黄金集。

## 当前结果

- 总样本：{report["total_records"]}
- 已人工完成并原样保留：{report["done_preserved_count"]}
- 需要继续人工复核：{report["candidate_input_count"]}
- 已生成机器候选：{report["suggested_count"]}
- 仍无保守候选：{report["unsuggested_count"]}
- 机器候选标签数：{report["generated_tag_count"]}
- 分批 CSV 数：{report["batch_count"]}

## 边界

- `annotation_status=machine_suggested_review_required` 只表示“机器已预填，等人确认”。
- 本包 `counts_as_human_review=false`，不能算人工复核完成。
- 只有人工确认标签和证据后，把 `annotation_status` 改成 `done`，才可以用 `scripts/golden_review_closeout.ps1` 合并。

## 下一批建议先看

```text
{next_file}
```
"""
    (out_dir / "README.md").write_text(content, encoding="utf-8")


def main() -> int:
    args = parse_args()
    if args.batch_size < 1:
        raise SystemExit("--batch-size must be positive")

    input_path = Path(args.input)
    out_dir = Path(args.out_dir)
    out_dir.mkdir(parents=True, exist_ok=True)

    created_at = datetime.now(timezone.utc).isoformat()
    records = read_jsonl(input_path)
    suggested_records, report = apply_suggestions(records, created_at)

    jsonl_path = out_dir / args.jsonl_name
    write_jsonl(jsonl_path, suggested_records)
    batches = write_batches(out_dir, suggested_records, args.prefix, args.batch_size)

    report.update(
        {
            "input": str(input_path).replace("\\", "/"),
            "out_dir": str(out_dir).replace("\\", "/"),
            "output_jsonl": str(jsonl_path).replace("\\", "/"),
            "batch_size": args.batch_size,
            "batch_count": len(batches),
            "batches": batches,
            "next_step": "Human reviewer must confirm the prefilled tags/evidence and set annotation_status=done before merge.",
        }
    )
    report_path = out_dir / args.report
    report_path.write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
    write_readme(out_dir, report)

    print(json.dumps(report, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
