package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

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
	Doctor(context.Context) (tr064.Doctor, error)
	Status(context.Context) (tr064.Status, error)
	Overview(context.Context) (tr064.Overview, error)
	WAN(context.Context) (tr064.WAN, error)
	Traffic(context.Context) (tr064.Traffic, error)
	Calls(context.Context) ([]tr064.Call, error)
	Devices(context.Context) ([]tr064.Device, error)
	Leases(context.Context) ([]tr064.Lease, error)
	WiFi(context.Context) ([]tr064.Radio, error)
	WiFiMutation(context.Context, uint64, bool, bool) (tr064.WiFiMutation, error)
	Forwards(context.Context) ([]tr064.Forward, error)
	Reboot(context.Context, bool) (tr064.RebootResult, error)
	ConfigExport(context.Context, string) ([]byte, error)
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
	command, host, action, output   string
	json, help, all, confirm, force bool
	instance                        uint64
	instanceSet                     bool
}

type callResult struct {
	Calls   []tr064.Call `json:"calls"`
	Total   int          `json:"total"`
	Omitted int          `json:"omitted"`
}

type deviceResult struct {
	Devices []tr064.Device `json:"devices"`
	Total   int            `json:"total"`
	Omitted int            `json:"omitted"`
}

type leaseResult struct {
	Leases  []tr064.Lease `json:"leases"`
	Total   int           `json:"total"`
	Omitted int           `json:"omitted"`
}

type wifiResult struct {
	Radios []tr064.Radio `json:"radios"`
	Total  int           `json:"total"`
}

type wifiMutationResult struct {
	WiFi wifiMutationState `json:"wifi"`
}

type wifiPreviewResult struct {
	WiFi wifiPreviewState `json:"wifi"`
}

type wifiMutationState struct {
	Instance string `json:"instance"`
	Action   string `json:"action"`
	Previous bool   `json:"previous"`
	Current  bool   `json:"current"`
	Changed  bool   `json:"changed"`
}

type wifiPreviewState struct {
	Instance string `json:"instance"`
	Action   string `json:"action"`
	Current  bool   `json:"current"`
	Intended bool   `json:"intended"`
	Preview  bool   `json:"preview"`
}

func wifiMutationJSON(result tr064.WiFiMutation) wifiMutationResult {
	return wifiMutationResult{WiFi: wifiMutationState{result.Instance, result.Action, result.Previous, result.Current, result.Changed}}
}

func wifiPreviewJSON(result tr064.WiFiMutation) wifiPreviewResult {
	return wifiPreviewResult{WiFi: wifiPreviewState{result.Instance, result.Action, result.Current, result.Intended, true}}
}

const rebootEffect = "the router will restart and temporarily interrupt all local services"
const rebootRecovery = "wait for the router to recover, then run router-axi doctor; do not automatically repeat reboot"

const backupNext = "keep the export passphrase safe; the backup can only be restored with it"

type rebootPreviewState struct {
	Endpoint string `json:"endpoint"`
	Preview  bool   `json:"preview"`
	Effect   string `json:"effect"`
	Execute  string `json:"execute"`
}

type rebootAcceptedState struct {
	Endpoint string `json:"endpoint"`
	Accepted bool   `json:"accepted"`
	Recovery string `json:"recovery"`
}

type forwardResult struct {
	Forwards []tr064.Forward `json:"forwards"`
	Total    int             `json:"total"`
	Omitted  int             `json:"omitted"`
}

type backupResult struct {
	Path   string `json:"path"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type backupJSONResult struct {
	Backup backupResult `json:"backup"`
}

func (a *App) Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	opts, err := parse(args)
	if err != nil {
		for _, arg := range args {
			if arg == "--json" {
				opts.json = true
			}
		}
		hint := "router-axi help"
		if opts.command == "reboot" {
			hint = "router-axi reboot [--confirm] [--host ADDRESS] [--json] [--help]"
		}
		if opts.command == "backup" {
			hint = "router-axi backup --output PATH [--force] [--host ADDRESS] [--json] [--help]"
		}
		return writeError(stderr, opts.json, ExitUsage, "invalid_arguments", err.Error(), hint)
	}
	if opts.help || opts.command == "help" {
		if _, err := io.WriteString(stdout, help(opts.command, opts.action)); err != nil {
			return ExitInternal
		}
		return ExitOK
	}
	if opts.command == "" {
		opts.command = "status"
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
		message := err.Error()
		if opts.command == "reboot" {
			message = "invalid router endpoint; use an HTTP or HTTPS host without userinfo, query, fragment, or a non-root path"
		}
		return writeError(stderr, opts.json, ExitUsage, "invalid_configuration", message, "router-axi help")
	}
	if opts.command == "reboot" {
		result, err := reader.Reboot(ctx, opts.confirm)
		if err != nil {
			return renderProtocolError(stderr, opts.json, err)
		}
		return writeReboot(stdout, result, opts.json)
	}
	if opts.command == "backup" {
		passphrase := a.getenv("ROUTER_AXI_BACKUP_PASSWORD")
		if passphrase == "" {
			return writeError(stderr, opts.json, ExitUsage, "backup_passphrase_missing", "backup requires ROUTER_AXI_BACKUP_PASSWORD; the export passphrase is never read from arguments or other variables", "router-axi backup --help")
		}
		return a.runBackup(ctx, reader, opts, stdout, stderr, passphrase)
	}

	var value any
	mutation := opts.command == "wifi" && opts.action != ""
	if mutation {
		var result tr064.WiFiMutation
		result, err = reader.WiFiMutation(ctx, opts.instance, opts.action == "enable", opts.confirm)
		if err == nil && result.Preview {
			if opts.json {
				if writeJSON(stdout, wifiPreviewJSON(result)) != ExitOK {
					return ExitInternal
				}
			} else if err := writeWiFiPreview(stdout, result, opts.host); err != nil {
				return ExitInternal
			}
			return ExitOK
		}
		value = wifiMutationJSON(result)
	} else {
		switch opts.command {
		case "doctor":
			value, err = reader.Doctor(ctx)
		case "status":
			value, err = reader.Status(ctx)
		case "overview":
			value, err = reader.Overview(ctx)
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
		case "wifi":
			var radios []tr064.Radio
			radios, err = reader.WiFi(ctx)
			if radios == nil {
				radios = []tr064.Radio{}
			}
			value = wifiResult{Radios: radios, Total: len(radios)}
		case "devices":
			var devices []tr064.Device
			devices, err = reader.Devices(ctx)
			if err == nil {
				total := len(devices)
				if !opts.all && len(devices) > 20 {
					devices = devices[:20]
				}
				value = deviceResult{Devices: devices, Total: total, Omitted: total - len(devices)}
			}
		case "leases":
			var leases []tr064.Lease
			leases, err = reader.Leases(ctx)
			if err == nil {
				if leases == nil {
					leases = []tr064.Lease{}
				}
				total := len(leases)
				if !opts.all && len(leases) > 20 {
					leases = leases[:20]
				}
				value = leaseResult{Leases: leases, Total: total, Omitted: total - len(leases)}
			}
		case "forwards":
			var forwards []tr064.Forward
			forwards, err = reader.Forwards(ctx)
			if err == nil {
				if forwards == nil {
					forwards = []tr064.Forward{}
				}
				total := len(forwards)
				if !opts.all && len(forwards) > 20 {
					forwards = forwards[:20]
				}
				value = forwardResult{Forwards: forwards, Total: total, Omitted: total - len(forwards)}
			}
		}
	}
	if err != nil {
		if opts.command == "doctor" {
			if opts.json {
				if writeJSON(stdout, value) != ExitOK {
					return ExitInternal
				}
			} else if writeErr := writeCompact(stdout, opts.command, value); writeErr != nil {
				return ExitInternal
			}
		}
		return renderProtocolError(stderr, opts.json, err)
	}
	if opts.json {
		return writeJSON(stdout, value)
	}
	if mutation {
		if err := writeWiFiMutation(stdout, value.(wifiMutationResult).WiFi); err != nil {
			return ExitInternal
		}
		return ExitOK
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
		case "--confirm":
			opts.confirm = true
		case "--instance":
			i++
			if i >= len(args) || strings.HasPrefix(args[i], "-") {
				return opts, errors.New("--instance requires a value")
			}
			instance, err := strconv.ParseUint(args[i], 10, 64)
			if err != nil || instance == 0 {
				return opts, errors.New("--instance requires a WLANConfiguration number of 1 or greater")
			}
			opts.instance, opts.instanceSet = instance, true
		case "--host":
			i++
			if i >= len(args) || strings.HasPrefix(args[i], "-") {
				return opts, errors.New("--host requires a value")
			}
			opts.host = args[i]
		case "--output":
			i++
			if i >= len(args) || args[i] == "" || strings.HasPrefix(args[i], "-") {
				return opts, errors.New("--output requires a file path")
			}
			opts.output = args[i]
		case "--force":
			opts.force = true
		default:
			if strings.HasPrefix(args[i], "-") {
				option, _, _ := strings.Cut(args[i], "=")
				return opts, fmt.Errorf("unknown option: %s", option)
			}
			if opts.command == "" {
				opts.command = args[i]
				continue
			}
			if opts.command == "wifi" && opts.action == "" && (args[i] == "enable" || args[i] == "disable") {
				opts.action = args[i]
				continue
			}
			if opts.command == "wifi" && opts.action != "" {
				return opts, errors.New("wifi accepts one action: enable or disable")
			}
			return opts, errors.New("exactly one command is required")
		}
	}
	wifiMutation := opts.command == "wifi" && opts.action != ""
	if opts.instanceSet && !wifiMutation {
		return opts, errors.New("--instance is valid only with wifi enable or wifi disable")
	}
	if opts.confirm && !wifiMutation && opts.command != "reboot" {
		return opts, errors.New("--confirm is valid only with reboot, wifi enable, or wifi disable")
	}
	if opts.output != "" && opts.command != "backup" {
		return opts, errors.New("--output is valid only with backup")
	}
	if opts.force && opts.command != "backup" {
		return opts, errors.New("--force is valid only with backup")
	}
	if opts.command == "backup" && opts.output == "" && !opts.help {
		return opts, errors.New("backup requires --output PATH")
	}
	if opts.all && opts.command != "calls" && opts.command != "devices" && opts.command != "leases" && opts.command != "forwards" {
		return opts, errors.New("--all is valid only for calls, devices, leases, or forwards")
	}
	return opts, nil
}

func validCommand(command string) bool {
	return command == "doctor" || command == "status" || command == "overview" || command == "wan" || command == "traffic" || command == "calls" || command == "devices" || command == "leases" || command == "wifi" || command == "forwards" || command == "reboot" || command == "backup"
}

func help(command, action string) string {
	if command == "reboot" {
		return "usage: router-axi reboot [--confirm] [--host ADDRESS] [--json] [--help]\nWithout --confirm: preview only. With --confirm: restart the router and temporarily interrupt all local services.\nNo prompts, retries, or recovery polling; reboot is not idempotent.\nexamples: router-axi reboot; router-axi reboot --confirm\n"
	}
	if command == "backup" {
		return "usage: router-axi backup --output PATH [--force] [--host ADDRESS] [--json] [--help]\nDownloads the documented DeviceConfig:X_AVM-DE_GetConfigFile export to PATH with an atomic owner-only write.\nThe export passphrase is read only from ROUTER_AXI_BACKUP_PASSWORD and is required to restore the file.\nAn existing file is never overwritten without --force; the router origin must be HTTPS (for example --host https://fritz.box:49443) with a locally trusted certificate, so the passphrase never travels in plaintext.\nNo prompts. Examples: router-axi backup --host https://fritz.box:49443 --output fritz.export; router-axi backup --host https://fritz.box:49443 --output fritz.export --force\n"
	}
	if action != "" && command == "wifi" {
		return "usage: router-axi wifi " + action + " [--instance N] --confirm [--host ADDRESS] [--json]\n"
	}
	if validCommand(command) {
		extra := ""
		if command == "calls" || command == "devices" || command == "leases" || command == "forwards" {
			extra = " [--all]"
		}
		if command == "wifi" {
			extra = " [enable|disable [--instance N] --confirm]"
		}
		return "usage: router-axi " + command + " [--host ADDRESS] [--json]" + extra + "\n"
	}
	return "usage: router-axi [--host ADDRESS] [--json] [command]\n\ncommands:\n  doctor    bounded connectivity and capability diagnosis\n  status    router identity and firmware (default)\n  overview  identity, WAN state, and traffic totals\n  wan       internet connection state\n  traffic   byte totals\n  calls     call history\n  devices   connected and known LAN clients\n  leases    observed Hosts table lease metadata\n  wifi      Wi-Fi inspection; wifi enable|disable changes a radio with --confirm\n  forwards  port-forwarding rules\n  reboot    preview router restart; execute once with --confirm\n  backup    download the documented configuration export to a file\n  version   CLI version\n\nauthentication: ROUTER_AXI_USERNAME and ROUTER_AXI_PASSWORD\nbackup export passphrase: ROUTER_AXI_BACKUP_PASSWORD\n"
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
	case "doctor":
		v := value.(tr064.Doctor)
		if _, err := fmt.Fprintf(w, "doctor:\n  endpoint: %s\n  reachability: %s\n  protocol: %s\n  authentication: %s\n  model: %s\n  firmware: %s\ncapabilities:\n", scalar(v.Endpoint), check(v.Reachability), check(v.Protocol), check(v.Authentication), scalar(v.Model), scalar(v.Firmware)); err != nil {
			return err
		}
		for _, capability := range []struct {
			name  string
			check tr064.DoctorCheck
		}{
			{"status", v.Capabilities.Status}, {"overview", v.Capabilities.Overview}, {"wan", v.Capabilities.WAN}, {"traffic", v.Capabilities.Traffic}, {"calls", v.Capabilities.Calls}, {"devices", v.Capabilities.Devices}, {"leases", v.Capabilities.Leases}, {"wifi", v.Capabilities.WiFi}, {"forwards", v.Capabilities.Forwards}, {"reboot", v.Capabilities.Reboot}, {"backup", v.Capabilities.Backup},
		} {
			if _, err := fmt.Fprintf(w, "  %s: %s\n", capability.name, check(capability.check)); err != nil {
				return err
			}
		}
		return nil
	case "status":
		v := value.(tr064.Status)
		_, err := fmt.Fprintf(w, "router:\n  manufacturer: %s\n  model: %s\n  software: %s\n  hardware: %s\n  serial: %s\n  uptime: %s\nnext: router-axi wan\n", scalar(v.Manufacturer), scalar(v.Model), scalar(v.Software), scalar(v.Hardware), scalar(v.Serial), duration(v.UptimeSeconds))
		return err
	case "overview":
		v := value.(tr064.Overview)
		_, err := fmt.Fprintf(w, "overview:\n  router: %s (%s)\n  wan: %s, %s, %s\n  traffic: %s downloaded, %s uploaded\n  observed_at: %s\n", scalar(v.Router.Model), scalar(v.Router.Software), scalar(v.WAN.Status), scalar(v.WAN.ExternalIP), scalar(v.WAN.IPFamily), size(v.Traffic.TotalDownloadBytes), size(v.Traffic.TotalUploadBytes), scalar(v.Traffic.ObservedAt))
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
	case "wifi":
		result := value.(wifiResult)
		if len(result.Radios) == 0 {
			_, err := io.WriteString(w, "radios[0]: no Wi-Fi services found\n")
			return err
		}
		if _, err := fmt.Fprintf(w, "radios[%d]{service_id,ssid,enabled,channel,band,standard,associated_devices,security_mode}:\n", len(result.Radios)); err != nil {
			return err
		}
		for _, radio := range result.Radios {
			if _, err := fmt.Fprintf(w, "  %s,%s,%t,%d,%s,%s,%d,%s\n", toon(radio.ServiceID), toon(radio.SSID), radio.Enabled, radio.Channel, toon(radio.Band), toon(radio.Standard), radio.AssociatedDevices, toon(radio.SecurityMode)); err != nil {
				return err
			}
		}
	case "devices":
		result := value.(deviceResult)
		if len(result.Devices) == 0 {
			_, err := io.WriteString(w, "devices[0]: no devices found\n")
			return err
		}
		if _, err := fmt.Fprintf(w, "devices[%d]{name,ip_address,mac_address,interface_type,active}:\n", len(result.Devices)); err != nil {
			return err
		}
		for _, device := range result.Devices {
			if _, err := fmt.Fprintf(w, "  %s,%s,%s,%s,%t\n", toon(device.Name), toon(device.IPAddress), toon(device.MACAddress), toon(device.InterfaceType), device.Active); err != nil {
				return err
			}
		}
		if result.Omitted > 0 {
			_, err := fmt.Fprintf(w, "omitted: %d\nnext: router-axi devices --all\n", result.Omitted)
			return err
		}
	case "leases":
		result := value.(leaseResult)
		if len(result.Leases) == 0 {
			_, err := io.WriteString(w, "leases[0]: no host observations found\n")
			return err
		}
		if _, err := fmt.Fprintf(w, "leases[%d]{name,ip_address,mac_address,address_source,lease_time_remaining,interface_type,active}:\n", len(result.Leases)); err != nil {
			return err
		}
		for _, lease := range result.Leases {
			remaining := "unknown"
			if lease.LeaseTimeRemaining != nil {
				remaining = strconv.FormatInt(*lease.LeaseTimeRemaining, 10)
			}
			if _, err := fmt.Fprintf(w, "  %s,%s,%s,%s,%s,%s,%t\n", toon(lease.Name), toon(lease.IPAddress), toon(lease.MACAddress), toon(lease.AddressSource), remaining, toon(lease.InterfaceType), lease.Active); err != nil {
				return err
			}
		}
		if result.Omitted > 0 {
			_, err := fmt.Fprintf(w, "omitted: %d\nnext: router-axi leases --all\n", result.Omitted)
			return err
		}
	case "forwards":
		result := value.(forwardResult)
		if len(result.Forwards) == 0 {
			_, err := io.WriteString(w, "forwards[0]: no port forwards found\n")
			return err
		}
		if _, err := fmt.Fprintf(w, "forwards[%d]{enabled,protocol,external_port,internal_client,internal_port,description,remote_host,lease_duration}:\n", len(result.Forwards)); err != nil {
			return err
		}
		for _, forward := range result.Forwards {
			lease := "unknown"
			if forward.LeaseDuration != nil {
				lease = strconv.FormatUint(*forward.LeaseDuration, 10)
			}
			if _, err := fmt.Fprintf(w, "  %t,%s,%d,%s,%d,%s,%s,%s\n", forward.Enabled, toon(forward.Protocol), forward.ExternalPort, toon(forward.InternalClient), forward.InternalPort, toon(forward.Description), toon(forward.RemoteHost), lease); err != nil {
				return err
			}
		}
		if result.Omitted > 0 {
			_, err := fmt.Fprintf(w, "omitted: %d\nnext: router-axi forwards --all\n", result.Omitted)
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
	case "usage":
		hint := "router-axi wifi enable|disable --instance N"
		if protocolErr.Operation == "reboot" {
			hint = "router-axi reboot --help"
		}
		if protocolErr.Operation == "backup" {
			hint = "router-axi backup --help"
		}
		return writeError(w, jsonOutput, ExitUsage, protocolErr.Code, protocolErr.Message, hint)
	case "auth":
		return writeError(w, jsonOutput, ExitAuth, "authentication_failed", protocolErr.Message, "set ROUTER_AXI_USERNAME and ROUTER_AXI_PASSWORD")
	case "network":
		if protocolErr.Code == "tls_untrusted" {
			return writeError(w, jsonOutput, ExitNetwork, "tls_untrusted", protocolErr.Message, "trust the router's HTTPS certificate on this system; verification is never skipped")
		}
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

// runBackup performs the documented configuration export and stores it at the
// explicitly requested destination. The destination is validated before the
// router is contacted, the write is atomic and owner-only, and an existing
// file is never replaced without --force. The export passphrase is never
// echoed, logged, or written anywhere but the encrypted export itself.
func (a *App) runBackup(ctx context.Context, reader Reader, opts options, stdout, stderr io.Writer, passphrase string) int {
	if strings.HasSuffix(opts.output, string(os.PathSeparator)) {
		return writeError(stderr, opts.json, ExitUsage, "invalid_output", "backup --output must name a file, not a directory", "router-axi backup --help")
	}
	if info, err := os.Stat(opts.output); err == nil {
		if info.IsDir() {
			return writeError(stderr, opts.json, ExitUsage, "invalid_output", "backup --output names an existing directory", "router-axi backup --help")
		}
		if !opts.force {
			return writeError(stderr, opts.json, ExitUsage, "output_exists", "backup --output already exists; pass --force to overwrite it", "router-axi backup --output "+shellWord(opts.output)+" --force")
		}
	}
	dir := filepath.Dir(opts.output)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return writeError(stderr, opts.json, ExitUsage, "invalid_output", "backup --output parent directory does not exist", "router-axi backup --help")
	}
	data, err := reader.ConfigExport(ctx, passphrase)
	if err != nil {
		return renderProtocolError(stderr, opts.json, err)
	}
	if writeErr := writeBackupFile(opts.output, data, opts.force); writeErr != nil {
		if errors.Is(writeErr, os.ErrExist) && !opts.force {
			return writeError(stderr, opts.json, ExitUsage, "output_exists", "backup --output already exists; pass --force to overwrite it", "router-axi backup --output "+shellWord(opts.output)+" --force")
		}
		if errors.Is(writeErr, errNoReplaceUnsupported) {
			return writeError(stderr, opts.json, ExitInternal, "backup_link_unsupported", "the destination filesystem does not support the atomic no-replace write; --force writes with an atomic rename that replaces any existing file", "router-axi backup --output "+shellWord(opts.output)+" --force")
		}
		return writeError(stderr, opts.json, ExitInternal, "backup_write_failed", "the configuration export could not be written to the requested path", "check the destination directory and permissions")
	}
	sum := sha256.Sum256(data)
	result := backupResult{Path: opts.output, Bytes: len(data), SHA256: hex.EncodeToString(sum[:])}
	if opts.json {
		return writeJSON(stdout, backupJSONResult{Backup: result})
	}
	if _, err := fmt.Fprintf(stdout, "backup:\n  path: %s\n  bytes: %d\n  sha256: %s\nnext: %s\n", strconv.Quote(result.Path), result.Bytes, result.SHA256, backupNext); err != nil {
		return ExitInternal
	}
	return ExitOK
}

// writeBackupFile stores the export through a temporary file in the
// destination directory, so the target is either fully written or untouched.
// Without force the file is created with an atomic no-replace operation, so an
// existing target survives even when it appears between the preflight check
// and the write. The temporary file is always removed.
var (
	linkFile                = os.Link
	errNoReplaceUnsupported = errors.New("filesystem does not support hard links")
)

func writeBackupFile(path string, data []byte, force bool) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".router-axi-backup-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if force {
		if err := os.Rename(tmpName, path); err != nil {
			return err
		}
	} else if err := linkFile(tmpName, path); err != nil {
		if errors.Is(err, errors.ErrUnsupported) || errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EXDEV) || errors.Is(err, syscall.EMLINK) {
			return errNoReplaceUnsupported
		}
		return err
	}
	if dir, err := os.Open(filepath.Dir(path)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

func writeReboot(w io.Writer, result tr064.RebootResult, jsonOutput bool) int {
	var err error
	if result.Preview {
		execute := "router-axi reboot --host " + shellWord(result.Endpoint) + " --confirm"
		if jsonOutput {
			execute += " --json"
			return writeJSON(w, struct {
				Reboot rebootPreviewState `json:"reboot"`
			}{rebootPreviewState{result.Endpoint, true, rebootEffect, execute}})
		}
		_, err = fmt.Fprintf(w, "reboot:\n  endpoint: %s\n  preview: true\n  effect: %s\n  execute: %s\n", strconv.Quote(result.Endpoint), rebootEffect, strconv.Quote(execute))
	} else {
		if jsonOutput {
			return writeJSON(w, struct {
				Reboot rebootAcceptedState `json:"reboot"`
			}{rebootAcceptedState{result.Endpoint, result.Accepted, rebootRecovery}})
		}
		_, err = fmt.Fprintf(w, "reboot:\n  endpoint: %s\n  accepted: %t\n  recovery: %s\n", strconv.Quote(result.Endpoint), result.Accepted, rebootRecovery)
	}
	if err != nil {
		return ExitInternal
	}
	return ExitOK
}

func writeWiFiPreview(w io.Writer, result tr064.WiFiMutation, host string) error {
	hostFlag := ""
	if host != "" {
		hostFlag = " --host " + shellWord(host)
	}
	_, err := fmt.Fprintf(w, "wifi:\n  action: %s\n  instance: %s\n  current: %s\n  intended: %s\n  changed: false\nnext: router-axi wifi %s --instance %s --confirm%s\n", result.Action, scalar(result.Instance), state(result.Current), state(result.Intended), result.Action, instanceSuffix(result.Instance), hostFlag)
	return err
}

func shellWord(value string) string {
	if value != "" && strings.Trim(value, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789.:/_-") == "" {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func writeWiFiMutation(w io.Writer, result wifiMutationState) error {
	_, err := fmt.Fprintf(w, "wifi:\n  action: %s\n  instance: %s\n  previous: %s\n  current: %s\n  changed: %t\nnext: router-axi wifi\n", result.Action, scalar(result.Instance), state(result.Previous), state(result.Current), result.Changed)
	return err
}

func state(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

func instanceSuffix(serviceID string) string {
	return strings.TrimPrefix(serviceID, "urn:WLANConfiguration-com:serviceId:WLANConfiguration")
}

func check(value tr064.DoctorCheck) string {
	if value.Remediation == "" {
		return value.State
	}
	return value.State + "; remediation: " + value.Remediation
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
