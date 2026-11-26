## `src/util.ts` ported to Go

| | |
| --- | --- |
| Crate | `github.com/XiaoBinGan/golang-anything/testdata/waves/rust/util-go` |
| Recorded cases matched | 108 / 108 (100.0%) |
| Speedup, call included | 0.44x (geometric mean) |
| Throw paths with a recorded failing case | 0 of 0 |
| Bridge | stdio process |
| Measured | 2026-11-25 23:30 on darwin/arm64, 10 CPUs |

| Function | Cases | Original per call | Go per call | Speedup |
| --- | ---: | ---: | ---: | ---: |
| `pad` | 54 | 16 ns | 35 ns | 0.46x |
| `repeat` | 54 | 15 ns | 35 ns | 0.42x |

Every number above comes from `golang-anything diff` and `golang-anything bench` on this crate; regenerate with `golang-anything report github.com/XiaoBinGan/golang-anything/testdata/waves/rust/util-go`.
