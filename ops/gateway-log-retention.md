# gateway 日志保留与清理说明

记录日期：2026-11-20
适用范围：生产 gateway 主机（SSH 别名 `gateway`，Ubuntu，服务器时区 CST / Asia/Shanghai）

本文只记录已在服务器上核实的事实，不包含任何密码、连接串或环境文件内容。

## 1. 结论

gateway 上有三类自动清理，以及一个需要手动触发的应用级清理。

| 类别 | 对象 | 机制 | 是否自动 | 保留期 |
| --- | --- | --- | --- | --- |
| 数据库请求日志 | PostgreSQL `new-api` 库的 `public.logs` 表 | systemd timer + shell 脚本执行 SQL `DELETE` | 每天 00:30 自动 | 90 天 |
| 应用日志文件 | `/opt/new-api/instances/*/logs/oneapi-*.log`，`/var/log/new-api-upgrade-*.log` | systemd timer + `find -delete` | 每天自动 | 90 天 |
| 系统日志 | systemd journal | `journald.conf.d/retention.conf` | 持续自动 | 最多 90 天，且总量上限 1G |
| 系统日志（nginx 等） | `/var/log/*` | `logrotate.timer` | 每天 00:00 自动 | 以 logrotate 配置为准（未核实具体保留份数） |
| 应用级手动清理 | `logs` 表 | `POST /api/system-task/log-cleanup?target_timestamp=<unix秒>` | 否，需手动触发 | 由调用方指定 |

## 2. 数据库请求日志自动清理（重点）

### 2.1 组成

- Timer：`/etc/systemd/system/new-api-db-log-cleanup.timer`
  - `OnCalendar=*-*-* 00:30:00`，`Persistent=true`，已启用。
- Service：`/etc/systemd/system/new-api-db-log-cleanup.service`（oneshot）
  - `ExecStart=/usr/local/sbin/new-api-db-log-cleanup.sh`
- 脚本：`/usr/local/sbin/new-api-db-log-cleanup.sh`（root 所有，权限 700，修改于 2026-11-10）
  - 读取 `/etc/new-api/logcleanup.env` 中的 PostgreSQL 连接参数（文件内容不记录在本文）。
  - `RETENTION_DAYS=90`，`BATCH=5000`。
  - 计算截止时间 `CUTOFF = 当前时间 - 90 天`（Unix 秒）。
  - 循环执行 `DELETE FROM public.logs WHERE id IN (SELECT id FROM public.logs WHERE created_at < CUTOFF ORDER BY id LIMIT 5000)`，直到删除 0 行。

### 2.2 删除范围（需要确认）

- 脚本注释写的是"删除 consume/error 日志"，但 SQL **没有 `type` 过滤条件**。
- 实际效果：`logs` 表中**所有类型**、超过 90 天的记录都会被删除，包括消费记录。
- 是否符合对账和审计需求尚未确认。如需只删除部分类型，应在 SQL 中加 `type` 条件，并同步修正注释。

### 2.3 运行状态（2026-11-20 核实）

- 2026-11-19 00:30、2026-11-20 00:30 两次执行均成功，`deleted=0`。
- 原因：`logs` 表最早记录是 2026-11-05，尚未超过 90 天。
- 预计首次真正删除数据的时间：约 **2027-02-03**（2026-11-05 之后 90 天）。之后每天删除新超期的数据。

## 3. 应用日志文件自动清理

- Timer：`/etc/systemd/system/new-api-log-retention.timer`，`OnCalendar=daily`，`Persistent=true`，已启用。
- Service：`/etc/systemd/system/new-api-log-retention.service`，执行两条 `find ... -mtime +90 -delete`：
  - `/opt/new-api/instances/*/logs/oneapi-*.log`（文件修改时间超过 90 天）
  - `/var/log/new-api-upgrade-*.log`（文件修改时间超过 90 天）
- 当前这两类文件都还没有超过 90 天，所以尚未删除过文件。

## 4. 系统日志

- journald：`/etc/systemd/journald.conf.d/retention.conf`
  - `SystemMaxUse=1G`，`MaxRetentionSec=90day`。
  - 2026-11-20 时 journal 占用约 1.0G，已接近上限。
- logrotate：`logrotate.timer` 每天 00:00 执行（nginx 访问日志 `/var/log/nginx/access.log` 已按天轮转并压缩）。具体 `/etc/logrotate.d/` 配置未逐项核实。

## 5. 应用代码中的清理逻辑

- `POST /api/system-task/log-cleanup?target_timestamp=<unix秒>`：手动触发，按批删除 `created_at` 早于指定时间的记录（`service/system_task.go`，`model/log.go` 的 `DeleteOldLogBatch`）。
- 这条路径**不在定时任务列表中**（定时任务仅有渠道测试、上游模型更新、Midjourney 轮询、异步任务轮询）。
- 环境变量 `LOG_SQL_CLICKHOUSE_TTL_DAYS` 只对 ClickHouse 日志库生效。gateway 使用 PostgreSQL，该设置不适用。
- `audit_logs` 表**不参与**用量日志的清理（见 `model/audit_log.go`）。
- 应用代码检查基于本地工作区 `dev` 分支；线上运行的是 v1.0.28，二者是否完全一致未验证。

## 6. 待确认事项

1. `new-api-db-log-cleanup.sh` 的删除范围是否应只包含部分 `type`（见 2.2）。
2. 90 天保留期是否满足消费记录的对账、审计和退款需求。
3. 删除前是否需要备份或归档。
4. 应用代码与线上 v1.0.28 是否一致。

## 7. 复查命令

以下命令只读，可用于之后核对状态（在 gateway 上执行，数据库命令需要 root 或 postgres 权限）：

```bash
systemctl list-timers --all | grep -E 'new-api|logrotate'
systemctl status new-api-db-log-cleanup.timer new-api-log-retention.timer
journalctl -u new-api-db-log-cleanup.service --no-pager -n 20
journalctl --disk-usage
sudo -u postgres psql -d new-api -Atc "select min(created_at), count(*) from logs"
```

修改清理脚本或 timer 后，应重新记录本文，并更新"运行状态"和"待确认事项"。
