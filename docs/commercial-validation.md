# 商业验证记录模板

目标：用 3-5 个真实开发者/内容工具验证客户是否愿意为 AI 诗词知识库 API 付费。

## 验证标准

阶段 8 不以“写完代码”为最终完成标准，而以真实商业信号为准：

- 至少 3 个真实试用客户完成 API Key 创建和一次真实调用。
- 至少 1 个客户完成 Qanlo 首充，或明确表达付费意向和预算。
- 能记录最常用查询、缺失数据、失败原因和需要新增的标签/场景。
- 能根据反馈调整后续数据增强和产品优先级。

## 试用客户记录表

| 编号 | 客户/项目 | 客户类型 | 接入场景 | API Key ID | `tier` | 开始日期 | 7 日调用量 | 高频查询/场景 | 缺失数据/问题 | 是否充值/付费意向 | 下一步 |
| --- | --- | --- | --- | --- | --- | --- | ---: | --- | --- | --- | --- |
| 1 | 待填 | 教育 App / 小程序 / 内容工具 / AI 智能体 | 待填 | 待填 | free | 待填 | 0 | 待填 | 待填 | 待填 | 待填 |
| 2 | 待填 | 教育 App / 小程序 / 内容工具 / AI 智能体 | 待填 | 待填 | free | 待填 | 0 | 待填 | 待填 | 待填 | 待填 |
| 3 | 待填 | 教育 App / 小程序 / 内容工具 / AI 智能体 | 待填 | 待填 | free | 待填 | 0 | 待填 | 待填 | 待填 | 待填 |
| 4 | 待填 | 教育 App / 小程序 / 内容工具 / AI 智能体 | 待填 | 待填 | free | 待填 | 0 | 待填 | 待填 | 待填 | 待填 |
| 5 | 待填 | 教育 App / 小程序 / 内容工具 / AI 智能体 | 待填 | 待填 | free | 待填 | 0 | 待填 | 待填 | 待填 | 待填 |

## 机器可审计记录

真实试用记录落到 `data/commercial/trials.jsonl`，每行一条 JSON。字段建议：

```json
{"customer_project":"客户/项目名","customer_type":"教育 App","scenario":"接入场景","api_key_id":"真实 API Key ID","tier":"free","start_date":"2026-06-30","seven_day_calls":1,"real_call_completed":true,"top_queries":["思乡"],"missing_data":"待补问题","paid_signal":"none|paid_intent|recharge","paid_amount":0,"paid_intent_budget":"","next_step":"下一步"}
```

审计命令：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\commercial_validation_audit.ps1 `
  -RecordFile data\commercial\trials.jsonl `
  -Out data\commercial\trials.audit.json `
  -RequireFinal
```

`-RequireFinal` 会在未达到 5 个真实试用且至少 1 个充值/明确付费意向时返回失败。示例文件见 `data/commercial/trials.example.jsonl`，示例只能验证格式，不能当真实商业证据。

也可以用脚本追加真实记录，避免手写 JSON 出错：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\add_commercial_trial.ps1 `
  -CustomerProject "真实客户或项目名" `
  -CustomerType "教育 App / 内容工具 / AI 智能体" `
  -Scenario "真实接入场景" `
  -ApiKeyId "真实 API Key ID" `
  -SevenDayCalls 1 `
  -RealCallCompleted `
  -TopQueries "中秋 月亮","思乡" `
  -PaidSignal none `
  -NextStep "继续跟进"
```

如果已有付费意向，把 `-PaidSignal` 改成 `paid_intent`，并填写 `-PaidIntentBudget`。

## 每个客户的跟进清单

1. 发 `/pricing` 和 `/docs`。
2. 协助创建 API Key，默认 `tier=free`，`daily_limit=100`。
3. 让客户完成 1 个真实业务场景调用。
4. 7 天后导出 usage：

```bash
curl "http://localhost:1279/api/v1/admin/usage/queries?api_key_id=1&days=7&limit=20" \
  -H "X-Admin-Token: replace-with-random-secret"
```

5. 查看客户反馈：

```bash
curl "http://localhost:1279/api/v1/admin/feedback?api_key_id=1&status=all&limit=50" \
  -H "X-Admin-Token: replace-with-random-secret"
```

6. 如果客户持续调用，引导 Qanlo 首充 99 元或 999 元。
7. 付费后用 `PATCH /api/v1/admin/api-keys/:id` 调整 `tier` 和 `daily_limit`。

## 决策规则

- 0 个愿意付费：继续打磨数据标签和场景召回，不扩大开发。
- 1-2 个愿意付费：补客户案例、文档站和更多样本数据增强。
- 3 个以上愿意付费：再考虑正式价格页、合同/发票、私有部署报价和部分 SDK 开源。

## SDK/基础工具开放建议

首版增强版先不开源。等验证到稳定付费后，可优先开放：

- `examples/curl`、`examples/python`、`examples/javascript` 示例。
- 只包含调用封装的轻量 SDK。
- 不开放增强标签数据、客户后台、Qanlo 商业链路和运营数据。
