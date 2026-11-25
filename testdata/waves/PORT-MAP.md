# Port map: waves

2 of 4 units have a Go port.

| Unit | Wave | Status | Exports | Crate | Cases | Last diff | Built |
| --- | ---: | --- | ---: | --- | ---: | --- | --- |
| `src/util.ts` | 0 | ready | 2 | `rust/util-go` | 56 | 56/56 | yes |
| `src/generic.ts` | 0 | blocked | 1 | — | — | not run | — |
| `src/widget.tsx` | 0 | blocked | 1 | — | — | not run | — |
| `src/format.ts` | 1 | ready | 1 | `rust/format-go` | 28 | 28/28 | yes |

## Not ported yet

In the order `analyze` suggests.

| Unit | Status | Lines | Why it stays |
| --- | --- | ---: | --- |
| `src/generic.ts` | blocked | 3 | 1 of 1 exports stay in the original |
| `src/widget.tsx` | blocked | 3 | 1 of 1 exports stay in the original |
