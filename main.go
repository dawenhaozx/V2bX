package main

import (
	"os"

	"github.com/InazumaV/V2bX/cmd"
)

func main() {
	if err := cmd.Run(); err != nil {
		os.Exit(1)
	}
}
