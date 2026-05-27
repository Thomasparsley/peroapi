package main

import (
	"os"

	"github.com/Thomasparsley/peroapi/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
