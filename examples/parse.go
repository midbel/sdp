package main

import (
	"flag"
	"fmt"
	"io"
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

	f, err := sdp.Parse(io.TeeReader(r, os.Stdout))
	if err != nil {
		fmt.Fprintln(os.Stderr, "parse:", err)
		os.Exit(2)
	}
	// fmt.Printf("%+v\n", f)
	fmt.Println("---")
	fmt.Println("medias:")
	for _, m := range f.Medias {
		addr := m.ConnInfo.Addr
		if addr == "" {
			addr = f.ConnInfo.Addr
		}
		fmt.Printf("- %s: %s", m.Media, addr)
		fmt.Println()
	}
}
