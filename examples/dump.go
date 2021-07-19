package main

import (
	"flag"
	"fmt"
	// "io"
	"os"

	"github.com/midbel/sdp"
)

func main() {
	flag.Parse()
	r, err := os.Open(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "open:", err)
		os.Exit(1)
	}
	defer r.Close()

	f, err := sdp.Parse(r)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parse:", err)
		os.Exit(2)
	}
	raw := f.Dump()
	fmt.Println(raw)
}
