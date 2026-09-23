package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/Azd325/router-axi/internal/tr064"
)

const (
	ExitOK          = 0
	ExitInternal    = 1
	ExitUsage       = 2
	ExitAuth        = 3
	ExitNetwork     = 4
	ExitUnsupported = 5
	ExitRouter      = 6
)

type Config struct{ Host, Username, Password string }
type Reader interface {
	Status(context.Context) (tr064.Status, error)
	WAN(context.Context) (tr064.WAN, error)
	Traffic(context.Context) (tr064.Traffic, error)
	Calls(context.Context) ([]tr064.Call, error)
}
type Factory func(Config) (Reader, error)
type App struct {
	factory Factory
	getenv  func(string) string
	Version string
}

func New(factory Factory, getenv func(string) string) *App {
	return &App{factory: factory, getenv: getenv, Version: "dev"}
}

type options struct {
	command, host   string
	json, help, all bool
}

type callResult struct {
	Calls   []tr064.Call `json:"calls"`
	Total   int          `json:"total"`
	Omitted int          `json:"omitted"`
}

func (a *App) Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	opts, err := parse(args)
	if err != nil {
		return writeError(stderr, false, ExitUsage, "invalid_arguments", err.Error(), "router-axi help")
	}
	if opts.help || opts.command == "help" || opts.command == "" {
		if _, err := io.WriteString(stdout, help(opts.command)); err != nil {
			return ExitInternal
		}
		return ExitOK
	}
	if opts.command == "version" {
		if opts.json {
			return writeJSON(stdout, map[string]string{"version": a.Version})
		}
		_, err := fmt.Fprintf(stdout, "version: %s\n", a.Version)
		if err != nil {
			return ExitInternal
		}
		return ExitOK
	}
	if !validCommand(opts.command) {
		return writeError(stderr, opts.json, ExitUsage, "unknown_command", "unknown command: "+opts.command, "router-axi help")
	}
	host := opts.host
	if host == "" {
		host = a.getenv("ROUTER_AXI_HOST")
	}
	if host == "" {
		host = "fritz.box"
	}
	reader, err := a.factory(Config{Host: host, Username: a.getenv("ROUTER_AXI_USERNAME"), Password: a.getenv("ROUTER_AXI_PASSWORD")})
	if err != nil {
		return writeError(stderr, opts.json, ExitUsage, "invalid_configuration", err.Error(), "router-axi help")
	}

	var value any
	switch opts.command {
	case "status":
		value, err = reader.Status(ctx)
	case "wan":
		value, err = reader.WAN(ctx)
	case "traffic":
		value, err = reader.Traffic(ctx)
	case "calls":
		var calls []tr064.Call
		calls, err = reader.Calls(ctx)
		if err == nil {
			total := len(calls)
			if !opts.all && len(calls) > 20 {
				calls = calls[:20]
			}
			value = callResult{Calls: calls, Total: total, Omitted: total - len(calls)}
		}
	}
	if err != nil {
		return renderProtocolError(stderr, opts.json, err)
	}
	if opts.json {
		return writeJSON(stdout, value)
	}
	if err := writeCompact(stdout, opts.command, value); err != nil {
		return ExitInternal
	}
	return ExitOK
}

func parse(args []string) (options, error) {
	var opts options
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--json":
			opts.json = true
		case "--help", "-h":
			opts.help = true
		case "--all":
			opts.all = true
		case "--host":
			i++
			if i >= len(args) || strings.HasPrefix(args[i], "-") {
				return opts, errors.New("--host requires a value")
			}
			opts.host = args[i]
		default:
			if strings.HasPrefix(args[i], "-") {
				return opts, fmt.Errorf("unknown option: %s", args[i])
			}
			if opts.command != "" {
				return opts, errors.New("exactly one command is required")
			}
			opts.command = args[i]
		}
	}
	if opts.all && opts.command != "calls" {
		return opts, errors.New("--all is valid only for calls")
	}
	return opts, nil
}

func validCommand(command string) bool {
	return command == "status" || command == "wan" || command == "traffic" || command == "calls"
}

func help(command string) string {
	if validCommand(command) {
		extra := ""
		if command == "calls" {
			extra = " [--all]"
		}
		return "usage: router-axi " + command + " [--host ADDRESS] [--json]" + extra + "\n"
	}
	return "usage: router-axi [--host ADDRESS] [--json] <command>\n\ncommands:\n  status   router identity and firmware\n  wan      internet connection state\n  traffic  current rates and byte totals\n  calls    call history\n  version  CLI version\n\nauthentication: ROUTER_AXI_USERNAME and ROUTER_AXI_PASSWORD\n"
}

func writeJSON(w io.Writer, value any) int {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return ExitInternal
	}
	return ExitOK
}

func writeCompact(w io.Writer, command string, value any) error {
	switch command {
	case "status":
		v := value.(tr064.Status)
		_, err := fmt.Fprintf(w, "router:\n  manufacturer: %s\n  model: %s\n  software: %s\n  hardware: %s\n  serial: %s\n  uptime: %s\nnext: router-axi wan\n", scalar(v.Manufacturer), scalar(v.Model), scalar(v.Software), scalar(v.Hardware), scalar(v.Serial), duration(v.UptimeSeconds))
		return err
	case "wan":
		v := value.(tr064.WAN)
		_, err := fmt.Fprintf(w, "wan:\n  status: %s\n  external_ip: %s\n  ip_family: %s\n  uptime: %s\n  last_error: %s\nnext: router-axi traffic\n", scalar(v.Status), scalar(v.ExternalIP), scalar(v.IPFamily), duration(v.UptimeSeconds), scalar(v.LastError))
		return err
	case "traffic":
		v := value.(tr064.Traffic)
		_, err := fmt.Fprintf(w, "traffic:\n  downloaded: %s\n  uploaded: %s\n  observed_at: %s\n", size(v.TotalDownloadBytes), size(v.TotalUploadBytes), scalar(v.ObservedAt))
		return err
	case "calls":
		result := value.(callResult)
		if len(result.Calls) == 0 {
			_, err := io.WriteString(w, "calls[0]: no calls found\n")
			return err
		}
		if _, err := fmt.Fprintf(w, "calls[%d]{id,direction,remote,name,date,duration}:\n", len(result.Calls)); err != nil {
			return err
		}
		for _, call := range result.Calls {
			if _, err := fmt.Fprintf(w, "  %s,%s,%s,%s,%s,%s\n", toon(call.ID), toon(call.Direction), toon(call.Remote), toon(call.Name), toon(call.Date), toon(call.Duration)); err != nil {
				return err
			}
		}
		if result.Omitted > 0 {
			_, err := fmt.Fprintf(w, "omitted: %d\nnext: router-axi calls --all\n", result.Omitted)
			return err
		}
	}
	return nil
}

func renderProtocolError(w io.Writer, jsonOutput bool, err error) int {
	var protocolErr *tr064.Error
	if !errors.As(err, &protocolErr) {
		return writeError(w, jsonOutput, ExitInternal, "internal_error", err.Error(), "")
	}
	switch protocolErr.Kind {
	case "auth":
		return writeError(w, jsonOutput, ExitAuth, "authentication_failed", protocolErr.Message, "set ROUTER_AXI_USERNAME and ROUTER_AXI_PASSWORD")
	case "network":
		return writeError(w, jsonOutput, ExitNetwork, "router_unreachable", protocolErr.Message, "check --host and local network access")
	case "unsupported":
		return writeError(w, jsonOutput, ExitUnsupported, "unsupported_capability", protocolErr.Message, "")
	default:
		return writeError(w, jsonOutput, ExitRouter, "router_protocol_error", protocolErr.Message, "")
	}
}

func writeError(w io.Writer, jsonOutput bool, exit int, code, message, hint string) int {
	if jsonOutput {
		payload := struct {
			Error struct {
				Code, Message, Hint string `json:",omitempty"`
			} `json:"error"`
		}{}
		payload.Error.Code, payload.Error.Message, payload.Error.Hint = code, message, hint
		if writeJSON(w, payload) != ExitOK {
			return ExitInternal
		}
		return exit
	}
	if _, err := fmt.Fprintf(w, "error:\n  code: %s\n  message: %s\n", code, scalar(message)); err != nil {
		return ExitInternal
	}
	if hint != "" {
		if _, err := fmt.Fprintf(w, "  hint: %s\n", scalar(hint)); err != nil {
			return ExitInternal
		}
	}
	return exit
}

func scalar(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}
func toon(value string) string {
	if value == "" {
		return `""`
	}
	if strings.ContainsAny(value, ",\n\r\"") {
		return strconv.Quote(value)
	}
	return value
}
func duration(seconds uint64) string {
	return fmt.Sprintf("%dd %02dh %02dm", seconds/86400, seconds%86400/3600, seconds%3600/60)
}
func size(bytes uint64) string { return fmt.Sprintf("%.2f GB", float64(bytes)/1_000_000_000) }
