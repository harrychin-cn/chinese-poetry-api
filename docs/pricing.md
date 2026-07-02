# 价格与套餐页

> 首版验证价：用于验证开发者和内容产品是否愿意为“更好查、更适合 AI 的诗词知识库 API”付费。正式报价可在有持续付费客户后再调整。

## 计费原则

- 只保留 QanloAPI 充值入口，不在本项目内自建支付、订单、订阅系统。
- 本项目负责：API Key、套餐标记、每日限额、调用统计、接口保护、客户反馈和运营后台。
- QanloAPI 负责：充值入口与按调用消耗余额。
- `tier` 是运营标记；`daily_limit` 是每日调用上限；真实充值状态通过 `/api/v1/billing/status` 查询。

## 首版套餐

| 档位 | `tier` | 适合客户 | 建议限制 | 收费方式 |
| --- | --- | --- | --- | --- |
| 人工体验 | `trial` | 个人开发者、演示客户 | 每日 100 次增强/知识库调用，7-14 天试用 | 需管理员或 Qanlo 开通链路发放 Key，不公开自助免费生成 |
| 开发者包 | `developer` | 小程序、课程工具、内容站 | 每日 1,000-10,000 次 | QanloAPI 充值后按调用消耗，建议首充 99 元起 |
| 商业包 | `business` | 教育 App、内容平台、批量内容工具 | 每日 10,000-100,000 次 | QanloAPI 充值后按调用消耗，建议首充 999 元起 |
| 私有部署 | `enterprise` | 企业、学校、文旅项目 | 客户自有服务器，限额按合同配置 | 部署/维护单独报价；线上调用可继续保留 QanloAPI |

## 可计费接口

- `GET /api/v1/poems/query`
- `GET /api/v1/poems/search/fulltext`
- `GET /api/v1/knowledge/recall`
- `POST /api/v1/knowledge/batch`

以下接口只做管理/状态/反馈，不消耗每日诗词查询额度：

- `GET /api/v1/keys/current`
- `POST /api/v1/billing/qanlo/provision`
- `POST /api/v1/billing/qanlo/recharge-session`
- `GET /api/v1/billing/status`
- `GET /api/v1/usage/*`
- `POST /api/v1/feedback`

## 客户接入路径

1. 客户进入 `/console` 或客户端。
2. 填写管理员或 Qanlo 开通链路发放的 API Key，保存 `cp_live_xxx`。
3. 绑定/充值 Qanlo。
4. 调用增强查询或 AI 知识库接口。
5. 通过 usage 接口查看调用趋势。
6. 通过反馈接口提交缺失数据、功能建议或充值问题。

## 运营动作

管理员可用：

```bash
curl "http://localhost:1279/api/v1/admin/api-keys" \
  -H "X-Admin-Token: replace-with-random-secret"

curl -X PATCH "http://localhost:1279/api/v1/admin/api-keys/1" \
  -H "X-Admin-Token: replace-with-random-secret" \
  -H "Content-Type: application/json" \
  -d '{"tier":"developer","daily_limit":5000,"notes":"首充 99 元，开发者包"}'
```

## 客户可见页面

内置页面：

- `/pricing`：价格套餐页。
- `/console`：填写已开通 Key、充值、试调用和提交反馈。
- `/docs`：开发者文档。
