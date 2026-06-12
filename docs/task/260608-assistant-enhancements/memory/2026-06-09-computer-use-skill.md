# 需求三：computer-use 作为 CLI + Skill（multica 零代码改动）

## 背景

需求三经三次纠正定为「CLI + skill」：computer-use 本机预装（PATH），multica 用一份 SKILL.md 教 agent 调它。2026-06-09 落地。

## 结论

multica 侧**真的零代码改动**——skill 机制现成。需求三的全部产出 = `computer-use-harness/SKILL.md`（teaching doc，frontmatter `name: computer-use`，符合 multica workspace skill 格式）。用户把它建成 workspace skill 挂到 agent 即可。

## 关键现实校正（摸了真实 CLI 才知道）

computer-use-harness 的 CLI 是**用例驱动（use-case-driven），不是自由原子命令**。

- 命令面只有：`version / apps / capabilities --app / usecases list|dry-run|run <id> (--fake|--mac-helper <path>) / trace --last`。
- **没有** `computer-use click x y` / `type ...` 这种自由原子命令。原子动作（observe/open/click/secondary-click/hover/drag/type/key/scroll/extract）是预定义用例（`usecases/cases.yaml`）里的**步骤**，agent 靠「跑用例 + 读 trace」驱动。
- 要做用例没覆盖的动作 → 得先在 YAML 加用例，不能让 agent 凭空 click。
- 这推翻了 design 3.4 原写的「agent 用 shell 调 computer-use 完成 open app + click」——能力在（Swift mac-helper 支持这些 JSON-RPC：listApps/screenshot/click/type/key/scroll…），但 CLI 没把它们暴露成独立命令。给 CLI 加原子命令属于 computer-use-harness 自己的增强，不在 multica 需求三范围（零改动原则）。

## 真实接口要点（SKILL.md 据此写,逐条验证过）

- 输出永远 JSON：`{ok, command, data?|error?}`。`ok:false` 也是合法 JSON，带 `error.code`（INVALID_RUN_MODE / UNKNOWN_USE_CASE / MISSING_APP_NAME / TRACE_NOT_FOUND…）。退出码 0/2/1。
- `usecases run --fake`（模拟，不碰真 UI）vs `--mac-helper <helper-bin>`（真跑），二者互斥。
- trace = JSONL，`.computer-use/traces/<traceId>.jsonl`，最新路径在 `.computer-use/traces/last`。
- 平台：macOS 14+ only，Node ≥22，Swift 6。真跑要 Accessibility + Screen Recording 权限（UC-001 查），读 `ANTHROPIC_API_KEY` 做 vision/extraction。
- 安装：`npm install && npm run build`（+ `npm install -g .` 上 PATH）；原生执行 `cd native/mac-helper && swift build`。
- 验证过：UC-001 权限检查 passed；`--fake --mac-helper` → INVALID_RUN_MODE；UC-999 → UNKNOWN_USE_CASE。

## 未做（trace 回流）

trace 先留本地，是否接需求二状态栏「来源区」留后议（design 3.3.4）。

## 证据

来源：2026-06-09 摸 `/Users/sumo/workplace/opensource/computer-use-harness` 真实 CLI（dist/cli/index.js）+ 实跑验证。SKILL.md 落在该 repo 根。OpenAI Codex 的 SkyComputerUseClient 是闭源签名二进制不可用，接的是用户自己开源的 computer-use-harness。
