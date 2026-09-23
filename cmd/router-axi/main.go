package main

import (
	"context"
	"io"
	"os"

	"github.com/Azd325/router-axi/internal/app"
	"github.com/Azd325/router-axi/internal/tr064"
)

var version = "dev"

func run(args []string, stdout, stderr io.Writer) int {
	application := app.New(func(config app.Config) (app.Reader, error) {
		return tr064.New(config.Host, config.Username, config.Password, nil)
	}, os.Getenv)
	application.Version = version
	return application.Run(context.Background(), args, stdout, stderr)
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
