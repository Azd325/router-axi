package app

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/Azd325/router-axi/internal/tr064"
)

func writeEventLog(w io.Writer, result tr064.EventLog) error {
	if _, err := fmt.Fprintf(w, "event_log:\n  groups: %s\n  total: %d\n  omitted: %d\nlines[%d]{group,date,time,text}:\n", strings.Join(result.Groups, ","), result.Total, result.Omitted, len(result.Lines)); err != nil {
		return err
	}
	for _, line := range result.Lines {
		if _, err := fmt.Fprintf(w, "  %s,%s,%s,%s\n", line.Group, strconv.Quote(line.Date), strconv.Quote(line.Time), strconv.Quote(line.Text)); err != nil {
			return err
		}
	}
	if result.More != "" {
		return writeNext(w, result.More)
	}
	return nil
}

func eventLogNext(opts options, result tr064.EventLog) string {
	if result.Omitted == 0 {
		return ""
	}
	limit := " --limit " + strconv.Itoa(tr064.MaxEventLogLimit)
	if opts.limit < tr064.MaxEventLogLimit {
		return hintCommand(opts, "event-log --group "+strings.Join(result.Groups, ",")+limit)
	}
	if len(result.Groups) > 1 {
		return hintCommand(opts, "event-log --group <group>"+limit)
	}
	return ""
}
