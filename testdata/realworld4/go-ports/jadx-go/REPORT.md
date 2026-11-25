## `src/android/JadxInterruption.ts` ported to Go

| | |
| --- | --- |
| Crate | `github.com/xiaobingan/go-anything/testdata/realworld4/go-ports/jadx-go` |
| Recorded cases matched | 2 / 2 (100.0%) |
| Speedup, call included | 9.47x (geometric mean) |
| Throw paths with a recorded failing case | 1 of 1 |
| Bridge | none built |
| Measured | 2026-11-25 23:30 on darwin/arm64, 10 CPUs |

| Function | Cases | Original per call | Go per call | Speedup |
| --- | ---: | ---: | ---: | ---: |
| `throwIfJadxInterrupted` | 2 | 103 ns | 11 ns | 9.47x |

Every number above comes from `go-anything diff` and `go-anything bench` on this crate; regenerate with `go-anything report github.com/xiaobingan/go-anything/testdata/realworld4/go-ports/jadx-go`.
