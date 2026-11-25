# 共享契约：哪些逻辑必须只有一份实现

这份文档不是愿望清单，是一次次 bug 换来的。每条都写清楚：**曾经出过什么事**，以及**现在由什么守住**。

规则只有一条：**同一份判断，如果它在两个地方被写第二遍，第二遍迟早会漏掉第一遍后来学到的知识。**

## 一、必须单点实现的东西

| 逻辑 | 唯一实现 | 曾经漂移出的 bug |
| --- | --- | --- |
| 生成器能产生哪些字符 | `internal/pool` | `record` 学会了引号、结构字符、斜杠，`fuzz` 三次都没跟上；每次都是「A 能抓、B 抓不到」 |
| 线协议的 `decode` / `encode` / `canon` | `internal/jswire` | `check` 判 `-0 == 0`，`shadow` 判不等——返回 0 而不是 -0 的端口过了一个、挂在另一个 |
| 一次调用的身份（配对键） | `diff.CallKey` | `fuzz` 有一份拷贝；零参数函数录制写 `"args":[]`、驱动省略该字段，两边 key 一个 `[]` 一个 `null`，永远配不上 |
| 错误类型的比较 | `diff.same` + `shadow.cjs` 的 `errorKind` | `shadow` 的包装把原式调用放在 try 外面，原式一抛错包装就炸，**抛错路径从未被比对** |
| 单元能否过桥的判断 | `analyze` 的 `unbridgeableKind` / `isFunctionType` | 函数参数、`Promise`、`Map`/`Set`、`Iterator`、流：436 个导出曾被标成 ready 而实际不可移植 |

## 二、必须被生成器覆盖的东西

| 形状 | 为什么 | 守住它的是什么 |
| --- | --- | --- |
| 会改变函数行为的标点 `' " \ / ` \| ; & < > ( ) [ ] { } * ? ~ # % ! . , : = @ + $` | `shellQuote` 的转义、JSON Pointer 的 `~`/`/`——删掉转义仍然全绿过 | `pool_test.go` 里**独立写死**的清单（不是读同一个常量） |
| `-0`、`NaN`、`±Infinity`、`undefined`、孤立代理项、`bigint` | JSON 装不下，端口若返回「看起来一样」的值就没人发现 | `testdata/markers`：每个值一个函数，三个桥都跑 |
| `Uint8Array` / `AbortSignal` / 日期 / Map / Set | 平台对象没有自有可枚举字段，`{}` 会让「会抛错的端口」看起来「永不抛错」 | `record` 的方言表 + `fuzz` 的同一个池 + `jswire.Encode` |
| 零参数、多参数、对象返回、可选参数、抛错 | 每种都出过一次「用例录到了但证明不了任何东西」 | 九个 crate，其中四个来自真实仓库 |

## 三、必须被两条路径都验证的东西

`gate` 走驱动，`check` 走调用方真正用的桥。**两者必须同时绿**，否则就是「驱动对、桥错」：

```
gate    → 驱动（cmd/godriver）
check   → index.cjs（addon 或 stdio）
shadow  → 真实调用，原式与端口并排
```

`scripts/matrix.sh` 强制三列全过；落回 stdio 会标 `stdio!` 并计为失败。

## 四、已被证明的「不能靠人肉记住」

这些规则都曾经在**我修好一次之后又出现**：

- 标点：第 21 轮修 `record`，第 32 轮发现 `fuzz` 没有，第 42 轮又发现 `fuzz` 缺 `/`
- 假绿灯：第 16 轮加「导出必须有录制用例」，但只看 blocked 导出 → 第 29 轮空录制仍然通过
- 陈旧证据：`diff` 复用旧的 driver 二进制 → 编不过的端口报「every case matches」

**结论**：靠记忆和自觉不够，靠测试和单一实现才够。

## 五、每个结论都要能被推翻（`scripts/negative.sh`）

一份不能变红的检查是装饰。`make negative` 逐条把下面这些弄坏，**14 条都必须返回非零**：

| 弄坏什么 | 谁必须抓到 |
| --- | --- |
| 端口原样返回输入 | `matrix` |
| 把 `canon` 抄回 `shadow.go` | `contract` |
| 往 `cases.jsonl` 塞一条不成立的用例 | `evidence` |
| 桥的 `encode` 丢掉 `-0` | `check`（而 `gate` 必须仍然通过，证明两条路径是分开的） |
| 驱动的 `mark` 丢掉 `-0` | `gate` |
| wasm 包装的 `encode` 丢 `-0` | `allbridges` |
| pipe 二进制把参数丢掉 | `allbridges` |
| addon 里编译进去的 `mark` 丢符号 | `allbridges` |
| 端口答错（shadow 在跑） | `shadow-report` |
| 端口不再抛错 | `shadow-report` |
| README 里少写一个命令 | `doccheck` |
| 缺 rust-anything 一侧的 rig | `perf`（拒绝测量） |
| loader 返回写死的错答案 | `npm-install-check` |

**用法**：改完任何一处共享逻辑，先跑 `make negative`——它比 `make check` 更能说明你没改坏东西。
