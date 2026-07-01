# 增值标签 API

标签让原始诗词数据从“能查”变成“能卖”：客户可以按主题、情绪、年级、使用场景筛选内容。

## 标签模型

每个标签包含：

- `name`：标签名，例如 `思乡`、`月亮`、`小学`。
- `category`：标签分类，例如 `theme`、`mood`、`grade`、`scenario`、`difficulty`。
- `description`：可选说明。
- `source`：来源，例如 `manual`、`import`、`ai_reviewed`，用于追踪质量。

## 查看标签

```bash
curl "http://localhost:1279/api/v1/tags"
```

按分类查看：

```bash
curl "http://localhost:1279/api/v1/tags?category=theme"
```

## 新增或更新标签（管理员）

```bash
curl -X POST "http://localhost:1279/api/v1/admin/tags" \
  -H "X-Admin-Token: replace-with-a-long-random-secret" \
  -H "Content-Type: application/json" \
  -d '{"name":"思乡","category":"theme","description":"表达思念家乡","source":"manual"}'
```

## 给诗词打标签（管理员）

```bash
curl -X POST "http://localhost:1279/api/v1/admin/poems/1/tags" \
  -H "X-Admin-Token: replace-with-a-long-random-secret" \
  -H "Content-Type: application/json" \
  -d '{"tags":[{"name":"思乡","category":"theme","source":"manual"},{"name":"月亮","category":"theme","source":"manual"}]}'
```

这个接口支持一次给同一首诗绑定多个标签，适合批量脚本逐条导入。

## 按标签查询诗词

`/api/v1/poems/query` 支持重复传 `tag`，多标签之间是“同时满足”：

```bash
curl "http://localhost:1279/api/v1/poems/query?tag=思乡&tag=月亮&tag_category=theme"
```

也可以和作者、朝代、体裁、关键词一起组合：

```bash
curl "http://localhost:1279/api/v1/poems/query?dynasty=唐&author=李白&tag=思乡&q=月&search_in=content"
```

## 建议优先做的高价值标签

- 主题：`思乡`、`送别`、`春天`、`月亮`、`边塞`、`爱情`、`励志`。
- 年级：`小学`、`初中`、`高中`。
- 场景：`节日`、`节气`、`文旅`、`短视频文案`、`作文素材`。
- 难度：`入门`、`进阶`、`专业`。

## 商业价值

- 教育客户：按年级、主题找素材。
- 小程序客户：按节日、节气、场景做推荐。
- AI 应用客户：作为知识库召回前的结构化过滤条件。
