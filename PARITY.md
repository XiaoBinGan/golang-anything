# 与 rust-anything 的能力对照

目标：**全部 26 条命令对齐**，并在两项性能上超过它（桥接单次调用、工具启动与扫描）。

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
| 9 | `profile` | — | ⬜ | 跑真实负载找热点函数 |
| 10 | `go` | — | ⬜ | 一条命令串完 profile→bridge→record→交接 |
| 11 | `enable` | `enable` | ✅ | 在原式末尾插一段标记块，把导出重绑到 Go 端口；`--only` 选函数，`--async` 走 stdio；未构建或导出名写错直接拒绝 |
| 12 | `disable` | `disable` | ✅ | 精确移除该块，文件回到**字节一致**（有测试断言） |
| 13 | `report` | `report` | ✅ | `REPORT.md` + `card.svg`（840×240，不通过时换色），数字全部来自当场跑的 `diff`/`bench` |
| 14 | `clean` | `clean` | ✅ | 删 `work/`、`portctl`、`cmd/gobench`，不动源码 |
| 15 | `shadow` | `shadow` + `shadow-report` | ✅ | 生成 `shadow.cjs`：同一调用跑原式与端口，比对后**返回原式的答案**，差异写 `shadow.jsonl`；`shadow-report` 汇总 |
| 16 | `gate` | `gate` | ✅ | 每例匹配 + 每个 `throw` 必须有录到的失败用例 + 导出不缺 + 不许 panic |
| 17 | `package` | — | ⬜ | 多平台打包与 CI 工作流 |
| 18 | `app record` | — | ⬜ | 整程序（CLI/HTTP 服务）黑盒录制 |
| 19 | `app replay` | — | ⬜ | 用录到的用例回放任意构建 |
| 20 | `app front-door` | — | ⬜ | 生成接管入口的 Go 二进制 |
| 21 | `app wire` | — | ⬜ | 把命令交给某个单元的端口 |
| 22 | `app lift` | — | ⬜ | fallback 归零后变成纯 Go 应用 |
| 23 | `setup` / `mcp` | — | ⬜ | 给 agent 注册 MCP 工具 |
| 24 | `map` | `map` | ✅ | 写 `PORT-MAP.md`：每个单元、它的 crate、用例数、上次 diff、是否已构建 |
| 25 | `doctor` | `doctor` | ✅ | 检查 go/node/python3/cgo/Node 是否自带类型剥离，并给修复命令 |
| 26 | — | `check` | ✅ 额外 | 用真正被调用的那条桥（addon 或 stdio）回放全部用例 |
| 27 | — | `fast` | ✅ 额外 | 只构建进程内桥 |

**当前：26 条里 16 条已实现（含 3 条自创：`check` `fast` `shadow-report`），10 条待做。**

## 性能（本机 macOS arm64，Node 22.22，Go 1.26）

### 桥接单次调用

| 路径 | 每次调用 | 每秒调用 |
| --- | ---: | ---: |
| 原式（TS，V8 内联的 `count`） | 0.05 µs | 20,500,000 |
| **go-anything 进程内（c-shared + N-API）** | **0.73 µs** | **1,360,000** |
| go-anything stdio 进程 | 12.02 µs | 83,000 |
| rust-anything 宣称（napi-rs，JSON 过线） | ~1 µs | ~1,000,000 |

→ 进程内桥比 rust-anything 宣称的 ~1 µs 快约 **27%**；stdio 桥只在不到 100 次/秒的场景用。

### 端口对原式的逐函数速度（`bench`，60 ms/侧）

```
| Function | Cases | Original per call | Go per call | Speedup |
| `count`   |  20 | 27 ns |  45 ns | 1.68x slower |
| `rangeOf` | 400 | 13 ns | 196 ns | 15.28x slower |
| `ratio`   | 196 | 595 ns |   8 ns | 78.09x faster |
| `words`   |  20 | 71 ns |  40 ns | 1.76x faster |

Geometric mean: 1.52x faster over 4 function(s)
```

诚实说明：`count`/`rangeOf` 这类**微函数**上原式更快（V8 把它们内联成几条机器指令，而我们每调用要跨一次语言边界、分配几个值）；`ratio` 这类带异常路径的函数端口快 78 倍（Go 的错误返回没有 JS 的异常构造与栈展开成本）。几何平均 1.52x 领先，但微函数是短板的真相要写在这里，而不是只报平均值。

## 下一轮的顺序（按对“对齐 + 超越”的贡献排序）

1. ~~`enable` / `disable` / `shadow`~~ —— 已完成，端口能在原仓库里真的被用起来
2. ~~`fuzz`~~ / ~~`refresh`~~ / ~~`report`~~ —— 已完成
3. `package` —— 多平台打包与 CI 工作流
5. `app record` / `replay` / `front-door` / `wire` / `lift` —— 整程序那一层
6. `setup` / `mcp` —— agent 接入
7. `profile` / `go` —— 热点驱动的一键流程
