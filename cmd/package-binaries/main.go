// Command package-binaries builds Release Planner's release archives and checksum manifest.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/fabricahq/release-planner/internal/distribution"
)

func main() {
	version := flag.String("version", "", "release version to stamp, such as v0.1.0")
	out := flag.String("output", "", "new directory for the archives and SHA256SUMS")
	source := flag.String("source", ".", "Release Planner source checkout")
	flag.Parse()
	if *version == "" || *out == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: package-binaries --version <tag> --output <dir> [--source <dir>]")
		os.Exit(2)
	}
	files, err := distribution.Build(context.Background(), *source, *version, *out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, f := range files {
		fmt.Println(f)
	}
}
