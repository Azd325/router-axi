package app

import (
	"io"

	"github.com/Azd325/router-axi/internal/tr064"
)

const firmwareCheckEffect = "the router will check for a firmware update; no update will be installed"
const firmwareCheckRecovery = "acceptance does not mean an update exists; run router-axi firmware to read the reported state; do not automatically repeat firmware check"

func writeFirmwareCheck(w io.Writer, result tr064.FirmwareCheckResult, jsonOutput bool) int {
	return writeSendOnce(w, sendOnceReport{key: "firmware_check", command: "firmware check", effect: firmwareCheckEffect, recovery: firmwareCheckRecovery}, result.Endpoint, result.Preview, result.Accepted, jsonOutput)
}
