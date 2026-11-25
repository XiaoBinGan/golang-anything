// Command textcli is the sample CLI the app layer records: three flags, one subcommand, stable output.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

const version = "0.3.1"

func main() {
	help := flag.Bool("help", false, "print this help")
	ver := flag.Bool("version", false, "print the version")
	countSpaces := flag.Bool("count", false, "count the spaces in the argument after --")
	flag.Parse()

	switch {
	case *help:
		fmt.Println("textcli: count spaces, pad text")
		fmt.Println("  --version      print the version")
		fmt.Println("  --count TEXT   count the spaces in TEXT")
		fmt.Println("  --pad N TEXT   pad TEXT with spaces to width N")
		return
	case *ver:
		fmt.Printf("textcli %s\n", version)
		return
	}
	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "textcli: nothing to do")
		os.Exit(2)
	}
	if *countSpaces {
		fmt.Println(strings.Count(strings.Join(args, " "), " "))
		return
	}
	if len(args) >= 2 && args[0] == "pad" {
		var width int
		fmt.Sscanf(args[1], "%d", &width)
		text := strings.Join(args[2:], " ")
		for len(text) < width {
			text += " "
		}
		fmt.Println(text)
		return
	}
	fmt.Fprintf(os.Stderr, "textcli: unknown arguments %v\n", args)
	os.Exit(2)
}
