// The same imports as the CLI, and a main that only prints: isolates "what the packages cost to bring
// up" from "what the dispatch does".
package main

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"

	_ "github.com/xiaobingan/go-anything/internal/analyze"
	_ "github.com/xiaobingan/go-anything/internal/app"
	_ "github.com/xiaobingan/go-anything/internal/bench"
	_ "github.com/xiaobingan/go-anything/internal/bridge"
	_ "github.com/xiaobingan/go-anything/internal/buildinfo"
	_ "github.com/xiaobingan/go-anything/internal/diff"
	_ "github.com/xiaobingan/go-anything/internal/fastbridge"
	_ "github.com/xiaobingan/go-anything/internal/frontdoor"
	_ "github.com/xiaobingan/go-anything/internal/fuzz"
	_ "github.com/xiaobingan/go-anything/internal/mapdoc"
	_ "github.com/xiaobingan/go-anything/internal/mcp"
	_ "github.com/xiaobingan/go-anything/internal/parsehelper"
	_ "github.com/xiaobingan/go-anything/internal/pkg"
	_ "github.com/xiaobingan/go-anything/internal/profile"
	_ "github.com/xiaobingan/go-anything/internal/record"
	_ "github.com/xiaobingan/go-anything/internal/report"
	_ "github.com/xiaobingan/go-anything/internal/scaffold"
	_ "github.com/xiaobingan/go-anything/internal/shadow"
	_ "github.com/xiaobingan/go-anything/internal/switched"
	_ "github.com/xiaobingan/go-anything/internal/wasmbridge"
)

func main() {
	info, _ := debug.ReadBuildInfo()
	_, _ = info, fmt.Sprintf("%s", runtime.Version())
	fmt.Fprintln(os.Stdout, "probe")
}
