package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Azd325/router-axi/internal/skill"
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

	invalidRouterEndpoint = "invalid router endpoint; use an HTTP or HTTPS host without userinfo, query, fragment, or a non-root path"
)

// defaultListLimit caps rows shown by the calls, devices, leases and forwards
// list commands unless --all is passed. It is high enough to cover common
// home-LAN sizes in a single call.
const defaultListLimit = 100

type Config struct{ Host, Username, Password string }
type Reader interface {
	Doctor(context.Context) (tr064.Doctor, error)
	Status(context.Context) (tr064.Status, error)
	Overview(context.Context) (tr064.Overview, error)
	WAN(context.Context) (tr064.WAN, error)
	WANDetail(context.Context) (tr064.WANDetail, error)
	Traffic(context.Context) (tr064.Traffic, error)
	WatchSnapshot(context.Context) (tr064.WatchSnapshot, error)
	Calls(context.Context) ([]tr064.Call, error)
	Devices(context.Context) ([]tr064.Device, error)
	DeviceDetail(context.Context, netip.Addr) (tr064.DeviceDetail, error)
	Leases(context.Context) ([]tr064.Lease, error)
	DHCP(context.Context) (tr064.DHCP, error)
	DSL(context.Context) (tr064.DSL, error)
	Firmware(context.Context) (tr064.Firmware, error)
	EventLog(context.Context, string, int) (tr064.EventLog, error)
	Account(context.Context) (tr064.Account, error)
	WiFi(context.Context) ([]tr064.Radio, error)
	WiFiDetail(context.Context, uint64) (tr064.RadioDetail, error)
	GuestWiFi(context.Context) ([]tr064.GuestNetwork, error)
	WiFiMutation(context.Context, uint64, bool, bool) (tr064.WiFiMutation, error)
	Forwards(context.Context) ([]tr064.Forward, error)
	Reboot(context.Context, bool) (tr064.RebootResult, error)
	WANReconnect(context.Context, bool) (tr064.WANReconnectResult, error)
	Wake(context.Context, string, bool) (tr064.WakeResult, error)
	ConfigExport(context.Context, string) ([]byte, error)
}
type Factory func(Config) (Reader, error)
type App struct {
	factory    Factory
	getenv     func(string) string
	Version    string
	executable func() (string, error)
	homeDir    func() (string, error)
	// watchSignals controls whether watch registers real OS signal
	// handling. Tests inside a synthetic-time bubble disable it.
	watchSignals bool
}

func New(factory Factory, getenv func(string) string) *App {
	return &App{factory: factory, getenv: getenv, Version: "dev", watchSignals: true, executable: os.Executable, homeDir: os.UserHomeDir}
}

type options struct {
	mac                                        string
	command, host, action, output, path, agent string
	json, help, all, confirm, force            bool
	instance                                   uint64
	instanceSet                                bool
	ip                                         netip.Addr
	ipSet                                      bool
	interval                                   time.Duration
	count                                      int
	intervalSet, countSet                      bool
	flags                                      map[string]bool
	versionFlag                                bool
	group                                      string
	limit                                      int
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

type guestResult struct {
	Guests []tr064.GuestNetwork `json:"guests"`
	Total  int                  `json:"total"`
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

type sendOncePreviewState struct {
	Endpoint string `json:"endpoint"`
	MAC      string `json:"mac,omitempty"`
	Preview  bool   `json:"preview"`
	Effect   string `json:"effect"`
	Execute  string `json:"execute"`
}

type sendOnceAcceptedState struct {
	Endpoint string `json:"endpoint"`
	MAC      string `json:"mac,omitempty"`
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

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

type errorResult struct {
	Error errorDetail `json:"error"`
}

type doctorPartialErrorResult struct {
	Doctor tr064.Doctor `json:"doctor"`
	Error  errorDetail  `json:"error"`
}

type errorSpec struct {
	exit   int
	detail errorDetail
}

type skillResult struct {
	Path      string `json:"path"`
	Installed bool   `json:"installed"`
}

type skillJSONResult struct {
	Skill skillResult `json:"skill"`
}

func (a *App) Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	diagnostics := stderr
	stderr = stdout
	opts, err := parse(args)
	if err != nil {
		for _, arg := range args {
			if arg == "--json" {
				opts.json = true
			}
		}
		return writeError(stderr, opts.json, ExitUsage, "invalid_arguments", err.Error(), usageHint(opts))
	}
	if opts.versionFlag {
		if _, err := fmt.Fprintln(stdout, a.Version); err != nil {
			return ExitInternal
		}
		return ExitOK
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
	if opts.command == "skill" {
		return a.runSkill(opts, stdout, stderr)
	}
	if opts.command == "setup" {
		return a.runSetup(opts, stdout)
	}
	if opts.command == "session" {
		return a.runSession(opts, stdout)
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
		return writeError(stderr, opts.json, ExitUsage, "invalid_configuration", invalidRouterEndpoint, "router-axi help")
	}
	if opts.command == "watch" {
		return runWatch(ctx, reader, opts, stdout, diagnostics, a.watchSignals)
	}
	if opts.command == "reboot" {
		result, err := reader.Reboot(ctx, opts.confirm)
		if err != nil {
			return renderProtocolError(stderr, opts.json, err)
		}
		return writeReboot(stdout, result, opts.json)
	}
	if wanReconnect(opts) {
		result, err := reader.WANReconnect(ctx, opts.confirm)
		if err != nil {
			return renderProtocolError(stderr, opts.json, err)
		}
		return writeWANReconnect(stdout, result, opts.json)
	}
	if opts.command == "wake" {
		result, err := reader.Wake(ctx, opts.mac, opts.confirm)
		if err != nil {
			return renderProtocolError(stderr, opts.json, err)
		}
		return writeWake(stdout, result, opts.json)
	}
	if opts.command == "backup" {
		passphrase := a.getenv("ROUTER_AXI_BACKUP_PASSWORD")
		if passphrase == "" {
			return writeError(stderr, opts.json, ExitUsage, "backup_passphrase_missing", "backup requires ROUTER_AXI_BACKUP_PASSWORD; the export passphrase is never read from arguments or other variables", "router-axi backup --help")
		}
		return a.runBackup(ctx, reader, opts, stdout, stderr, passphrase)
	}

	var value any
	mutation := opts.command == "wifi" && (opts.action == "enable" || opts.action == "disable")
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
			if opts.action == "detail" {
				value, err = reader.WANDetail(ctx)
			} else {
				value, err = reader.WAN(ctx)
			}
		case "traffic":
			value, err = reader.Traffic(ctx)
		case "calls":
			var calls []tr064.Call
			calls, err = reader.Calls(ctx)
			if err == nil {
				total := len(calls)
				if !opts.all && len(calls) > defaultListLimit {
					calls = calls[:defaultListLimit]
				}
				value = callResult{Calls: calls, Total: total, Omitted: total - len(calls)}
			}
		case "wifi":
			if opts.action == "detail" {
				var radio tr064.RadioDetail
				radio, err = reader.WiFiDetail(ctx, opts.instance)
				value = radio
				break
			}
			var radios []tr064.Radio
			radios, err = reader.WiFi(ctx)
			if radios == nil {
				radios = []tr064.Radio{}
			}
			value = wifiResult{Radios: radios, Total: len(radios)}
		case "guest":
			var guests []tr064.GuestNetwork
			guests, err = reader.GuestWiFi(ctx)
			if guests == nil {
				guests = []tr064.GuestNetwork{}
			}
			value = guestResult{Guests: guests, Total: len(guests)}
		case "devices":
			if opts.action == "detail" {
				value, err = reader.DeviceDetail(ctx, opts.ip)
				break
			}
			var devices []tr064.Device
			devices, err = reader.Devices(ctx)
			if err == nil {
				total := len(devices)
				if !opts.all && len(devices) > defaultListLimit {
					devices = devices[:defaultListLimit]
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
				if !opts.all && len(leases) > defaultListLimit {
					leases = leases[:defaultListLimit]
				}
				value = leaseResult{Leases: leases, Total: total, Omitted: total - len(leases)}
			}
		case "dhcp":
			value, err = reader.DHCP(ctx)
		case "dsl":
			value, err = reader.DSL(ctx)
		case "event-log":
			value, err = reader.EventLog(ctx, opts.group, opts.limit)
		case "firmware":
			value, err = reader.Firmware(ctx)
		case "account":
			value, err = reader.Account(ctx)
		case "forwards":
			var forwards []tr064.Forward
			forwards, err = reader.Forwards(ctx)
			if err == nil {
				if forwards == nil {
					forwards = []tr064.Forward{}
				}
				total := len(forwards)
				if !opts.all && len(forwards) > defaultListLimit {
					forwards = forwards[:defaultListLimit]
				}
				value = forwardResult{Forwards: forwards, Total: total, Omitted: total - len(forwards)}
			}
		}
	}
	if err != nil {
		if opts.command == "doctor" {
			if opts.json {
				return writeDoctorPartialError(stdout, value.(tr064.Doctor), protocolError(err))
			} else if writeErr := writeCompact(a, stdout, opts.command, value); writeErr != nil {
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
	if err := writeCompact(a, stdout, opts.command, value); err != nil {
		return ExitInternal
	}
	return ExitOK
}

func parse(args []string) (options, error) {
	opts := options{limit: tr064.DefaultEventLogLimit, interval: defaultWatchInterval, count: defaultWatchCount, flags: map[string]bool{}}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--group":
			if opts.flags["--group"] || i+1 >= len(args) || args[i+1] == "" {
				return opts, errors.New("--group requires one group selection")
			}
			opts.flags["--group"] = true
			i++
			if _, err := tr064.EventLogGroups(args[i]); err != nil {
				return opts, errors.New("--group requires distinct sys, net, fon, wlan, usb groups separated by commas, or all (includes telephony)")
			}
			opts.group = args[i]
		case "--limit":
			if opts.flags["--limit"] || i+1 >= len(args) {
				return opts, errors.New("--limit requires one integer from 1 to 1000")
			}
			opts.flags["--limit"] = true
			i++
			limit, err := strconv.Atoi(args[i])
			if err != nil || limit < 1 || limit > tr064.MaxEventLogLimit {
				return opts, errors.New("--limit must be an integer from 1 to 1000")
			}
			opts.limit = limit
		case "--interval":
			opts.flags["--interval"] = true
			i++
			if opts.intervalSet || i >= len(args) {
				return opts, errors.New("--interval requires one duration from 1s to 1m")
			}
			interval, err := time.ParseDuration(args[i])
			if err != nil || interval < time.Second || interval > time.Minute {
				return opts, errors.New("--interval must be a duration from 1s to 1m")
			}
			opts.interval, opts.intervalSet = interval, true
		case "--count":
			opts.flags["--count"] = true
			i++
			if opts.countSet || i >= len(args) {
				return opts, errors.New("--count requires one integer from 1 to 3600")
			}
			count, err := strconv.Atoi(args[i])
			if err != nil || count < 1 || count > maxWatchCount {
				return opts, errors.New("--count must be an integer from 1 to 3600; unbounded watch is not supported")
			}
			opts.count, opts.countSet = count, true
		case "--json":
			opts.flags["--json"] = true
			opts.json = true
		case "--help", "-h":
			opts.flags["--help"] = true
			opts.help = true
		case "--all":
			opts.flags["--all"] = true
			opts.all = true
		case "--confirm":
			opts.flags["--confirm"] = true
			opts.confirm = true
		case "--instance":
			opts.flags["--instance"] = true
			i++
			if i >= len(args) || strings.HasPrefix(args[i], "-") {
				return opts, errors.New("--instance requires a value")
			}
			instance, err := strconv.ParseUint(args[i], 10, 64)
			if err != nil || instance == 0 {
				return opts, errors.New("--instance requires a WLANConfiguration number of 1 or greater")
			}
			opts.instance, opts.instanceSet = instance, true
		case "--ip":
			opts.flags["--ip"] = true
			i++
			if opts.ipSet || i >= len(args) {
				return opts, errors.New(deviceDetailTarget)
			}
			address, err := netip.ParseAddr(args[i])
			if err != nil || !address.Is4() {
				return opts, errors.New(deviceDetailTarget)
			}
			opts.ip, opts.ipSet = address, true
		case "--host":
			opts.flags["--host"] = true
			i++
			if i >= len(args) || strings.HasPrefix(args[i], "-") {
				return opts, errors.New("--host requires a value")
			}
			opts.host = args[i]
		case "--output":
			opts.flags["--output"] = true
			i++
			if i >= len(args) || args[i] == "" || strings.HasPrefix(args[i], "-") {
				return opts, errors.New("--output requires a file path")
			}
			opts.output = args[i]
		case "--path":
			opts.flags["--path"] = true
			i++
			if i >= len(args) || args[i] == "" || strings.HasPrefix(args[i], "-") {
				return opts, errors.New("--path requires a directory")
			}
			opts.path = args[i]
		case "--agent":
			opts.flags["--agent"] = true
			i++
			if i >= len(args) || args[i] == "" || strings.HasPrefix(args[i], "-") {
				return opts, errors.New("--agent requires claude, codex, opencode, or all")
			}
			opts.agent = args[i]
		case "--force":
			opts.flags["--force"] = true
			opts.force = true
		case "--version", "-v", "-V":
			opts.versionFlag = true
		default:
			if strings.HasPrefix(args[i], "-") {
				option, _, _ := strings.Cut(args[i], "=")
				return opts, fmt.Errorf("unknown option: %s", option)
			}
			if opts.command == "" {
				opts.command = args[i]
				continue
			}
			if opts.command == "wake" {
				if opts.mac != "" {
					return opts, errors.New("wake requires exactly one MAC address")
				}
				mac, err := tr064.WakeMAC(args[i])
				var invalid *tr064.Error
				if errors.As(err, &invalid) {
					return opts, errors.New(invalid.Message)
				}
				if err != nil {
					return opts, err
				}
				opts.mac = mac
				continue
			}
			if opts.command == "wan" && opts.action == "" && (args[i] == "detail" || args[i] == "reconnect") {
				opts.action = args[i]
				continue
			}
			if opts.command == "wan" && opts.action != "" {
				return opts, errors.New("wan accepts one action: detail or reconnect")
			}
			if opts.command == "wifi" && opts.action == "" && (args[i] == "enable" || args[i] == "disable" || args[i] == "detail") {
				opts.action = args[i]
				continue
			}
			if opts.command == "wifi" && opts.action != "" {
				return opts, errors.New("wifi accepts one action: detail, enable, or disable")
			}
			if opts.command == "devices" && opts.action == "" && args[i] == "detail" {
				opts.action = args[i]
				continue
			}
			if opts.command == "devices" && opts.action != "" {
				return opts, errors.New("devices accepts one action: detail; select the device with --ip ADDRESS")
			}
			if opts.command == "skill" {
				if args[i] == "install" && opts.action == "" {
					opts.action = args[i]
					continue
				}
				return opts, errors.New("skill accepts one action: install")
			}
			if opts.command == "setup" {
				if (args[i] == "install" || args[i] == "check" || args[i] == "uninstall") && opts.action == "" {
					opts.action = args[i]
					continue
				}
				return opts, errors.New("setup accepts one action: install, check, or uninstall")
			}
			if opts.command == "session" {
				if args[i] == "dashboard" && opts.action == "" {
					opts.action = args[i]
					continue
				}
				return opts, errors.New("session accepts one action: dashboard")
			}
			return opts, errors.New("exactly one command is required")
		}
	}
	if opts.versionFlag && len(args) != 1 {
		return opts, errors.New("--version, -v, and -V must be used alone")
	}
	if deviceDetail(opts) && opts.all {
		return opts, errors.New("--all is not valid with devices detail; it reports exactly one device")
	}
	if err := validateFlagContext(opts); err != nil {
		return opts, err
	}
	if deviceDetail(opts) && !opts.ipSet && !opts.help {
		return opts, errors.New("devices detail requires --ip ADDRESS")
	}
	skillAction := opts.command == "skill" && opts.action != ""
	if opts.path != "" && !skillAction {
		return opts, errors.New("--path is valid only with skill install")
	}
	if opts.command == "skill" && opts.action == "" && !opts.help {
		return opts, errors.New("skill requires the action install")
	}
	if opts.command == "setup" && opts.action == "" && !opts.help {
		return opts, errors.New("setup requires one action: install, check, or uninstall")
	}
	if opts.command == "session" && opts.action == "" && !opts.help {
		return opts, errors.New("session requires the action dashboard")
	}
	if opts.command == "backup" && opts.output == "" && !opts.help {
		return opts, errors.New("backup requires --output PATH")
	}
	if opts.command == "wake" && opts.mac == "" && !opts.help {
		return opts, errors.New("wake requires exactly one MAC address")
	}
	return opts, nil
}

func validCommand(command string) bool {
	return command == "event-log" || command == "watch" || command == "doctor" || command == "status" || command == "overview" || command == "wan" || command == "traffic" || command == "calls" || command == "devices" || command == "leases" || command == "dhcp" || command == "dsl" || command == "firmware" || command == "account" || command == "wifi" || command == "guest" || command == "forwards" || command == "reboot" || command == "wake" || command == "backup" || command == "skill" || command == "setup" || command == "session" || command == "version"
}

// commandHelp holds dedicated per-command help text: usage line, purpose,
// flag defaults and bounds, and concrete examples.
var commandHelp = map[string]string{
	"event-log": "usage: router-axi event-log [--host ADDRESS] [--group GROUPS] [--limit N] [--json] [--help]\nBounded read-only router event text from DeviceInfo:X_AVM-DE_GetDeviceLogPath. Default groups: sys,net,wlan,usb (excludes telephony); --group accepts comma-separated sys,net,fon,wlan,usb or all (includes telephony). Default 100 lines, hard maximum 1000; download maximum 8 MiB. No redirects; HTTPS certificates are always verified. Text may contain usernames, client addresses, and explicitly requested call data.\nexamples: router-axi event-log; router-axi event-log --group sys,net --limit 1000 --json\n",

	"wake":     "usage: router-axi wake MAC [--confirm] [--host ADDRESS] [--json] [--help]\nMAC is exactly one nonzero unicast address of six colon-separated hexadecimal octets. Without --confirm: preview only. With --confirm: send Hosts:X_AVM-DE_WakeOnLANByMACAddress once.\nReports router acceptance, not that the device woke. No device lookup, retries, or polling; a lost response is uncertain.\nexamples: router-axi wake 02:00:00:00:00:01; router-axi wake 02:00:00:00:00:01 --confirm\n",
	"doctor":   "usage: router-axi doctor [--host ADDRESS] [--json] [--help]\nOne bounded read-only diagnosis: reachability, TR-064 availability, authentication, model and firmware, and candidate capabilities for every command.\nUnsupported optional capabilities are a successful diagnosis and include remediation; no command's actions are invoked beyond DeviceInfo:GetInfo.\nexamples: router-axi doctor; router-axi doctor --host 192.0.2.1 --json\n",
	"status":   "usage: router-axi status [--host ADDRESS] [--json] [--help]\nRead-only router identity and firmware; the default command when no command is given.\nexamples: router-axi status; router-axi status --json\n",
	"overview": "usage: router-axi overview [--host ADDRESS] [--json] [--help]\nRead-only combined view: router identity, WAN state, and traffic totals in one read.\nexamples: router-axi overview; router-axi overview --json\n",
	"wan":      "usage: router-axi wan [detail] [--host ADDRESS] [--json] [reconnect [--confirm]] [--help]\nRead-only internet connection state: status, external address, IP family, uptime, and last error. Use wan detail for bounded physical-link properties and optional router-reported rates, totals, and DNS. wan reconnect drops the internet connection once: preview without --confirm.\nexamples: router-axi wan; router-axi wan --json; router-axi wan detail; router-axi wan reconnect\n",
	"traffic":  "usage: router-axi traffic [--host ADDRESS] [--json] [--help]\nRead-only total downloaded and uploaded byte counters with the observation time.\nexamples: router-axi traffic; router-axi traffic --json\n",
	"calls":    "usage: router-axi calls [--all] [--host ADDRESS] [--json] [--help]\nRead-only call history. Defaults to the 100 most recent entries; --all lists everything. No required arguments.\nexamples: router-axi calls; router-axi calls --all; router-axi calls --all --json\n",
	"devices":  "usage: router-axi devices [--all] [detail --ip ADDRESS] [--host ADDRESS] [--json] [--help]\nRead-only connected and remembered LAN clients. Defaults to 100 entries; --all lists everything. No required arguments. devices detail reports one device selected by its IPv4 address.\nexamples: router-axi devices; router-axi devices --all --json; router-axi devices detail --ip 192.0.2.20\n",
	"leases":   "usage: router-axi leases [--all] [--host ADDRESS] [--json] [--help]\nRead-only observed lease metadata from the Hosts table: name, addresses, address source, and remaining lease time. Defaults to 100 entries; --all lists everything. No required arguments.\nexamples: router-axi leases; router-axi leases --all --json\n",
	"dhcp":     "usage: router-axi dhcp [--host ADDRESS] [--json] [--help]\nRead-only DHCP server configuration from documented LANHostConfigManagement actions advertised by the router. Reports server state and available range, subnet, router, DNS, and domain settings; never reservation inventory.\nexamples: router-axi dhcp; router-axi dhcp --json\n",
	"firmware": "usage: router-axi firmware [--host ADDRESS] [--json] [--help]\nRead-only installed firmware, reported update availability, and auto-update configuration from UserInterface:GetInfo and X_AVM-DE_GetInfo. Does not refresh the update check or change configuration.\nexamples: router-axi firmware; router-axi firmware --json\n",
	"account":  "usage: router-axi account [--host ADDRESS] [--json] [--help]\nRead-only current username, configured rights, anonymous login, default password posture, and second-factor enabled state. Never enumerates users or retrieves passwords.\nexamples: router-axi account; router-axi account --json\n",
	"dsl":      "usage: router-axi dsl [--host ADDRESS] [--json] [--help]\nRead-only DSL link diagnostics from documented WANDSLInterfaceConfig:X_AVM-DE_GetDSLInfo. Reports link state, rates, margins, attenuation, and error counters; never credentials or line identifiers.\nexamples: router-axi dsl; router-axi dsl --json\n",
	"wifi":     "usage: router-axi wifi [detail [--instance N]] [--host ADDRESS] [--json] [enable|disable [--instance N] --confirm] [--help]\nRead-only Wi-Fi radio inspection; wifi detail reports one radio's safe documented properties. wifi enable|disable changes one radio: --instance N (1 or greater; required when the router advertises more than one radio), preview without --confirm.\nexamples: router-axi wifi; router-axi wifi detail --instance 1; router-axi wifi disable --instance 1 --confirm\n",
	"guest":    "usage: router-axi guest [--host ADDRESS] [--json] [--help]\nRead-only documented guest Wi-Fi inspection: public SSID and aggregate radio state only; never keys, BSSIDs, or client details.\nexamples: router-axi guest; router-axi guest --json\n",
	"forwards": "usage: router-axi forwards [--all] [--host ADDRESS] [--json] [--help]\nRead-only port-forwarding rules. Defaults to 100 entries; --all lists everything. No required arguments.\nexamples: router-axi forwards; router-axi forwards --all --json\n",
	"version":  "usage: router-axi version [--json] [--help]\nPrint the router-axi version without contacting the router.\nexamples: router-axi version; router-axi version --json\n",
}

// commandFlags is the single source of truth for the flags each command
// accepts in parse(). The unknown-option error hint is derived from it, so
// the suggested flags cannot drift from the flags parse actually accepts.
type flagSpec struct {
	valid   func(options) bool
	invalid string
}

func alwaysValid(options) bool { return true }

func wifiMutation(opts options) bool {
	return opts.command == "wifi" && (opts.action == "enable" || opts.action == "disable")
}

func wifiTargeted(opts options) bool { return opts.command == "wifi" && opts.action != "" }

func deviceDetail(opts options) bool { return opts.command == "devices" && opts.action == "detail" }

const deviceDetailTarget = "--ip requires exactly one IPv4 address"

func wanReconnect(opts options) bool { return opts.command == "wan" && opts.action == "reconnect" }

func confirmable(opts options) bool {
	return opts.command == "reboot" || opts.command == "wake" || wanReconnect(opts) || wifiMutation(opts)
}

var flagSpecs = map[string]flagSpec{
	"--group":    {valid: func(opts options) bool { return opts.command == "event-log" }, invalid: "--group is valid only with event-log"},
	"--limit":    {valid: func(opts options) bool { return opts.command == "event-log" }, invalid: "--limit is valid only with event-log"},
	"--host":     {valid: alwaysValid},
	"--json":     {valid: alwaysValid},
	"--help":     {valid: alwaysValid},
	"--interval": {valid: func(opts options) bool { return opts.command == "watch" }, invalid: "--interval and --count are valid only with watch"},
	"--count":    {valid: func(opts options) bool { return opts.command == "watch" }, invalid: "--interval and --count are valid only with watch"},
	"--all": {valid: func(opts options) bool {
		return opts.command == "calls" || opts.command == "devices" && opts.action == "" || opts.command == "leases" || opts.command == "forwards"
	}, invalid: "--all is valid only for calls, devices, leases, or forwards"},
	"--instance": {valid: wifiTargeted, invalid: "--instance is valid only with wifi detail, wifi enable, or wifi disable"},
	"--ip":       {valid: deviceDetail, invalid: "--ip is valid only with devices detail"},
	"--confirm":  {valid: confirmable, invalid: "--confirm is valid only with reboot, wake, wan reconnect, wifi enable, or wifi disable"},
	"--output":   {valid: func(opts options) bool { return opts.command == "backup" }, invalid: "--output is valid only with backup"},
	"--force":    {valid: func(opts options) bool { return opts.command == "backup" }, invalid: "--force is valid only with backup"},
	"--path":     {valid: func(opts options) bool { return opts.command == "skill" && opts.action == "install" }, invalid: "--path is valid only with skill install"},
	"--agent":    {valid: func(opts options) bool { return opts.command == "setup" }, invalid: "--agent is valid only with setup"},
}

var commandFlags = map[string][]string{
	"event-log": {"--host", "--group", "--limit", "--json", "--help"},

	"doctor":   {"--host", "--json", "--help"},
	"status":   {"--host", "--json", "--help"},
	"overview": {"--host", "--json", "--help"},
	"wan":      {"--host", "--json", "--confirm", "--help"},
	"traffic":  {"--host", "--json", "--help"},
	"guest":    {"--host", "--json", "--help"},
	"watch":    {"--host", "--json", "--interval", "--count", "--help"},
	"calls":    {"--host", "--json", "--all", "--help"},
	"devices":  {"--host", "--json", "--all", "--ip", "--help"},
	"leases":   {"--host", "--json", "--all", "--help"},
	"dhcp":     {"--host", "--json", "--help"},
	"dsl":      {"--host", "--json", "--help"},
	"firmware": {"--host", "--json", "--help"},
	"account":  {"--host", "--json", "--help"},
	"forwards": {"--host", "--json", "--all", "--help"},
	"wifi":     {"--host", "--json", "--instance", "--confirm", "--help"},
	"reboot":   {"--host", "--json", "--confirm", "--help"},
	"wake":     {"--host", "--json", "--confirm", "--help"},
	"backup":   {"--host", "--json", "--output", "--force", "--help"},
	"skill":    {"--path", "--json", "--help"},
	"setup":    {"--agent", "--json", "--help"},
	"session":  {"--json", "--help"},
	"version":  {"--json", "--help"},
}

func validateFlagContext(opts options) error {
	command := opts.command
	if command == "" {
		command = "status"
	}
	flags, ok := commandFlags[command]
	if !ok {
		return nil
	}
	allowed := make(map[string]bool, len(flags))
	for _, flag := range flags {
		allowed[flag] = true
	}
	for _, flag := range []string{"--group", "--limit", "--interval", "--count", "--all", "--instance", "--ip", "--confirm", "--output", "--force", "--path", "--agent", "--host", "--json", "--help"} {
		if !opts.flags[flag] {
			continue
		}
		spec := flagSpecs[flag]
		if !allowed[flag] || !spec.valid(opts) {
			message := spec.invalid
			if message == "" {
				message = flag + " is not valid for " + opts.command
			}
			return errors.New(message)
		}
	}
	return nil
}

func usageHint(opts options) string {
	if flags, ok := commandFlags[opts.command]; ok {
		valid := make([]string, 0, len(flags))
		for _, flag := range flags {
			if flagSpecs[flag].valid(opts) {
				valid = append(valid, flag)
			}
		}
		return "valid flags for " + opts.command + ": " + strings.Join(valid, ", ")
	}
	return "router-axi help"
}

func help(command, action string) string {
	if command == "wan" && action == "detail" {
		return "usage: router-axi wan detail [--host ADDRESS] [--json] [--help]\nRead-only bounded WAN physical-link detail from documented WANCommonInterfaceConfig actions.\nGetCommonLinkProperties is required. Advertised common-interface actions add sync/tariff bit rates, provider, byte/packet totals, and bounded per-sync-group byte-rate series. Active WAN X_GetDNSServers adds DNS servers. These are not watch's observed-delta rates. sync_groups carries the byte rates in router order; the two router_reported byte-rate fields are always unknown. A tariff rate of 0 is unknown.\nexamples: router-axi wan detail; router-axi wan detail --json\n"
	}
	if command == "wan" && action == "reconnect" {
		return "usage: router-axi wan reconnect [--confirm] [--host ADDRESS] [--json] [--help]\nWithout --confirm: preview only. With --confirm: send one documented ForceTermination to the active WAN connection service; the internet connection drops and a new external address may be assigned.\nNo prompts, retries, or recovery polling; wan reconnect is not idempotent.\nexamples: router-axi wan reconnect; router-axi wan reconnect --confirm\n"
	}
	if command == "wifi" && action == "detail" {
		return "usage: router-axi wifi detail [--instance N] [--host ADDRESS] [--json] [--help]\nRead-only per-radio Wi-Fi detail from documented WLANConfiguration:GetInfo and GetChannelInfo, validated against the service description first.\nReports enable status, status, standard, max bitrate, channel, and band; absent optional fields are unknown. --instance N (1 or greater) is required when the router advertises more than one radio.\nBSSIDs, keys, and client details are never read or printed.\nexamples: router-axi wifi detail; router-axi wifi detail --instance 2; router-axi wifi detail --instance 1 --json\n"
	}
	if command == "devices" && action == "detail" {
		return "usage: router-axi devices detail --ip ADDRESS [--host ADDRESS] [--json] [--help]\nRead-only detail for exactly one LAN device from documented Hosts:X_AVM-DE_GetSpecificHostEntryByIP, validated against the service description first.\nReports the devices fields plus Ethernet port, speed in Mbit/s, guest and VPN flags, WAN access, and firmware update state; absent optional fields are unknown. --ip takes one IPv4 address and is required; the command never reports more than one device.\nThe logged-in account needs the App or Phone right. No other returned field is kept or printed.\nexamples: router-axi devices detail --ip 192.0.2.20; router-axi devices detail --ip 192.0.2.20 --json\n"
	}
	if action != "" && command == "wifi" {
		return "usage: router-axi wifi " + action + " [--instance N] --confirm [--host ADDRESS] [--json] [--help]\nEnables or disables one WLANConfiguration radio. Without --confirm: preview only, nothing changes.\nWith --confirm: idempotent change; the router must confirm the new state. --instance N is required when the router advertises more than one radio; bounds N 1 or greater.\nNo prompts or retries; SSIDs, BSSIDs, and keys are never read or printed.\nexamples: router-axi wifi " + action + "; router-axi wifi " + action + " --instance 1 --confirm; router-axi wifi " + action + " --instance 2 --confirm --json\n"
	}

	if command == "watch" {
		return "usage: router-axi watch [--interval DURATION] [--count N] [--host ADDRESS] [--json] [--help]\nRead-only WAN state and traffic samples; defaults: --interval 5s --count 6.\nBounds: interval 1s..1m, count 1..3600; no unbounded mode. First sample is immediate; subsequent reads wait after completion.\n--json streams one object per line (JSONL). Errors stop polling; Ctrl-C/SIGTERM cancel with exit 130.\nexamples: router-axi watch; router-axi watch --interval 2s --count 10 --json\n"
	}
	if command == "reboot" {
		return "usage: router-axi reboot [--confirm] [--host ADDRESS] [--json] [--help]\nWithout --confirm: preview only. With --confirm: restart the router and temporarily interrupt all local services.\nNo prompts, retries, or recovery polling; reboot is not idempotent.\nexamples: router-axi reboot; router-axi reboot --confirm\n"
	}
	if command == "setup" {
		return "usage: router-axi setup install|check|uninstall --agent claude|codex|opencode|all [--json] [--help]\nExplicitly installs, inspects, or removes a managed offline SessionStart integration. No router request is made during setup or session start.\nRepeated installs repair the managed executable path; uninstall removes only router-axi-managed configuration.\nexamples: router-axi setup install --agent all; router-axi setup check --agent claude; router-axi setup uninstall --agent opencode\n"
	}
	if command == "session" {
		return "usage: router-axi session dashboard [--json] [--help]\nPrint the compact offline context used by installed session integrations; it never contacts the router.\nexamples: router-axi session dashboard; router-axi session dashboard --json\n"
	}
	if command == "backup" {
		return "usage: router-axi backup --output PATH [--force] [--host ADDRESS] [--json] [--help]\nDownloads the documented DeviceConfig:X_AVM-DE_GetConfigFile export to PATH with an atomic owner-only write.\nThe export passphrase is read only from ROUTER_AXI_BACKUP_PASSWORD and is required to restore the file.\nAn existing file is never overwritten without --force; the router origin must be HTTPS (for example --host https://fritz.box:49443) with a locally trusted certificate, so the passphrase never travels in plaintext.\nNo prompts. Examples: router-axi backup --host https://fritz.box:49443 --output fritz.export; router-axi backup --host https://fritz.box:49443 --output fritz.export --force\n"
	}
	if text, ok := commandHelp[command]; ok {
		return text
	}
	if validCommand(command) {
		extra := ""
		if command == "calls" || command == "devices" || command == "leases" || command == "forwards" {
			extra = " [--all]"
		}
		if command == "wifi" {
			extra = " [detail [--instance N]] [enable|disable [--instance N] --confirm]"
		}
		if command == "skill" {
			if action != "" {
				return "usage: router-axi skill install [--path DIRECTORY] [--json] [--help]\nInstalls the bundled router-axi Agent Skill (SKILL.md) for the current agent.\nThe default destination is the user agent skills directory; --path selects another parent directory.\nRepeated installs with identical content are silent no-ops. This command is the only installation path; nothing is registered automatically.\nexamples: router-axi skill install; router-axi skill install --path ~/.agents/skills\n"
			}
			return "usage: router-axi skill install [--path DIRECTORY] [--json] [--help]\n"
		}
		return "usage: router-axi " + command + " [--host ADDRESS] [--json]" + extra + " [--help]\n"
	}
	return "usage: router-axi [--host ADDRESS] [--json] [command]\n\ncommands:\n  doctor    bounded connectivity and capability diagnosis\n  status    router identity and firmware (default)\n  overview  identity, WAN state, and traffic totals\n  wan       internet connection state; wan detail adds bounded link details; wan reconnect drops the connection once with --confirm\n  traffic   byte totals\n  watch     bounded WAN state and traffic polling (6 samples, 5s interval)\n  calls     call history\n  event-log bounded router events (telephony excluded by default)\n  devices   connected and known LAN clients; devices detail adds one device's link and access state\n  leases    observed Hosts table lease metadata\n  dhcp      DHCP server configuration (never reservation inventory)\n  dsl       DSL link diagnostics\n  firmware  installed firmware, reported update availability, and auto-update state\n  account   own rights and login posture\n  wifi      Wi-Fi inspection; wifi detail adds per-radio properties; wifi enable|disable changes a radio with --confirm\n  guest     documented guest Wi-Fi inspection\n  forwards  port-forwarding rules\n  reboot    preview router restart; execute once with --confirm\n  wake      preview Wake-on-LAN to one MAC; send once with --confirm\n  backup    download the documented configuration export to a file\n  skill     install the router-axi agent skill (explicit opt-in)\n  setup     manage opt-in Claude Code, Codex, and OpenCode session integrations\n  session   print the offline session dashboard\n  version   CLI version\n\nauthentication: ROUTER_AXI_USERNAME and ROUTER_AXI_PASSWORD\nbackup export passphrase: ROUTER_AXI_BACKUP_PASSWORD\n"
}

func writeJSON(w io.Writer, value any) int {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return ExitInternal
	}
	return ExitOK
}

const cliDescription = "router-axi inspects and operates an AVM FRITZ!Box router over TR-064 with read-only reports and confirmed Wi-Fi, WAN reconnect, reboot, Wake-on-LAN, and backup actions"

func (a *App) selfIdentification() string {
	bin := "unknown"
	if path, err := a.executable(); err == nil {
		home, homeErr := a.homeDir()
		bin = collapseHome(path, home, homeErr)
	}
	return "bin: " + bin + "\ndescription: " + cliDescription + "\n"
}

func collapseHome(path string, home string, err error) string {
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+string(os.PathSeparator)) {
		return "~" + path[len(home):]
	}
	return path
}

func writeCompact(a *App, w io.Writer, command string, value any) error {
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
			{"status", v.Capabilities.Status}, {"overview", v.Capabilities.Overview}, {"wan", v.Capabilities.WAN}, {"traffic", v.Capabilities.Traffic}, {"watch", v.Capabilities.Watch}, {"calls", v.Capabilities.Calls}, {"devices", v.Capabilities.Devices}, {"leases", v.Capabilities.Leases}, {"dhcp", v.Capabilities.DHCP}, {"dsl", v.Capabilities.DSL}, {"firmware", v.Capabilities.Firmware}, {"account", v.Capabilities.Account}, {"wifi", v.Capabilities.WiFi}, {"forwards", v.Capabilities.Forwards}, {"reboot", v.Capabilities.Reboot}, {"backup", v.Capabilities.Backup}, {"event_log", v.Capabilities.EventLog},
		} {
			if _, err := fmt.Fprintf(w, "  %s: %s\n", capability.name, check(capability.check)); err != nil {
				return err
			}
		}
		return nil
	case "status":
		v := value.(tr064.Status)
		_, err := fmt.Fprintf(w, a.selfIdentification()+"router:\n  manufacturer: %s\n  model: %s\n  software: %s\n  hardware: %s\n  serial: %s\n  uptime: %s\nnext: router-axi wan\n", scalar(v.Manufacturer), scalar(v.Model), scalar(v.Software), scalar(v.Hardware), scalar(v.Serial), duration(v.UptimeSeconds))
		return err
	case "overview":
		v := value.(tr064.Overview)
		_, err := fmt.Fprintf(w, "overview:\n  router: %s (%s)\n  wan: %s, %s, %s\n  traffic: %s downloaded, %s uploaded\n  observed_at: %s\n", scalar(v.Router.Model), scalar(v.Router.Software), scalar(v.WAN.Status), scalar(v.WAN.ExternalIP), scalar(v.WAN.IPFamily), size(v.Traffic.TotalDownloadBytes), size(v.Traffic.TotalUploadBytes), scalar(v.Traffic.ObservedAt))
		return err
	case "wan":
		if v, ok := value.(tr064.WANDetail); ok {
			_, err := fmt.Fprintf(w, "wan_detail:\n  access_type: %s\n  physical_link_status: %s\n  max_download_bits_per_second: %d\n  max_upload_bits_per_second: %d\n  router_reported_download_bytes_per_second: %s\n  router_reported_upload_bytes_per_second: %s\n  total_download_bytes: %s\n  total_upload_bytes: %s\n  dns_servers: %s\n", scalar(v.AccessType), scalar(v.PhysicalLinkStatus), v.MaxDownloadBitsPerSecond, v.MaxUploadBitsPerSecond, optionalUint(v.RouterReportedDownloadBytesPerSecond), optionalUint(v.RouterReportedUploadBytesPerSecond), optionalUint(v.TotalDownloadBytes), optionalUint(v.TotalUploadBytes), strings.Join(v.DNSServers, ","))
			if err != nil {
				return err
			}
			provider := "unknown"
			if v.Provider != nil {
				provider = strconv.Quote(*v.Provider)
			}
			_, err = fmt.Fprintf(w, "  total_download_packets: %s\n  total_upload_packets: %s\n  sync_download_bits_per_second: %s\n  sync_upload_bits_per_second: %s\n  tariff_download_bits_per_second: %s\n  tariff_upload_bits_per_second: %s\n  provider: %s\n", optionalUint(v.TotalDownloadPackets), optionalUint(v.TotalUploadPackets), optionalUint(v.SyncDownloadBitsPerSecond), optionalUint(v.SyncUploadBitsPerSecond), optionalUint(v.TariffDownloadBitsPerSecond), optionalUint(v.TariffUploadBitsPerSecond), provider)
			if err != nil {
				return err
			}
			if v.SyncGroups == nil {
				_, err = fmt.Fprintln(w, "  sync_groups: unknown")
				return err
			}
			if len(v.SyncGroups) == 0 {
				_, err = fmt.Fprintln(w, "  sync_groups: 0 groups found")
				return err
			}
			_, err = fmt.Fprintf(w, "  sync_groups[%d]{index,max_download_bytes_per_second,max_upload_bytes_per_second,ds_current_bytes_per_second,mc_current_bytes_per_second,upload_bytes_per_second,realtime_upload_bytes_per_second,high_upload_bytes_per_second,default_upload_bytes_per_second,low_upload_bytes_per_second}:\n", len(v.SyncGroups))
			if err != nil {
				return err
			}
			for _, group := range v.SyncGroups {
				_, err = fmt.Fprintf(w, "    %d,%d,%d,%s,%s,%s,%s,%s,%s,%s\n", group.Index, group.MaxDownloadBytesPerSecond, group.MaxUploadBytesPerSecond, wanRateSeries(group.DSCurrentBytesPerSecond), wanRateSeries(group.MCCurrentBytesPerSecond), wanRateSeries(group.UploadBytesPerSecond), wanRateSeries(group.RealtimeUploadBytesPerSecond), wanRateSeries(group.HighUploadBytesPerSecond), wanRateSeries(group.DefaultUploadBytesPerSecond), wanRateSeries(group.LowUploadBytesPerSecond))
				if err != nil {
					return err
				}
			}
			return err
		}
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
		if v, ok := value.(tr064.RadioDetail); ok {
			_, err := fmt.Fprintf(w, "wifi_detail:\n  instance: %s\n  enabled: %t\n  status: %s\n  standard: %s\n  max_bit_rate: %s\n  channel: %s\n  band: %s\n", scalar(v.ServiceID), v.Enabled, optionalText(v.Status), scalar(v.Standard), optionalText(v.MaxBitRate), optionalUint(v.Channel), scalar(v.Band))
			return err
		}
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
	case "guest":
		result := value.(guestResult)
		if len(result.Guests) == 0 {
			_, err := io.WriteString(w, "guests[0]: no guest Wi-Fi networks found\n")
			return err
		}
		if _, err := fmt.Fprintf(w, "guests[%d]{service_id,ssid,enabled,channel,band,standard,associated_clients,security_mode}:\n", len(result.Guests)); err != nil {
			return err
		}
		for _, guest := range result.Guests {
			if _, err := fmt.Fprintf(w, "  %s,%s,%t,%d,%s,%s,%d,%s\n", toon(guest.ServiceID), toon(guest.SSID), guest.Enabled, guest.Channel, toon(guest.Band), toon(guest.Standard), guest.AssociatedClients, toon(guest.SecurityMode)); err != nil {
				return err
			}
		}
	case "devices":
		if v, ok := value.(tr064.DeviceDetail); ok {
			return writeDeviceDetail(w, v)
		}
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
	case "dhcp":
		v := value.(tr064.DHCP)
		_, err := fmt.Fprintf(w, "dhcp:\n  server_configurable: %s\n  server_enabled: %s\n  relay_enabled: %s\n  address_range_start: %s\n  address_range_end: %s\n  subnet_mask: %s\n  routers: %s\n  dns_servers: %s\n  domain_name: %s\n", optionalBool(v.ServerConfigurable), optionalBool(v.ServerEnabled), optionalBool(v.RelayEnabled), optionalText(v.AddressRangeStart), optionalText(v.AddressRangeEnd), optionalText(v.SubnetMask), scalar(strings.Join(v.Routers, ",")), scalar(strings.Join(v.DNSServers, ",")), optionalToon(v.DomainName))
		return err
	case "event-log":
		return writeEventLog(w, value.(tr064.EventLog))
	case "firmware":
		v := value.(tr064.Firmware)
		_, err := fmt.Fprintf(w, "firmware:\n  current_version: %s\n  update_available: %s\n  offered_version: %s\n  update_state: %s\n  build_type: %s\n  auto_update_mode: %s\n  update_time: %s\n  last_version: %s\n  update_successful: %s\n", optionalToon(v.CurrentVersion), optionalBool(v.UpdateAvailable), optionalToon(v.OfferedVersion), optionalToon(v.UpdateState), optionalToon(v.BuildType), optionalToon(v.AutoUpdateMode), optionalToon(v.UpdateTime), optionalToon(v.LastVersion), optionalToon(v.UpdateSuccessful))
		return err
	case "account":
		return writeAccount(w, value.(tr064.Account))
	case "dsl":
		v := value.(tr064.DSL)
		_, err := fmt.Fprintf(w, "dsl:\n  link_status: %s\n  modulation_type: %s\n  current_profile: %s\n  upstream_current_kbps: %d\n  downstream_current_kbps: %d\n  upstream_max_kbps: %d\n  downstream_max_kbps: %d\n  upstream_noise_margin_tenth_db: %d\n  downstream_noise_margin_tenth_db: %d\n  upstream_attenuation_tenth_db: %d\n  downstream_attenuation_tenth_db: %d\n  fec_errors: %d\n  crc_errors: %d\n  atur_vendor: %s\n  atur_country: %s\n  upstream_power_tenth_dbm: %s\n  downstream_power_tenth_dbm: %s\n", scalar(v.LinkStatus), scalar(v.ModulationType), scalar(v.CurrentProfile), v.UpstreamCurrentKbps, v.DownstreamCurrentKbps, v.UpstreamMaxKbps, v.DownstreamMaxKbps, v.UpstreamNoiseMarginTenthDB, v.DownstreamNoiseMarginTenthDB, v.UpstreamAttenuationTenthDB, v.DownstreamAttenuationTenthDB, v.FECErrors, v.CRCErrors, optionalText(v.ATURVendor), optionalText(v.ATURCountry), optionalInt(v.UpstreamPowerTenthDBm), optionalInt(v.DownstreamPowerTenthDBm))
		return err
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

func protocolError(err error) errorSpec {
	var protocolErr *tr064.Error
	if !errors.As(err, &protocolErr) {
		return errorSpec{ExitInternal, errorDetail{"internal_error", err.Error(), ""}}
	}
	switch protocolErr.Kind {
	case "usage":
		hint := "router-axi wifi enable|disable --instance N"
		if protocolErr.Operation == "wifi detail" {
			hint = "router-axi wifi detail --instance N"
		}
		if protocolErr.Operation == "reboot" {
			hint = "router-axi reboot --help"
		}
		if protocolErr.Operation == "wan reconnect" {
			hint = "router-axi wan reconnect --help"
		}
		if protocolErr.Operation == "wake" {
			hint = "router-axi wake --help"
		}
		if protocolErr.Operation == "backup" {
			hint = "router-axi backup --help"
		}
		if protocolErr.Operation == "devices detail" {
			hint = "router-axi devices"
		}
		return errorSpec{ExitUsage, errorDetail{protocolErr.Code, protocolErr.Message, hint}}
	case "auth":
		return errorSpec{ExitAuth, errorDetail{"authentication_failed", protocolErr.Message, "set ROUTER_AXI_USERNAME and ROUTER_AXI_PASSWORD"}}
	case "network":
		if protocolErr.Code == "tls_untrusted" {
			return errorSpec{ExitNetwork, errorDetail{"tls_untrusted", protocolErr.Message, "trust the router's HTTPS certificate on this system; verification is never skipped"}}
		}
		return errorSpec{ExitNetwork, errorDetail{"router_unreachable", protocolErr.Message, "check --host and local network access"}}
	case "unsupported":
		return errorSpec{ExitUnsupported, errorDetail{"unsupported_capability", protocolErr.Message, ""}}
	default:
		if hint, ok := backupDiagnosticHint(protocolErr.Operation, protocolErr.Code); ok {
			return errorSpec{ExitRouter, errorDetail{protocolErr.Code, protocolErr.Message, hint}}
		}
		return errorSpec{ExitRouter, errorDetail{"router_protocol_error", protocolErr.Message, ""}}
	}
}

func backupDiagnosticHint(operation, code string) (string, bool) {
	if operation != "backup" {
		return "", false
	}
	switch code {
	case "backup_action_rejected":
		return "the router advertised DeviceConfig but rejected configuration export; check supported firmware or use the FRITZ!Box web interface", true
	case "backup_invalid_response":
		return "the router returned an unexpected configuration export response; check supported firmware", true
	case "backup_unsafe_download_url":
		return "the router returned an unsafe configuration download address; export was refused", true
	case "backup_download_rejected":
		return "the router rejected the configuration download; check supported firmware or use the FRITZ!Box web interface", true
	case "backup_export_too_large":
		return "the router returned an export larger than the supported safety limit", true
	default:
		return "", false
	}
}

func renderProtocolError(w io.Writer, jsonOutput bool, err error) int {
	spec := protocolError(err)
	return writeError(w, jsonOutput, spec.exit, spec.detail.Code, spec.detail.Message, spec.detail.Hint)
}

func writeDoctorPartialError(w io.Writer, doctor tr064.Doctor, spec errorSpec) int {
	if writeJSON(w, doctorPartialErrorResult{Doctor: doctor, Error: spec.detail}) != ExitOK {
		return ExitInternal
	}
	return spec.exit
}

func writeError(w io.Writer, jsonOutput bool, exit int, code, message, hint string) int {
	if jsonOutput {
		if writeJSON(w, errorResult{Error: errorDetail{code, message, hint}}) != ExitOK {
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

func (a *App) runSkill(opts options, stdout, stderr io.Writer) int {
	dir := opts.path
	if dir == "" {
		home, err := a.homeDir()
		if err != nil {
			return writeError(stderr, opts.json, ExitInternal, "skill_install_failed", "could not determine the user home directory", "pass --path DIRECTORY with the agent skills parent directory")
		}
		dir = filepath.Join(home, ".agents", "skills")
	}
	path := filepath.Join(dir, skill.Name, "SKILL.md")
	written, err := skill.Install(path)
	if err != nil {
		return writeError(stderr, opts.json, ExitInternal, "skill_install_failed", "the skill file could not be written to the requested path", "check the destination directory and permissions")
	}
	if !written {
		return ExitOK
	}
	result := skillResult{Path: path, Installed: true}
	if opts.json {
		return writeJSON(stdout, skillJSONResult{Skill: result})
	}
	if _, err := fmt.Fprintf(stdout, "skill:\n  path: %s\n  installed: %t\n", strconv.Quote(result.Path), result.Installed); err != nil {
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
	return writeSendOnce(w, sendOnceReport{key: "reboot", command: "reboot", effect: rebootEffect, recovery: rebootRecovery}, result.Endpoint, result.Preview, result.Accepted, jsonOutput)
}

type sendOnceReport struct{ key, command, effect, recovery, mac string }

func writeSendOnce(w io.Writer, report sendOnceReport, endpoint string, preview, accepted, jsonOutput bool) int {
	var err error
	if preview {
		execute := "router-axi " + report.command + " --host " + shellWord(endpoint) + " --confirm"
		if jsonOutput {
			execute += " --json"
			return writeJSON(w, map[string]sendOncePreviewState{report.key: {Endpoint: endpoint, MAC: report.mac, Preview: true, Effect: report.effect, Execute: execute}})
		}
		_, err = fmt.Fprintf(w, "%s:\n  endpoint: %s\n%s  preview: true\n  effect: %s\n  execute: %s\n", report.key, strconv.Quote(endpoint), sendOnceTarget(report.mac), report.effect, strconv.Quote(execute))
	} else {
		if jsonOutput {
			return writeJSON(w, map[string]sendOnceAcceptedState{report.key: {Endpoint: endpoint, MAC: report.mac, Accepted: accepted, Recovery: report.recovery}})
		}
		_, err = fmt.Fprintf(w, "%s:\n  endpoint: %s\n%s  accepted: %t\n  recovery: %s\n", report.key, strconv.Quote(endpoint), sendOnceTarget(report.mac), accepted, report.recovery)
	}
	if err != nil {
		return ExitInternal
	}
	return ExitOK
}

func sendOnceTarget(mac string) string {
	if mac == "" {
		return ""
	}
	return "  mac: " + strconv.Quote(mac) + "\n"
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
func optionalUint(value *uint64) string {
	if value == nil {
		return "unknown"
	}
	return strconv.FormatUint(*value, 10)
}

func optionalInt(value *uint16) string {
	if value == nil {
		return "unknown"
	}
	return strconv.FormatUint(uint64(*value), 10)
}

func optionalText(value *string) string {
	if value == nil {
		return "unknown"
	}
	return *value
}

func optionalToon(value *string) string {
	if value == nil {
		return "unknown"
	}
	return toon(*value)
}

func optionalBool(value *bool) string {
	if value == nil {
		return "unknown"
	}
	return strconv.FormatBool(*value)
}
func duration(seconds uint64) string {
	return fmt.Sprintf("%dd %02dh %02dm", seconds/86400, seconds%86400/3600, seconds%3600/60)
}
func size(bytes uint64) string { return fmt.Sprintf("%.2f GB", float64(bytes)/1_000_000_000) }

func wanRateSeries(values []uint64) string {
	parts := make([]string, len(values))
	for index, value := range values {
		parts[index] = strconv.FormatUint(value, 10)
	}
	return strconv.Quote(strings.Join(parts, ","))
}
