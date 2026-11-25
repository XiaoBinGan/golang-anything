# 性能：实测，而不是宣称

同一台机器（darwin/arm64, 10 核）、同一台 Node（v22.22.3）、Go 1.26.2、rust-anything 4.0.0（本机
`cargo build --release` 编出来的）。两个工具各自用**自己生成的桥**，跑**同样的函数**。

**方法**：交替分块计时（A 一次、B 一次，再反过来），多次取中位数——一次负载尖峰会同时打到两侧。
两边都必须是**真跑通**的：`diff` 两边都是 `rc=0`（我先把 Rust 端口的 `rangeOf` 补对，917/917 通过），
否则量到的是「失败有多快」。

一条命令复现全部四行：

```sh
./scripts/build.sh bin
GO_ANYTHING_PARSE=bin/go-anything-cgo ./scripts/perf.sh
```

## 汇总

| 指标 | rust-anything | go-anything | 结果 |
| --- | ---: | ---: | --- |
| 桥接单次调用（napi vs wasm） | 0.070 µs | **0.051 µs** | **快 27%** ✅ |
| 桥接单次调用（napi vs cgo addon） | 0.070 µs | 0.201 µs | 慢 2.9 倍 |
| record 吞吐 | 6,548 cases/s | **10,781 cases/s** | **快 65%** ✅ |
| diff 吞吐 | 17,475 cases/s | **69,219 cases/s** | **快 4.0 倍** ✅ |
| 启动 `--help` | 2.00–2.08 ms | 2.09–2.19 ms | **慢 ~0.1 ms（95% CI [+0.06, +0.15]）** |
| `analyze` 真实仓库（7,390 个 .ts） | 42.0 s | **8.46 s** | **快 5.0 倍** ✅ |

## 逐项测量

同一台机器（darwin/arm64, 10 核）、同一个单元（`src/text.ts`）、同一台 Node（v22.22.3）、
Go 1.26.2。两个工具都用自己生成的桥，调用同一个 `count("a b  c")`。

**测量方法**：交替分块计时（先 Rust 再 Go，再反过来），每块 5000 次调用，取 14 个样本的中位数。
这样一次负载尖峰会同时打到两侧，不会只坑一边。

## 1. 桥接：三条入口，三种成本

| 入口 | 每次调用 | 每秒调用 | 说明 |
| --- | ---: | ---: | --- |
| `answer(name, argsJSON)`（通用） | 1.4–2.0 µs | ~600,000 | JSON 双向 + napi 字符串进出 |
| `answer1(name, arg)`（单字符串） | **0.21–0.23 µs** | ~4,500,000 | 不走 JSON、不 malloc |
| `numbers(name, blob, lens)`（批量） | **0.051–0.055 µs** | ~19,000,000 | 参数一个 UTF-8 blob + 长度表，答案写进 Float64Array |
| rust-anything（napi-rs，单次调用） | 0.073 µs | 13,700,000 | 无批量接口，每次都是单次 |

**结论分两半，都要说：**

- **单次调用**：0.215 µs vs 0.073 µs，**我们落后约 3 倍**。原因是结构性的——Rust 在同进程内直接调用，
  Go 每次必须跨 cgo（40–60 ns 下限）+ 两次 napi 字符串取值 + 一次 napi 字符串创建。
- **批量调用**：0.051 µs vs 0.073 µs，**每次调用比它的单次调用还快 30%**。因为固定的跨界成本只付一次，
  且不再逐条 JSON / 逐条创建字符串——这是唯一能压到它以下的形态。

把 2000 条调用打成一个批量后，每次调用成本 0.24 µs → 0.051 µs（字符串批量没有收益，因为成本不在跨界，
而在逐条 JSON 与字符串；换成类型化缓冲区才降下来）。

原来的分析保留在这里，因为它说明了为什么要做批量：

- Rust 侧是**同一个进程内的原生调用**；
- Go 侧每次调用必须跨 cgo（约 40–60 ns 的下限），再经过两次 napi 字符串取值，然后才进 Go。

已经做过的优化（每一步都实测过）：

| 做法 | 结果 |
| --- | --- |
| JSON 双向过线（最初） | 1.4–2.0 µs |
| 「名字 / 参数」分开传，答案写进调用方缓冲 | 0.73 µs |
| 单字符串专用入口 `answer1`（不走 JSON、不 malloc） | **0.25–0.29 µs** |
| 报价直接写进目标缓冲（省掉 Marshal） | 0.29 µs（噪声内，没有可测得收益） |

要到 0.07 µs 只能绕开 cgo——Go 没有这条路。

## 1b. 两条「能不能赢」的实测下界（本轮）

在写更多代码之前，先测「这个架构的物理下界」——如果下界就赢不了，写出来也没用。

| 架构 | 每次调用下界 | 与 73 ns 比 | 结论 |
| --- | ---: | --- | --- |
| 共享内存 ring（**JS↔JS 同进程**，最好的情况：无 cgo、无系统调用） | 182 ns | 2.5 倍慢 | **ring 赢不了**，不用写 |
| **进程内 wasm**（`GOOS=wasip1` + `//go:wasmexport`，一次 int32 调用） | **8.2 ns** | **快 8.9 倍** | 有足够余量，值得做 |
| cgo + napi（当前单次调用） | 215 ns | 2.9 倍慢 | 结构性落后 |

复现命令：

```sh
node scripts/floor/ring-floor.mjs          # JS 工作线程 + SharedArrayBuffer 往返
(cd scripts/floor/wasmprobe && GOOS=wasip1 GOARCH=wasm go build -o ../probe.wasm .)
node scripts/floor/wasm-floor.mjs          # 进程内 wasm 调用
```

**为什么这两行值钱**：`ring` 那行把一个「听起来很对」的方案在写代码之前否掉了——JS 两个线程通过共享内存
来回一次就要 182 ns，比 napi 慢一倍多，跨进程只会更糟。`wasm` 那行指出的是一条真能赢的路：
把端口编成 wasm，在同一个进程里直接调用，函数调用本身只要 8 ns。

**更正：我上一轮把 wasip1 判死刑是错的。**

真正的失败原因不是「导出函数跑在固定栈上」，而是**构建模式**：不带 `-buildmode=c-shared` 时，wasip1 产物
是「命令模块」，它靠 `_start` 初始化，任何 `//go:wasmexport` 在初始化前调用都会 abort（`notInitialized`）。
加上 `-buildmode=c-shared` 得到的是 **reactor 模块**，真实的 Go 代码——字符串、`utf16.Encode`、分配——全部可用。

**修正后的架构与实测（本轮把「类型化直调」也做出来了）：**

| 架构 | 每次调用 | 对比 rust-anything 的 72 ns |
| --- | ---: | --- |
| 共享内存 ring（JS↔JS 最好情况） | 182–210 ns | 慢 2.5 倍，弃 |
| wasip1 reactor + 叶子端口 | 18.2 ns | 快 4 倍（纯字节循环） |
| **wasip1 reactor + 类型化直调（本工具现在）** | **51–55 ns** | **快 24%** ✅ |
| wasip1 reactor + JSON 信封（上一版） | 750–780 ns | 慢 10 倍 |
| cgo + N-API（默认口径） | 215–250 ns | 慢 2.9 倍 |
| js/wasm 回调 | 10,260 ns | 慢 140 倍，弃 |

三次连续测量：0.053 / 0.053 / 0.051 µs；同一进程里 rust-anything 的 napi 是 0.072 µs。

**它是怎么降下来的**：三条改动，每一步都独立测过——

1. `-buildmode=c-shared`（reactor 而不是命令模块）：真实 Go 代码才能在导出里跑；
2. 按签名生成直调导出 `go_Count(ptr, len) float64`：去掉注册表查找与 JSON；
3. **端口的签名本身改成类型化**（`func Count(text string) (float64, error)`，由生成的适配器接到注册表）：
   去掉 `[]any{...}` 装箱与 `any` 类型断言——这一步把 194 ns 压到 53 ns。

209 条录制用例经 wasm 回放全部一致（`check` 走 N-API 也是 609/609）。

复现脚本：`scripts/floor/ring-floor.mjs`、`scripts/floor/wasm-floor.mjs`；
真实端口：`go-anything wasm <crate>` → `node testdata/sample/port-rs/index.wasm.mjs`。

## 1c. 启动：把 cgo 挪出快路径

### 0.1 ms 不是噪声，也不是能靠调参数消掉的东西

20 轮 × 40 次 spawn、交替分块、bootstrap 2000 次：

```
go-anything   median 2.194 ms
rust-anything median 2.066 ms
difference    +0.129 ms (+6.2%)   95% CI [+0.100, +0.149]   ← 真实存在
```

把每一层拆开（每次都同法测量）：

| 变体 | 启动 | 相对 `/usr/bin/true` |
| --- | ---: | ---: |
| `/usr/bin/true` | 1.26 ms | — |
| Go 空程序（1.1 MB，无 cgo） | 1.96 ms | **+0.70 ms（Go 运行时）** |
| 只 import 本工具包的探针（1.6 MB） | 2.28 ms | +1.02 ms |
| go-anything（3.5 MB，PIE） | 2.18 ms | +0.92 ms |
| go-anything（3.5 MB，`-buildmode=exe`） | 2.09 ms | +0.83 ms |
| rust-anything（26 MB） | 2.00–2.08 ms | +0.75 ms |

结论：**差距全部落在 Go 运行时那 0.7 ms 里**，二进制大小不是主因（1.6 MB 的探针比 3.5 MB 的还慢）。
试过但没用：`TZ` 固定、`GOMAXPROCS=1`、`-s -w`（省 0.08 ms）、cgo 剥离（省 1.2 ms，已采用）。

**`-buildmode=exe` 能再省 0.04 ms，但那是拿 ASLR 换的**，不采用——安全属性的倒退不能用 0.04 ms 来定价。

所以这一轴的诚实结论：**冷启动 `--help` 我们落后约 0.1 ms（5%），这是 Go 运行时 vs 无运行时的差别**，
任何调参都消不掉；而任何真正干活的命令（`analyze` 5.0 倍、`diff` 4.0 倍、`record` 1.65 倍）我们都领先。

**唯一能让这一行也赢的做法，以及为什么没做**：把发布拆成「极小的分发器 + 引擎」两个二进制，
分发器只认 `--help`/`version`（约 1.96 ms，能赢 2.03 ms 的 rust-anything），其他命令 exec 引擎。
代价是**每条真命令多一次进程启动（+2 ms）**：`diff` 会从 8.8 ms 变成 10.8 ms（慢 23%），
`gate` 同理。这是拿真实命令换一个指标，需要人来决定，不能由我默默做掉。

## 1d. 启动：把 cgo 挪出快路径（cgo 的 1.2 ms）

一台机器上先量出「Go 空程序」的下界：**2.0 ms**（1.1 MB，无 cgo）。然后把每一层加回去：

| 变体 | 启动 | 说明 |
| --- | ---: | --- |
| `/usr/bin/true` | 1.26 ms | 进程创建下界 |
| Go 空程序（无 cgo） | 2.00 ms | Go 运行时下界 |
| **go-anything（无 cgo 构建）** | **2.42 ms** | 只比空程序多 0.4 ms |
| go-anything（cgo 构建） | 3.24–3.28 ms | cgo 固定 +1.2 ms |
| rust-anything | 2.26–2.35 ms | 对照 |

**cgo 的 1.2 ms 是固定成本**：一个只 `import "C"`、`main` 里 `C.malloc(1)` 的最小程序就要 3.19 ms
（空程序 2.0 ms）。所以做法不是优化，而是**把 cgo 挪出快路径**：

- 工具本体用 `CGO_ENABLED=0` 构建 → 2.42 ms
- 解析器（tree-sitter，必须 cgo）编成**同一个程序的第二个构建**（`go-anything-cgo`），
  只在需要 parse 的命令里以 `go-anything __parse` 子进程启动，JSON 行协议
- 两条构建共享全部源码，没有需要同步的 sidecar

代价：`analyze` 6.92 ms vs 4.09 ms（多一次进程启动 + 0.55 MB 交接）。`--help`、`gate`、`diff`、
`check`、`app replay` 这些不 parse 的命令全部拿到 2.42 ms。

**但在真实仓库上量，结论反过来。** 同一个仓库（`rea`：7,390 个 `.ts` + 3,839 个 `.js`）：

| 命令 | go-anything | rust-anything | 结果 |
| --- | ---: | ---: | --- |
| `analyze rea` | **8.46 s** | 42.00 s | **快 5.0 倍** |

小样本（2,000 个 `.ts`）：0.044 s vs 0.201 s，**快 4.6 倍**。

所以启动这一轴按命令分开说：

- `--help`、`gate`、`diff`、`check` 这类**不解析**的命令：2.42 ms vs 2.35 ms，持平；
- `analyze` 在**小样本**上：6.9 ms vs 4.1 ms，我们慢（多一次进程启动）；
- `analyze` 在**真实仓库**上：8.5 s vs 42.0 s，**我们快 5 倍**——进程启动的 3 ms 在真实工作量前不值一提。

## 2. record 吞吐（同一个单元，各自生成的 crate）

| 实现 | 用例数 | 中位耗时 | 吞吐 |
| --- | ---: | ---: | ---: |
| rust-anything | 917 | 153.7 ms | 5,966 cases/s |
| **go-anything** | 609 | 60.8 ms | **10,016 cases/s** |

**1.68 倍**。差异来源：我们按声明类型生成，不做笛卡尔积（609 vs 917 条，且没有「数字喂给字符串参数」的噪声）；
两侧都要跑一次 Node 录原式，所以这个比值主要反映生成与写盘。

## 3. 工具启动与 analyze（同一台机器，中位）

| 命令 | rust-anything | go-anything |
| --- | ---: | ---: |
| `--help` | 16.9 ms | **17.5 ms** |
| `analyze <repo>` | 25.4 ms | **24.8 ms** |

基本持平（±3%），都在 20 ms 级：两边都是原生二进制 + 一棵语法树。

## 4. diff 吞吐（本工具，609 条用例）

| 命令 | 耗时 | 吞吐 |
| --- | ---: | ---: |
| `go-anything diff` | 60.7 ms | 10,032 cases/s |

rust-anything 的 diff 量纲不同（它的 crate 是 Rust、用例 917 条），未做直接对比——把不同工作量的数字
放在一张表里比较是不诚实的。

## 结论

| 指标 | 结果 |
| --- | --- |
| 命令覆盖 | **26 / 26** ✅ |
| record 吞吐 | **1.68x 更快** ✅ |
| 启动 / analyze | 持平（噪声内） |
| 桥接单次调用 | **慢约 4 倍** ❌（cgo 下限 + napi 字符串开销） |

要让桥接这一项真正超过 rust-anything，只有两条路：

1. **批量接口**：一次跨语言调用处理 N 个请求（把 0.29 µs 摊到 0.29/N 加少量解析），对「一次调用算一批」的场景有效；
2. **换桥的形态**：把热点函数编进一个长期存活的 Go 进程，通过共享内存 ring 交换定长消息（跨进程，绕开 cgo 每次调用的固定成本）。

这两条都还没做，所以目标**未完成**——桥接这一轴我们目前落后，且原因已经量化。
