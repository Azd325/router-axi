package app

import (
	"io"

	"github.com/Azd325/router-axi/internal/tr064"
)

const wanReconnectEffect = "the internet connection will drop and a new external address may be assigned"
const wanReconnectRecovery = "wait for the connection to return, then run router-axi wan; do not automatically repeat wan reconnect"

func writeWANReconnect(w io.Writer, result tr064.WANReconnectResult, jsonOutput bool) int {
	return writeSendOnce(w, sendOnceReport{"wan_reconnect", "wan reconnect", wanReconnectEffect, wanReconnectRecovery}, result.Endpoint, result.Preview, result.Accepted, jsonOutput)
}
