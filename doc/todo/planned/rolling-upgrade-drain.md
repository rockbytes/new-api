---
title: 滚动升级的显式排空
status: planned
created: 2026-11-19
updated: 2026-11-19
owner: Rock
completed:
---

# 滚动升级的显式排空

## 背景

生产环境在同一台主机上部署两个 new-api 实例（A `:3000`、B `:3001`），nginx 反代轮转，升级走 `/opt/new-api/bin/upgrade.sh`（内容见 `DEPLOYMENT.md` 附录 A）。

当前脚本摘除实例后的等待是固定 `sleep 3`：

```bash
set_upstream 3001 down
sleep 3
restart_instance new-api-b 3001 b
```

脚本**没有任何机制判断该实例是否还有进行中的 HTTP 请求**。实际起到保护作用的只有两层隐式机制：

1. nginx reload 不打断已建立的连接，旧 worker 继续处理手上的请求。
2. `systemctl restart` 发 SIGTERM，触发 `main.go` 的 `srv.Shutdown(ctx)` 优雅关闭，阻塞到连接处理完或 `SHUTDOWN_TIMEOUT_SECONDS`（默认 120 秒）超时。

## 现有缺口

### 缺口 1：流式请求只有 120 秒预算

`DEPLOYMENT.md` 的 nginx 配置允许 `proxy_read_timeout 600s`，即单条 SSE 可以合法跑 10 分钟；而优雅关闭只有 120 秒。超过 120 秒的流会在 `Shutdown` 超时后被强制断开，客户端收到截断的流。

问题不是"120 秒太短"，而是**唯一的保证只有 120 秒**：既没有提前排空的阶段，也没有任何请求可以被等待更久。

### 缺口 2：WebSocket 完全不受保护

`http.Server.Shutdown` 不跟踪 hijacked 连接，这是标准库的既定行为。以下两处 upgrade 的连接在进程退出时会被直接切断：

- `controller/relay.go` 的 `Relay`（`RelayFormatOpenAIRealtime`）
- `controller/responses_websocket.go` 的 `ResponsesWebSocket`

`Shutdown` 不会等它们，也不会等它们把计数减到零。

额外风险：`relay/responses_websocket.go` 的 `responsesWSSession.ctx` 派生自 `c.Request.Context()`，hijack 之后连接关闭**不会**取消这个 ctx。强制关闭只能依赖读写返回错误让循环退出。

### 缺口 3：现成的计数器用不上

`middleware.StatsMiddleware()` 提供了 `activeConnections`，但有两个问题：

- **挂载范围错误**：`router/relay-router.go` 调用 `router.Use(middleware.StatsMiddleware())`，而 `router/main.go` 的 `SetRouter` 先执行 `SetApiRouter`、再执行 `SetRelayRouter`。gin 的 `engine.Use` 只作用于注册之后的路由，因此 `/api/*`、dashboard、`NoRoute` 全部不计数。
- **暴露位置不可用**：只在 `GET /api/status/test` 输出，且该路由要求 `middleware.AdminAuth()`，升级脚本没有可用的凭据。

结论：脚本无法获取在途请求数，统计本身也不完整。

## 已确定的设计决策（2026-11-19）

### 排空总时长：600 秒

`DRAIN_TIMEOUT_SECONDS=600`，对齐 nginx 的 `proxy_read_timeout 600s`。

- 该值只在脚本侧，不写入 `shared/new-api.env`。
- `SHUTDOWN_TIMEOUT_SECONDS` 保持 120，与 systemd `TimeoutStopSec=130` 配套不动。
- 代价：最坏情况单实例排空 600 秒，两个阶段合计约 20 分钟，全程另一实例在线。正常情况下有在途请求时数秒归零，600 只是上限。
- 不采用"把 `proxy_read_timeout` 降到 180 秒"的替代方案：那会截断所有客户端的超长流，属于产品决定而非工程决定。

### 管理端点形态：unix socket

选 unix socket 而非回环 TCP，理由是暴露面而非便利性。

`DEPLOYMENT.md` 已记录「3000 和 3001 对外可达，绕过 nginx 也能访问，需要云安全组兜底」。再加两个 TCP 管理端口就是给同一条限制再加两个待守的洞口；且 `DRAIN_ADDR` 一旦被误填为 `:3002`（漏写 `127.0.0.1`），`POST /drain` 就直接上了公网——那是一个能让任何人把线上实例踢出轮转的接口。

unix socket 没有这个失败模式：没有端口，`ss -ltnp` 与防火墙都不会多出东西，访问权限由文件模式控制。

已知代价与对策：

- **多约十行 listener 代码**。
- **stale socket 文件**：`Restart=always` 触发的重启不会清理 `RuntimeDirectory`（那只在 stop 时发生），残留的 socket 文件会让 `net.Listen` 报 `address already in use`。必须在 listen 前 `os.Remove(path)`。这是 unix socket 唯一比 TCP 麻烦的地方。

### 排空失败时默认中止升级

`drain_instance` 超时后默认 `die`，而非强行重启。强行重启会静默切断在途流与 WebSocket；中止的代价只是两个实例暂时处于混合版本，而这本来就已经是脚本要求 `CONFIRM_BACKWARD_COMPATIBLE=yes` 人工确认过的前提。

提供 `FORCE_RESTART=yes` 逃生开关，供明确的紧急场景使用。

### 管理端点监听失败不阻断启动

drain 是运维辅助设施，不应因为它起不来就把整个网关拖死。监听失败只记日志并跳过；`DRAIN_SOCKET` 为空时完全不启动，其他部署方式零影响。

## 目标

1. 进程维护精确的在途 HTTP 请求数与 WebSocket 连接数，覆盖 `/api/*`、dashboard、relay 与 `NoRoute` 全部路由。
2. 提供一个仅本机可访问的管理端点，供升级脚本查询计数、进入与退出排空状态。
3. 升级脚本改为按计数归零推进，取代固定 `sleep 3`；排空超时中止并恢复流量。
4. WebSocket 连接纳入排空范围：排空中拒绝新连接，关闭前主动发 `1001 Going Away`。
5. 不影响未配置 `DRAIN_SOCKET` 的部署，也不改变任何现有响应语义。

## 验收条件

1. 普通 `/api/*` 请求能被 `active_requests` 计入（改动前该值恒为 0）。
2. 发起一条 300 秒 SSE 后执行升级：`/drain/status` 显示 `active_requests` 由 1 变 0 后脚本才继续，客户端全程不断流。
3. 发起一条 realtime WebSocket 后执行升级：连接以 `1001` 结束或自然结束，不出现裸 TCP reset；`active_websockets` 计数在连接结束后归零。
4. 排空期间新到达的 WebSocket upgrade 在握手阶段返回 503，不出现"建立后又立刻被踢"。
5. 从非授权身份访问 socket 被拒（文件模式 `0660`）；升级期间 `ss -ltnp` 只显示 3000/3001 两个监听，`ss -xl` 显示 drain socket。
6. 制造排空超时（如临时把 `DRAIN_TIMEOUT_SECONDS` 设为 1）：脚本中止，`restore_traffic` 把实例加回 nginx，且该实例的 `/drain/status` 回到 `running`。
7. `restore_traffic` 恢复实例时同时解除 draining，避免实例回到轮转却拒绝所有 WebSocket。
8. 手动 `systemctl restart` 时有在途流，优雅等待仍然生效。
9. 未设置 `DRAIN_SOCKET` 时，进程不创建 socket，行为与改动前一致。
10. `go build ./...` 通过；改动包的 `go test` 通过。

## 实施计划

### 1. 统一在途计数

`middleware/stats.go` 扩展为进程级统计：

```go
type inFlightStats struct {
    requests int64                          // atomic
    sockets  map[*websocket.Conn]struct{}   // mu 保护
    draining atomic.Bool
}
```

- `router/main.go` 的 `SetRouter` 开头加 `router.Use(middleware.StatsMiddleware())`，删除 `router/relay-router.go` 中的重复注册。
- 新增 `TrackWebSocket(conn *websocket.Conn) (release func())` 与 `ActiveWebSocketCount() int64`。
- `controller/relay.go` 与 `controller/responses_websocket.go` 在 `upgrader.Upgrade` 成功后 `defer release()`。
- 排空状态下，两处 upgrade 之前检查并直接返回 503。

### 2. 管理监听器

新增 `controller/drain.go`（或放入 `middleware` 同级的新文件），提供仅绑定 unix socket 的 `http.Server`：

| 路由 | 语义 |
| --- | --- |
| `GET /drain/status` | `{"state":"running\|draining","active_requests":N,"active_websockets":N}` |
| `POST /drain` | 进入 draining，幂等，立即返回 202，不阻塞 |
| `DELETE /drain` | 恢复 running |

实现要点：

```go
os.Remove(path)                      // 清理上次异常重启残留
ln, err := net.Listen("unix", path)
if err != nil { log and skip }       // 不 FatalLog
os.Chmod(path, 0o660)
go drainServer.Serve(ln)
```

- 由 `DRAIN_SOCKET` 环境变量开关，空值不启动。
- 探针不走 gin 引擎，因此健康检查不会把自己计入 `active_requests`。

### 3. 关闭序列

`main.go` 收到 SIGTERM 后：

1. 置 draining，阻止新 WebSocket 连接。
2. 调用 `srv.Shutdown(ctx)`（上限 `SHUTDOWN_TIMEOUT_SECONDS`）。
3. 等待 WebSocket 计数归零；超时则批量发送 `1001 Going Away` 后关闭。

必须验证 `relay/responses_websocket.go` 中三处 `<-s.ctx.Done()` 所在的 select 都能被读写错误打断，否则连接关闭后 goroutine 仍在运行、计数不归零。这三处是实施前必须先核实的前提。

### 4. systemd unit

两个 unit 各自配置（socket 路径必须按实例区分）：

```ini
[Service]
RuntimeDirectory=newapi-drain
RuntimeDirectoryMode=0750
Environment=DRAIN_SOCKET=/run/newapi-drain/drain.sock
```

`RuntimeDirectory` 的价值是 systemd 在服务停止时自动删除目录，socket 文件随之消失，无需自行清理。

### 5. 升级脚本

```bash
DRAIN_TIMEOUT_SECONDS=600
DRAIN_SOCK_A=/run/newapi-drain/drain.sock

drained() {  # drained <socket>
  curl -sf -m 2 --unix-socket "$1" http://localhost/drain/status \
    | grep -q '"active_requests":0,"active_websockets":0'
}

drain_instance() {  # drain_instance <socket> <label>
  curl -sf -m 3 -X POST --unix-socket "$1" http://localhost/drain >/dev/null || return 1
  local deadline=$((SECONDS + DRAIN_TIMEOUT_SECONDS))
  while [ "$SECONDS" -lt "$deadline" ]; do
    drained "$1" && return 0
    sleep 1
  done
  curl -s --unix-socket "$1" http://localhost/drain/status | tee -a "$LOG"
  return 1
}
```

- `restore_traffic` 中为每个待恢复实例补一次 `curl -X DELETE http://localhost/drain`（同样走 `--unix-socket`）。
- 阶段 1 / 阶段 2 把 `sleep 3` 换成 `drain_instance`，失败时检查 `FORCE_RESTART`。
- Ubuntu 20.04 自带 curl 7.68，`--unix-socket`（7.40+）可用。

### 6. 文档

`DEPLOYMENT.md` 需同步更新：脚本正文、systemd unit 片段、`DRAIN_TIMEOUT_SECONDS` 说明，以及「已知限制」中关于长连接的那一条。

## 未覆盖项与风险

1. **最坏耗时从 3 秒变成约 20 分钟**：这是把隐式截断换成显式等待的直接代价。若长期观测到排空经常打满 600 秒，说明存在异常长连接或计数泄漏，应排查而非调大上限。
2. **`relay/responses_websocket.go` 的 ctx 不随 hijack 取消**：强制关闭依赖读写返回错误，实施前必须逐处核实三处 select 的退出路径。
3. **排空期间返回 503 的语义**：nginx 已摘除该实例，正常流量不应到达，503 只用于兜底直连场景。若存在绕过 nginx 的直连客户端，其重试行为需单独确认。
4. **计数准确性依赖中间件挂载位置**：`SetRouter` 是唯一入口，任何新增的独立 `gin.Engine`（如 `controller/responses_websocket.go` 中的 `responsesWSRequestEngine`）都不会被计数。后者的请求由已计数的外层 WebSocket 覆盖，目前是安全的，但新增独立引擎时需注意这一点。
5. **多实例 socket 路径冲突**：两个实例若共用同一 `DRAIN_SOCKET`，第二个会因 `os.Remove` 误删第一个的 socket 而抢占。必须在各自 unit 中配置不同路径，部署文档需显式提醒。

## 验证结果

（实施后填写）

## 阻塞原因

（不适用）
