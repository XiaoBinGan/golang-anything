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

## 与 rust-anything 的能力对齐

见 [PARITY.md](PARITY.md)：26 条命令的逐项状态。**已实现 13 条**（`analyze` `scaffold/bridge` `record` `build` `diff` `bench` `clean` `gate` `map` `doctor` `enable` `disable` `shadow`，外加三条自创的 `check` `fast` `shadow-report`），13 条待做（`refresh` `fuzz` `profile` `go` `report` `package` 与 `app *` 五条、`setup/mcp`）。

## 性能：进程内桥 0.73 µs/次

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
