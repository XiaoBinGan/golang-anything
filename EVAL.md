# 评测：golang-anything vs rust-anything

一条命令复现：`./scripts/eval.sh`（`ROUNDS=40` 可加采样）。规则：

* 两侧各跑**自己的** crate，各自先过 `diff`——拿真跑和失败跑比快慢没有意义；
* 每轮两个工具**随机顺序**各跑一次，报的是**配对差值的中位数 + bootstrap 95% 区间**；
* 区间跨零就写「分辨不出」，不拿中位数当结论；
* 启动这一轴以**子进程 CPU 时间**判定，wall 只作参考（机器一忙，wall 量的是排队）。

机器状态写在表头（`load average`）。这台机器是开发机，load 常在 5–21 之间。

Load average at the start: **17.51**. Paired rounds, medians with a bootstrap 95% interval.

| axis | golang-anything | rust-anything | difference | verdict |
| --- | ---: | ---: | ---: | --- |
| startup --help (cpu) | 1.57 ms | 1.64 ms | -0.06 ms [-0.12, -0.02] | faster |
| startup --help (wall, best of 40) | 2.19 ms | 2.29 ms | -0.11 ms | faster |
| startup --version (cpu) | 1.78 ms | 1.74 ms | -0.04 ms [-0.12, +0.02] | indistinguishable |
| startup --version (wall, best of 40) | 2.21 ms | 2.31 ms | -0.10 ms | faster |
| startup no arguments (cpu) | 3.08 ms | 2.59 ms | +0.26 ms [-0.07, +0.65] | indistinguishable |
| startup no arguments (wall, best of 40) | 2.37 ms | 2.40 ms | -0.03 ms | faster |
| diff | 10.5 ms (328 cases) | 57.0 ms (917 cases) | -44.2 ms | faster — 32.0 vs 62.1 µs/case |
| record | 108.9 ms (328 cases) | 145.3 ms (917 cases) | -36.4 ms | faster |
| rss --help | 4.0 MB | 3.1 MB | | |
| binary size | 1.7 MB | 27.3 MB | | |

```
golang-anything callSync (cgo addon)   0.245 µs/call   4,083,855 calls/s
  rust-anything napi                 0.116 µs/call   8,652,390 calls/s
  golang-anything wasm (in-process)      0.084 µs/call   11,970,077 calls/s
VERDICT wasm faster than napi: 1.38x (wasm 0.084 µs, napi 0.116 µs)
```

Correctness: `gate` on the go crate: passes.
Both tools passed their own `diff` before any timing started.
