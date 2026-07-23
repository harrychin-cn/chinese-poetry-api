<div align="center">

<img src="docs/icon.png" alt="chinese-poetry" height="100px">

<h2>中国古诗词 API 服务</h2>

[![Docker Image](https://img.shields.io/docker/v/palemoky/chinese-poetry-api?sort=semver&label=docker)](https://hub.docker.com/r/palemoky/chinese-poetry-api)
[![Docker Image Size](https://img.shields.io/docker/image-size/palemoky/chinese-poetry-api/latest)](https://hub.docker.com/r/palemoky/chinese-poetry-api)
[![Go Report Card](https://goreportcard.com/badge/github.com/palemoky/chinese-poetry-api)](https://goreportcard.com/report/github.com/palemoky/chinese-poetry-api)
[![pre-commit](https://img.shields.io/badge/pre--commit-enabled-brightgreen?logo=pre-commit)](https://github.com/pre-commit/pre-commit)
[![License](https://img.shields.io/github/license/palemoky/chinese-poetry-api)](https://github.com/palemoky/chinese-poetry-api/blob/main/LICENSE)

基于 Go 语言的高性能中国古诗词 API 服务，支持 REST 和 GraphQL 接口，提供简体/繁体中文、爬虫练习场等功能。

在线版：https://poetry.palemoky.com

</div>

## 特性

- **高性能**: Go 语言编写，支持并发处理，性能优化（简繁转换 ~300ns/op）
- **海量数据**: 包含唐诗、宋词、元曲等近 40 万首诗词
- **强大搜索**: 支持全文搜索、标题/内容/作者分类搜索
- **商业增强查询**: 支持作者、朝代、体裁、标签、关键词、句数、字数的复合查询
- **AI 知识库召回**: 支持“毕业离别”“中秋月亮”等自然语言场景召回，返回标签、推荐理由和引用格式
- **AI 数据增强与抽检**: 支持小样本生成、格式校验、待审队列、人工通过/退回/修正、抽检报告和批次回滚
- **商业化底座**: 支持 API Key、每日额度/限额调整、API Key 短周期限流、QanloAPI 精简充值、请求审计、每日趋势、接口错误率、热门查询、客户反馈和管理员接口
- **双语支持**: 同一数据库同时存储简体和繁体中文，通过 `?lang=` 参数切换
- **多种接口**: REST API 和 GraphQL 双接口支持
- **限流保护**: 内置 IP 限流，防止滥用
- **容器化**: Docker 镜像开箱即用，支持多架构（amd64/arm64）
- **智能分类**: 按朝代、作者、诗词类型自动分类

## 快速开始

### 使用 Docker（推荐）

```bash
docker run -d -p 1279:1279 palemoky/chinese-poetry-api:latest
```

完整配置参见 [docker-compose.yml](docker-compose.yml)。

生产环境建议使用 `docker compose`，它会持久化数据库和备份目录。需要手动备份时可执行：

```bash
docker compose exec poetry-api ./backup --db /app/data/poetry.db --out /app/backups --keep 7
```

### 使用 Makefile

```bash
make help          # 查看所有可用命令
make build         # 构建项目
make process-data  # 处理数据
make run-server    # 启动服务
```

### 克隆仓库

本项目使用 Git Submodules 管理诗词数据，推荐使用以下命令快速克隆：

```bash
# 完整克隆（包含 submodules）
git clone --recurse-submodules --depth=1 https://github.com/palemoky/chinese-poetry-api.git
```

如果已经克隆了仓库，可以单独更新 submodules：

```bash
git submodule update --init
```

## API 使用

### 多语言支持

所有接口支持 `lang` 参数切换简繁体：

|  参数值   |       说明       |
| :-------: | :--------------: |
| `zh-Hans` | 简体中文（默认） |
| `zh-Hant` |     繁体中文     |

### REST API

```bash
# 简体中文（默认）
curl "http://localhost:1279/api/v1/poems"

# 繁体中文
curl "http://localhost:1279/api/v1/poems?lang=zh-Hant"

# 搜索诗词
curl "http://localhost:1279/api/v1/poems/search?q=静夜思"

# 增强复合查询
curl "http://localhost:1279/api/v1/poems/query?author=李白&q=月&search_in=content&type=五言绝句"

# 全文搜索（Docker 镜像默认开启 SQLite FTS5）
curl "http://localhost:1279/api/v1/poems/search/fulltext?author=李白&q=明月&search_in=content"

# 按增值标签查询
curl "http://localhost:1279/api/v1/poems/query?tag=思乡&tag=月亮&tag_category=theme"

# AI 知识库召回：自然语言意图 -> 场景/标签/关键词召回
curl "http://localhost:1279/api/v1/knowledge/recall?q=找中秋月亮诗句&page_size=5"

# 热门知识库场景
curl "http://localhost:1279/api/v1/knowledge/scenarios"

# 批量知识库召回
curl -X POST "http://localhost:1279/api/v1/knowledge/batch" \
  -H "Content-Type: application/json" \
  -d '{"queries":[{"id":"moon","q":"中秋月亮"},{"id":"farewell","q":"毕业离别"}],"page_size":3}'

# 随机诗词
curl "http://localhost:1279/api/v1/poems/random"

# 随机诗词（带过滤）
curl "http://localhost:1279/api/v1/poems/random?author=李白"
curl "http://localhost:1279/api/v1/poems/random?type=五言绝句"
curl "http://localhost:1279/api/v1/poems/random?author=李白&type=五言绝句"
curl "http://localhost:1279/api/v1/poems/random?author=李白&type=五言绝句&dynasty=唐"
curl "http://localhost:1279/api/v1/poems/random?author=李白&dynasty=唐&type=五言绝句&type=七言绝句&type=五言律诗"

# 作者列表
curl "http://localhost:1279/api/v1/authors?page=1&page_size=20"

# 朝代列表
curl "http://localhost:1279/api/v1/dynasties"
```

商业增强能力文档：

- [开发规划](docs/development-plan.md)
- [增强复合查询 API](docs/advanced-query-api.md)
- [AI 知识库召回 API](docs/knowledge-api.md)
- [全文搜索 API](docs/fulltext-search.md)
- [增值标签 API](docs/value-added-tags.md)
- [AI 数据增强与人工抽检](docs/data-enrichment.md)
- [API Key、额度控制、短周期限流、封禁、客户反馈与 Usage 运营统计](docs/commercial-api-keys.md)
- [产品包装与收费方案](docs/product-offer.md)
- [客户可见价格与套餐页](docs/pricing.md)
- [商业验证记录模板](docs/commercial-validation.md)
- [SQLite 备份策略](docs/backup-strategy.md)
- [生产运营与恢复演练 Runbook](docs/operations-runbook.md)

商业化闭环统一为 QanloAPI 精简链路：控制台先通过 `POST /api/v1/keys` 创建本地 `cp_live_` Key（含每日 20 次初始额度），再点击“快捷充值”调用 `/api/v1/billing/qanlo/recharge-session` 跳转 Qanlo 精简充值流程并使用已有回调链路。Qanlo 的 `qk_` / `sk_` 密钥只填在生图 Key 输入框。

AI 数据增强试跑建议先走一键脚本：

```powershell
.\scripts\enrichment_trial.ps1 -Db data\poetry.db -Limit 100 -Provider rules
```

Windows 本地如果没有 CGO/gcc，可以用一键验证脚本自动切到 Docker 跑测试：

```powershell
.\scripts\test_local.ps1
```

需要同时验 Docker 镜像时：

```powershell
.\scripts\test_local.ps1 -DockerBuild
```

服务启动后，可以用低成本商业闭环 smoke 检查创建 Key、鉴权查询、用量和反馈链路：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\smoke_commercial.ps1
```

启动服务后也可以直接打开：

- 控制台：`http://localhost:1279/console`
- 文档站雏形：`http://localhost:1279/docs`
- 价格套餐页：`http://localhost:1279/pricing`

### GraphQL API

端点：`http://localhost:1279/graphql`

```graphql
# 繁体中文查询
query {
  poems(lang: ZH_HANT, pageSize: 10) {
    edges {
      node {
        title
        content
        author {
          name
        }
      }
    }
    totalCount
  }
}

# 搜索诗词
query {
  searchPoems(query: "静夜思", searchType: TITLE) {
    edges {
      node {
        title
        author {
          name
        }
      }
    }
  }
}

# 统计信息
query {
  statistics {
    totalPoems
    totalAuthors
    poemsByDynasty {
      dynasty {
        name
      }
      count
    }
  }
}
```

## 搜索功能

|   类型    |       说明       |             示例             |
| :-------: | :--------------: | :--------------------------: |
|   `all`   | 全文搜索（默认） |           `?q=月`            |
|  `title`  |     标题搜索     |    `?q=静夜思&type=title`    |
| `content` |     内容搜索     | `?q=床前明月光&type=content` |
| `author`  |     作者搜索     |    `?q=李白&type=author`     |

## 数据集

本项目基于 [chinese-poetry](https://github.com/chinese-poetry/chinese-poetry) 数据集，包含：

|   分类   |  数量  |   分类   |  数量  |   分类   |  数量  |   分类   |  数量  |
| :------: | :----: | :------: | :----: | :------: | :----: | :------: | :----: |
| 五言绝句 | 18,895 | 七言绝句 | 85,032 | 五言律诗 | 71,400 | 七言律诗 | 69,028 |
|  乐府诗  | 9,315  |  五代词  |  543   |   宋词   | 21,369 |   元曲   | 10,905 |
|   诗经   |  305   |   楚辞   |   65   |   论语   |   20   | 四书五经 |   14   |
|   其他   | 96,232 |          |        |          |        |          |        |

```mermaid
pie title 收录数据分布概览 (忽略极小值)
"七绝/七律" : 154060
"五绝/五律" : 90295
"宋词/五代词" : 21912
"元曲" : 10905
"乐府诗" : 9315
"其他" : 96232
```

## 致谢

- 数据来源：[chinese-poetry](https://github.com/chinese-poetry/chinese-poetry)
- 简繁转换：[gocc](https://github.com/liuzl/gocc)
