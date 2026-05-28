package main

import (
	"fmt"
	"os"

	"github.com/w1ndy/zjunet-go/pkg/cli"
)

func main() {
	if err := cli.Run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "zjunet-go: %v\n", err)
		os.Exit(1)
	}
}
