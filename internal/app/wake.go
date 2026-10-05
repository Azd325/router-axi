package app

import (
	"io"

	"github.com/Azd325/router-axi/internal/tr064"
)

const wakeEffect = "the router will send a Wake-on-LAN request to the selected MAC address"
const wakeRecovery = "acceptance does not mean the device woke; check the device manually; do not automatically repeat wake"

func writeWake(w io.Writer, result tr064.WakeResult, jsonOutput bool) int {
	return writeSendOnce(w, sendOnceReport{key: "wake", command: "wake " + shellWord(result.MAC), effect: wakeEffect, recovery: wakeRecovery, mac: result.MAC}, result.Endpoint, result.Preview, result.Accepted, jsonOutput)
}
