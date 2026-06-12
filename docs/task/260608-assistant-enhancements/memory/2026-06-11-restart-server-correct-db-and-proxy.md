---
name: restart-server-correct-db-and-proxy
description: 重启本地 server 做实机验证时的两个坑——真数据在 multica 库不是 .env 的 local_medeo；curl 必须 --noproxy 否则被 http_proxy 打成 502
metadata:
  type: project
---

实机重启 server 验证本轮改动时连踩两坑,都让我误判"服务没起/token 失效"。沉淀避免重复烧时间。

## 坑 1（核心）：真实运行数据在 `multica` 库,不是 `.env` 写的 `local_medeo`

- `.env` 里 `DATABASE_URL=...local_medeo`,但那个库是**空的(0 表)**。系统真正在用的数据(52 表、4 个在线 runtime、`personal_access_token`/`daemon_token`/`task_token`)全在 **`multica`** 库。
- 桌面端(路径 C)起的本地 server 用的是 `multica` 库(desktop 自带 env,不读仓库 `.env`)。我按仓库 `.env` 起 server → 连到空的 `local_medeo` → 没有任何 token → 桌面 daemon 的 token 被判 **401 invalid token**,我一度误以为是 JWT secret 变了。
- `JWT_SECRET` 在 `.env` 里**是固定的**,所以重启 server 本身不会让 token 失效——401 的真因是**连错库**。
- **正解**:实机起 server 要 `export DATABASE_URL="postgres://sumo@localhost:5432/multica?sslmode=disable"` 再起(覆盖 `.env` 的 local_medeo)。判据:连对库后 server.log 立刻有 desktop WS client connected、`agent_runtime` 有 4 个 online、`grep -c "invalid token"` = 0。
- 诊断手法:`psql .../<db> -tAc "SELECT count(*) FROM pg_tables WHERE schemaname='public'"` 比较两个库;`SELECT tablename FROM pg_tables WHERE tablename LIKE '%token%'`。

## 坑 2：`curl localhost` 被 `http_proxy` 打成 502/403

- shell 里 `http_proxy=http://127.0.0.1:6454`(同 [[daemon-agent-env-bedrock-403]] 那个代理)。`curl http://localhost:8080/health` 走代理 → **502**,我误判"server 没起"。
- **正解**:本机健康检查一律 `curl -s --noproxy '*' http://localhost:8080/...`。加了 `--noproxy '*'` → 200。

## 重启全栈的正确顺序(本轮验证用)

1. `go build -o /tmp/multica-server-new2 ./cmd/server`(从 server 目录,含当前改动)。
2. `kill <8080 listener>`;`export DATABASE_URL=...multica...`;`nohup /tmp/multica-server-new2 &`。
3. `pnpm --filter @multica/desktop run bundle-cli`(重建桌面 daemon 二进制 `apps/desktop/resources/bin/multica`,否则用旧 prompt——[[realmachine-daemon-must-rebuild]])。
4. `apps/desktop/resources/bin/multica daemon start --foreground --profile desktop-localhost-8080 &`(profile 是 **desktop-localhost-8080**,不是 `local`;`local` profile 没登录会卡交互式 `multica login`)。
5. 验:health 200、`invalid token` 计数 0、4 runtime online、daemon log 有 `authenticated` + `task wakeup websocket connected`。

关联:[[desktop-is-the-target-end]]、[[realmachine-daemon-must-rebuild]]、[[stale-process-gotcha]]、[[daemon-agent-env-bedrock-403]]。
