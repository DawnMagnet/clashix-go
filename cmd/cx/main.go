package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/DawnMagnet/clashix-go/internal/cli"
)

// version is replaced at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := cli.Execute(version); err != nil {
		var exitErr *cli.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}
		fmt.Fprintf(os.Stderr, "cx: %v\n", err)
		os.Exit(1)
	}
}
