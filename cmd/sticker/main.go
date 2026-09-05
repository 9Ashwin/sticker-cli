package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/9Ashwin/sticker-cli/internal/cli"
)

var version = "dev"
var commit = "unknown"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr, version, commit))
}
