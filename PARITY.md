# 与 rust-anything 的能力对照

目标：**全部 26 条命令对齐**，并在性能上超过它（桥接单次调用、diff/record 吞吐、工具启动）。

**当前：31 行——25 行对应上游命令（26 个上游命令：`setup` 与 `mcp` 共用一行），6 行自创（`check` `fast` `promote` `scaffold` `shadow-report` `wasm`）。**

这个数字由 `scripts/doccheck.sh` 算出来并核对，不是记着的：它取 `--help` 的命令集合减去 PARITY 左列的集合。
性能五项里四项赢、一项（`--help` 冷启动）慢 0.1 ms，原因与实测见 [PERF.md](PERF.md) 的「启动的下限」一节。

图例：`✅` 已实现并测过 · `🟡` 部分（说明差在哪） · `⬜` 未做

| # | rust-anything | golang-anything | 状态 | 说明 |
| --- | --- | --- | --- | --- |
| 1 | `analyze` | `analyze` | ✅ | tree-sitter 解析 TS/TSX/JS：导出、参数与返回类型、`throw` 行、导入、风险；波次、readiness、priority、start_here 用同一套公式 |
| 2 | `bridge` | `bridge` | ✅ | 生成 `index.cjs`、`port.py`、stdio 桥 |
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
| 15 | `shadow` | `shadow` | ✅ | 生成 `shadow.cjs`：同一调用跑原式与端口，比对后**返回原式的答案**，差异写 `shadow.jsonl` |
| 16 | `gate` | `gate` | ✅ | 每例匹配 + 每个 `throw` 必须有录到的失败用例 + 导出不缺 + 不许 panic |
| 17 | `package` | `package` | ✅ | 三种形态（library / cli / service）：npm 主包 + 每平台子包 + Go module + Dockerfile + GitHub Actions；重跑只替换上次写的文件 |
| 18 | `app record` | `app record` | ✅ | 黑盒跑整程序（argv/stdin/env 进，stdout/stderr/exit 出），每例跑两次以识别不稳定输出 |
| 19 | `app replay` | `app replay` | ✅ | 任意构建回放；自动把程序路径替换成 `<prog>`，所以换路径不等于是行为变化 |
| 20 | `app front-door` | `front-door new` | ✅ | 生成接管入口的 Go 二进制：录制到的命令按 argv 分发，接管的走 Go，其余原样转给原程序（可 `GOLANG_ANYTHING_TRACE` 计数） |
| 21 | `app wire` | `front-door wire` | ✅ | 把某条录制命令接到端口的某个函数；未录制的命令拒绝（没有证据） |
| 22 | `app lift` | `front-door lift` | ✅ | 全部命令接管后才允许：删掉 fallback 分支与其 import，变成纯 Go 应用 |
| 23 | `setup` / `mcp` | `setup` + `mcp` | ✅ | `mcp` 是 JSON-RPC over stdio：**15 个工具**——CLI 已经实现的命令全部委托过去（`check`/`doctor`/`bench`/`shadow-report`/`enable`/`disable`/`clean`/`refresh` 通过捕获命令输出来回答，不写第二份实现），加上原本就有的 7 个；`setup` 把服务器注册进已装 agent。用官方 MCP SDK 客户端核对过（`scripts/mcp-client.mjs`） 的配置，只加自己的键 |
| 24 | `map` | `map` | ✅ | 写 `PORT-MAP.md`：每个单元、它的 crate、用例数、上次 diff、是否已构建 |
| 25 | `doctor` | `doctor` | ✅ | 八项：go、node、python3、`clang`（`bench` 计时 C/C++ 单元要编译它）、Node 是否自带类型剥离、**能否真的编译一个程序**、**解析器在哪**（问 `ts.ParserPath` 而不是自己再搜一遍）、cgo；每项给修复命令 |
| 26 | — | `check` | ✅ 额外 | 用真正被调用的那条桥（addon 或 stdio）回放全部用例，**每个用例跑两遍**：一遍传录制里的标记、一遍传解码后的值——只有第二遍会走到包装器的 `encode` |
| 27 | — | `fast` | ✅ 额外 | 只构建进程内桥 |
| 28 | — | `promote` | ✅ 额外 | 给**不碰状态**的实例方法写一条能拿到实例的入口，让它们进得了 plan：Ruby 就地重开类（`def self.x(*args)` → `allocate.x(*args)`，跳过 `initialize`）；PHP 不能重开类，写 `promoted.php` 代理类（带方法自己的类型、`require_once` 原文件），`analyze` 把它当独立单元读；Java `MathsPromoted.java`、Kotlin `object TimerPromoted`、C# `MathsPromoted.cs` 同为静态/单例代理（`new Owner().name(args)`，需类无字段、有无参构造），分别用 `javac`/`kotlinc`/`dotnet build` 校验。`--revert` 按标记删除、文件**字节一致**，PHP 只剩代理时整文件删除。module 的方法不提供——module 不能实例化；读 `$this`/`@ivar` 的方法也不提供 |
| 29 | — | `scaffold` | ✅ 额外 | 生成 `port.go` 骨架、驱动与清单；`--force` 覆盖已填的实现 |
| 30 | — | `shadow-report` | ✅ 额外 | 汇总 `shadow.jsonl`：调用数、按函数的不一致、第一次出现的位置 |
| 31 | — | `wasm` | ✅ 额外 | 生成 wasm 包装（`index.wasm.mjs`）：类型化直调，比 JSON 桥快；不能承载时明确拒绝 |

**当前：31 行全部 ✅（含 6 条自创）。**

## 性能（本机 macOS arm64，Node 22.22，Go 1.26，rust-anything 4.0.0 本地编出）

| 指标 | rust-anything | golang-anything | 结果 |
| --- | ---: | ---: | --- |
| 桥接单次（wasm 直调） | 0.070 µs | **0.051 µs** | **快 27%** |
| 桥接单次（N-API 口径） | 0.070 µs | 0.201 µs | 慢 2.9 倍（cgo ABI 下限） |
| diff 吞吐 | 17,475 cases/s | **69,219 cases/s** | **快 4.0 倍** |
| record 一个单元（墙钟） | 142 ms | **66 ms** | **快 2.1 倍** |
| `analyze` 真实仓库（7,390 个 .ts） | 42.0 s | **8.46 s** | **快 5.0 倍** |
| 启动 `--help` | 2.03–2.21 ms | 2.14–2.28 ms | 点估计慢 0.07–0.11 ms，95% CI 跨零（**打平**） |

逐项方法与负向对照在 [PERF.md](PERF.md)。一条命令复现：`GOLANG_ANYTHING_PARSE=bin/golang-anything-cgo ./scripts/perf.sh`。

### 启动这一轴：测量上打平，点估计略慢

三次独立采样，后两次的 bootstrap 95% CI 都跨过零（噪声底约 0.2 ms）。一个只打印 3 KB 静态串的
Go 程序启动 1.93 ms，比 rust-anything 还快——所以差距不在语言，在本工具引的 133 个包（`inittrace`
合计 0.156 ms，几乎全是 stdlib）。确定性赢它需要拆「分发器 + 引擎」，代价是每条真命令 +2 ms。

## 语言：目前两种，rust-anything 十一种

这一节是本文件里唯一一处**明确落后**的地方，写清楚比含糊过去好。

| 语言 | rust-anything | golang-anything |
| --- | --- | --- |
| TypeScript / JavaScript | ✅ | ✅ 完整（analyze → scaffold → record → gate → check → bench → fuzz → shadow → enable） |
| **Python** | ✅ | 🟡 analyze / scaffold / record / gate / diff / check / bench / report / map ✅；`fuzz`、`shadow` 还没有 Python 驱动（会明说原因） |
| **Ruby** | ✅ | 🟡 同上（record 用 `ruby`，bench 用同一份用例） |
| **Java** | ✅ | 🟡 analyze / record / gate / check / bench ✅（record 生成 `Recorder.java`，`javac` 编一次） |
| **C** | ✅ | 🟡 analyze / record / gate / check / bench ✅（unity build：把原文件 include 进生成的 harness） |
| **C++** | ✅ | 🟡 同上，`clang++ -std=c++17` |
| **PHP** | ✅ | 🟡 analyze / scaffold / record / gate / check ✅（record 用 `php`，值以 PHP 字面量生成） |
| **Kotlin** | ✅ | 🟡 analyze / record / gate / check ✅（record 生成 `Recorder.kt`，`kotlinc` 编一次） |
| **C#** | ✅ | 🟡 analyze / record / gate / check ✅（`dotnet build` 一个生成的 csproj） |
| **PowerShell** | ✅ | 🟡 analyze / record / gate / check ✅（record 用 `pwsh` 的 splatting） |

### 每种语言的实测（本机，`make smoke` 每次都会重跑）

| 语言 | 用例数 | gate | check | bench（原式 → Go） |
| --- | ---: | --- | --- | --- |
| TypeScript（port-rs 样例） | 328 | ✅ | 656/656 | 1.5–3.5x 更快 |
| Python（mathx.py） | 116 | ✅ | 181/181 | 1.31x 更快 |
| Ruby（strings.rb） | 114 | ✅ | 170/170 | **13.3x 更快** |
| Java（MathX.java） | 96 | ✅ | 185/185 | 2.21x 更快 |
| C++（mathx.cpp） | 96 | ✅ | 185/185 | — |
| C（strs.c） | 91 | ✅ | 182/182 | 6.5x 更慢（2 ns 的原式赢不了跨界成本） |
| PHP（strings.php） | 82 | ✅ | 164/164 | — |
| Kotlin（Strings.kt） | 212 | ✅ | 368/368 | — |
| C#（Strings.cs） | 212 | ✅ | 424/424 | — |
| PowerShell（Strings.ps1） | 28 | ✅ | 50/50 | — |

| PHP（strings.php） | 82 | ✅ | 139/139 | 2.41x 更快 |
| PHP static methods（Carbon 真实源码） | 506 导出 / 199 可移植 | — | — | 307 个被拒：`touches state outside its arguments` |
| Ruby module functions（真实源码） | 55 导出 / 47 可移植 | — | — | 8 个被拒：碰实例变量 |
| Kotlin（Strings.kt） | 212 | ✅ | 368/368 | 7.11x 更快 |
| C#（Strings.cs） | 212 | ✅ | 368/368 | 9.91x 更快 |
| PowerShell（Strings.ps1） | 28 | ✅ | 50/50 | 779x 更快 |

### 每种语言现在支持哪些命令

| 命令 | TS/JS | Python | Ruby | Java | C | C++ | PHP | Kotlin | C# | PowerShell |
| --- | :-: | :-: | :-: | :-: | :-: | :-: | :-: | :-: | :-: | :-: |
| analyze / map / scaffold | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| record / diff / gate / check | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| bench | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| report / package | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| fuzz | ✅ | — | ✅ | — | — | — | — | — | — | — |
| shadow / shadow-report | ✅ | — | — | — | — | — | — | — | — | — |
| enable / disable | ✅ | ✅ | ✅ | — | — | — | — | — | — | — |

`fuzz` 与 `shadow` 只对 TS/JS 有：它们要把**原式**在 node 里跑起来并注入包装，那是 JavaScript 的机制。
`enable` 现在覆盖 TS/JS、Python、**Ruby**：Ruby 可以重开模块/类并重定义方法，块放在文件末尾，`disable`
按字节移除。实测（`make smoke` 里每次重跑）：

```
$ golang-anything enable go-ports/u-go && ruby -I. -e 'require "util"; puts Util.double(21)'
42
$ golang-anything disable go-ports/u-go && cmp util.rb util.before   # 字节一致
```

PHP 的 `enable` 仍缺：PHP 不能重定义已声明的函数，接管必须改类体内部，那不是「末尾插块」能做的。
缺的那些**会明确拒绝并说明**，不是 node 的报错。

一条命令验证全部十种语言的 bench：`./scripts/bench-all-languages.sh`。

一条命令看出差别：

```
$ golang-anything analyze /path/to/java-repo
# Port plan: java-repo (nothing this tool reads)

812 source file(s) in languages this tool does not read: .java ×812
  it reads TypeScript, TSX, JavaScript and Python.
```

Python 的实测（`make smoke` 里也有）：`analyze` 认得出、`record` 用 python3 跑原式、`gate` 116/116 通过，
而且它抓到了我写 Go 端口时想当然的两处 Python 语义——`list + list` 是拼接、`bool` 是 `int`
（`False/False` 抛 `ZeroDivisionError`）。

**为什么只做了两种语言**：每种语言的成本不在解析（tree-sitter 有现成语法），而在**录制器**——要能用*那种语言的
运行时*把原式跑起来并抓答案（Python 是 `python3`，Java 要 JVM 载体，C++ 要编译一次载体）。之后的驱动、桥、
`gate`/`diff`/`check` 全部与语言无关，所以第二种语言验证了架构能扩展；剩下的七种是同一套管道再写七遍。
