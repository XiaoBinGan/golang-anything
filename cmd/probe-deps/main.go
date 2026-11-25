// Imports the tool's packages and does nothing: isolates "how much do the dependencies cost to start"
// from "what the CLI does".
package main

import (
	_ "github.com/xiaobingan/go-anything/internal/analyze"
	_ "github.com/xiaobingan/go-anything/internal/app"
	_ "github.com/xiaobingan/go-anything/internal/bench"
	_ "github.com/xiaobingan/go-anything/internal/diff"
	_ "github.com/xiaobingan/go-anything/internal/mcp"
	_ "github.com/xiaobingan/go-anything/internal/pkg"
	_ "github.com/xiaobingan/go-anything/internal/report"
)

func main() {}
