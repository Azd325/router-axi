package app

import (
	"fmt"
	"io"

	"github.com/Azd325/router-axi/internal/tr064"
)

func writeWiFiConfiguration(w io.Writer, v tr064.RadioDetail) error {
	if n := v.NightControl; n == nil {
		if _, err := io.WriteString(w, "  night_control: unavailable\n"); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintf(w, "  night_control:\n    schedule: %s\n    no_forced_off: %s\n", optionalToon(n.Schedule), optionalBool(n.NoForcedOff)); err != nil {
		return err
	}
	if p := v.WPS; p == nil {
		_, err := io.WriteString(w, "  wps: unavailable\n")
		return err
	} else {
		_, err := fmt.Fprintf(w, "  wps:\n    mode: %s\n    status: %s\n", optionalText(p.Mode), optionalText(p.Status))
		return err
	}
}

func writeGuestConfiguration(w io.Writer, guests []tr064.GuestNetwork) error {
	if _, err := fmt.Fprintf(w, "guest_configuration[%d]{service_id,timeout_active,timeout,time_remain,no_forced_off,user_isolation}:\n", len(guests)); err != nil {
		return err
	}
	for _, guest := range guests {
		c := guest.Configuration
		if _, err := fmt.Fprintf(w, "  %s,%s,%s,%s,%s,%s\n", tableCell(toon(guest.ServiceID)), tableCell(optionalToon(c.TimeoutActive)), tableCell(optionalToon(c.Timeout)), tableCell(optionalToon(c.TimeRemain)), tableCell(optionalToon(c.NoForcedOff)), tableCell(optionalToon(c.UserIsolation))); err != nil {
			return err
		}
	}
	return nil
}
