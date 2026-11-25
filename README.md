# go-anything

把代码移植到 Go 的完整流水线：**analyze → record → scaffold → diff → gate → bridge/build**，
一个单元一个单元地过，每一步都有可复现的证据。

```sh
export GOROOT=$(brew --prefix go)/libexec   # 本机 goenv 与 brew 的 GOROOT 曾不一致
CGO_ENABLED=1 go build -o go-anything .

./go-anything analyze testdata/sample
./go-anything record  --unit src/text.ts testdata/sample/port-rs
./go-anything scaffold --unit src/text.ts --crate <module>/testdata/sample/port-rs testdata/sample/port-rs
./go-anything gate    testdata/sample/port-rs
./go-anything bridge  testdata/sample/port-rs && ./go-anything build testdata/sample/port-rs
```

## 端口真的被用起来：enable / shadow

```sh
go-anything enable testdata/sample/port-rs     # 原式末尾插入一段标记块
# 现在 import 来的 count 走 Go：count("a b  c") === 3，String(count) 里是 callSync 包装
go-anything disable testdata/sample/port-rs    # 移除该块，文件回到字节一致
```

```
$ node -e 'import("./src/text.ts").then(m => console.log(m.count("a b  c")))'
3                                     # 已切到 Go（sync 模式，签名不变）
$ go-anything disable port-rs && cmp src/text.ts 原件   # 字节一致 ✅
```

`shadow` 是切换前的保险：同一调用跑原式与端口，**返回原式的答案**，差异写进 `shadow.jsonl`。

```
$ go-anything shadow testdata/sample/port-rs
$ node -e 'require("./port-rs/shadow.cjs").count("pad  ")'
go-anything shadow: count differs (the answers differ)
2                                     # 调用方拿到的仍是原式的答案
$ go-anything shadow-report testdata/sample/port-rs
1 mismatch(es) recorded: count ×1
| `count` | `["pad  "]` | `2` | `1` | the answers differ |
```

（上面这组是真发生的：我把端口改成 `i+1 < len(units)` 制造 off-by-one，shadow 立刻抓到。）

## fuzz：新种子找录不到的分歧

```sh
$ go-anything fuzz --count 25 --seed 3 testdata/sample/port-rs
100/100 fresh calls match (seed 3).

# 端口被我改成 off-by-one（末尾空格少算）之后：
$ go-anything fuzz --count 30 --seed 7 testdata/sample/port-rs
116/120 fresh calls match (seed 7).
- `count("_	$ 😀 ")`: expected 2, got 1 (the answer differs)
$ go-anything fuzz --count 30 --seed 7 --keep testdata/sample/port-rs
kept 4 failing call(s) in cases.jsonl: `diff` and `gate` now check them.
$ go-anything gate testdata/sample/port-rs
go-anything: gate: 7 of 640 cases do not match      ← 从此这条分歧是常驻用例
```

`--seed` 固定就能重放同一次运行；`--keep` 把失败调用变成正式用例。

## report：给 PR 和 README 的结果

```sh
$ go-anything report testdata/sample/port-rs
## `src/text.ts` ported to Go
| Recorded cases matched | 609 / 609 (100.0%) |
| Speedup, call included | 1.91x (geometric mean) |
| Throw paths with a recorded failing case | 1 of 1 |
| Bridge | in-process addon |
| Measured | 2026-11-25 20:56 on darwin/arm64, 10 CPUs |
```

同时写 `card.svg`（840×240，README 可嵌）；用例不全会自动换成另一种主色，不会假装通过。

## 无扩展名 import 的单元也能录

真实源码写 `import { pad } from './util'`，Node 的 ESM 解析器要扩展名。`record` 现在写一个加载钩子
（`work/resolve.mjs`）依次试 `.ts`/`.tsx`/`.js`/`.mjs`/`.cjs`，所以这样的单元照样能录：

```sh
$ go-anything record --unit src/format.ts testdata/waves/rust/format-go
Recorded 7 cases for 1 functions (74d57a3e64c9d0aa) from src/format.ts
$ go-anything gate testdata/waves/rust/format-go
gate: every case matches, every export is in the port, and every throw path has a failing case.
```

## app：整程序黑盒录制与回放

```sh
$ go-anything app record --command /tmp/textcli --inputs cases.jsonl /tmp/appcli
6/6 cases replay identically in 0.12s.
Wrote 6 case(s) to /tmp/appcli/go-anything-cases

$ go-anything app replay --command /tmp/textcli /tmp/appcli
6/6 cases replay identically in 0.01s.

# 换了版本的构建：
$ go-anything app replay --command /tmp/textcli2 /tmp/appcli
5/6 cases replay identically; 1 differ:
- --version: stdout differs
```

每个用例跑两次：输出在两次之间不同（时间戳、pid）就标成 `noisy`，回放只比对退出码并说明原因。
程序路径会规范成 `<prog>`——否则换个构建路径就会被误判成行为变化（这条是实测撞出来的）。

## package：让别人能装上

```sh
$ go-anything package --name port-rs --version 1.2.3 <crate>
port-rs 1.2.3 (library) → <crate>/package
  .github/workflows/release.yml
  README.md
  go.mod
  index.cjs
  package.json
```

三种形态：`library`（npm 主包 + 每平台子包 + `go.mod`）、`cli`（每平台二进制 + 回落到原包）、
`service`（Dockerfile）。工作流**不会替你发布**；换形态重跑会删掉上次写、这次不写的文件（有测试）。

## profile / go：按真实负载决定先移植什么

```
$ go-anything profile --command workload.mjs --top 5 testdata/sample
| Function | Where | Share | Unit | Port? |
| `words` | `src/text.ts:8` | 15.7% | `src/text.ts` | yes |
| `(top level)` | `workload.mjs:1` | 13.7% | — | not part of a unit |
| `count` | `src/text.ts:14` | 2.0% | `src/text.ts` | yes |

$ go-anything go --command workload.mjs .
Hottest portable unit: `src/text.ts`
Scaffolded 4 file(s), recorded 609 cases, wrote 4 wrapper file(s) for `src/text.ts`.
Next: fill in go-ports/text-go/port.go, then `go-anything gate go-ports/text-go`.
```

`profile` 按**自身耗时**排（不是总耗时），剔除 runtime 帧与 `node_modules`；`go` 把
profile → 选单元 → scaffold → record → 包装器串成一条。刚生成的那个 crate 直接 `gate` 通过 609/609、
`build --fast` 后 `check` 609/609 经进程内 addon——这条链在本轮实测过。

## mcp / setup：给 agent 用

```sh
$ printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
    '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' | go-anything mcp
initialize → {'name': 'go-anything', 'version': '0.5.0'}
tools → ['analyze', 'map', 'diff', 'gate', 'record', 'fuzz', 'profile']

$ go-anything setup --print          # 只打印，不写
$ go-anything setup                  # 注册进已安装 agent 的配置
Registered go-anything with claude (~/.claude.json)
```

协议行为都按规范：通知不回包（客户端按位置读流）、未知方法回 `-32601`、非 JSON 回 `-32700`、
**工具自己出错**回 `isError` 内容而不是协议错误（agent 能读到原因）。`setup` 只增删自己的键，
重复执行幂等。

## front-door / wire / lift：让 Go 接管入口

```sh
$ go-anything front-door new --name textcli <repo>      # 6 条录制命令
$ go-anything front-door wire --command "--count a b  c" --call count go-app
$ cd go-app && go build -o frontdoor .

$ GO_ANYTHING_TRACE=/tmp/t.txt ./frontdoor "--count a b  c"
3                                    # 走 Go
$ GO_ANYTHING_TRACE=/tmp/t.txt ./frontdoor --version
textcli 0.3.1                        # 转给原程序
{"command":"--count a b  c","fallback":false}
{"command":"--version","fallback":true}

$ go-anything front-door lift go-app
go-anything: 1 of 6 commands are wired: wire the rest, or keep the fallback   ← 不许提前收工
# 全部接管后：
$ go-anything front-door lift go-app && cd go-app && go build -o frontdoor .
$ ./frontdoor --version
{"args":[],"port":"echo"}            # 纯 Go，原程序已不在
```

`lift` 会连 `os/exec` 的 import 一起删掉——否则编不过（这条是实测撞出来的）。

## 与 rust-anything 的能力对齐

见 [PARITY.md](PARITY.md)：26 条命令的逐项状态。**26 条全部对齐**（`analyze` `scaffold/bridge` `record` `build` `diff` `bench` `clean` `gate` `map` `doctor` `enable` `disable` `shadow` `fuzz` `refresh` `report` `package` `app record` `app replay` `app front-door` `app wire` `app lift` `profile` `go` `setup` `mcp`），外加三条自创的 `check` `fast` `shadow-report`。

## 性能：实测对照（详见 [PERF.md](PERF.md)）

| 指标 | rust-anything | go-anything | 结果 |
| --- | ---: | ---: | --- |
| 桥接单次调用（wasm 直调） | 0.070 µs | **0.051 µs** | **快 27%** ✅ |
| 桥接单次调用（N-API 默认口径） | 0.070 µs | 0.201 µs | 慢 2.9 倍（cgo 下限） |
| 桥接批量每次成本 | 0.070 µs（无批量接口） | **0.051 µs** | **快 27%** |
| record 吞吐 | 6,548 cases/s | **10,781 cases/s** | **快 65%** ✅ |
| diff 吞吐 | 17,475 cases/s | **69,219 cases/s** | **快 4.0 倍** ✅ |
| 启动 `--help` | 2.00–2.08 ms | 2.09–2.19 ms | **慢 0.1 ms**（95% CI [+0.06,+0.15]，Go 运行时下限） |
| `analyze` 小样本 | 4.09 ms | 6.92 ms | 慢 1.7 倍（要起解析子进程） |
| **`analyze` 真实仓库**（7,390 .ts） | 42.0 s | **8.46 s** | **快 5.0 倍** ✅ |
两种实现都是各自工具生成的桥，同一台机器、同一个单元、同一个函数、交替分块取中位，两边都 `rc=0`。
**wasm 这一路赢，cgo addon 这一路输**——差距是 ABI 决定的：Rust 在同进程内直接调用，
Go 走 cgo 每次要付 40–60 ns 再加两次 napi 取串。

四步优化都实测过：JSON 双向 1.4–2.0 µs → 分开传参 0.73 µs → 单字符串入口 0.21–0.23 µs →
**类型化 wasm 直调 0.051 µs**。

## 两个构建：快的那个不带 cgo

```sh
./scripts/build.sh bin      # bin/go-anything（无 cgo）+ bin/go-anything-cgo（解析器）
```

cgo 有 1.2 ms 的固定启动成本（一个只 `import "C"` 的最小程序就要 3.19 ms，空程序 2.0 ms）。
所以工具本体不带 cgo（2.42 ms 启动），解析器是**同一个程序的 cgo 构建**，只在需要 parse 的命令里
以 `go-anything __parse` 子进程启动，JSON 行协议交接。`--help`/`gate`/`diff`/`check` 全部走快路径。

## 三条架构的下界（实测，见 PERF.md）

| 方案 | 每次调用下界 |
| --- | ---: |
| rust-anything napi（对照） | 73 ns |
| 共享内存 ring（JS↔JS 最好情况） | 182 ns → **赢不了，没写** |
| **wasip1 reactor（`-buildmode=c-shared`）** | **18 ns 叶子 / 121 ns 带分配 / 780 ns 带 JSON 信封** |
| js/wasm 回调（Go 调度器） | 10,260 ns → 慢 140 倍，弃 |
| cgo + napi（现在） | 215 ns |

复现：`node scripts/floor/ring-floor.mjs`、`node scripts/floor/wasm-floor.mjs`。
真实端口：`go-anything wasm <crate>` 生成 `port.wasm` + `index.wasm.mjs`，Node 里直接调用。

## 批量接口：`callNumbers`

```js
const port = require('./rust/text-go/index.cjs')
port.callNumbers('count', ['a b  c', 'pad  ', '', 'x y z'])   // Float64Array [3, 2, 0, 2]
```

参数打包成一个 UTF-8 blob + 长度表，答案直接写进 `Float64Array`：不逐条 JSON、不逐条建字符串。
出错的那条是 `NaN`（数字返回值里没有更好的表示），调用方自己判断。

## 旧记录：进程内桥 0.73 µs/次

| 路径 | 每次调用 | 每秒 |
| --- | ---: | ---: |
| 原式（TS，被 V8 内联） | 0.05 µs | 20,500,000 |
| **go-anything 进程内（c-shared + N-API）** | **0.73 µs** | **1,360,000** |
| go-anything stdio | 12.02 µs | 83,000 |
| rust-anything 宣称（napi-rs） | ~1 µs | ~1,000,000 |

进程内桥比 rust-anything 宣称的快约 27%；三口是同一台机器、同一个函数 `count("a b  c")`、每组 2 万次。

`bench` 逐函数对比（60 ms/侧）：

```
| `count`   |  20 | 27 ns |  45 ns | 1.68x slower |
| `rangeOf` | 400 | 13 ns | 196 ns | 15.28x slower |
| `ratio`   | 196 | 595 ns |   8 ns | 78.09x faster |
| `words`   |  20 | 71 ns |  40 ns | 1.76x faster |
Geometric mean: 1.52x faster
```

微函数（`count`/`rangeOf`）原式更快——V8 把它们内联成几条指令，而每次跨语言调用都要分配几个值；带异常路径的 `ratio` 端口快 78 倍。平均值领先，短板也照实写。

## 真跑出来的输出

```
$ ./go-anything analyze testdata/sample
| Unit | Lines | Exports | Wave | Readiness | Priority |
| `src/text.ts` | 25 | 4 | 0 | 86 | 70 |
Start here: src/text.ts

## Exports in `src/text.ts`
- `rangeOf(text: string, word: string): any`
- `words(text: string): string[]`
- `count(text: string): number`
- `ratio(part: number, whole: number): number` — 1 throw path(s) at 23

$ ./go-anything record --unit src/text.ts testdata/sample/port-rs
Recorded 636 cases for 4 functions (13d623b7382de4d1) from src/text.ts; 1 throw paths need a failing case each.

$ ./go-anything gate testdata/sample/port-rs
636/636 recorded cases match.
gate: every case matches, every export is in the port, and every throw path has a failing case.
```

跨语言调用真的通：

```
$ node -e 'const p=require("./testdata/sample/port-rs/index.cjs"); p.count("a b  c").then(console.log)'
3
$ python3 -c 'import sys; sys.path.insert(0,"testdata/sample/port-rs"); import port; print(port.count("a b  c"))'
3
```

## 四个负向对照（每一条都实测会红）

| 破坏 | 工具的反应 |
| --- | --- |
| `count` 遇到第一个空格就 return | `308/312 match`，逐例指出 `count("a b  c"): expected 3, got 1` |
| 删掉 `ratio` 的错误用例 | 用例全绿，`gate: no recorded failing case for ratio (1 throw path(s))` |
| 端口少一个导出 | `gate: 20 of 636 cases do not match; no port for count` |
| 移植时按 rune 切 emoji | `rangeOf("😀😀","a")` 期望 `$utf16:[56832]`，得到 `"�"` |

## 六条命令在做什么

| 命令 | 实现要点 |
| --- | --- |
| `analyze` | tree-sitter（TS/TSX/JS）解析出导出、参数与返回类型、`throw` 行、导入；按导入排波次；readiness/priority 与 rust-anything 同一套公式 |
| `record` | 从**声明的类型**生成边界值（字符串含 emoji/代理对/组合字符，数字含 `-0`/`1/3`），可选 `--inputs` 回放真实调用，可选 `--test-coverage` 跑原式测试看哪些导出没人碰 |
| `scaffold` | 写 `port.go` 骨架（带 TS 签名注释）、`wire.go`（编解码）、`cmd/godriver`、`go-anything.json` |
| `diff` | 按**调用**而非行号配对；键序无关、浮点容差、错误**类型**必须一致、Go panic 可对应原式的异常 |
| `gate` | 每条 `throw` 必须有录到的失败用例；导出不能缺；出现 panic 即失败 |
| `bridge`/`build` | 长驻 stdio 进程 + `index.cjs`/`port.py` 薄包装，每次调用约几十微秒 |

## 与 rust-anything 的差异（设计上最要紧的部分）

| 环节 | rust-anything | go-anything |
| --- | --- | --- |
| 解析 | 每种语言一棵 tree-sitter 树 | TS/TSX/JS 用 tree-sitter（`smacker/go-tree-sitter`）；其他语言待接 |
| 跨语言 | napi-rs / PyO3 / JNI / FFI 插件 | **Go 没有对等物**：先给长驻 stdio 桥（任何 runtime 都能 spawn），c-shared 快路径留给热点函数 |
| 线上格式 | JSON 起步 + 二进制 wire | JSON + 标记 `{"$number":"-0"}`、`{"$utf16":[…]}`、`{"$undefined":true}` |
| 用例生成 | 按类型生成 + 原式测试 | 按类型生成（不交叉）+ `--inputs` 真实调用 + `--test-coverage` |
| 端口实现 | Rust | Go，`[]uint16` 承载 JS 的 UTF-16 语义 |

## 开发中撞到的真实问题（都写进了代码注释）

1. **正则读不出类型**：原型按参数位置生成，把数字喂给字符串参数，产出 440 条噪声用例。改成 tree-sitter 后同类用例降到 636 条**有意义**的用例。
2. **`repeat('a', 1e9)` 会把录制挂死**：现在数字表上限 1e6，单次调用超 250ms 即停该函数，单条答案超 64KB 即跳过，整个录制 120s 硬超时。
3. **按行号配对会错位**：驱动漏答一行，后面全部比错。改成按「函数名 + 参数」配对。
4. **孤立代理项过不了 JSON**：JS 的 `slice` 能切出半个 emoji，Go 的 string 不能。现在答案以 `$utf16` 码元传输，比较时再按需还原。
5. **`i < 1.5` 会跑两次**：原式是循环而非 `String.repeat`，小数次数的语义不同——这条正是工具自己抓出来的。
6. **GOROOT 与 goenv 不一致**：`go build` 报 `compile: version "go1.22.5" does not match go tool version "go1.26.2"`，需 `export GOROOT=$(brew --prefix go)/libexec`。

## 目录

```
main.go                 CLI：analyze / record / scaffold / diff / gate / bridge / build
internal/ts             tree-sitter 解析：导出、类型、throw 行、导入、风险
internal/analyze        单元、波次、readiness/priority、start_here、blocked 原因
internal/record         类型定向生成、--inputs 回放、--test-coverage、指纹
internal/scaffold       port.go / wire.go / cmd/godriver / go-anything.json
internal/diff           逐例比对 + gate 的附加条件
internal/bridge         stdio 桥与 index.cjs / port.py
internal/wire           NaN / ±Inf / -0 / 孤立代理项 的编解码
testdata/sample         单文件单元，636 例全绿，含手写 Go 端口
testdata/waves          多文件（波次、泛型拒绝、UI 组件拒绝、throw 路径），27 例全绿
```

## 测试

```sh
CGO_ENABLED=1 go test ./...     # analyze / diff / ts / wire 四套
```

## 下一步（按价值）

1. **错误路径全覆盖**：现在只要求「每个 `throw` 有一条失败用例」，下一步要能**生成**能触发它的输入（变异参数搜索）。
2. **c-shared 快路径**：热函数走 `-buildmode=c-shared`，其余走 stdio。
3. **更多语言**：Python（tree-sitter-python）、Java、C#。桥接各自不同，方法论可复用。
4. **`--from-tests` 录制**：跑原式测试时把调用参数拦截下来录成用例（现在只有覆盖率）。
5. **副作用录制**：出站 HTTP、邮件、SQL 行。

## 真实代码：两个来自 `rea` 仓库的单元

不是我自己写的 fixture——`testdata/realworld/` 里两个文件原样来自一个 7,390 个 TypeScript 文件的仓库：

| 单元 | 原式 | 移植时撞到的事 |
| --- | --- | --- |
| `src/kernel/byteToHex.ts` | `byte.toString(16).padStart(2, "0")` | 不是「字节转十六进制」：`byteToHex(1.5)` 是 `"1.8"`，`byteToHex(-1)` 是 `"-1"`（`padStart` 连负号一起数） |
| `src/domain/desktop/shellQuote.ts` | `"'${value.replaceAll("'", `'"'"'`)}'"` | 全部逻辑都在那个 `replaceAll`：单引号要变成 `'"'"'` |

两个都走完整条链：

```
scaffold → record（13 例 / 20 例）→ 手写 port.go → gate 全绿
        → bridge → build --fast → check 13/13、20/20（进程内 addon）
        → fuzz 60/60、80/80（新种子）
```

`byteToHex` 第一版我按「字节」实现，`gate` 立刻用 `byteToHex(-1)` 这一条打出 `expected "-1", got "-001"`——
**是录制用例先发现了我的误解，不是我读代码读出来的**。

## 第三个真实单元：`Uint8Array` 参数、对象返回、`undefined`

`testdata/realworld3/`：`src/domain/elfAttributeUleb.ts`（来自 `rea` 仓库），签名是
`(bytes: Uint8Array, offset: number, end: number) => { value, next } | undefined`。

它一次压出三个能力缺口，都修了：

| 缺口 | 修法 |
| --- | --- |
| `Uint8Array` 参数落到 `any` 表 → 生成 `null`/`true`/`"a b  c"` 喂给字节解析器 | 新增 `bytes` 方言（`Uint8Array`/`Buffer`/`Readonly<Uint8Array>`），`record` 与 `fuzz` 共用 |
| 端口无法表达 `undefined`（返回 `nil` 变成 `null`） | crate 里加 `port.Undefined{}`，`mark` 与驱动都认它，写成 `{"$undefined":true}` |
| `fuzz` 的原式运行没有注册解析钩子 → 带 import 的单元直接模块找不到 | 钩子抽成 `record.WriteResolver`，`record` 与 `fuzz` 共用 |

还有一个不在工具里、在**端口里**的坑，是录制用例逼出来的：JS 里 `bytes[0.3]` 是 `undefined`，
所以 `readElfAttributeUleb(bytes, 0.3, 1)` 返回 `{value: 0, next: 1.3}`——不是失败。我第一版按
`int(0.3)` 取字节，229 条用例里 12 条不一致，`gate` 全部点名。

```
gate    229/229 ✅（三种写法：整数、分数、越界）
fuzz    50/50（seed 9）
check   229/229 经进程内 addon
```

## 第四个真实单元：可选参数 + 平台对象 + 抛错路径

`testdata/realworld4/`：`throwIfJadxInterrupted(signal?: AbortSignal): void`——三种形状一次全占。

| 缺口 | 现象 | 修法 |
| --- | --- | --- |
| Node 的 strip 模式不支持**参数属性**（`constructor(readonly reason: unknown)`） | 整个单元录不了：`ERR_UNSUPPORTED_TYPESCRIPT_SYNTAX` | `nodeflags` 优先 `--experimental-transform-types`，老 node 退回 strip |
| `AbortSignal` 参数落到 `any` 表 | 函数总表现得像「没被中断」，**抛错路径永远没被证明** | 新增 `signal` 方言（空闲 / 已中断带 reason）+ `$abortSignal` 线标记，crate 里解成可分支的类型 |

```
gate    2/2 ✅（空闲 → undefined；已中断 → JadxInterruption，抛错路径被证明）
fuzz    20/20（seed 4）
check   2/2 经进程内 addon
```

## shadow / enable 在真实 crate 上（不是 fixture）

`shadow` 和 `enable` 之前只在 fixture 上验证过。拿 `uleb-go`（有 `import './leb128.js'`、229 条用例）
一跑，两个都不成立：

| 命令 | 真实 crate 上的失败 | 修法 |
| --- | --- | --- |
| `shadow` | `shadow.cjs` 直接 `require` 原式，**没注册解析钩子** → 带 import 的单元一加载就找不到模块 | 生成时顺带写 `resolve.mjs`，并在 `shadow.cjs` 里先 `register` 再 require |
| `enable` | 原式是 `export const read = …`，块里写 `read = …` → 加载即 **`Assignment to constant variable`** | 把该声明改成 `export let`，并记录改了哪些名字；`disable` 改回 `const`，文件仍**字节一致**（sha256 相同） |

实测证据：

```
shadow 72 次真实调用 → 0 处不一致
故意把分数偏移截断（改了端口）→ shadow 点名那 1 条：
  {"fn":"readElfAttributeUleb","args":[[255,255,255,127],2.5,3],"want":{"value":0,"next":3.5},"why":"the answers differ"}
enable 后，从原式路径导入 → {"next":2,"value":128}（走 Go 端口）
disable 后 sha256 与 enable 前完全相同
```

## 七个 crate 的真实性能谱（进程内桥，`bench`）

| crate | 结论 |
| --- | --- |
| format-go（`box`，字符串拼接） | **96x 快** |
| jadx-go（只做一次判断） | **7.6x 快** |
| port-rs（`count`/`words`/`rangeOf`/`ratio`） | 3.8x 快 |
| byteToHex-go | 1.5x 快 |
| shellQuote-go | 1.4x 快 |
| **util-go（`pad`/`repeat`，纯内存小函数）** | **2.1x 慢** |
| **uleb-go（字节循环）** | **1.0x（持平）** |

**短板照实写**：极小的纯函数赢不了——`pad('a', 3)` 在 V8 里是 14 ns，我们的桥一次调用就要 50 ns。
越靠近「真实工作量」赢得越多，这是这类桥的固有形状，不是没优化。

`bench` 之前对带 `import` 的单元直接失败（没注册解析钩子），现在和 `record`/`fuzz` 共用同一个钩子。

## 一条命令验证「能力 + 性能」

```sh
./scripts/build.sh bin && ./scripts/matrix.sh
```

```
crate               cases   gate bridge                 speed
port-rs               181     ok addon                  4.16x faster
util-go                56     ok addon                  1.12x slower
format-go              28     ok addon                  117.06x faster
byteToHex-go           13     ok addon                  1.78x faster
shellQuote-go          32     ok addon                  1.90x faster
uleb-go               229     ok addon                  1.11x faster
jadx-go                 2     ok addon                  14.09x faster
every crate gates and answers through a real bridge
```

脚本对坏掉的 crate 报 `FAIL` 而不是跳过——这一条是它存在的理由。

## 两个「绿灯说谎」的 bug（都修了）

| bug | 怎么发现的 | 后果 |
| --- | --- | --- |
| `diff` 复用 `build` 留下的 driver，**不检查端口还能不能编译** | 故意写一个编不过的 shellQuote 端口 | 报 "every case matches"——它在用旧二进制回答 |
| 字符串表里**没有任何标点** | 把 `shellQuote` 的引号转义删掉 | 仍然全绿，因为没生成过含 `'` 的用例 |

现在：源文件比 driver 新就重建；表里有引号、反斜杠、竖线、分号，并有测试盯着它们别被删掉。
把引号转义删掉后，`gate` 精确报 3 条：

```
- shellQuote("it's"): expected "'it'\"'\"'s'", got "'it's'"
```

## `app record` / `app replay` 在真实 CLI 上

拿 **go-anything 自己**当被测程序（它是个真 CLI）：

```
$ go-anything app record --command ./bin/go-anything --inputs inputs.jsonl /tmp/appwise
6/6 cases replay identically
$ go-anything app replay --command ./bin/go-anything /tmp/appwise
6/6 cases replay identically

# 负向对照：只把 help 文案改一个感叹号
$ go-anything app replay --command /tmp/go-changed /tmp/appwise
3/6 cases replay identically; 3 differ:
- --help: stdout differs
- --version: stderr differs
- no arguments: stderr differs
```

**一个字符的改动，三个用例同时点名**——这正是 `app` 层存在的意义。

顺手补了 CLI 的常识：`--version`/`version` 之前是「unknown command」，现在答
`go-anything v0.0.0-... (go1.26.2)`；`--help` 走 stdout 且退出码 0。有测试直接调 dispatch 验这两条。

## `mcp` 在真实 crate 上

```
tools/call gate  {crate: uleb-go}        → {"cases":229,"matched":229,"passed":true}
tools/call diff  {crate: jadx-go}        → {"cases":2,"matched":2,"passed":true}
tools/call fuzz  {crate: shellQuote-go}  → {"calls":15,"matched":15,"seed":7}
```
