# 生产运营与恢复演练 Runbook

目标：让非技术运营也能按固定步骤完成备份、恢复、封禁、解封和上线前 smoke 验证。

## 1. 每日检查

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\smoke_commercial.ps1 -BaseUrl http://localhost:1279
```

检查项：

- `/api/v1/health` 正常。
- 公开 `POST /api/v1/keys` 返回 403；Key 由管理员或受信任的 Qanlo 开通链路发放。
- Qanlo 状态/充值入口可返回 URL；脚本不会打开外部充值页，也不会发起付费调用。
- 增强查询、客户 usage、客户反馈链路可用。

## 2. 备份

本地：

```powershell
go run ./cmd/backup --db data/poetry.db --out backups --keep 7
```

Docker Compose：

```bash
docker compose exec poetry-api ./backup --db /app/data/poetry.db --out /app/backups --keep 7
```

备份成功必须看到：

- `Backup created: ...`
- `Manifest: ...`
- `Quick check: ok`

## 3. 恢复演练

每月至少做 1 次，不要直接覆盖生产库，先恢复到临时目录验证。

### Windows PowerShell

```powershell
New-Item -ItemType Directory -Force .\tmp\restore-drill | Out-Null
Copy-Item .\backups\poetry-YYYYMMDDTHHMMSS.xxxxxxxxxZ.db .\tmp\restore-drill\poetry.db -Force
go run ./cmd/backup --db .\tmp\restore-drill\poetry.db --out .\tmp\restore-drill\check --keep 1
```

### Docker Compose

```bash
docker compose stop poetry-api
docker compose run --rm --no-deps --entrypoint sh poetry-api -c "cp /app/backups/poetry-YYYYMMDDTHHMMSS.xxxxxxxxxZ.db /app/data/poetry.db"
docker compose up -d poetry-api
```

恢复生产后必须再跑：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\smoke_commercial.ps1 -BaseUrl http://localhost:1279
```

## 4. 已完成的生产大小库恢复演练记录

2026-06-30 已对当前生产大小 SQLite 库完成一次完整演练：

- 源库：`data/poetry.db`
- 源库大小：648450048 bytes
- 命令：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\backup_restore_drill.ps1 `
  -Db data\poetry.db `
  -OutDir backups\drill-full `
  -RestoreDir .codex-temp\restore-drill-full `
  -Keep 1 `
  -Runner docker
```

- 源库备份 manifest：`backups/drill-full/poetry-20260630T121025.737906955Z.manifest.json`
- 恢复库二次校验 manifest：`backups/drill-full/poetry-restored-20260630T121235.847186704Z.manifest.json`
- 结果：源库备份 `quick_check=ok`，恢复库二次备份 `quick_check=ok`

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\smoke_commercial.ps1 -BaseUrl http://localhost:1279
```

## 4. 手动封禁

查看有效封禁：

```bash
curl "http://localhost:1279/api/v1/admin/abuse/blocks?active_only=true" \
  -H "X-Admin-Token: replace-with-a-long-random-secret"
```

封禁 IP 60 分钟：

```bash
curl -X POST "http://localhost:1279/api/v1/admin/abuse/blocks" \
  -H "X-Admin-Token: replace-with-a-long-random-secret" \
  -H "Content-Type: application/json" \
  -d '{"target_type":"ip","target_value":"203.0.113.10","reason":"刷接口","ttl_minutes":60}'
```

封禁 API Key ID：

```bash
curl -X POST "http://localhost:1279/api/v1/admin/abuse/blocks" \
  -H "X-Admin-Token: replace-with-a-long-random-secret" \
  -H "Content-Type: application/json" \
  -d '{"target_type":"api_key","target_value":"1","reason":"退款争议","enabled":true}'
```

解封：

```bash
curl -X PATCH "http://localhost:1279/api/v1/admin/abuse/blocks/1" \
  -H "X-Admin-Token: replace-with-a-long-random-secret" \
  -H "Content-Type: application/json" \
  -d '{"enabled":false,"notes":"误封，已解封"}'
```

## 5. 自动封禁配置

```bash
ABUSE_PROTECTION_ENABLED=true
ABUSE_AUTO_BLOCK_ENABLED=true
ABUSE_FAILURE_THRESHOLD=20
ABUSE_WINDOW_SECONDS=60
ABUSE_BLOCK_MINUTES=60
```

含义：

- 60 秒内同一 IP 或 API Key 出现 20 次 `401` / `429`，自动写入封禁表。
- 自动封禁默认 60 分钟。
- 生产误封时用上面的 PATCH 解封。

## 6. 故障处理顺序

1. 先看 `/api/v1/health`。
2. 再跑 `scripts/smoke_commercial.ps1`。
3. 如果客户说 403，先查 `GET /api/v1/admin/abuse/blocks?active_only=true`。
4. 如果客户说 429，先查 usage 和 daily_limit，再查短周期限流。
5. 如果数据库异常，先用最近备份做临时恢复演练，不要直接覆盖生产库。
