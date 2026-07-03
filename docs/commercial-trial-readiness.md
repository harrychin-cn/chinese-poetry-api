# 商业试用准备包说明

当暂时没有真实外部试用客户时，不再卡在“等人测试”。先由机器生成一套可直接发给外部开发者或内容团队的试用准备包。

## 一键生成

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\export_commercial_trial_readiness.ps1 `
  -BaseUrl http://localhost:1279 `
  -RequireReady
```

输出目录：

```text
data/commercial/trial-readiness/
```

## 生成内容

- `README.md`：当前是否已具备外部试用条件。
- `trial-invite.md`：可复制给外部试用者的邀请话术。
- `trial-test-plan.md`：15 分钟试用步骤。
- `trial-feedback-form.md`：反馈表。
- `trial-record-template.example.jsonl`：真实试用记录字段示例。
- `trial-readiness-report.json`：机器可读状态。

## 边界

这套包只证明“产品已经可以拿去给别人试用”，不等于真实商业验证。

不会写入：

```text
data/commercial/trials.jsonl
```

真实商业验证仍需要外部用户实际调用 API 后，再用下面命令记录：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\add_commercial_trial.ps1 `
  -CustomerProject "真实项目名" `
  -CustomerType "developer/content-tool/tourism/education" `
  -Scenario "真实试用场景" `
  -ApiKeyId "真实发放的 Key ID" `
  -SevenDayCalls 10 `
  -RealCallCompleted `
  -PaidSignal paid_intent `
  -PaidIntentBudget "例如 100-500 元/月" `
  -NextStep "继续跟进"
```
