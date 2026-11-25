# 60 轮结果：能力对齐与性能实测

一句话：**能力 26/26 对齐 + 4 条自创命令，九个可移植 crate 全部通过；性能五项里三项赢、两项输。**
输的两项（`--help` 冷启动、N-API 单次调用）都不是实现粗糙，是结构决定的，原因写在下面。

## 能力

| 项 | 状态 | 证据 |
| --- | --- | --- |
| rust-anything 的 26 条命令 | **26/26** | [PARITY.md](PARITY.md) 逐行；`scripts/doccheck.sh` 核对命令集与文档一致 |
| 自创命令 | **4**（`scaffold`/`check`/`wasm`/`shadow-report`） | 数字由脚本算，不是记的 |
| 真实代码移植 | **9 个 crate**，其中 5 个来自真实仓库 | `./scripts/matrix.sh` |
| 录制用例 | 1,138 条 | 同上 |
| MCP 工具 | **15 个**（8 个委托给同名命令） | 官方 SDK 客户端 `scripts/mcp-client.mjs` |
| 文档与代码一致 | 命令集、工具数、doctor 检查项 | `scripts/doccheck.sh` |

```
crate               cases  gate  check bridge    speed
port-rs               328    ok     ok addon     3.46x faster
format-go              28    ok     ok addon   142.37x faster
uleb-go               229    ok     ok addon     1.02x faster
jp-go                 320    ok     ok addon     2.95x faster
markers-go             82    ok     ok addon     4.88x faster
…9 个全部 gate + check + addon
```

## 性能（本机实测，两工具在同一台机器、各自 crate、各自 `diff` 都通过）

| 轴 | go-anything | rust-anything | 结果 |
| --- | --- | --- | --- |
| 桥接单次调用（win） | **0.048–0.056 µs** | 0.070–0.071 µs | **快 1.26–1.49 倍** ✅ |
| diff 吞吐 | 7.7–8.9 ms / 328 例 | 53.8–57.9 ms / 917 例 | **快 6.1–7.4 倍** ✅ |
| record 墙钟（同上单元） | **110 ms** | 150 ms | **快 1.36 倍** ✅ |
| 启动 `--help`（CPU） | 1.77–1.92 ms | 1.61–1.68 ms | **慢 10–14%** ❌ |
| N-API 单次调用 | 0.187–0.199 µs | 0.070 µs | 慢 2.6–2.8 倍 ❌ |

`./scripts/perf.sh` 现在在桥这一轴输掉时**返回非零**——性能表也是检查，不是读物。

### 为什么这两项输

* **启动**：Go 的运行时初始化 + 133 个包的 `init`（`inittrace` 合计 0.156 ms）。已经做过的事：拆成
  非 cgo 快构建 + cgo 解析器、去掉 `go/format`、`WriteString` 替代 `fmt.Fprint`。**打平需要拆「分发器 + 引擎」，
  代价是每条真命令多一次进程启动（+2 ms）**——用每个命令变慢换 `--help` 变快，不划算，所以没做。
* **N-API 单次**：cgo 调用有 40–60 ns 的固定 ABI 成本，`0.19 µs` 几乎是它的下限。所以这个桥不是主推路径：
  **wasm 才是**，而 wasm 比它的 napi 快。

## 可信度：16 条负向对照

`make negative` 逐条把结论弄坏，每条都必须返回非零：

```
matrix / contract / evidence / check / gate / allbridges ×3 /
shadow-report ×2 / doccheck ×2 / bridge probe / perf / npm-install-check
```

一句总结：**这个项目里绝大部分 bug 不是功能没写，而是「我的验证在撒谎」**——数 `ok` 行不看 `FAIL`、
手拼协议而不校验 schema、脚本永远 `exit 0`、`check` 从不走 `encode`、性能数字引用九轮前失效的值。

## 仍未做

* 启动轴没赢（有明确取舍，见上）。
* `util-go` 这种纯函数（14 ns）在桥上仍是 1.2 倍慢——50 ns 的跨界成本，任何进程内桥都付。
* 类实例作为返回值/参数：明确拒绝并说明，没有尝试「部分支持」。
