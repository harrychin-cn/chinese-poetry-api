# AI 知识库召回 API

这组接口面向 AI/RAG、教育 App、内容工具和智能体。它不只是返回原始诗词字段，还会返回标签、场景、推荐理由和引用格式，方便直接喂给大模型。

## 接口总览

| 接口 | 用途 | 是否商业接口 |
| --- | --- | --- |
| `GET /api/v1/knowledge/scenarios` | 查看内置热门场景 | 否 |
| `GET /api/v1/knowledge/recall` | 单次知识库召回 | 是，开启 `API_AUTH_ENABLED=true` 后需要 API Key |
| `POST /api/v1/knowledge/batch` | 批量知识库召回 | 是，开启 `API_AUTH_ENABLED=true` 后需要 API Key |

## 热门场景

```bash
curl "http://localhost:1279/api/v1/knowledge/scenarios"
```

当前内置场景包括：

- `farewell_graduation`：毕业送别
- `mid_autumn_moon`：中秋月亮
- `homesickness`：思乡怀人
- `frontier_war`：边塞战争
- `spring`：春天景物
- `love_longing`：爱情相思
- `landscape_travel`：山水文旅
- `patriotic`：家国情怀
- `short_video_copy`：短视频文案

## 单次召回

```bash
curl "http://localhost:1279/api/v1/knowledge/recall?q=找中秋月亮诗句&page_size=5" \
  -H "X-API-Key: cp_live_xxx"
```

常用参数：

| 参数 | 说明 |
| --- | --- |
| `q` / `intent` | 自然语言需求，例如“找适合毕业离别的诗” |
| `scenario_id` | 强制指定场景，例如 `mid_autumn_moon` |
| `tag` | 可重复传入标签，例如 `tag=思乡&tag=月亮` |
| `tag_category` | 标签类别，例如 `theme`、`scenario`、`festival` |
| `author` / `dynasty` / `type` | 继续复用复合查询过滤 |
| `lines` / `chars_per_line` | 句数和每句字数过滤 |
| `page` / `page_size` | 分页，`page_size` 最大 100 |
| `lang` | `zh-Hans` 或 `zh-Hant` |

示例：毕业送别

```bash
curl "http://localhost:1279/api/v1/knowledge/recall?q=找适合毕业离别的诗&page_size=5" \
  -H "X-API-Key: cp_live_xxx"
```

示例：强制中秋场景 + 唐代过滤

```bash
curl "http://localhost:1279/api/v1/knowledge/recall?scenario_id=mid_autumn_moon&q=月亮&dynasty=唐&page_size=5" \
  -H "X-API-Key: cp_live_xxx"
```

返回结构重点字段：

```json
{
  "data": [
    {
      "id": 1,
      "title": "静夜思",
      "content": ["床前明月光", "疑是地上霜"],
      "author": {"id": 1, "name": "李白"},
      "dynasty": {"id": 1, "name": "唐"},
      "tags": [
        {"id": 1, "name": "月亮", "category": "theme", "source": "manual"}
      ],
      "knowledge": {
        "reason": "命中“中秋月亮”场景，适合作为候选引用。",
        "scenario_id": "mid_autumn_moon",
        "citation_format": "静夜思 / 李白 / 唐",
        "citation_text": ["床前明月光", "疑是地上霜"],
        "source": "chinese-poetry-api"
      }
    }
  ],
  "pagination": {"page": 1, "page_size": 5, "total": 1, "total_pages": 1},
  "knowledge": {
    "intent": "找中秋月亮诗句",
    "scenario": {"id": "mid_autumn_moon", "name": "中秋月亮"},
    "matched_tags": [],
    "recall_mode": "scenario_rules+keyword",
    "citation_hint": "建议引用 title、author.name、dynasty.name 和 content 中的原句；不要把推荐理由当作原文。"
  }
}
```

## 批量召回

```bash
curl -X POST "http://localhost:1279/api/v1/knowledge/batch" \
  -H "X-API-Key: cp_live_xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "page_size": 3,
    "queries": [
      {"id": "moon", "q": "中秋月亮"},
      {"id": "farewell", "q": "毕业离别"},
      {"id": "travel", "q": "文旅山水"}
    ]
  }'
```

限制：

- 单次最多 20 个 query。
- 每个 query 默认返回 5 条，`page_size` 最大 20。
- 批量接口适合 RAG 预召回、内容工具批量取候选诗句。

## 当前实现边界

- 当前是规则召回：先根据自然语言匹配内置场景，再用标签和关键词查库。
- 如果标签数据还没铺全，会自动回退到关键词召回，保证早期可用。
- 后续阶段会补 AI 批量标签、人工抽检和向量检索表。
