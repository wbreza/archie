package main

import (
	"os"

	"github.com/wbreza/archie/internal/cli"
)

func main() { os.Exit(cli.Run(os.Args[1:], os.Stdout)) }
