package tr064

import "strings"

// readActionPrefixes name the TR-064 read actions the client may send.
var readActionPrefixes = []string{"Get", "X_Get", "X_AVM-DE_Get", "X_AVM_DE_Get"}

// approvedMutations is the only set of non-read SOAP actions the client may
// send. AGENTS.md admits no other mutation without an approved design change.
var approvedMutations = map[string]bool{
	"SetEnable":                      true,
	"Reboot":                         true,
	"ForceTermination":               true,
	"X_AVM-DE_WakeOnLANByMACAddress": true,
	"X_AVM-DE_CheckUpdate":           true,
}

func checkAction(action string) error {
	if approvedMutations[action] {
		return nil
	}
	for _, prefix := range readActionPrefixes {
		if strings.HasPrefix(action, prefix) {
			return nil
		}
	}
	return &Error{Kind: "internal", Operation: action, Message: "SOAP action is neither a read action nor an approved mutation"}
}
