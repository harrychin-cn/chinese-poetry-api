# SQLite 数据库备份策略 MVP

## 目标

给当前商业版先补一个可执行、可验证的 SQLite 备份方案，覆盖诗词数据、API Key、用量日志、Qanlo 绑定、反馈和 AI 增强数据。

推荐命令：

```bash
go run ./cmd/backup --db data/poetry.db --out backups --keep 7
```

Docker/Compose 部署后，镜像内也已经包含 `backup` 命令：

```bash
docker compose exec poetry-api ./backup --db /app/data/poetry.db --out /app/backups --keep 7
```

`docker-compose.yml` 默认把 `/app/data` 和 `/app/backups` 分别挂到 `poetry-data`、`poetry-backups` 两个 Docker volume，避免容器重建时丢失数据库和备份。

参数说明：

- `--db`：源 SQLite 数据库路径，默认 `data/poetry.db`。
- `--out`：备份输出目录，默认 `backups`。
- `--keep`：保留最近多少份备份，默认 `7`；设为 `0` 表示不自动清理。

## 备份产物

每次执行会在 `--out` 目录生成：

1. 时间戳数据库备份，例如 `poetry-20260629T120000.000000000Z.db`。
2. 单次 manifest，例如 `poetry-20260629T120000.000000000Z.manifest.json`。
3. 汇总 manifest：`manifest.json`。

manifest 记录：

- 备份方法：`sqlite-vacuum-into`。
- 源库路径、备份文件名和文件大小。
- 备份文件 SHA256。
- SQLite 版本、`page_count`、`freelist_count`。
- `PRAGMA quick_check` 结果。
- 当前 keep 保留策略。

## 一致性说明

`VACUUM INTO` 会让 SQLite 生成一致的新数据库文件，比直接复制 `.db` 更适合存在 WAL 的在线数据库。

命令会设置 busy timeout，尽量等待正在进行的写入完成；如果数据库被长事务占用，命令会失败并保留错误信息。

## 保留策略

`--keep 7` 表示同一个源库只保留最近 7 份备份，同时删除对应的旧 manifest。清理依据 manifest 中的 `created_at` 排序。

## 恢复步骤

恢复前先停掉 API 服务，避免覆盖正在使用的数据库。

Linux/macOS：

```bash
cp backups/poetry-YYYYMMDDTHHMMSS.xxxxxxxxxZ.db data/poetry.db
```

Docker/Compose：

```bash
docker compose stop poetry-api
docker compose run --rm --no-deps --entrypoint sh poetry-api -c "cp /app/backups/poetry-YYYYMMDDTHHMMSS.xxxxxxxxxZ.db /app/data/poetry.db"
docker compose up -d poetry-api
```

Windows PowerShell：

```powershell
Copy-Item backups\poetry-YYYYMMDDTHHMMSS.xxxxxxxxxZ.db data\poetry.db -Force
```

恢复后建议执行一次备份命令到临时目录，用它内置的 `quick_check` 校验恢复后的库：

```bash
go run ./cmd/backup --db data/poetry.db --out backups/check --keep 1
```

## 运营建议

MVP 阶段建议：

- 每天至少备份 1 次，保留最近 7 份。
- 每周下载一份到服务器外部位置。
- 每月做 1 次恢复演练：用备份文件恢复到测试目录，再跑 `quick_check`。

后续可继续补自动定时任务、异地对象存储、备份告警和一键恢复脚本。
