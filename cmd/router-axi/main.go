package main

import (
	"context"
	"io"
	"os"
	"runtime/debug"

	"github.com/Azd325/router-axi/internal/app"
	"github.com/Azd325/router-axi/internal/tr064"
)

var version = "dev"
var readBuildInfo = debug.ReadBuildInfo

func buildVersion() string {
	if info, ok := readBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return version
}

func run(args []string, stdout, stderr io.Writer) int {
	application := app.New(func(config app.Config) (app.Reader, error) {
		return tr064.New(config.Host, config.Username, config.Password, nil)
	}, os.Getenv)
	application.Version = buildVersion()
	return application.Run(context.Background(), args, stdout, stderr)
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
