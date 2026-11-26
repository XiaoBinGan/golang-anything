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

**现状**：只有 TS/JS（重绑定导出）与 Python（包一层）。其余八种语言的 `enable` 会明确拒绝。
**要做什么**：Ruby 用 `alias_method` + 覆盖、PHP 用 `class_alias`/装饰、Java/Kotlin/C# 用静态转发。
**怎么算做完**：每种语言一个 `enable → 调用 → disable → 字节一致` 的测试，像 Python 现在那样。

## 三、性能：没有赢下来的两处，要么赢要么写死理由

| 轴 | 现状 | 计划 |
| --- | --- | --- |
| 桥（wasm） | 已赢 1.3–1.5x | 保持；`perf.sh` 已在输掉时返回非零 |
| diff / record | 已赢 5–7x / 1.4x | 保持 |
| 启动 | 已赢（拆 front/engine 之后） | 保持 |
| **N-API addon 单次调用** | 慢 2.5–2.8x | cgo ABI 下限；**不再追**，文档写明 wasm 是主推路径 |
| **C 的 bench** | 32x「慢」是因为 clang 把纯函数折叠成 0 ns | 在 bench 里检测 `ns == 0`，标注「原式被常量折叠，此数字无意义」 |

## 四、可信度：继续加负向对照

`make negative` 现在 19 条。要补的：
* 类方法被误判为可移植（构造一个带 `$this->` 的反例）；
* `enable` 在非 TS/JS 语言上不能静默成功；
* `bench` 遇到被常量折叠的原式必须标注而不是报慢。

## 五、真实项目：扩大到每语言 3 个

`scripts/survey-real.sh` 已能跑任意目录。下一步：把 Kotlin-arrow（3,170 导出）、fmt（483）、zlib（222）
里的**各一个真实单元**移植到 gate 绿，作为「真实代码能过」的证据，而不是只有玩具函数。
