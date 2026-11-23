---
title: 滚动升级的显式排空
status: done
created: 2026-11-19
updated: 2026-11-23
owner: Rock
completed: 2026-11-23
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

两个 unit 各自配置，目录名与 socket 路径都必须按实例区分：

```ini
# new-api-a.service
RuntimeDirectory=newapi-drain-a
RuntimeDirectoryMode=0750
Environment=DRAIN_SOCKET=/run/newapi-drain-a/drain.sock

# new-api-b.service
RuntimeDirectory=newapi-drain-b
RuntimeDirectoryMode=0750
Environment=DRAIN_SOCKET=/run/newapi-drain-b/drain.sock
```

`RuntimeDirectory` 的价值是 systemd 在服务停止时自动删除目录，socket 文件随之消失，无需自行清理。

**修正（第 5 步，2026-11-23）**：原文两个 unit 都写 `newapi-drain`。两个实例共用目录时，一个实例停止会删除另一个实例的 socket，因此改为按实例区分。仓库内 `DEPLOYMENT.md` 第 10.2、10.3 节已同步更新；服务器上的 unit 文件尚未修改，需要在确认后应用。

### 5. 升级脚本

```bash
DRAIN_TIMEOUT_SECONDS=600
DRAIN_SOCK_A=/run/newapi-drain-a/drain.sock
DRAIN_SOCK_B=/run/newapi-drain-b/drain.sock

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
6. **空闲 WebSocket 不会自行结束（第 0 步核实发现，待决策）**：两条路径的读循环都阻塞在 `ReadMessage` 上，既没有读超时，也没有空闲关闭。客户端保持连接但不发请求时，会话永远不结束。如果排空只等计数归零，任何一个空闲会话都会让排空打满 600 秒并中止升级。因此排空开始时必须主动处理 WebSocket，而不是等待它们结束。具体策略见下方"待决策"。

## 前置核实记录（第 0 步）

核实对象：`relay/responses_websocket.go` 的 `:166`、`:512`、`:651`，以及 realtime 路径 `relay/channel/openai/relay_realtime.go`、`relay/websocket.go`。

- **结论：前置条件成立。** 三处 `<-s.ctx.Done()` 都能被唤醒，但机制与原方案描述不同。原方案写的是"依赖读写错误让循环退出"，实际链路是：客户端连接关闭 → 主读循环 `client.ReadMessage()` 返回错误 → 函数返回时的 `defer` 调用 `s.shutdown()` → `s.cancel()` 取消 ctx → 三处 select 退出 → `s.workers.Wait()` 返回。
- **realtime 路径**：客户端或上游任一端关闭，对应读协程返回并关闭 `clientClosed` / `targetClosed`，`OpenaiRealtimeHandler` 返回；`WssHelper` 中 `defer info.TargetWs.Close()` 关闭上游。两端都会收尾，没有泄漏。
- **未核实项**：`runCall` 中不在 select 内的阻塞点（渠道选择、`DoWssRequest` 拨号）是否有超时。写入侧有 `responsesWSWriteTimeout`（30 秒）与 `clientWriteBudget` 截止时间，但拨号超时尚未确认。该项在第 2 步前补查。
- **与计数的关系**：`ResponsesWebSocketHelper` 在 `s.workers.Wait()` 之后才返回，因此在它返回前释放 WebSocket 登记，会把仍在收尾的 goroutine 漏算在外。登记释放必须放在 handler 返回之后。

## 待决策：排空时如何处理空闲 WebSocket

需要在第 2 步之前确定，否则排空逻辑无法完成。

**已决定（2026-11-23）：采用方案 A。** 具体规则：

- Responses WebSocket：排空开始时，没有进行中请求的会话立即以 `1001` 关闭；有进行中请求的会话，在该请求完成并把终止事件写回客户端后以 `1001` 关闭；排空期间收到的新 `response.create` 返回 503。
- Realtime WebSocket：没有请求边界，排空开始后等待 `DRAIN_WS_GRACE_SECONDS`（默认 60 秒，实施时确定），仍在线则以 `1001` 关闭。宽限期内若排空已被撤销（`DELETE /drain`），则不关闭。
- 排空期间新的 WebSocket 升级请求返回 503。

- **方案 A（推荐）：分两类处理。** `POST /drain` 时，立即以 `1001` 关闭没有进行中请求的 Responses WebSocket 会话（`s.current == nil`），进行中的请求等它完成。Realtime 会话没有请求边界，按下面的宽限期处理。
- **方案 B：统一宽限期。** `POST /drain` 后等待 `DRAIN_WS_GRACE_SECONDS` 秒，再对所有剩余 WebSocket 发 `1001`。实现简单，但会切断仍在对话中的 realtime 会话。
- **方案 C：空闲超时。** 为读循环增加空闲截止时间，仅在排空期间生效。改动面最大，且会改变正常运行时的行为。

若选 A 或 B，需要额外确定：Realtime 会话在排空中被 `1001` 关闭后，客户端重连会落到 nginx 上的另一个实例，这是否可以接受。

## 升级脚本测试记录（第 6 步）

脚本位于仓库 `deploy/upgrade.sh`。服务器上的 `/opt/new-api/bin/upgrade.sh` 尚未替换，替换需要在确认后随新版本一起发布。

在 WSL Ubuntu 24.04 的沙箱中（root，`/opt/new-api` 布局与生产一致；`systemctl`、`nginx`、`journalctl` 为 stub，实例为真实 `new-api` 二进制，A、B 共用一个 SQLite 文件以模拟共享数据库）运行六个场景，结果 31 项断言全部通过：

- A：无流量的正常升级。B、A 都先排空再重启，upstream 全部恢复，两个实例运行新版本。
- B：B 上有一个未完成的 HTTP 请求。排空期间 `active_requests` 为 1，B 等请求完成（约 16 秒）后才重启，请求拿到正常响应。
- C：`DRAIN_TIMEOUT_SECONDS=5` 且请求未完成。脚本以 `not drained` 中止，upstream 恢复，B 未重启，drain 状态回到 `running`。
- D：`FORCE_RESTART=yes` 时超时后强制重启 B。
- E：运行中的旧版本没有 drain socket，预检阶段直接拒绝，未改动任何实例或 nginx 配置。
- F：B 上一个空闲的 Responses WebSocket 会话。排空时客户端收到 `1001 server draining`，升级约 4 秒完成，未等待该会话。

测试过程中发现的两个沙箱问题（均与脚本无关）：

- `NODE_TYPE=slave` 的实例不执行 AutoMigrate，必须与主节点共享数据库；生产环境 A、B 本来就共用 PostgreSQL。
- 命令替换会等待后台子进程的 stdout 关闭，导致测试中的 held 请求被同步执行。测试脚本已修正。

## 线上上线记录（第 8 步，2026-11-23）

- 版本：`v1.0.29`（本地 tag 指向 `a8785ee14`，未推送）。二进制 SHA-256 `4FDF8FF54EA635A42ACE468302CFAC00B8851296479072E770E2F4D72060EE4C`，上传前后一致。
- schema 检查：`v1.0.28..HEAD` 的 `model/` 净差异为空，代码中无迁移语句，`CONFIRM_BACKWARD_COMPATIBLE=yes` 有依据。
- 线上操作：
  - release 安装到 `/opt/new-api/releases/v1.0.29/`，v1.0.28 保留作为回滚路径；
  - 两个 unit 增加 `RuntimeDirectory` 与 `DRAIN_SOCKET`，改动前备份为 `*.service.bak-before-drain`；
  - 旧版脚本执行滚动升级：B 与 A 均 `drained` 后重启，`upgrade complete`；
  - `/opt/new-api/bin/upgrade.sh` 替换为本仓库 `deploy/upgrade.sh`，旧版备份为 `upgrade.sh.bak-before-drain`。
- 线上已确认：两个实例 active 且健康，运行 v1.0.29，nginx `:80` 返回 200，upstream 无 `down`，fatal/panic 为 0，A 启动后台任务、B 未启动，drain socket 存在且权限为 `0660`，TCP 监听仅 3000、3001。
- **未在线上验证**（需要测试账号，本次未进行）：验收条件 2（在途 SSE 排空后再重启）、3（空闲 WebSocket 收到 1001）、5（socket 的访问控制）、6（排空超时中止并恢复流量）。这些项目前只有 WSL 沙箱和单元测试的结果。

## 验证结果

- 单元与集成测试：见上文各节的记录（middleware、controller drain、relay WebSocket 回归）。
- 沙箱端到端：第 4 步（SIGTERM 空闲会话）与第 6 步（六个升级场景，31 项断言）。
- 线上部署：v1.0.29 已在网关上线，两个实例健康，运行新版本，upstream 正常，drain socket 存在（见"线上上线记录"）。
- **线上端到端：未执行（2026-11-23 决定）。** 原因是缺少可用的测试账号，且当前不便安排测试流量与重启窗口。以下验收条件**在线上未验证**，仅有沙箱或单元测试结果：
  - 验收条件 2：在途 SSE 排空后才重启，客户端流不被截断；
  - 验收条件 3：空闲 WebSocket 在升级时收到 `1001`；
  - 验收条件 5：drain socket 的访问控制（非授权用户被拒绝、`ss` 输出符合预期）；
  - 验收条件 6：排空超时后中止、流量恢复、实例不重启。
- 结论：功能代码与升级脚本已通过单元、集成与沙箱验证，并已上线。线上的排空行为将在下一次升级时首次真实触发，届时需要留意脚本日志中的 `drained` 与超时记录。

## 阻塞原因

（不适用）
