# SQLite FTS5 全文搜索

复合查询接口 `/api/v1/poems/query` 适合精确筛选；全文搜索接口适合做更强的关键词检索和相关性排序。

## 接口

```http
GET /api/v1/poems/search/fulltext
```

支持参数和 `/api/v1/poems/query` 基本一致：

- `q` / `keyword`：必填，关键词。
- `search_in`：`all`、`title`、`content`、`author`。
- `dynasty` / `dynasty_id`。
- `author` / `author_id`。
- `type` / `type_id`。
- `tag` / `tag_category`。
- `lines`、`chars_per_line`。
- `page`、`page_size`。
- `sort`：默认 `relevance`，也支持 `id_desc`、`id_asc`、`title_asc`、`title_desc`。

## 示例

全文搜索李白作品里和“明月”相关的内容：

```bash
curl "http://localhost:1279/api/v1/poems/search/fulltext?author=李白&q=明月&search_in=content"
```

按标签组合搜索：

```bash
curl "http://localhost:1279/api/v1/poems/search/fulltext?q=明月&tag=思乡&tag_category=theme"
```

## 返回结构

每条数据会多一个 `search` 字段：

```json
{
  "data": [
    {
      "id": 1,
      "title": "静夜思",
      "content": ["床前明月光", "疑是地上霜"],
      "search": {
        "rank": -1.23,
        "hit_fields": ["content"],
        "snippets": {
          "content": "床前明月光"
        }
      }
    }
  ],
  "pagination": {
    "page": 1,
    "page_size": 20,
    "total": 1,
    "total_pages": 1
  }
}
```

## 构建要求

SQLite FTS5 需要用构建标签开启：

```bash
go build -tags sqlite_fts5 ./cmd/server
```

项目 Dockerfile 已经默认使用 `-tags sqlite_fts5` 构建服务端。

## 重建索引

管理员 HTTP 接口：

```bash
curl -X POST "http://localhost:1279/api/v1/admin/search/rebuild" \
  -H "X-Admin-Token: replace-with-a-long-random-secret"
```

命令行方式：

```bash
go run -tags sqlite_fts5 ./cmd/apikey --db data/poetry.db rebuild-search
```

服务启动后，如果发现 FTS 索引为空，第一次全文搜索会自动补建索引。大数据量生产环境建议提前用管理员接口或 CLI 重建。
