package main

import (
	"context"
	"fmt"
	"os"

	"cedil-rss/internal/cedilrss"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "cedil-rss: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	return cedilrss.Run(ctx, cedilrss.DefaultConfig())
}
