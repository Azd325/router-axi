package app

import (
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/Azd325/router-axi/internal/tr064"
)

var numberLike = regexp.MustCompile(`^-?\d+(\.\d+)?([eE][+-]?\d+)?$`)

func ambiguousScalar(value string) bool {
	switch value {
	case "true", "false", "null":
		return true
	}
	return numberLike.MatchString(value)
}

func quoteAmbiguous(value string) string {
	if ambiguousScalar(value) {
		return strconv.Quote(value)
	}
	return value
}

func tableCell(value string) string {
	if strings.Contains(value, ":") && !strings.HasPrefix(value, `"`) {
		return strconv.Quote(value)
	}
	return value
}

type tableColumn[T any] struct {
	name string
	cell func(T) string
}

func columnNames[T any](columns []tableColumn[T]) []string {
	names := make([]string, len(columns))
	for i, column := range columns {
		names[i] = column.name
	}
	return names
}

func writeTable[T any](w io.Writer, key string, rows []T, columns []tableColumn[T], defaults, fields []string) error {
	if len(fields) == 0 {
		fields = defaults
	}
	selected := make([]tableColumn[T], 0, len(fields))
	for _, field := range fields {
		for _, column := range columns {
			if column.name == field {
				selected = append(selected, column)
			}
		}
	}
	if _, err := fmt.Fprintf(w, "%s[%d]{%s}:\n", key, len(rows), strings.Join(columnNames(selected), ",")); err != nil {
		return err
	}
	cells := make([]string, len(selected))
	for _, row := range rows {
		for i, column := range selected {
			cells[i] = column.cell(row)
		}
		for i := range cells {
			cells[i] = tableCell(cells[i])
		}
		if _, err := fmt.Fprintf(w, "  %s\n", strings.Join(cells, ",")); err != nil {
			return err
		}
	}
	return nil
}

var callColumns = []tableColumn[tr064.Call]{
	{"id", func(c tr064.Call) string { return toon(c.ID) }},
	{"direction", func(c tr064.Call) string { return toon(c.Direction) }},
	{"remote", func(c tr064.Call) string { return toon(c.Remote) }},
	{"name", func(c tr064.Call) string { return toon(c.Name) }},
	{"date", func(c tr064.Call) string { return toon(c.Date) }},
	{"duration", func(c tr064.Call) string { return toon(c.Duration) }},
}

var deviceColumns = []tableColumn[tr064.Device]{
	{"name", func(d tr064.Device) string { return toon(d.Name) }},
	{"ip_address", func(d tr064.Device) string { return toon(d.IPAddress) }},
	{"mac_address", func(d tr064.Device) string { return toon(d.MACAddress) }},
	{"interface_type", func(d tr064.Device) string { return toon(d.InterfaceType) }},
	{"active", func(d tr064.Device) string { return strconv.FormatBool(d.Active) }},
}

var radioColumns = []tableColumn[tr064.Radio]{
	{"service_id", func(r tr064.Radio) string { return toon(r.ServiceID) }},
	{"ssid", func(r tr064.Radio) string { return toon(r.SSID) }},
	{"enabled", func(r tr064.Radio) string { return strconv.FormatBool(r.Enabled) }},
	{"channel", func(r tr064.Radio) string { return strconv.FormatUint(r.Channel, 10) }},
	{"band", func(r tr064.Radio) string { return toon(r.Band) }},
	{"standard", func(r tr064.Radio) string { return toon(r.Standard) }},
	{"associated_devices", func(r tr064.Radio) string { return strconv.FormatUint(r.AssociatedDevices, 10) }},
	{"security_mode", func(r tr064.Radio) string { return toon(r.SecurityMode) }},
}

var guestColumns = []tableColumn[tr064.GuestNetwork]{
	{"service_id", func(g tr064.GuestNetwork) string { return toon(g.ServiceID) }},
	{"ssid", func(g tr064.GuestNetwork) string { return toon(g.SSID) }},
	{"enabled", func(g tr064.GuestNetwork) string { return strconv.FormatBool(g.Enabled) }},
	{"channel", func(g tr064.GuestNetwork) string { return strconv.FormatUint(g.Channel, 10) }},
	{"band", func(g tr064.GuestNetwork) string { return toon(g.Band) }},
	{"standard", func(g tr064.GuestNetwork) string { return toon(g.Standard) }},
	{"associated_clients", func(g tr064.GuestNetwork) string { return strconv.FormatUint(g.AssociatedClients, 10) }},
	{"security_mode", func(g tr064.GuestNetwork) string { return toon(g.SecurityMode) }},
}

var leaseColumns = []tableColumn[tr064.Lease]{
	{"name", func(l tr064.Lease) string { return toon(l.Name) }},
	{"ip_address", func(l tr064.Lease) string { return toon(l.IPAddress) }},
	{"mac_address", func(l tr064.Lease) string { return toon(l.MACAddress) }},
	{"address_source", func(l tr064.Lease) string { return toon(l.AddressSource) }},
	{"lease_time_remaining", func(l tr064.Lease) string {
		if l.LeaseTimeRemaining == nil {
			return "unknown"
		}
		return strconv.FormatInt(*l.LeaseTimeRemaining, 10)
	}},
	{"interface_type", func(l tr064.Lease) string { return toon(l.InterfaceType) }},
	{"active", func(l tr064.Lease) string { return strconv.FormatBool(l.Active) }},
}

var forwardColumns = []tableColumn[tr064.Forward]{
	{"enabled", func(f tr064.Forward) string { return strconv.FormatBool(f.Enabled) }},
	{"protocol", func(f tr064.Forward) string { return toon(f.Protocol) }},
	{"external_port", func(f tr064.Forward) string { return strconv.FormatUint(f.ExternalPort, 10) }},
	{"internal_client", func(f tr064.Forward) string { return toon(f.InternalClient) }},
	{"internal_port", func(f tr064.Forward) string { return strconv.FormatUint(f.InternalPort, 10) }},
	{"description", func(f tr064.Forward) string { return toon(f.Description) }},
	{"remote_host", func(f tr064.Forward) string { return toon(f.RemoteHost) }},
	{"lease_duration", func(f tr064.Forward) string {
		if f.LeaseDuration == nil {
			return "unknown"
		}
		return strconv.FormatUint(*f.LeaseDuration, 10)
	}},
}

var (
	callDefaults    = columnNames(callColumns)
	deviceDefaults  = columnNames(deviceColumns)
	radioDefaults   = []string{"ssid", "enabled", "band", "channel", "associated_devices"}
	guestDefaults   = []string{"ssid", "enabled", "band", "channel", "associated_clients"}
	leaseDefaults   = []string{"name", "ip_address", "mac_address", "active"}
	forwardDefaults = []string{"enabled", "protocol", "external_port", "internal_client", "internal_port"}
)

func fieldNames(command string) []string {
	switch command {
	case "calls":
		return columnNames(callColumns)
	case "devices":
		return columnNames(deviceColumns)
	case "wifi":
		return columnNames(radioColumns)
	case "guest":
		return columnNames(guestColumns)
	case "leases":
		return columnNames(leaseColumns)
	case "forwards":
		return columnNames(forwardColumns)
	}
	return nil
}

func fieldsCommand(opts options) bool {
	return fieldNames(opts.command) != nil && opts.action == ""
}

type usageError struct{ message, hint string }

func (e *usageError) Error() string { return e.message }

func parseFields(command, value string) ([]string, error) {
	valid := fieldNames(command)
	if valid == nil {
		return nil, nil
	}
	names := strings.Split(value, ",")
	seen := map[string]bool{}
	for _, name := range names {
		known := false
		for _, candidate := range valid {
			known = known || candidate == name
		}
		if !known {
			return nil, &usageError{
				message: "unknown field for " + command + ": " + strconv.Quote(name) + "; valid fields: " + strings.Join(valid, ", "),
				hint:    "router-axi " + command + " --fields " + strings.Join(valid[:2], ","),
			}
		}
		if seen[name] {
			return nil, &usageError{
				message: "--fields lists " + name + " more than once",
				hint:    "router-axi " + command + " --fields " + strings.Join(valid[:2], ","),
			}
		}
		seen[name] = true
	}
	return names, nil
}

// hostFlag renders the --host argument for a hint. It is empty unless the user
// passed --host, and it never repeats user information from a host URL.
func hostFlag(host string) string {
	if host == "" {
		return ""
	}
	if strings.Contains(host, "://") {
		if parsed, err := url.Parse(host); err == nil && parsed.User != nil {
			parsed.User = nil
			host = parsed.String()
		}
	} else if at := strings.LastIndex(host, "@"); at >= 0 {
		host = host[at+1:]
	}
	return " --host " + shellWord(host)
}

func hintCommand(opts options, args string) string {
	return "router-axi " + args + hostFlag(opts.host)
}

func writeNext(w io.Writer, hints ...string) error {
	if len(hints) == 1 {
		_, err := fmt.Fprintf(w, "next: %s\n", toon(hints[0]))
		return err
	}
	cells := make([]string, len(hints))
	for i, hint := range hints {
		cells[i] = toon(hint)
	}
	_, err := fmt.Fprintf(w, "next[%d]: %s\n", len(hints), strings.Join(cells, ","))
	return err
}

func writeOmitted(w io.Writer, omitted int, opts options, extra ...string) error {
	hints := []string{hintCommand(opts, opts.command+" --all")}
	hints = append(hints, extra...)
	if _, err := fmt.Fprintf(w, "omitted: %d\n", omitted); err != nil {
		return err
	}
	return writeNext(w, hints...)
}
