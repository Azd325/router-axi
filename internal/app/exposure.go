package app

import (
	"fmt"
	"io"
	"strings"

	"github.com/Azd325/router-axi/internal/tr064"
)

func writeExposure(w io.Writer, value tr064.Exposure) error {
	var b strings.Builder
	unsupported := func(name string) { b.WriteString("    " + name + ": unsupported\n") }
	b.WriteString("exposure:\n  remote:\n")
	if v := value.Remote.RemoteAccess; v == nil {
		unsupported("remote_access")
	} else {
		fmt.Fprintf(&b, "    remote_access:\n      enabled: %t\n      port: %d\n      letsencrypt_enabled: %s\n      letsencrypt_state: %s\n", v.Enabled, v.Port, optionalBool(v.LetsEncryptEnabled), v.LetsEncryptState)
	}
	if v := value.Remote.DDNS; v == nil {
		unsupported("ddns")
	} else {
		fmt.Fprintf(&b, "    ddns:\n      enabled: %t\n      status_ipv4: %s\n      status_ipv6: %s\n", v.Enabled, v.StatusIPv4, v.StatusIPv6)
	}
	if v := value.Remote.MyFRITZ; v == nil {
		unsupported("myfritz")
	} else {
		fmt.Fprintf(&b, "    myfritz:\n      enabled: %t\n      port: %d\n      device_registered: %t\n      state: %s\n", v.Enabled, v.Port, v.DeviceRegistered, v.State)
	}
	b.WriteString("  local:\n")
	if v := value.Local.Storage; v == nil {
		unsupported("storage")
	} else {
		fmt.Fprintf(&b, "    storage:\n      ftp_enabled: %t\n      ftp_status: %s\n      smb_enabled: %t\n      ftp_wan_enabled: %s\n      ftp_wan_ssl_only: %s\n      ftp_wan_port: %s\n", v.FTPEnabled, v.FTPStatus, v.SMBEnabled, optionalBool(v.FTPWANEnabled), optionalBool(v.FTPWANSSLOnly), optionalUint(v.FTPWANPort))
	}
	if v := value.Local.UPnP; v == nil {
		unsupported("upnp")
	} else {
		fmt.Fprintf(&b, "    upnp:\n      enabled: %t\n      media_server_enabled: %t\n", v.Enabled, v.MediaServerEnabled)
	}
	if v := value.Local.WebDAV; v == nil {
		unsupported("webdav")
	} else {
		fmt.Fprintf(&b, "    webdav:\n      enabled: %t\n", v.Enabled)
	}
	if v := value.Local.Speedtest; v == nil {
		unsupported("speedtest")
	} else {
		fmt.Fprintf(&b, "    speedtest:\n      tcp_enabled: %t\n      udp_enabled: %t\n      udp_bidirect_enabled: %t\n      wan_tcp_enabled: %t\n      wan_udp_enabled: %t\n      tcp_port: %d\n      udp_port: %d\n      udp_bidirect_port: %d\n", v.TCPEnabled, v.UDPEnabled, v.UDPBidirectEnabled, v.WANTCPEnabled, v.WANUDPEnabled, v.TCPPort, v.UDPPort, v.UDPBidirectPort)
	}
	if v := value.Local.TR069; v == nil {
		unsupported("tr069")
	} else {
		fmt.Fprintf(&b, "    tr069:\n      periodic_inform_enabled: %t\n      upgrades_managed: %t\n", v.PeriodicInformEnabled, v.UpgradesManaged)
	}
	_, err := io.WriteString(w, b.String())
	return err
}
