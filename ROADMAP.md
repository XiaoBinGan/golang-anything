# 接下来做什么

按「值不值」排序，每条都写清楚**现状**、**要做什么**、**怎么算做完**。

## 一、功能：让类库也能移植（最高价值）

**现状**：`analyze` 只认自由函数。真实项目里 Kotlin/Java 的 `object`/静态方法、Ruby 的 `def self.`、
PHP 的 `public static function`、C# 的静态方法**已经能读**（本轮补的），但 Carbon 这类**实例方法库**仍然读不到。
survey 的结果：php-carbon 37 单元/506 导出、ruby-sinatra 47/454（本轮从 1/3 和 0/0 涨上来的）。

**要做什么**：识别**不碰实例状态**的实例方法（方法体里没有 `self.`/`this.`/`$this->`），把它提升为
`Class.method(instance-free args)`，并在 `analyze` 里标注「无状态方法，可移植」。
**怎么算做完**：Carbon 的 `isLeapYear()` 这类方法能进 plan；不对任何有状态方法误判（用一个反例测试：
方法体里出现 `$this->` 就必须拒绝）。

## 二、功能：`enable` 覆盖更多语言

**现状**：有块的语言是 TS/JS（重绑定导出）与 Ruby（`class << self` 覆盖，`enable → 调用 → disable → 字节一致`
已验）。没有块的语言（Rust/Go/C/Python…）现在**直接拒绝**：`switched.takeover` 只认 `.ts .tsx .js .jsx .mjs .cjs`
与 `.rb`，其余报 `enable has no takeover for …`，不写任何字节（对照 22）。在这之前它会把 JS 块写进 `.rs` 文件并
报成功——那个文件只是不再能编译。
**要做什么**：Python 用模块属性覆盖、PHP 用 `class_alias`/装饰、Java/Kotlin/C# 用静态转发。
**怎么算做完**：每种语言一个 `enable → 调用 → disable → 字节一致` 的测试，并把这些扩展加进 `takeover`；
在加进去之前，对照 22 必须一直是绿的。

## 三、性能：没有赢下来的两处，要么赢要么写死理由

| 轴 | 现状 | 计划 |
| --- | --- | --- |
| 桥（wasm） | 已赢 1.3–1.5x | 保持；`perf.sh` 已在输掉时返回非零 |
| diff / record | 已赢 5–7x / 1.4x | 保持 |
| 启动 | 已赢（拆 front/engine 之后） | 保持 |
| **N-API addon 单次调用** | 慢 2.5–2.8x | cgo ABI 下限；**不再追**，文档写明 wasm 是主推路径 |
| **C 的 bench** | 已修：字面量参数是编译期常量，clang 把整次调用折叠掉，量到的是循环不是函数（本机 `int magic(int n){return 42;}` 旧 harness 报 1 ns/13.46x slower） | 每个数值实参走 `volatile`（`__typeof__` 取类型，不另建类型表）；同一 crate 实测 1 ns→3 ns、13.46x→6.99x |
| **`bench-all-languages.sh` 量的是脚手架桩** | 那个脚本 scaffold 之后只 `record`，不实现端口，于是 `bench` 比的是「原式 vs `return nil, fmt.Errorf("not implemented: port …")`」。同一个 C crate（`int twice(int n){return n*2;}`）实测：桩 17 ns / **8.83x slower**，把端口写成 `return int(args[0].(float64)) * 2, nil` 后 5 ns / **3.10x slower**。9 个语言的 ok 行都是这个桩比原式 | 已修：驱动在预备轮里把 `not implemented: port …` 记成 `unported`，整行都是桩就丢掉；一行不剩时 `bench` 直接报错（对照 25）。脚本里的九个玩具端口也真的写出来了 |
| **bench 的端口侧读数是噪声** | 原来整段循环只取一次 `time.Now()`，一个调度抖动就改答案：同一个 C crate 五次跑出 4/5/5/7/9 ns（±40%）。改成「每轮单独取样、取中位数」后同一 crate 五次都是 5 ns/5 ns/5 ns/5 ns/5 ns；九语言复测 kt 11.40x、cs 8.42x、ps 7674.07x、php 9.55x、rb 12.67x、py 6.50x、java 4.75x faster，c 2.69x、cpp 3.52x slower | 已同样改：按 256 轮一段取样取中位数（macOS 的 `clock_gettime` 粒度约 41.67 ns，按单轮取样会读出 0）；并把 `sink` 改成 `volatile`，否则 clang 会把整段循环向量化（`twice` 读成 0.23 ns/call，比一个周期还快）。改后同一 crate 连跑三次都是 1 ns / 4.25x slower |
| **c/cpp 的 3.5–3.8x slower** | 已排除两个猜测，都有数字：① 注册表派发不是原因（空循环 0.73 ns、typed 调用 0.74 ns、端口 body 1.05 ns，差 0.3 ns）；② 参数切片的局部性**在隔离实验里**是原因（17 个各自分配的 `[]any` 7.69 ns/call，共用一个 2.48 ns/call），但把驱动改成「每个函数一个连续底层数组、每个用例取子切片」后，同一个 crate 五次跑仍是 5 ns/5 ns/5 ns/5 ns/5 ns（改前 5/5/7/5/5）——没有可测收益，改动已回退 | 已定因一半，读数是稳的：在 crate 里用同样的「每段 256 轮取中位数」测同一形状——驱动形状（17 个各自分配的 `[]any` + 注册表 `Fn`）4.232 ns/call；同样 17 次但共用一个切片 2.451；空循环 0.326。也就是说 ~2.1 ns 是调用本身、~1.8 ns 是逐个切片追指针。**还没解释的**：把驱动改成「每函数一个连续底层数组」后 bench 仍报 5 ns（隔离实验说该变成 2.5），下一轮要查驱动里是不是还有别的差异（`repeat` 外层循环、`f` 的间接层、`samples` 增长）|

## 四、可信度：继续加负向对照

`make negative` 现在 23 条（打出 24 行 ok：对照 4 还带一条「gate 在坏 bridge 下仍通过」的确认行）。已销账：
* `enable` 在非 TS/JS/Ruby 语言上不能静默成功 —— 对照 22，坏版本会把 JS 块写进 `.rs` 且 exit 0；
* 类方法被误判为可移植 —— 对照 23 盯住 `$this`（和 `self::` 不是同一个 token），同一文件里的纯方法必须仍然通过；
* `bench` 的折叠 —— 对照 24 断言生成的计时循环里没有任何字面量实参（查 harness 源码，不查纳秒：本机字面量
  harness 是 1 ns / 13.46x slower，`volatile` harness 是 3 ns / 6.99x slower）。

还要补的：无（第四节的旧清单已全部落地）。下一步看第五节：真实项目每语言 3 个。

## 五、真实项目：扩大到每语言 3 个

`scripts/survey-real.sh` 已能跑任意目录。下一步：把 Kotlin-arrow（3,170 导出）、fmt（483）、zlib（222）
里的**各一个真实单元**移植到 gate 绿，作为「真实代码能过」的证据，而不是只有玩具函数。
