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
| diff | 50.7 ms / 917 例 | **6.9 ms / 181 例** | **墙钟快 7.3 倍**（每例快 45%）✅ |
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

## 四个命令在真实 crate 上（doctor / clean / refresh / map）

| 命令 | 真实 crate 上的结果 |
| --- | --- |
| `doctor` | 新增两项检查：**真编译一个程序**（`GOROOT` 与 `go` 二进制不匹配时立刻暴露）、**解析器是否可用**（cgo 构建能自己解析，快构建要边上有个 `go-anything-cgo`） |
| `clean` | `uleb-go` 清掉 3 项 / 6.0 MB（`portctl`、`cmd/gobench`、`work`），清完 `gate` 仍绿、`check` 229/229 |
| `refresh` | `jadx-go` 重写 4 个文件后 `gate` 仍绿 |
| `map` | 发现计划里混进了工具自己的产物（`resolve-hook.mjs` 被当成一个单元） |

最后一条是真 bug：`analyze` 会把 crate 文件夹里的生成文件（`resolve.mjs`、`check.mjs`、`shadow.cjs`）
当成源码。现在**含有 `go-anything.json` 的目录整个跳过**——工具的输出不该出现在计划里。

`doctor` 的负向对照：

```
$ GOROOT=/nonexistent go-anything doctor
MISS go builds a program  go: cannot find GOROOT directory: /nonexistent
     fix: the go binary and its GOROOT disagree: set GOROOT to the installation the binary came from
```

## 一条命令验证全部主张

```sh
./scripts/build.sh bin && ./scripts/evidence.sh
```

```
== capability: every crate gates and answers through a real bridge ==
port-rs  181  ok addon  3.91x faster      uleb-go  229  ok addon  1.11x faster
util-go   56  ok addon  1.65x slower      jadx-go    2  ok addon  11.17x faster
format-go 28  ok addon 107.90x faster     byteToHex-go / shellQuote-go  1.49x faster
PASS  every crate gates and answers through a real bridge
== app ==      PASS  recorded a real CLI / PASS 5/5 replay identically
== mcp ==      PASS  four replies for four requests / PASS passed:true over the protocol
== package ==  PASS laid out package/ / PASS installed package answers {"next":2,"value":128}
== doctor ==   PASS reports the toolchain it cannot use
== plan ==     PASS PORT-MAP.md holds only source units
every claim above was checked and held
```

写这个脚本时自己踩了两个坑，都修了：

1. **`{ … } | tee` 让失败计数丢在子 shell 里** → 脚本永远 exit 0。改成一个函数 + 重定向。
2. **子脚本的 FAIL 没被计入** → 矩阵里坏掉的 crate 会被「every claim held」盖过去。现在解析矩阵输出计数。

负向对照：把一个 crate 的端口改坏，脚本 `rc=1` 并且点名是哪一段失败。

## 对抗性检查：四个真实会踩的坑（都修了）

拿一个独立 crate 故意弄坏，看工具会不会说谎：

| 弄坏方式 | 之前 | 现在 |
| --- | --- | --- |
| **`cases.jsonl` 清空**（导出没有抛错路径） | `gate` 说「every case matches」——**假绿灯** | `no recorded case for shellQuote: nothing proved it`，rc=1 |
| **用例里有一百万字符的值** | 一条不匹配打印 **150 KB** 终端 | 参数与答案都截断到 160 字节，并说明截掉多少 |
| **`port.go` 被删** | 用上一次编好的 driver 回答，看起来像结果 | `has no port.go: the crate has nothing to compare` |
| **`cases.jsonl` 有一行坏 JSON** | — | `cases.jsonl:34: invalid character…`，带行号 |

第一条最严重：第 16 轮加的「导出必须有录制用例」规则，实际只检查了**被 blocked 的导出**——
因为传给它的表里只有 blocked 条目。现在表里是全部导出（可移植的用空字符串），规则才真的成立。

## 路径与并发（实测过的，不是「应该没问题」）

| 场景 | 结果 |
| --- | --- |
| crate 路径含**空格与中文**（`/tmp/空格 测试/`） | `gate` / `check` / `shadow` 全部正常 |
| 通过**符号链接**访问 crate（相对与绝对两种） | 正常 |
| **6 个 `gate` 同时**跑同一个 crate | 6/6 结论一致 |
| **`gate` + `diff` + `record` 同时** | 三者全绿，之后 `gate` 仍绿 |
| `record` 写、`gate` 读，连跑 8 轮 | 0 次失败 |

最后一条靠一处改动保证：`record` 现在写 `cases.jsonl.staged`，完成后 `rename` 到位——
读者看到的是旧录制或新录制，不会看到半个文件。

**坦白一处**：这轮我一度以为发现了并发竞态，其实是**我自己的 shell 用错了**——
`cd X && a & b &` 里 `cd` 只对第一个后台子 shell 生效，第二个在原来的目录里跑。
改成绝对路径的脚本后，8 轮压测 0 失败。工具没错，测试写错了。

## 大仓库与手工编辑（第 31 轮）

| 场景 | 结果 |
| --- | --- |
| `map` 在 `rea`（7,390 个 `.ts`，2,969 个单元） | **8.8 s**，写出 5,951 行的 `PORT-MAP.md` |
| `report` 在一个真实 crate 上（含 bench + SVG） | 229/229、`card.svg` 840×240 合法 |
| `enable` 后**用户加了一行代码**再 `disable` | 用户那行保留，`let` 还原成 `const`，其余字节一致 |
| `enable` 后**用户改了被宽松化的那行**（加注释）再 `disable` | 注释保留，声明还原 |
| `enable` 后**用户删掉结束标记**再 `disable` | 拒绝并说明：`has the opening marker … but not the closing one: the block was edited by hand, so nothing was removed`；块后代码**没有被吞掉** |

最后一条之前只说「没有开启过」，会让人找错地方。

## 协议与生成器的边界（第 32 轮）

| 场景 | 之前 | 现在 |
| --- | --- | --- |
| MCP 收到 **>1 MB 的一行** | `bufio.Scanner: token too long` —— **整个服务器退出**，后续请求全丢 | 只对该请求回 `-32600 request line longer than 64 MB`，连接继续 |
| MCP 流水线 50 个请求 | — | 50 个回复，顺序一致 |
| MCP 字符串 id / 缺 params / `name` 不是字符串 | — | id 原样回；缺参数 `-32602`；类型不对 `-32602` |
| **`fuzz` 200 次新采样找不到 shellQuote 的转义 bug** | 生成器字母表里**没有标点**——和 `record` 的表在 21 轮修过一次后**又漂移了** | 字母表加入 `'"\|;&<>()[]{}\*?~#%!,:`，同一次 60 次就抓到 8 条 |
| `fuzz --keep` 连跑两次 | — | 32 → 37 → 43 例累积；重录后回到 32 且全绿 |

第二条是这轮最值得记的：**同一个错误在 `record` 修过，`fuzz` 里还留着**——因为它们是两份独立的生成器。
现在两边都有测试盯着字母表/表里必须有那些标点。

## 第八个 crate：每个「JSON 装不下的值」各一个函数

`testdata/markers/` 存在的唯一目的是把每个桥都按同一份契约校验一遍：

| 原式 | 线上的写法 |
| --- | --- |
| `n / 0` | `{"$number":"Infinity"}` |
| `0 / 0` | `{"$number":"NaN"}` |
| `() => -0` | `{"$number":"-0"}` |
| `() => "\ud800"` | `{"$utf16":[55296]}` |
| `() => undefined` | `{"$undefined":true}` |

82 条用例，`gate` / `check`（经 addon）/ `fuzz` 全绿。它一上来就暴露了三处**同一逻辑的多份实现互相漂移**：

| 漂移 | 现象 |
| --- | --- |
| `diff.callKey` 与 `fuzz.key` 两份拷贝 | **零参数函数永远对不上**（录制写 `"args":[]`，驱动省略该字段 → key 一个 `[]` 一个 `null`） |
| 同上，修在 `diff` 后 `fuzz` 仍错 | `fuzz` 报 `the port did not answer` |
| `check.mjs` 把参数**解码后再序列化** | `-0` → `JSON.stringify([-0])` → `[0]`，标记在路上就丢了：`isNegativeZero` 经桥上答 `false`、经驱动答 `true` |

现在 `CallKey` 只有一份，`check` 原样传标记。

## 三个桥必须给出同一个答案（`scripts/allbridges.mjs`）

```sh
go-anything build <crate> && go-anything build --fast <crate> && go-anything wasm <crate>
node scripts/allbridges.mjs testdata/markers/go-ports/markers-go
```

```
ok   -0                 addon=-0        wasm=-0        stdio=-0
ok   lone surrogate     addon=<ud800>   wasm=<ud800>   stdio=<ud800>
ok   undefined          addon=undefined wasm=undefined stdio=undefined
ok   Infinity           addon=Infinity  wasm=Infinity  stdio=Infinity
ok   NaN                addon=NaN       wasm=NaN       stdio=NaN
ok   -0 as an argument  addon=true      wasm=true      stdio=true
```

它一写出来就抓到两个真 bug：

1. **参数方向没有标记**。`decode`（答案）有标记，`encode`（参数）没有——`JSON.stringify([-0])` 是 `[0]`，
   所以 `isNegativeZero(-0)` 在每个真实的桥上都答 `false`，只有驱动答 `true`。
2. **`wasm` 对「脚手架形状」的端口根本编不过**：生成器调的是类型化函数，而 `scaffold` 生成的是
   `func f(args []any)`。现在走注册表（每个 crate 都有），端口若是类型化的则直接调用（保住 53 ns）。

## 两个比较函数不一致（第 35 轮）

`check.mjs` 与 `shadow.cjs` 各有一份 `canon`。同一组值喂给两边：

| 值对 | `check.canon`（修前） | `shadow.canon` |
| --- | --- | --- |
| `-0` vs `0` | **判为相同（bug）** | 判为不同 ✅ |
| `NaN` vs `null` | **判为相同（bug）** | 判为不同 ✅ |
| `Infinity` vs `null` | **判为相同（bug）** | 判为不同 ✅ |
| `undefined` vs `null` | **判为相同（bug）** | 判为不同 ✅ |

后果是实测出来的：把 `negativeZero()` 改成返回 `0`，**`gate` 与 `check` 都报全绿**——
因为 `JSON.stringify(-0)` 是 `"0"`，而 `NaN`/`undefined` 都序列化成 `null`。

现在 `check.mjs` 的 `canon` 与 `shadow.cjs` 用的是同一套标记；报错也从 `want 0, got 0` 改成
`want -0, got 0`。`internal/canon_test.go` 直接跑**生成脚本里的那段代码**，不相同就失败。

## `package` 现在能真正装起来（第 35 轮）

之前 `package` 生成的 npm 主包会去 `require('<name>-<platform>')`，但**没有任何东西生成那个平台包**——
工作流只上传编译产物，用户拿到的是装不上的东西。

现在多生成一个 `pack.mjs`：

```sh
node pack.mjs darwin-arm64 dist/     # 把一个平台产物组装成 <name>-darwin-arm64/
```

实测：组装出的包放进 `node_modules` 后 `require('mk').negativeZero()` 返回 **-0**（而不是 0）。

## 高阶函数：`analyze` 现在会拒绝（第 36 轮）

真实代码里有这样的单元（`rea` 的 `src/kernel/trimEdges.ts`）：

```ts
export const edgeSpan = (value: string, predicate: (character: string) => boolean) => …
```

之前 `analyze` 把它标成 **ready / bridgeable: 3**，`scaffold` 照做，`record` 生成的用例长这样：

```json
{"fn":"edgeSpan","args":["a",0],"error":"TypeError: predicate is not a function"}
```

**函数参数过不了 JSON 桥**，这些用例什么也没证明。现在：

```
edgeSpan | blocked: 'takes a function: a callback cannot cross the bridge'
bridgeable: 0, status: blocked
```

顺带修掉一个连带 bug：全被 blocked 的 crate 只有空 `cases.jsonl`，而生成的驱动在**空行**上报
`unexpected end of JSON input`——空 crate 变成了坏驱动。现在驱动跳过空行（有测试）。

## `analyze` 的第二轮收紧：Promise / Map / Set / symbol（第 37 轮）

拿真实仓库（2,969 个单元、7,951 个导出）统计「被标为 ready 但其实不可移植」的形状：

| 形状 | 数量 | 之前 |
| --- | ---: | --- |
| 返回 `Promise<T>`（箭头函数写 `: Promise<T>`，不是 `async`） | 103 | ready ❌ |
| 参数或返回带 `Map` / `Set` / `WeakMap` / `WeakSet` | 55 | ready ❌ |
| 返回/参数是 `symbol` | 10 | ready ❌ |

收紧后：

```
ready 6,092 → 5,954      blocked 1,859 → 1,997
新增理由：returns a Promise 103 / carries a Map 26 / Set 13 / WeakMap 15 / WeakSet 1
```

**没有一刀切**：名字以大写开头的类型（如 `SetupOptions`）不拦——它是数据接口时能作为对象过桥。
只有在类型明确没有 JSON 形态时才拒绝。

## 静态方法与类实例（第 38 轮）

真实仓库里有 44 个 `static` 方法导出。拿 `src/kuna/KunaRunSlots.ts` 试，撞到两件事：

| 问题 | 现象 |
| --- | --- |
| **静态方法根本没被录到** | `forHost` 在运行时是 `mod.KunaRunSlots.forHost`，而录制器只查 `mod.forHost` → `typeof !== 'function'` → **静默跳过，0 例，一声不响** |
| **类实例被录成 `{}`** | `forHost(0)` 返回 `KunaRunSlots` 实例，`JSON.stringify` 只剩字段、方法在原型上 → 录到 `"ok":{}`，端口永远对不上 |

修法：

1. 录制计划从「函数名 → 类型表」改成「函数名 → { 路径, 类型表 }」，静态方法按 `Class.name` 解析；
2. 解析不到时报出来（此前静默）：`go-anything: X.y is not a function in the original; it is skipped`
3. 答案无法过线时（类实例）明确拒绝：`forHost answers a KunaRunSlots, which JSON cannot carry: no case proves it`

`canCarry` 是递归判断：原始值、数组、日期、Map/Set、TypedArray、原型为 `Object.prototype` 的普通对象可以过；
类实例不行。

## `analyze` 会说清楚「什么过不了桥，为什么」（第 39 轮）

真实仓库（2,969 个单元）跑一遍，输出顶部现在是这样一张表：

| Reason | Exports |
| --- | ---: |
| is async: the call would have to cross asynchronously | 1,231 |
| is generic: the bridge carries concrete types only | 242 |
| is a generator | 191 |
| takes a function: a callback cannot cross the bridge | 172 |
| returns a Promise: the bridge answers in one turn | 103 |
| carries a Map: it has no JSON form | 25 |
| carries an Iterable: it is lazy, and the bridge cannot pull | 23 |
| carries a WeakMap / Set / Readable / Generator / Writable | 15 / 12 / 5 / 4 / 2 |

本轮新增：`Generator` / `AsyncIterable` / `Iterable`（惰性，桥没法 pull）与 `Readable` / `Writable`
（流不是值）。ready 5,954 → **5,925**。

**为什么重要**：之前 5,954 个「ready」里混着这些；一个人从 `Start here` 挑一个出来，
很可能挑到根本过不了桥的东西。现在他先看到的是这张表。

## 第九个 crate：RFC 6901 指针（端到端复跑，第 40 轮）

`testdata/realworld5/`：`src/domain/jsonPointer.ts` 四个函数，来自 `rea`。
走的是完整链：record 128 例 → 手写端口 → gate → check → fuzz → shadow → enable → disable。

```
gate    128/128 ✅
check   128/128 经进程内 addon ✅
fuzz    160/160（seed 11）
shadow  20 次真实调用，0 处不一致 ✅
enable  → 从原式路径调用 jsonPointerTokens("/a/b") 得到 ["a","b"] ✅
disable → 字节一致（sha 相同）✅
```

### 它挖出的两个 bug

| bug | 现象 |
| --- | --- |
| **`pointer.slice(1)` 在 Go 里是切字节** | `jsonPointerTokens("中文 字")` 得到损坏的首个 token——JS 按 UTF-16 码元切，Go 按字节切。**是记录用例抓到的** |
| **单字符串快路径跳过了标记** | `answer1` 直接 `json.Marshal(v)`，`[]uint16` 变成 `[[97],[98]]`：`gate`（驱动）通过、`check`（桥）31 条不一致、`shadow` 4 条 |

第二个尤其值得记：**同一份端口，驱动答对、桥答错**——正是 40 轮前设计 `check` 就是为了抓这类。

## 矩阵多了两列，字符串表多了结构字符（第 41 轮）

`scripts/matrix.sh` 现在强制 **gate + check + bridge** 三列都过，`stdio` 落回会标成 `stdio!` 并计为失败：

```
crate               cases  gate  check bridge    speed
port-rs               253    ok     ok addon     3.54x faster
util-go                56    ok     ok addon     1.31x slower
format-go              28    ok     ok addon   128.66x faster
shellQuote-go          56    ok     ok addon     1.41x faster
jp-go                 224    ok     ok addon     2.84x faster
…
every crate gates, answers through the in-process bridge, and can be timed
```

负向对照：删掉 `addon.node` → 该行 `bridge` 变成 **`stdio!`**，脚本报 problem。

### 更重要的是它抓出的一个假绿灯

把 `escapeJsonPointerToken` 改成**原样返回**，`gate` 依然全绿——因为 64 条用例里**没有一条含 `~` 或 `/`**，
而这两个字符正是这个函数存在的理由。

修法：字符串表加入**结构字符** `/ ~ . : = & ? # % + @ , * ! [] {} ()` 与 `a/b/c`、`~0~1` 这类组合。
重录后 `escape` 有 112 条用例、8 条含 `~`、12 条含 `/`；再弄坏端口：

```
- escapeJsonPointerToken("a/b/c"): expected "a~1b~1c", got "a/b/c"
go-anything: gate: 9 of 224 cases do not match
```

同一次修改把 `port-rs` 从 181 例推到 **253 例**（`shellQuote` 32 → 56）。

## `fuzz` 的四件事（第 42 轮）

| 问题 | 之前 | 现在 |
| --- | --- | --- |
| 字母表没有 `/` | 「只漏了 `/` 转义」的端口在 **400 次新采样里全过** | 加入 `/ = @ +`；同一端口 400 次里抓到 **14 条** |
| 生成器会要 10^14 次重复 | node 吃掉 4 GB 后 `scavenge` 崩溃 | 数值上界收到 10^6，并对单次答案设 64 KB 上限 |
| 巨大答案原样打印 | 一次不匹配输出 **590 KB** | 截断到 160 字节（和 `diff` 同一套） |
| 原式跳过的调用仍算不匹配 | `61/80 differ`——看起来像 19 个回归 | `61/61 match` + `19 call(s) skipped: ...` |

第二条顺带修了 `cases.Read` 的扫描器上限（一条用例可以很大，Scanner 会在 1 MB 处停下）。

九个 crate 全部 `fuzz` 通过：`120/120、48/48、24/24、30/30、30/30、30/30、30/30、210/210、120/120`。

## 一份清单，两个生成器（第 43 轮）

`record` 的字符串表和 `fuzz` 的字母表**漂移过三次**（引号、结构字符、斜杠），每次都是「修了 A，B 还留着」。
这轮把它们收敛成一个清单 `internal/pool`：

```go
const Structural = "'\"\\/`|;&<>()[]{}*?~#%!.,:=@+$"
func Values() []string   // record 的表：精选值 + 每个结构字符单独一条
func Alphabet() []rune   // fuzz 的字母表
func JSValues() string   // 渲染成 JS 字面量，注入生成的录制脚本
```

**测试写死了一份「必须存在」的清单**——不是我读同一个常量再断言它，而是独立写出来：

```
从 Structural 删掉 '/' 与 '['：
  pool_test.go:20: '/' is not in the pool at all
  pool_test.go:23: the fuzz alphabet cannot produce '/'
  pool_test.go:20: '[' is not in the pool at all
```

重录后用例数：`port-rs 253 → 328`、`shellQuote 56 → 80`、`jp-go 224 → 320`，九个 crate 全部 gate + check + addon 绿。

## 一份线协议实现，四处生成（第 44 轮）

`decode` / `encode` / `canon` 曾经有四份拷贝：`index.cjs`、`index.wasm.mjs`、`shadow.cjs`、`check.mjs`。
第 35 轮就是它们漂移出的 bug：**`check` 判 `-0 == 0`，`shadow` 不判**，于是「返回 0 而不是 -0」的端口
过了 `check` 却挂在 `shadow`。

现在它们都从 `internal/jswire` 取同一份文本（`Decode` / `Encode` / `Canon`），生成时替换占位符。

**测试直接在两种模块形态里跑这段代码**（CommonJS 与 ESM 各一份文件），断言：

```
canon(-0) == '"$-0"'   canon(0) == '0'   （两者必须不同）
encode(-0) → decode → canon 与原值相同    （往返不丢）
canon(NaN) == '"$NaN"'  而不是 'null'
```

负向对照：把 `Canon` 里的 `-0` 分支删掉 → `node` 输出 `"0|0|true|..."` → 测试立刻红。
（我这次删错了行、删到 `Encode` 的，测试同样立刻红——两边都在网里。）

## `shadow` 现在也比对抛错（第 45 轮）

之前 `shadow` 的包装是：

```js
const want = ORIGINAL[fn](...args)   // 原式抛错 → 整个包装炸掉，什么也没记
```

和端口的行为对比只覆盖「两边都返回」。用一个会抛错的真实单元（`JadxInterruption`）一试：

| 情况 | 之前 | 现在 |
| --- | --- | --- |
| 两边都抛同类错 | 原式把包装炸掉，无记录 | 一致，不记 |
| 端口不再抛错 | 无记录 | `one side failed and the other did not` |
| 端口抛错类型不同 | 无记录 | `the error type differs`（`want {"error":"JadxInterruption"} got {"error":"Error"}`） |

顺带修了两个连带问题：

1. **`AbortSignal` 参数过桥变成 `{}`** —— 它没有自有可枚举字段，`encode` 没处理，于是「会在中断时抛错的端口」看起来像「永不抛错」。现在写成 `$abortSignal` 标记。
2. **桥返回的错误类型名读错** —— 从 Go 回来的错误是普通 `Error`，`constructor.name` 是 `"Error"`，会把正确的端口判成错的。现在按消息前缀读（`JadxInterruption: …`）。

两条负向对照都跑过：端口不抛 → 2/4 不一致；抛错类型不同 → 2/4 不一致且点名 `the error type differs`。

## 契约是可执行的（第 46 轮）

前三轮（43/44/45）都在做同一件事：**把「同一逻辑两处实现」收敛成一处**。这轮把它写成
[CONTRACT.md](CONTRACT.md) —— 每条规则写清楚「曾经出过什么事」，然后变成可跑的检查：

```sh
$ ./scripts/contract.sh
== one implementation per rule ==
ok    the generator pool is the only list of characters
ok    pool: every structural character is reachable in both generators
ok    jswire: decode, encode and canon are the only copies
ok    jswire: the helpers behave in both module forms
ok    diff: one call key, used by fuzz too
ok    analyze: a callback, a Promise, a Map and an iterator all stay behind
== both paths, per crate ==
ok    matrix: every crate gates, checks through the addon, and can be timed
every rule in CONTRACT.md still has something holding it
```

三条负向对照都跑过：

| 破坏 | 谁抓到 |
| --- | --- |
| 把 `canon` 抄回 `shadow.go` | `FAIL  jswire: … only copies` |
| 某 crate 没建 addon | `FAIL  matrix: … fell back to the pipe` |
| 从 `pool` 删掉 `/` | `FAIL  pool: … reachable in both generators` |

它已并入 `scripts/evidence.sh`，所以每次「一条命令验证全部主张」都会顺带验证这份契约还在。

## 从零 clone 到全绿：60 秒（第 47 轮）

之前没人真的试过。试了一次，`make check` 连挂三处——**而我自己的工作树里它们也一直是红的，因为我的检查只数了 `ok` 行，没看 `FAIL`**。

| 问题 | 原因 |
| --- | --- |
| `internal/canon_test.go: undefined: checkScript` | 测试放错包（它读 `main` 包里的东西） |
| `TestTheStringTableHas...` 失败 | 测试仍在解析 `record.go` 里的表，而表在 43 轮搬进了 `internal/pool` |
| `Cannot find module index.cjs` | `index.cjs`/`cmd/portctl`/`answer.go`/addon 都是生成物且被 gitignore，clone 里没有 |

修法：删掉重复的 `canon_test.go`（`internal/jswire` 的测试覆盖同样的性质）、表测试指向 `pool.Values()`、
**矩阵在测量前先生成缺失的文件**（每个 crate 15 ms）。

### `make check`

```sh
$ make check
...  build → test → contract → matrix → evidence
all checks passed
real  0m59.566s        # 全新 clone，第一次跑
```

### 顺带发现的第三个问题

`contract.sh` 现在多一条规则：**重新生成每个 crate 后，`git diff` 必须为空**。因为它一加上就报出三份
陈旧文件——`port-rs/answer.go` 缺 `Undefined` 标记，两份 `index.cjs` 还内联着 44 轮已搬走的线协议代码。
负向对照：往 `answer.go` 里加一行注释提交 → `FAIL generated files: stale committed copies in testdata/sample/port-rs`。

## 用官方 MCP 客户端跑一遍（第 48 轮）

之前只用 `printf` 拼 JSON-RPC 测过 `mcp`。这轮装了 `@modelcontextprotocol/sdk`，用**真正的客户端**
连上来：它做 `initialize`、`notifications/initialized`、`tools/list`、`tools/call`，并按 schema 校验。

立刻抓到三个问题：

| 问题 | 现象 |
| --- | --- |
| **`doctor` 不是工具** | CLI 有 27 个命令，MCP 只暴露 7 个；`doctor`、`check`、`bench`、`enable`/`disable`、`clean`、`refresh`、`shadow-report` 全缺 |
| **`"required": null` 让整个工具列表被拒** | 无参数工具的 `required` 是 nil slice，序列化成 `null`；SDK 按 JSON Schema 校验，直接拒绝**整张表** |
| **没有任何检查盯着这两件事** | 我手拼的 JSON-RPC 从不校验 schema |

修法：

1. 新增 8 个工具，**全部委托给已存在的命令**（`mcp.SetCommands` + 捕获 stdout），而不是写第二份实现——
   否则就是本项目反复付账的那种漂移；
2. `obj()` 里 nil 变空数组；
3. `internal/mcp/mcp_test.go` 加两条：每个工具的 schema 必须是客户端能接受的样子；CLI 有的命令必须是工具。

```sh
$ node scripts/mcp-client.mjs .        # 需要 npm i @modelcontextprotocol/sdk
15 tools: analyze, map, diff, gate, record, fuzz, doctor, check, bench, shadow-report, enable, disable, clean, refresh, profile
ok   analyze … ok   check … ok   clean … ok   unknown tool → isError
```

### 顺带：`doctor` 与它诊断的代码不一致

第二次跑客户端时 `doctor` 报 `MISS parser available`——但 `bin/go-anything-cgo` 就在旁边，而真正会去启动
解析器的代码（`ts.helperPath`）**能找到它**。

原因：`doctor` 自己又写了一遍「去哪里找解析器」的搜索，只看 `GO_ANYTHING_PARSE` 和 `PATH`，不看二进制旁边。
**同一逻辑的第二份实现，又一次。** 现在 `doctor` 调 `ts.ParserPath()`，两种构建各有实现（cgo 构建自己就是解析器）。

验证：

```
有 helper           → ok   parser available   /tmp/nocgo/go-anything-cgo
删掉 helper         → MISS parser available   no cgo in this build: …
```

### 这一轮的账

| 发现 | 由谁发现 |
| --- | --- |
| 8 个命令不是 MCP 工具 | 官方 SDK 客户端 `tools/list` |
| `"required": null` 让整张工具表被拒 | 同上（schema 校验） |
| 委托给命令而不是重写 | 我，按 CONTRACT.md 的原则 |
| `doctor` 与 `ParserPath` 不一致 | 客户端第二次跑（第一次是第一次构建，恰好没有 helper 缺失） |

## 真的 `npm install` 一次（第 49 轮）

上一轮的 `package` 只是「工作流会构建其他平台」。这轮走真实消费者路径：`npm pack` 两个包 → 起一个
本地 registry → `npm install jp` → 让 npm 自己解析 `optionalDependencies` → `require('jp')`。

```sh
$ ./scripts/npm-install-check.sh
== package ==
platform: darwin-arm64
== tarballs ==
  jp-1.0.0.tgz
  jp-darwin-arm64-1.0.0.tgz
== registry ==
== install ==
added 2 packages in 159ms
== use it ==
ok   the installed package answers correctly
== the platform package is required, not optional at runtime ==
ok   a missing platform package says so
```

### 过程中撞到的三件事

| 问题 | 现象 |
| --- | --- |
| registry 的 `shasum` 是占位符 | `npm error code EINTEGRITY … wanted sha1-AAAA…`，一个包都装不上 |
| `npm pack` 把 tgz 写进**运行时的目录** | 平台包的 tgz 落在 `jp-darwin-arm64/` 里，脚本按父目录找 → 缺失但没报 |
| 脚本永远 `exit 0` | `FAIL` 只是一行文字，CI 看不到 |

三件事都修好后，两条负向对照都能红：

| 破坏 | 结果 |
| --- | --- |
| 生成的 loader 不 `require` 平台包 | `FAIL a missing platform package did not say so`，`rc=1` |
| loader 返回写死的错答案 | `FAIL {"answer":[],"escaped":""}`，`rc=1` |

## 检查脚本必须能变红（第 50 轮）

三个脚本**报告结论却永远 `exit 0`**：

| 脚本 | 之前 | 现在 |
| --- | --- | --- |
| `matrix.sh` | 坏端口在表格里显示 `FAIL`，退出码 **0** | `exit 1` |
| `perf.sh` | 缺 rig 时打 traceback 或一排 0.0 ms，退出码 **0** | 拒绝测量，`exit 2` |
| `npm-install-check.sh`（49 轮） | `FAIL` 是装饰 | `exit 1` |

`perf.sh` 现在还会先跑两边的 `diff`——它自己的注释早就写着「两边都必须 diff 通过」，但从来没检查过。

### `scripts/negative.sh` / `make negative`

**故意坏一次，确认它红**：

```
$ make negative
ok    matrix rejects a broken port (exit 1)
ok    contract notices a copy of the wire (exit 1)
ok    evidence notices a case that does not hold (exit 1)
ok    perf refuses a missing rig (exit 2)
ok    npm-install-check notices a loader that answers wrongly (exit 1)
every check in this folder fails when it should          # 19 秒
```

它慢且会临时破坏文件，所以**不放进 `make check`**——它是「为什么可以相信 `make check`」的那个东西。

## 桥和驱动各自坏掉时，谁抓到（第 51 轮）

上一轮的负向对照只弄坏**端口**。这轮分别弄坏**桥**和**驱动**，检查两条路径是不是真的分开：

| 破坏 | `gate`（驱动路径） | `check`（桥路径） |
| --- | --- | --- |
| 桥的 `encode` 丢掉 `-0` | 仍然通过 ✅（它不走桥） | **12 条不一致** ✅ |
| 驱动的 `mark` 丢掉 `-0` | **7 条不一致** ✅ | 通过（它不走驱动） |

**第一次跑的时候两边全绿**——说明有真问题，查下去找到两个：

1. **`check` 只走一条路**：它把录制里的标记原样传给桥（33 轮的修法），所以 `encode` 从来没被真正调用过。
   现在每个用例跑**两遍**：一遍传标记（驱动路径），一遍传解码后的值（真实调用者路径）。`164 = 82 × 2`。
2. **`diff` 认为 `-0 == 0`**：`compare` 用数值容差比较，`|(-0) - 0| = 0`。所以驱动丢掉符号也能过。
   现在零值要求符号位一致：`the answer differs in sign of zero`。

这两条是同一个盲区的两面：**线协议专门为 `-0` 设了标记，而两条比较路径都不认它。**

## 三个桥各有各的负向对照（第 52 轮）

`scripts/allbridges.mjs` 的第三列原来叫 `stdio`，其实还是 addon 的另一个入口——**pipe 从没在装好 addon 的机器上被测过**。

现在：`GO_ANYTHING_NO_ADDON=1` 强制走 pipe，`allbridges.mjs` 多跑一遍并与 wasm 对比：

```
ok   -0                 addon=-0        wasm=-0        stdio=-0
...
pipe: 6/6 agree with the wasm bridge
```

`make negative` 现在逐个弄坏三个桥，**三个都必须红**：

| 破坏 | 谁抓到 |
| --- | --- |
| wasm 包装的 `encode` 丢掉 `-0` | `allbridges notices a wasm wrapper that drops -0` |
| pipe 二进制把参数丢掉（`Answer(name, nil)`） | `allbridges notices a pipe that answers without arguments` |
| addon 里编译进去的 `mark` 丢符号 | `allbridges notices an addon that loses the sign of zero` |

顺带一条经验：**修复源码不足以恢复**——addon 里是编译进去的副本，控制脚本必须重新 `build --fast`，
否则它报的是自己留下的残留（第一次跑就撞上了，`FAIL allbridges does not recover`）。
