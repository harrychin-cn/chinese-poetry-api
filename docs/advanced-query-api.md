# 增强复合查询 API

阶段 1 新增接口：

```http
GET /api/v1/poems/query
```

这个接口用于安全地做多条件组合查询，不暴露裸 SQL。

## 支持参数

| 参数 | 说明 | 示例 |
|---|---|---|
| `q` / `keyword` | 关键词 | `q=月` |
| `search_in` | 搜索范围：`all`、`title`、`content`、`author` | `search_in=content` |
| `lang` | 语言：`zh-Hans`、`zh-Hant` | `lang=zh-Hant` |
| `dynasty_id` | 朝代 ID | `dynasty_id=6` |
| `dynasty` | 朝代名称 | `dynasty=唐` |
| `author_id` | 作者 ID | `author_id=123` |
| `author` | 作者名称 | `author=李白` |
| `type_id` | 诗词类型 ID，可重复 | `type_id=11&type_id=12` |
| `type` | 诗词类型名称，可重复 | `type=五言绝句&type=七言绝句` |
| `tag` | 增值标签名，可重复；多个标签表示同时满足 | `tag=思乡&tag=月亮` |
| `tag_category` | 标签分类，可选 | `tag_category=theme` |
| `lines` | 句数 | `lines=4` |
| `chars_per_line` | 每句字数 | `chars_per_line=5` |
| `page` | 页码，默认 1 | `page=1` |
| `page_size` | 每页数量，默认 20，最大 100 | `page_size=20` |
| `sort` | 排序：`id_desc`、`id_asc`、`title_asc`、`title_desc` | `sort=id_desc` |

## 示例

### 查李白作品里内容含“月”的诗

```bash
curl "http://localhost:1279/api/v1/poems/query?author=李白&q=月&search_in=content&page=1&page_size=20"
```

### 查唐代五言绝句

```bash
curl "http://localhost:1279/api/v1/poems/query?dynasty=唐&type=五言绝句"
```

### 查四句、每句五字、标题含“春”的作品

```bash
curl "http://localhost:1279/api/v1/poems/query?q=春&search_in=title&lines=4&chars_per_line=5"
```

### 查同时带“思乡”和“月亮”标签的作品

```bash
curl "http://localhost:1279/api/v1/poems/query?tag=思乡&tag=月亮&tag_category=theme"
```

## 返回结构

返回结构沿用现有分页格式：

```json
{
  "data": [],
  "pagination": {
    "page": 1,
    "page_size": 20,
    "total": 0,
    "total_pages": 0
  }
}
```

## 设计边界

- 不支持用户传入 SQL。
- 所有查询条件都走白名单参数和参数化查询。
- 当前接口用于结构化复合筛选；更强的相关性搜索见 `docs/fulltext-search.md`。
