# 记录：克服了什么，优化了什么

这份文件从 `git log` 里挑出讲得清楚的提交，一条一行：标题是去掉 `type:` 前缀的提交标题，正文取提交说明的
**第一段**——也就是当时为什么改。它**不是**完整日志（很长一段时间里一条都没补），被漏掉的提交在 `git log` 里。
两条规则由 `scripts/doccheck.sh` 守着：每节标题上的条数等于该节实际条数；每个标题都必须对应一个真实存在的提交。
数字都在对应的文档里（[PERF.md](PERF.md)、[EVAL.md](EVAL.md)、[PARITY.md](PARITY.md)、[CONTRACT.md](CONTRACT.md)）。

## 一、修掉的错误（32 条）

每一条都是**工具说了假话**或**产物不对**，被一次具体实验抓到：

- **a method that touches state is not a function, and two controls had gone stale**
  - Carbon's static factories read and write class variables; analyze offered all 506 of them as portable, and a port would have recorded answers that depend on a call order nobody wrote down. A method body that touches @ivar, self.x, $this, self:: or static:: now blocks with that sentence: Carbon drops to 199 portable of 506, sinatra to 47 of 55.
- **the smoke test used a relative binary path and the language blocks cd away from it**
  - make smoke runs GO=bin/golang-anything, and the per-language checks cd into a temporary repository, so the relative path stopped resolving and four cases failed there while passing when run by hand with an absolute path. It is absolute from the top now.
- **crypto out of the binary, and a guard against my own cgo trap**
  - sha256 was used for one thing: a stable short id written into recorded-from.json and into the name of the block enable writes. It dragged crypto/internal/fips140, sha3 and sha256 into a CLI that does no cryptography — 0.19 ms of init for a value FNV-1a computes in nanoseconds. go list -deps now has no crypto at all, and the fingerprints were regenerated for all nine crates.
- **addon.node was tracked and changes on every build, and a pack test left a directory in the tree**
  - A fresh clone's make check failed on the stale-artifact rule for a reason the rule could not fix: port-rs's addon.node is a binary that build --fast rewrites and git was tracking it. internal/pkg/mk-darwin-arm64 was the output of a test that ran npm pack in the source tree.
- **seven controls passed for the wrong reason in an environment without GOROOT or node**
  - Running make negative from a shell that had exported neither produced 'compile: version go1.22.5 does not match go tool version go1.26.2' for the Go ones, and for the JavaScript ones it produced silence: nothing ran, so nothing differed, so the control reported success.
- **three PARITY rows were stale, and the stated counts are checked now**
  - Row 23 said seven MCP tools; there are fifteen, and have been since round 48. Row 25 listed doctor's checks without the two that matter most (does go actually compile something, and where is the parser). Row 26 did not mention that check runs every case twice.
- **the wasm fast path was dead code for nine rounds, and the startup axis was measuring the machine**
  - portStyle was declared in round 34 and never filled, so every generated wasm wrapper went through the registry and the typed direct call that measured 51 ns was never generated. The bridge has been 0.23 µs a call since then — slower than rust-anything's napi — while PERF.md kept quoting 0.051. detectPortStyle now reads port.go with go/parser, recognises a hand-written signature and its exported spelling, and the measurement is back to 0.049-0.055 µs across three runs.
- **the stale-artifact rule only ran bridge, and portlib was stale in eight crates**
  - cmd/portlib/main.go is written by build --fast, and eight of the nine crates had a committed copy that predated the round-40 fix to the one-string fast path. The rule that checks for staleness only ran bridge, so it could not see them; it runs both now, and leaves wasm out because that output is not reproducible byte for byte.
- **neither path could see a lost sign of zero**
  - Breaking the bridge's encode and the driver's mark in turn showed both checks green, which meant two real gaps rather than two lucky breaks.
- **three scripts reported verdicts and always exited 0, and a target that proves it**
  - matrix printed FAIL in a column for a broken port and exited 0, perf printed a traceback or a table of zeroes for a missing rig and exited 0, and the contract had no way to notice either. All three now carry their status out; perf refuses to measure when either side is missing or does not diff, which is what its own header already claimed.
- **doctor searched for the parser itself and disagreed with the code that starts it**
  - The second run of the real MCP client made doctor report MISS parser available while golang-anything-cgo sat beside the binary: doctor had its own copy of the search, looking at GOLANG_ANYTHING_PARSE and PATH but not next to the executable, while ts.helperPath looked in all three.
- **three committed generated files were stale, and a rule that would have said so**
  - Regenerating every crate in a fresh clone changed three tracked files: port-rs's answer.go still lacked the Undefined marker, and two index.cjs files still carried the wire helpers inline before round 44 moved them to internal/jswire. Anyone reading those files in the repository was reading code the tool no longer writes.
- **a fresh clone has none of the generated files, and the matrix assumed it did**
  - index.cjs, cmd/portctl, answer.go and the addon are all generated and all gitignored, so 'make check' in a clone failed with 'Cannot find module index.cjs' — after it had already failed on a test in the wrong package and a table test reading a literal that moved.
- **shadow never compared a throw, and a signal crossed as {}**
  - shadow's wrapper called the original outside a try, so a unit that raises took the wrapper down and nothing was recorded: the only comparison it made was between two answers. A real throwing unit (jadx-go) showed it three ways — both raise, the port stops raising, the port raises another kind — and only the first was invisible-because-broken.
- **fuzz could not see a slash, and a skipped call read as a regression**
  - Four things, each found by running fuzz on the real crates.
- **a broken escaper passed the gate, because no case contained a slash**
  - escapeJsonPointerToken was recorded 64 times and not one argument held '~' or '/', the two characters the function exists for. Returning the input unchanged passed the gate. The string table now carries structural characters — / ~ . : = & ? # % + @ , * ! and the brackets — and the same broken port fails with nine named cases.
- **static methods recorded nothing silently, and class instances recorded as {}**
  - Two shapes from the real repository, both wrong before.
- **analyze called 168 unbridgeable exports ready**
  - Measured on the real repository: 103 exports return Promise<T> written on an arrow rather than with async (which the existing rule looked for), 55 carry a Map, Set, WeakMap or WeakSet, and 10 carry a symbol. All of them were marked ready and would have recorded cases that answer 'cannot read property of undefined' or compared two different inventions of the same value.
- **a function parameter is not bridgeable, and an empty recording broke the driver**
  - src/kernel/trimEdges.ts takes a predicate callback. analyze called it ready with three bridgeable exports, scaffold stubbed it, and record produced calls like edgeSpan("a", 0) that answered 'TypeError: predicate is not a function' — cases that prove nothing about the port. A parameter whose type carries '=>' now blocks the export with that reason, and the unit reads blocked instead of ready.
- **check called -0 equal to 0, so two wrong ports passed**
  - check.mjs and shadow.cjs each had a canon(). Rewriting negativeZero() to return 0 made gate and check report green, because JSON.stringify(-0) is '0' and NaN and undefined both stringify to null: the comparison could not tell apart the values the wire exists to carry. shadow's canon already spelled them; check's did not, and nothing compared the two.
- **the cross-bridge check takes a relative path like every other script**
- **arguments lost their markers on the way in, on all three bridges**
  - The marker fixture answered correctly through the driver and wrongly through every bridge, because only the answer direction had markers: JSON.stringify([-0]) is '[0]', so isNegativeZero(-0) came back false everywhere a caller would actually look.
- **three copies of one rule, and a fixture that found all three**
  - testdata/markers has one function per value JSON cannot carry — NaN, -0, Infinity, a lone surrogate, undefined — because a marker one bridge writes and another does not read is a silent mismatch. Its 82 cases gate, check through the addon and fuzz green, and finding that out took three fixes.
- **one big MCP line ended the server, and fuzz could not see what record could**
  - An MCP client sending a request larger than the scanner buffer got 'token too long' and the server stopped: every later request in that session was lost. The reader is a bufio.Reader now, lines are capped at 64 MB, an oversized one is answered with -32600 and the stream continues. Fifty pipelined requests answer in order, string ids come back unchanged, and a missing or wrongly typed params is -32602.
- **a hand-edited block says so instead of 'nothing to remove'**
  - disable found the opening marker but no closing one and answered 'was not switched on: nothing to remove', which sends a person looking in the wrong place and hides that their own edit is the reason. It now names the crate, the file and the missing marker, and removes nothing — including not taking the code that follows the block with it, which the new test checks.
- **a recording is published by rename, and paths with spaces are covered**
  - record wrote cases.jsonl in place, so a gate running at the same time could read half a file. It now writes cases.jsonl.staged and renames it into place: a reader sees the old recording or the new one.
- **an empty recording passed the gate, and one long value printed 150 KB**
  - Four adversarial cases against a throwaway crate, three of which the tool got wrong.
- **doctor trusted a broken toolchain, and the plan listed the tool's own output**
  - doctor said 'ok go' and 'ok cgo' while GOROOT pointed at a directory that does not exist — the exact state that had earlier rounds chasing 'compile: version go1.22.5 does not match go tool version go1.26.2'. It now compiles a one-line program and reports the compiler's own sentence when that fails. It also reports whether the parser is available: a cgo build is its own parser, a fast build needs golang-anything-cgo, which the fast build now finds on PATH as well as beside it.
- **two ways a green gate lied, and the table that let the second one happen**
  - The first: diff reused the driver binary that 'build' had left, without checking whether the port still compiled. So a shellQuote port with an unused import, which cannot build at all, came back 'every case matches' — it was answering from the old binary. The driver is rebuilt when any .go file in the crate is newer than it.
- **a packaged port answered promises, and three commands could not load a unit with imports**
  - Packaging a real crate and installing it for real (npm install of the generated package) showed the wrapper exporting async functions even with the addon loaded: a drop-in for a synchronous original that hands back a promise is not a drop-in. With the addon present the function is the synchronous call and port.async keeps the promise form for the pipe.
- **enable could not switch a const export, and shadow could not load a unit with imports**
  - Two real-crate findings from running shadow and enable on uleb-go rather than on a fixture.
- **the case generator was a cross product, and a green gate hid an untested export**
  - The comment said each argument position walks its own table; the code crossed every table against every other, which is 4,000 calls for two string parameters. Two runs replace it: a paired walk with a per-position stride, and a bounded cross product over the first six values of each position. Cheap values are tried first, because a table that opened with 1,000,000 filled the answer limit and left whole exports with nothing recorded.

## 二、性能（11 条）

每条都有实测数字；赢的和没赢的都写着：

- **the help path skips fmt, and the startup gap is decomposed to the microsecond**
  - Measuring four binaries that differ by one property each says where the time goes: a 2 KB Go program that prints the same 2866 bytes starts at 2.08-2.21 ms and is faster than rust-anything; padding the same program to 3.6 MB changes nothing (-0.002 ms); our binary adds 0.24-0.32 ms of its own. Inside that: 0.02 ms of package init over the floor's eleven packages, 0.067 ms inside main, and about 0.15 ms of runtime setup for 118 linked packages.
- **the dispatcher split is worth 1 to 4.5 percent, measured three ways**
  - Three prototypes, measured in one run against the same rust-anything binary: a help-only dispatcher is 1.745 ms of CPU against 1.827, keeping every non-parsing command lands at 1.809, and the single binary is 1.917.
- **CPU time pins the startup gap at 0.076 ms, and three more attempts to close it**
  - Wall clock cannot resolve it, so the measurement moved to children CPU time, which scheduling noise does not move: golang-anything 1.768 ms, rust-anything 1.692, a Go program that only prints 1.558.
- **the 0.1 ms startup gap is real, and it is the Go runtime**
  - 20 rounds of 40 spawns, alternating, bootstrapped 2000 times: golang-anything 2.194 ms against rust-anything 2.066, difference +0.129 ms with a 95% interval of [+0.100, +0.149]. It is not noise.
- **diff is four times faster, and the harness now proves both sides run**
  - diff went from 14.6 to 8.8 ms for 609 cases because the driver binary build already makes is what runs: 'go run' compiled on every comparison and cost ten milliseconds before a case was read.
- **measure analyze on a real repository, where the helper overhead disappears**
  - 7390 .ts plus 3839 .js: golang-anything analyzes it in 8.46 s, rust-anything in 42.00 s. On 2000 .ts files it is 0.044 s against 0.201 s. The 3 ms process start the fast build pays for its parser is noise against real work, and the startup axis reads differently per command: help is a tie at 2.42 against 2.35, analyze on a tiny fixture loses, analyze on a real repo wins five times over.
- **take cgo out of the fast path, and measure what it was worth**
  - cgo costs a fixed 1.2 ms before any of this tool's code runs: a program that only imports C and calls malloc starts in 3.19 ms against 2.00 ms for an empty Go binary. So the tool is built without cgo (2.42 ms) and the parser is a second build of the same program, started as 'golang-anything __parse' by the commands that read TypeScript. One source, two builds, no sidecar to keep in step.
- **four architectures measured, three rejected, and the reason each falls**
  - wasip1 leaf export: 18.1 ns per call, four times faster than napi, and it aborts the runtime the moment the function does real string work (exports run on a fixed g0 stack, so utf16.Encode's growstack lands in badmorestackg0). js/wasm callbacks that do work: 10.3 us per call, 140 times slower, because every call goes through the Go scheduler. The ring was already rejected at 182 ns.
- **measure the floor of two architectures before building either**
  - A shared-memory ring sounded right and is not: two JS threads passing a counter through a SharedArrayBuffer round-trip in 182 ns, 2.5x slower than rust-anything's napi call, and a cross process version only adds to that. An in-process wasm call measures 8.2 ns — 8.9x of headroom under 73 ns, which is the only architecture left that can win.
- **a typed batch beats rust-anything's single call, and single calls cannot**
  - Three entries now, measured: the general JSON path (1.4-2.0 us), one string argument without JSON (0.215 us), and a batch that takes one UTF-8 blob plus a length table and writes answers into a Float64Array (0.051 us per call). rust-anything's napi single call is 0.073 us.
- **measure both bridges head to head and report where we lose**
  - Built rust-anything 4.0.0 here, bridged the same unit with both tools, filled both ports and timed one call alternately in blocks so a load spike hits both sides. rust-anything's napi bridge answers in 0.073 us, ours in 0.294 us after the one-string fast path (no JSON, no malloc, quote written straight into the caller's buffer). The gap is structural: cgo costs 40-60 ns per call.

## 三、能力（29 条）

功能与语言支持，每条都在提交里说明了如何验证：

- **Ruby takeover — enable, call through the port, disable byte for byte**
  - Ruby can reopen a module and redefine a method, so the block goes at the end of the file and disable lifts it out exactly. A module function is reached through its owner, which the manifest now carries; a top-level def goes on Object as a private method.
- **class methods for Ruby and PHP, which is what a real library is made of**
  - The survey said almost nothing was portable in sinatra (0 units) and Carbon (3 exports) because both are class libraries and only free functions were read. Ruby module functions (def self.x, module_function) and PHP public static methods are read now: sinatra goes to 454 exports in 47 units, Carbon to 506 in 37.
- **bench works in all ten languages, and a script that proves it**
  - PHP, Kotlin, C# and PowerShell had no timing harness: bench fell through to node and died. Each now has one, generated the way its recorder is, and scripts/bench-all-languages.sh runs the sweep from a clean scaffold so a harness that does not compile fails in a test rather than in front of a user.
- **PHP, Kotlin, C# and PowerShell, each recorded by its own runtime**
  - Four more languages, and the runtimes were installed rather than assumed (php 8.6, kotlin 2.4, dotnet 11, pwsh 7.7). Every one of them records with its own tool and produces cases the same gate checks.
- **Ruby, Java, C and C++, each recorded by its own runtime and proved end to end**
  - Four more languages, and the only part that had to be written per language is the recorder: everything after it — the driver, the bridge, gate, diff, check — reads cases.jsonl and knows nothing about where the answers came from.
- **Python, the second language, and an honest census of the ones we cannot read**
  - rust-anything reads eleven languages; this reads two. That gap is now in PARITY.md instead of being implied away by a 26/26 command count.
- **split the front from the engine, so --help stops paying for the 27 commands it describes**
  - The startup axis was structurally lost: one binary carrying every command loads 118 packages before it can print a constant. It is two binaries now — a 1.7 MB front that answers --help, --version and no arguments, and the engine that everything else execs into (syscall.Exec, so no second process and no signal forwarding to get wrong).
- **the bridge measurement fails when the wasm path loses, and a control that proves it**
  - The axis that was dead for nine rounds is now a probe that exits non-zero when the wasm bridge is not faster than napi, and perf.sh carries that status out: a run where the axis lost is a failed run, not a table to read.
- **the documents are checked against the tool, which found the invented-command count wrong**
  - PARITY.md said three invented commands; the set difference between --help and its own left column is four — scaffold was never listed. scripts/doccheck.sh computes it instead of trusting the sentence, checks every command against PARITY.md and README.md, and checks every MCP tool against README.md. It runs in contract.sh and has a negative control of its own.
- **each of the three bridges gets its own negative control, and the pipe can be forced**
  - allbridges.mjs asked the addon and the wasm module and called the third column 'stdio' while it was the addon again through a different entry point: the pipe was never tested on a machine where the addon is built. GOLANG_ANYTHING_NO_ADDON=1 forces it, and allbridges now runs a second pass through it and compares against the wasm bridge.
- **install the package the way a consumer does, through npm and a registry**
  - The package path had only ever been tested by hand-assembling node_modules, which skips everything npm does: optionalDependencies resolution, tarball packing, integrity checks.
- **the MCP server answers as many commands as the CLI, and one client proved it**
  - Tested with the official SDK client for the first time instead of printf'd JSON-RPC, which found three things at once.
- **a ninth crate end to end, and the two bugs a fresh unit still finds**
  - testdata/realworld5 is jsonPointer.ts from the real repository, taken through record, a hand-written port, gate, check, fuzz, shadow, enable and disable. It found two things forty rounds in.
- **analyze blocks lazy and streaming returns, and says what stays behind and why**
  - Generators, Iterables, AsyncIterables and streams cannot cross a bridge that answers in one turn: the first three are lazy and nothing can pull them, the last are long-lived objects rather than values. ready drops from 5954 to 5925 on the real repository.
- **package writes the step that turns an artifact into the platform package**
  - The generated npm loader asks for '<name>-<platform>', and nothing produced that package: the workflow builds and uploads artifacts, which is where the story stopped. package now also writes pack.mjs, which takes one built artifact and writes the platform package next to it; a test runs it and checks the manifest, and a real assembly of the markers crate answers -0 through the installed package.
- **record the tool against itself, and give the CLI a version**
  - app record/replay had only ever been exercised on a throwaway CLI in /tmp. Recording golang-anything's own behaviour and replaying it is a stronger test: six cases, all identical, and after changing one character of the help text three of them name the difference (--help stdout, --version stderr, no arguments stderr).
- **load parameter properties, and give a signal a value on the wire**
  - Two more real units. throwIfJadxInterrupted(signal?: AbortSignal) is the whole shape at once: an optional parameter, a platform object, and a throw path.
- **a byte-container dialect, an undefined marker, and a real uint8 unit that found both**
  - testdata/realworld3 is elfAttributeUleb from the real repository: (Uint8Array, number, number) -> {value, next} | undefined. It broke three things at once.
- **port two units from a real repository, through the whole chain**
  - testdata/realworld holds two files copied verbatim from a 7390-file TypeScript repository, and both crates go scaffold, record, hand-written port, gate, bridge, build --fast, check and fuzz.
- **record a unit that imports without an extension, and a third crate to prove it**
  - A real source file writes './util', not './util.ts'. Node's ESM resolver wants the extension, so recording such a unit failed outright — the fixture's second unit had never been ported for that reason. record now writes a loader hook that tries .ts, .tsx, .js, .mjs and .cjs in turn.
- **a typed port behind a wasm reactor beats napi at 53 ns a call**
  - Three changes, each measured on its own. -buildmode=c-shared turns the wasip1 module into a reactor so real Go runs inside the export. Per-signature exports (go_Count(ptr, len) float64) remove the registry lookup and the JSON. Typing the port's own signature — Count(string) (float64, error), with a generated adapter for the registry — removes the []any boxing and the type assertion, which is the step that took 194 ns down to 53 ns.
- **the wasm bridge works, and the blocked reason last round was wrong**
  - wasip1 without -buildmode=c-shared is a command module: it initialises in _start and every export aborts with notInitialized. With -buildmode=c-shared it is a reactor, real Go runs inside the exports, and strings and allocation are fine. I had diagnosed that abort as a fixed g0 stack and was wrong; PERF.md now carries the correction.
- **front-door, wire and lift — the last three commands, 26 of 26 aligned**
  - front-door new lays out a Go binary that owns the entry point: dispatch matches the recorded argv, a wired command runs the port, everything else runs the original with the same argv, stdin and environment, and GOLANG_ANYTHING_TRACE records which path each command took. wire connects one recorded command to a port function and refuses a command that was never recorded. lift only removes the fallback when every command is wired, and takes os/exec with it.
- **mcp and setup — an agent can plan and check a port without a shell**
  - mcp answers JSON-RPC on stdio with seven tools (analyze, map, diff, gate, record, fuzz, profile). Protocol details are the ones a client depends on: notifications get no reply, an unknown method is -32601, a non-JSON line is -32700, and a tool that fails answers content with isError so the agent can read why instead of seeing a broken stream. setup registers the server with the agents already on the machine, adds only its own key, and is idempotent.
- **profile and go — decide what to port from what actually runs**
  - profile runs a workload under node --cpu-prof and ranks hot functions by self time, maps each onto the plan's unit, and drops the runtime's own frames and node_modules. go chains it: profile, take the hottest portable unit, scaffold, record and write the wrappers, so one command leaves a crate that already passes gate on 609 cases.
- **package and the app layer — users can install it, and the whole program is proved**
  - package lays out three shapes (library, cli, service): an npm package with one sub-package per platform, a Go module, a Dockerfile, and a workflow that builds the rest and publishes nothing. A rerun replaces exactly the files the previous run wrote, so changing shape cannot leave a half package behind.
- **fuzz, refresh, report — the recording stops being the only evidence**
  - fuzz throws fresh inputs from a seed at both sides; the seed is printed so a finding is replayable, and --keep turns a failed call into a case that gate then insists on. refresh regenerates the binding files and leaves port.go alone. report writes REPORT.md and card.svg from a run made on the spot: 609/609 cases, 1.91x, one throw path proved — and a failing report is drawn in another colour instead of claiming a pass.
- **enable, disable and shadow — the port gets used, and proved before it is**
  - enable inserts one marked block that rebinds the unit's exports to the Go port through the in-process addon; disable takes it out and the file comes back byte for byte (asserted in a test). Both refuse to run when the crate was never bridged or an export name is unknown, because a switch that lands nowhere is worse than no switch.
- **bench, map, clean, doctor, check, and the in-process bridge**
  - bench times both sides the same way; map writes PORT-MAP.md; clean and doctor round out the day-to-day commands. The fast bridge is a c-shared library plus a hand-written N-API addon: 0.73 us per call against rust-anything's ~1 us, and 16x faster than the stdio process.

## 四、让检查本身可信（5 条）

负向对照与冒烟测试——这些不增加功能，只增加「结论可信」：

- **a control for Ruby disable, and two ways it was passing without testing**
  - The control that a hand-edited block must be refused was green for two wrong reasons before it worked: negative.sh used a relative $GO, so the controls that cd into a crate exited 127 and 'failed' as expected; and enable silently did nothing because portctl had not been built, so there was no block to edit.
- **zlib's adler32.c end to end, and three C rules the real source forced**
  - A real unit rather than a toy: analyze, record 454 cases from the actual zlib source, gate 454/454, check 908/908, bench, report.
- **ten real projects surveyed, and the three bugs they found**
  - Downloaded the main library of each language and analysed it: kotlin-arrow gives 3170 bridgeable exports in 759 ms, cpp-fmt 478 in 196 ms, c-zlib 331, java-gson 83, python-click 75, python-requests 62. Two projects give almost nothing — php-carbon 3 and ruby-sinatra 0 — because they are class libraries, and a bridge calls plain functions.
- **smoke every command and every flag, because the table is a claim and this is the check**
  - scripts/smoke.sh runs 48 invocations on a throwaway copy — every command, and every flag that changes what it does: record --only/--inputs/--test-coverage, build/build --fast/wasm, gate/diff/check/bench/fuzz, enable/--only/--async/disable, shadow/shadow-report, report/report --out, the three package shapes, app record/replay, front-door new/wire/lift, profile, go, setup, mcp. make check runs it too.
- **one script that checks every claim, and the two ways it lied before it did**
  - scripts/evidence.sh runs the whole set: the capability matrix, app record/replay against a real CLI, mcp over stdio against real crates, a generated npm package that is installed and called, doctor against a GOROOT that does not exist, and the plan being free of the tool's own generated files.
