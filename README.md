# go-anything

把代码porting到 Go 的自动化骨架：**analyze → record → scaffold → diff → gate**，一个单元一个单元地过。
与 rust-anything 同一套方法论，目的地换成 Go。

```sh
go-anything analyze  testdata/sample          # 出计划：单元、波次、风险、start_here
go-anything record   --unit src/text.ts testdata/sample/port-rs
go-anything scaffold --unit src/text.ts --crate <module>/testdata/sample/port-rs testdata/sample/port-rs
go-anything diff     testdata/sample/port-rs  # 逐例对拆
go-anything gate     testdata/sample/port-rs  # 不合格即退出非 0
```

## 已经跑通的（`testdata/sample`，312 个用例）

```
$ go-anything analyze testdata/sample
| Unit | Lines | Exports | Wave | Readiness | Priority |
| `src/text.ts` | 25 | 4 | 0 | 85 | 71 |
Start here: src/text.ts
- String offsets and slicing (weight 6): src/text.ts:4
- Regex features (weight 6): src/text.ts:1

$ go-anything record --unit src/text.ts testdata/sample/port-rs
Recorded 312 cases for 4 functions (13d623b7382de4d1) from src/text.ts.

$ go-anything gate testdata/sample/port-rs
312/312 recorded cases match.
gate: every case matches, and every export is in the port.
```

负向对照——把 `count` 改成「遇到第一个空格就 return」：

```
308/312 recorded cases match.
- `count("  ")`: expected 2, got 1 (the answer differs)
- `count("a b  c")`: expected 3, got 1 (the answer differs)
- `count("x y z")`: expected 2, got 1 (the answer differs)
- `count("pad  ")`: expected 2, got 1 (the answer differs)
```

## 与 rust-anything 的差异（这是设计里最要紧的部分）

| 环节 | rust-anything | go-anything | 结论 |
| --- | --- | --- | --- |
| 解析 | 每种语言一棵 tree-sitter 语法树 | 目前是正则（`Report.Approximate`），可插 tree-sitter | 必须补；正则读不出类型就生成垃圾用例 |
| 跨语言调用 | napi-rs / PyO3 / JNI / FFI / ctypes | **Go 没有对等物**：只能 `go build -buildmode=c-shared` + cgo 导出 C 函数 | 桥接层要自己写，且每种宿主语言一份薄包装 |
| 线上格式 | JSON 起步，后加二进制 wire | 同样是 JSON + 标记（`{"$number":"NaN"}`、`{"$number":"-0"}`、`{"$undefined":true}`） | 已实现 `internal/wire` |
| 录制 | 按参数类型生成边界值 + 原式自带测试 | 按注解类型生成（`string`/`number`/`boolean`/`string[]`/`number[]`/`any`） | 已实现；`--from-tests` 未做 |
| 差分 | 忽略键序、浮点容差、NaN/-0 标记 | 同上，另加 `math.IsNaN` 视作相等 | 已实现 |
| 判定 | `gate`：覆盖率、错误路径、`unwrap`/`panic!` | `gate`：每例匹配 + 每个导出都在端口里 | 错误路径覆盖率与「禁止 panic!」未做 |

**Go 特有的三个坑（写端口时绕不开的）**

1. **UTF-16 语义**：JS 用 UTF-16 码元索引，Go 用字节/符文。示例端口用 `[]uint16` 才对齐，
   否则 `slice` 会把 emoji 劈成半个——`rangeOf('😀😀', …)` 的用例就是为此准备的。
2. **没有 lookbehind**：`/(?<=\$)\w+/g` 这类模式 Go 的 `regexp` 不支持，只能手写扫描。
3. **-0 / NaN / ±Inf 进不了 JSON**：两侧都做标记，比较前解码（`internal/wire`）。

## 目录

```
main.go                  CLI：analyze / record / scaffold / diff / gate
internal/analyze         遍历仓库、出口识别、波次、readiness/priority、start_here
internal/record          生成并运行 JS 驱动，写 cases.jsonl 与 recorded-from.json（源指纹）
internal/scaffold        生成 port.go 骨架 + cmd/godriver + go-anything.json
internal/diff            answers 与 cases 逐例比对（键序无关、浮点容差、错误类型）
internal/wire            NaN/-0/±Inf/undefined 标记的编解码
testdata/sample          示例：TS 单元 + 手写 Go 端口（312 例全绿）
```

## 还没做（按价值排序）

1. **真解析器**：tree-sitter 的 Go binding（`smacker/go-tree-sitter`），先做 TS/JS，再加 Python。
2. **`--from-tests`**：跑原式自带测试录制真实调用，覆盖率立刻上一个量级。
3. **c-shared 桥**：`//export go_anything_call` + 各宿主一面薄包装，让端口能真的在 TS/Python 里被调用。
4. **错误路径覆盖率**：`gate` 要求每个 `throw`/`raise` 分支都有一条失败用例。
5. **副作用录制**：出站 HTTP、邮件、SQL 行（rust-anything 已做，这里只留了接口位）。
6. **多单元/波次**：跨文件依赖已经算波次，但还没有「一次移植一个包」的批量模式。

## 怎样一条命令跑起来

```sh
export GOROOT=$(brew --prefix go)/libexec   # 本机 goenv 与 brew 的 GOROOT 曾不一致
go build -o go-anything .
./go-anything analyze testdata/sample
```

`go test ./...` 目前只有示例端口可跑；单元测试见下一步（`internal/diff` 的键序与容差、`internal/wire` 的标记往返）。
