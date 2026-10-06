package app

import (
	"fmt"
	"io"

	"github.com/Azd325/router-axi/internal/tr064"
)

func writeWANConnection(w io.Writer, value tr064.WANDetail) error {
	if _, err := fmt.Fprintf(w, "  connection_service: %s\n", scalar(value.ConnectionService)); err != nil {
		return err
	}
	if c := value.Connection; c == nil {
		if _, err := io.WriteString(w, "  connection: unsupported\n"); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintf(w, "  connection:\n    type: %s\n    ipv6_status: %s\n", c.Type, optionalText(c.IPv6Status)); err != nil {
		return err
	}
	if n := value.NAT; n == nil {
		if _, err := io.WriteString(w, "  nat: unsupported\n"); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintf(w, "  nat:\n    enabled: %t\n    rsip_available: %t\n", n.Enabled, n.RSIPAvailable); err != nil {
		return err
	}
	absent := "unsupported"
	if value.ConnectionService != tr064.WANPPPConnectionService {
		absent = "not_applicable"
	}
	if r := value.LinkLayerMaxBitRates; r == nil {
		if _, err := fmt.Fprintf(w, "  link_layer_max_bit_rates: %s\n", absent); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintf(w, "  link_layer_max_bit_rates:\n    upstream: %d\n    downstream: %d\n", r.Upstream, r.Downstream); err != nil {
		return err
	}
	p := value.DisconnectPrevention
	if p == nil {
		_, err := fmt.Fprintf(w, "  disconnect_prevention: %s\n", absent)
		return err
	}
	_, err := fmt.Fprintf(w, "  disconnect_prevention:\n    enabled: %t\n    hour: %d\n", p.Enabled, p.Hour)
	return err
}
