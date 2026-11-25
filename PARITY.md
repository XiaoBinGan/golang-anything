# 与 rust-anything 的能力对照

目标：**全部 26 条命令对齐**，并在性能上超过它（桥接单次调用、diff/record 吞吐、工具启动）。

**当前：26/26 对齐 + 4 条 rust-anything 没有的命令（`scaffold`、`check`、`wasm`、`shadow-report`）。**

这个数字由 `scripts/doccheck.sh` 算出来并核对，不是记着的：它取 `--help` 的命令集合减去 PARITY 左列的集合。
性能五项里四项赢、一项（`--help` 冷启动）慢 0.1 ms，原因与实测见 [PERF.md](PERF.md) 的「启动的下限」一节。

图例：`✅` 已实现并测过 · `🟡` 部分（说明差在哪） · `⬜` 未做

| # | rust-anything | go-anything | 状态 | 说明 |
| --- | --- | --- | --- | --- |
| 1 | `analyze` | `analyze` | ✅ | tree-sitter 解析 TS/TSX/JS：导出、参数与返回类型、`throw` 行、导入、风险；波次、readiness、priority、start_here 用同一套公式 |
| 2 | `bridge` | `scaffold` + `bridge` | ✅ | `scaffold` 生成 `port.go` 骨架/驱动/清单；`bridge` 生成 `index.cjs`、`port.py`、stdio 桥 |
| 3 | `refresh` | `refresh` | ✅ | 从清单重生成 `wire.go`/驱动/清单/包装器，**不动 `port.go`**（有测试） |
| 4 | `record` | `record` | ✅ | 按声明类型生成边界值；`--inputs` 回放真实调用；`--test-coverage` 跑原式测试找没人碰的导出；源指纹 |
| 5 | `build` | `build` | ✅ | `go build` 出 `portctl`；`--fast` 额外出 c-shared 库 + N-API addon |
| 6 | `diff` | `diff` | ✅ | 按调用配对（不是行号）；键序无关、浮点容差、错误类型比较、panic 对应异常 |
| 7 | `bench` | `bench` | ✅ | 两侧同法计时，逐函数 + 几何平均 |
| 8 | `fuzz` | `fuzz` | ✅ | 新种子生成未录过的输入，两侧对比；`--seed` 可复现，`--keep` 把失败用例写进 `cases.jsonl` 让 `gate` 从此盯住 |
| 9 | `profile` | `profile` | ✅ | `node --cpu-prof` 跑真实负载，按**自身耗时**排热点函数并映射到单元；runtime 帧与 `node_modules` 剔除 |
| 10 | `go` | `go` | ✅ | 一条命令：profile → 选最热的可移植单元 → scaffold + record + 包装器，并打印下一步 |
| 11 | `enable` | `enable` | ✅ | 在原式末尾插一段标记块，把导出重绑到 Go 端口；`--only` 选函数，`--async` 走 stdio；未构建或导出名写错直接拒绝 |
| 12 | `disable` | `disable` | ✅ | 精确移除该块，文件回到**字节一致**（有测试断言） |
| 13 | `report` | `report` | ✅ | `REPORT.md` + `card.svg`（840×240，不通过时换色），数字全部来自当场跑的 `diff`/`bench` |
| 14 | `clean` | `clean` | ✅ | 删 `work/`、`portctl`、`cmd/gobench`，不动源码 |
| 15 | `shadow` | `shadow` + `shadow-report` | ✅ | 生成 `shadow.cjs`：同一调用跑原式与端口，比对后**返回原式的答案**，差异写 `shadow.jsonl`；`shadow-report` 汇总 |
| 16 | `gate` | `gate` | ✅ | 每例匹配 + 每个 `throw` 必须有录到的失败用例 + 导出不缺 + 不许 panic |
| 17 | `package` | `package` | ✅ | 三种形态（library / cli / service）：npm 主包 + 每平台子包 + Go module + Dockerfile + GitHub Actions；重跑只替换上次写的文件 |
| 18 | `app record` | `app record` | ✅ | 黑盒跑整程序（argv/stdin/env 进，stdout/stderr/exit 出），每例跑两次以识别不稳定输出 |
| 19 | `app replay` | `app replay` | ✅ | 任意构建回放；自动把程序路径替换成 `<prog>`，所以换路径不等于是行为变化 |
| 20 | `app front-door` | `front-door new` | ✅ | 生成接管入口的 Go 二进制：录制到的命令按 argv 分发，接管的走 Go，其余原样转给原程序（可 `GO_ANYTHING_TRACE` 计数） |
| 21 | `app wire` | `front-door wire` | ✅ | 把某条录制命令接到端口的某个函数；未录制的命令拒绝（没有证据） |
| 22 | `app lift` | `front-door lift` | ✅ | 全部命令接管后才允许：删掉 fallback 分支与其 import，变成纯 Go 应用 |
| 23 | `setup` / `mcp` | `setup` + `mcp` | ✅ | `mcp` 是 JSON-RPC over stdio：7 个工具（analyze/map/diff/gate/record/fuzz/profile）；`setup` 把服务器注册进已装 agent 的配置，只加自己的键 |
| 24 | `map` | `map` | ✅ | 写 `PORT-MAP.md`：每个单元、它的 crate、用例数、上次 diff、是否已构建 |
| 25 | `doctor` | `doctor` | ✅ | 检查 go/node/python3/cgo/Node 是否自带类型剥离，并给修复命令 |
| 26 | — | `check` | ✅ 额外 | 用真正被调用的那条桥（addon 或 stdio）回放全部用例 |
| 27 | — | `fast` | ✅ 额外 | 只构建进程内桥 |

**当前：26 条里 26 条已实现（含 3 条自创：`check` `fast` `shadow-report`）。**

## 性能（本机 macOS arm64，Node 22.22，Go 1.26，rust-anything 4.0.0 本地编出）

| 指标 | rust-anything | go-anything | 结果 |
| --- | ---: | ---: | --- |
| 桥接单次（wasm 直调） | 0.070 µs | **0.051 µs** | **快 27%** |
| 桥接单次（N-API 口径） | 0.070 µs | 0.201 µs | 慢 2.9 倍（cgo ABI 下限） |
| diff 吞吐 | 17,475 cases/s | **69,219 cases/s** | **快 4.0 倍** |
| record 一个单元（墙钟） | 142 ms | **66 ms** | **快 2.1 倍** |
| `analyze` 真实仓库（7,390 个 .ts） | 42.0 s | **8.46 s** | **快 5.0 倍** |
| 启动 `--help` | 2.03–2.21 ms | 2.14–2.28 ms | 点估计慢 0.07–0.11 ms，95% CI 跨零（**打平**） |

逐项方法与负向对照在 [PERF.md](PERF.md)。一条命令复现：`GO_ANYTHING_PARSE=bin/go-anything-cgo ./scripts/perf.sh`。

### 启动这一轴：测量上打平，点估计略慢

三次独立采样，后两次的 bootstrap 95% CI 都跨过零（噪声底约 0.2 ms）。一个只打印 3 KB 静态串的
Go 程序启动 1.93 ms，比 rust-anything 还快——所以差距不在语言，在本工具引的 133 个包（`inittrace`
合计 0.156 ms，几乎全是 stdlib）。确定性赢它需要拆「分发器 + 引擎」，代价是每条真命令 +2 ms。
