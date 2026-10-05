package app

import (
	"fmt"
	"io"

	"github.com/Azd325/router-axi/internal/tr064"
)

func writeDeviceDetail(w io.Writer, value tr064.DeviceDetail) error {
	_, err := fmt.Fprintf(w, "device_detail:\n  name: %s\n  ip_address: %s\n  mac_address: %s\n  interface_type: %s\n  active: %t\n  port: %s\n  speed_mbps: %s\n  guest: %s\n  vpn: %s\n  wan_access: %s\n  update_available: %s\n  update_successful: %s\n", toon(value.Name), toon(value.IPAddress), toon(value.MACAddress), toon(value.InterfaceType), value.Active, optionalUint(value.Port), optionalUint(value.SpeedMbps), optionalBool(value.Guest), optionalBool(value.VPN), optionalToon(value.WANAccess), optionalBool(value.UpdateAvailable), optionalToon(value.UpdateSuccessful))
	return err
}
