# 接下来做什么

按「值不值」排序，每条都写清楚**现状**、**要做什么**、**怎么算做完**。

## 一、功能：让类库也能移植（已完成，五路）

**现状**：实例方法**已经能进 plan**——`analyze` 数出 `instance_methods` / `instance_free` / `instance_promotable`，
`promote` 在**原文件一侧**补一个能拿到实例的入口，于是这些方法像自由函数一样进入 scaffold → record → gate。
四条路，都有端到端 smoke 用例：

| 语言 | 入口怎么写 | 前置条件 | 实测 |
| --- | --- | --- | --- |
| Ruby | 就地重开类，`def self.x(*args)` → `allocate.x(*args)` | 方法不碰 `@ivar`/`self.` | `promote → record → gate` 绿 |
| PHP | `promoted.php` 里的代理类，带方法自己的类型 + `require_once` | 方法不碰 `$this` | record **0 → 15 例**，gate 绿 |
| Java | `MathsPromoted.java`，`new Maths().doubled(n)` | 无实例字段 + 有无参构造 | `javac` 通过，gate 绿 |
| Kotlin | `object TimerPromoted { fun doubled(...) }` | 无属性（类体/主构造器）+ 主构造器无参 | `kotlinc` 通过，record 17 例，gate 绿 |
| C# | `MathsPromoted.cs` 静态类，`new Maths().Doubled(n)` | 无实例字段 + 有无参构造 | `dotnet build` 0 警示 0 错误，record 17 例，gate 绿 |

**拒绝的理由分三种，逐条说清**（对照 30/31 盯着）：没有实例方法；方法全部读状态；不碰状态但住在 module 里
（module 不能实例化）。

**怎么加一门新语言**（下一门照这个做）：① 解析器把 `Fn.Receiver` 填对、并给出 `Promotable{Owner,Name,Params,Returns}`；
② `promote` 新增渲染器（标记用该语言的注释符）与 `Insert*`；③ 录制器能按 plan 的 `path` 调 `Owner.name(...)`，
并把同类兄弟文件一起编译；④ smoke 加四步：写出代理 → 编译 → record → gate。

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
| **c/cpp 的 3.5–3.8x slower** | 已排除两个猜测，都有数字：① 注册表派发不是原因（空循环 0.73 ns、typed 调用 0.74 ns、端口 body 1.05 ns，差 0.3 ns）；② 参数切片的局部性**在隔离实验里**是原因（17 个各自分配的 `[]any` 7.69 ns/call，共用一个 2.48 ns/call），但把驱动改成「每个函数一个连续底层数组、每个用例取子切片」后，同一个 crate 五次跑仍是 5 ns/5 ns/5 ns/5 ns/5 ns（改前 5/5/7/5/5）——没有可测收益，改动已回退 | 已定因：端口侧一个样本原来只有 17 次调用（约 85 ns 工作），两次 `time.Now()` 就占了三分之一——同一 crate 同一循环，17 次/样本读 4.882 ns，4352 次/样本读 3.973 ns。改成每样本 64 轮后 bench 稳定读 4 ns、比值 4x（原来 4.88 ns / 5x）。剩下的差距是结构性的：`[]any` 断言 + 装箱 + 接口调用（拆开测：调用本身 ~2.1 ns，逐个切片追指针 ~1.8 ns，空循环 0.33 ns；C 侧 1 ns 是 17 个内联常量调用）。已做到：`bench` 走 scaffold 生成的类型化入口后，c **3.28x → 1.72–2.07x**、cpp **3.31x → 1.72–2.10x**（原式 1 ns 对端口 2 ns，区间为多次全量 sweep 的最小/最大，越界会自动复测）；剩下的是签名下界，细节见 [PERF.md](PERF.md) 末节 |

**下一步候选（小）**：解析失败的文件里，来自损坏区域的导出（`def bad(:` 仍会产出一个 `bad`）现在只靠整单元的
「partial」告警覆盖。可以给这些导出单独标 `blocked`——判据是「函数节点自身 `HasError()`」，比整文件粒度更准。

## 四、可信度：继续加负向对照

`make negative` 现在 31 条（打出 36 行 ok：对照 4 与 33 各带一条确认行，33 又有三种语言的子断言）。已销账：
* `enable` 在非 TS/JS/Ruby 语言上不能静默成功 —— 对照 22，坏版本会把 JS 块写进 `.rs` 且 exit 0；
* 类方法被误判为可移植 —— 对照 23 盯住 `$this`（和 `self::` 不是同一个 token），同一文件里的纯方法必须仍然通过；
* `bench` 的折叠 —— 对照 24 断言生成的计时循环里没有任何字面量实参（查 harness 源码，不查纳秒：本机字面量
  harness 是 1 ns / 13.46x slower，`volatile` harness 是 3 ns / 6.99x slower）。

还要补的：无（第四节的旧清单已全部落地）。下一步看第五节：真实项目每语言 3 个。

## 五、真实项目：扩大到每语言 3 个

`scripts/survey-real.sh` 已能跑任意目录。下一步：把 Kotlin-arrow（3,170 导出）、fmt（483）、zlib（222）
里的**各一个真实单元**移植到 gate 绿，作为「真实代码能过」的证据，而不是只有玩具函数。
