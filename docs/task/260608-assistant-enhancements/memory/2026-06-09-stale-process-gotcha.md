# 陈旧进程坑：改了"启动时加载一次"的东西,跑着的进程必须重启才生效

## 背景

实机验证 goal 模式时连续踩了三次同一类坑,根因都是「磁盘上代码/配置已更新,但运行中的进程用的是启动时刻的内存快照」。

## 三次实例(同一根因)

| 改了什么 | 谁缓存了旧的 | 症状 | 修法 |
|---------|------------|------|------|
| server Go 代码(新增 /api/goals 路由) | 旧 server 二进制(早些时候启动) | 路由 404 / 行为不对 | 重新 build + 重启 server |
| daemon Go 代码(prompt.go 的 goal-planning 分支) | 旧 daemon 二进制(homebrew/desktop 打包) | claude 收到默认 issue prompt,goal 卡 planning | 重建 daemon 二进制 + 重启 daemon |
| `packages/core/package.json` 的 `./goals` exports | 运行中的 vite(electron-vite dev,启动时解析一次 exports map) | `"./goals/queries" is not exported` 解析报错 | 重启 dev:desktop,vite 重读 package.json |

## 规律

凡是「启动时加载一次、之后不重读」的东西被改了,运行中的进程都得重启:
- **package.json exports / 依赖** → vite / Next / turbo dev server
- **Go schema / 路由 / prompt** → server / daemon 二进制(且二进制是独立编译/分发的,server 和 daemon 要分别重建)
- **env / 凭据** → 任何 spawn 子进程的长驻进程(见 [[2026-06-09-realmachine-daemon-must-rebuild]] 的 403)

## 排查信号

报错/行为不符预期时,先问「跑着的这个进程是什么时候启动的?在我改这东西之前还是之后?」用 `ps -o lstart= -p <pid>` 对比文件修改时间。早于改动 = 大概率陈旧快照,重启即解,不要去改本已正确的代码。

## 证据

来源：2026-06-09 实机验证全程实测。三次都确认是进程启动时间早于代码/配置改动,重启后即解,代码本身无问题。
