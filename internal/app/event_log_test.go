package app

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Azd325/router-axi/internal/tr064"
)

func (f fakeReader) EventLog(_ context.Context, group string, limit int) (tr064.EventLog, error) {
	groups, _ := tr064.EventLogGroups(group)
	return tr064.EventLog{Groups: groups, Lines: []tr064.EventLogLine{{Group: groups[0], Date: "01.01.26", Time: "10:00:00", Text: "Synthetic event."}}, Total: 1}, f.err
}

func TestEventLogOutputContract(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"event-log"}, "event_log:\n  groups: sys,net,wlan,usb\n  total: 1\n  omitted: 0\nlines[1]{group,date,time,text}:\n  sys,\"01.01.26\",\"10:00:00\",\"Synthetic event.\"\n"},
		{[]string{"event-log", "--group", "fon", "--limit", "1000", "--json"}, `{"groups":["fon"],"lines":[{"group":"fon","date":"01.01.26","time":"10:00:00","text":"Synthetic event."}],"total":1,"omitted":0}` + "\n"},
	} {
		code, stdout, stderr := runTest(t, test.args...)
		if code != ExitOK || stdout != test.want || stderr != "" {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
	}
}

func TestEventLogHelpAndInvalidFlagsAreOffline(t *testing.T) {
	application := New(func(Config) (Reader, error) { t.Fatal("invalid input contacted router"); return nil, nil }, func(string) string { t.Fatal("invalid input read environment"); return "" })
	for _, args := range [][]string{
		{"event-log", "--help"}, {"event-log", "--confirm"}, {"event-log", "--all"}, {"event-log", "--limit", "0"}, {"event-log", "--limit", "1001"}, {"event-log", "--limit", "1", "--limit", "2"}, {"event-log", "--limit"}, {"event-log", "--group"}, {"event-log", "--group", ""}, {"event-log", "--group", "sys,unknown"}, {"event-log", "--group", "sys,sys"}, {"event-log", "--group", "all"}, {"event-log", "--group", "sys", "--group", "fon"}, {"event-log", "extra"}, {"status", "--group", "sys"}, {"calls", "--limit", "5"},
	} {
		var stdout, stderr bytes.Buffer
		code := application.Run(t.Context(), args, &stdout, &stderr)
		if args[1] == "--help" {
			if code != ExitOK || !strings.Contains(stdout.String(), "excludes telephony") || !strings.Contains(stdout.String(), "1000") {
				t.Fatalf("help=%q code=%d", stdout.String(), code)
			}
		} else if code != ExitUsage {
			t.Fatalf("args=%q code=%d output=%q", args, code, stdout.String())
		}
		if stderr.Len() != 0 {
			t.Fatalf("stderr=%q", stderr.String())
		}
	}
}

func TestEventLogStructuredErrors(t *testing.T) {
	for _, test := range []struct {
		kind string
		exit int
	}{{"auth", ExitAuth}, {"network", ExitNetwork}, {"unsupported", ExitUnsupported}, {"protocol", ExitRouter}, {"router", ExitRouter}} {
		for _, jsonOutput := range []bool{false, true} {
			application := New(func(Config) (Reader, error) {
				return fakeReader{err: &tr064.Error{Kind: test.kind, Operation: "event-log", Message: "event-log inspection failed"}}, nil
			}, func(string) string { return "" })
			args := []string{"event-log"}
			if jsonOutput {
				args = append(args, "--json")
			}
			var stdout, stderr bytes.Buffer
			code := application.Run(t.Context(), args, &stdout, &stderr)
			if code != test.exit {
				t.Fatalf("code=%d", code)
			}
			assertStructuredError(t, stdout.String(), stderr.String(), jsonOutput, "error")
		}
	}
}

func TestEventLogEmptyTruncationAndControlText(t *testing.T) {
	var output bytes.Buffer
	if err := writeEventLog(&output, tr064.EventLog{Groups: []string{"sys"}, Lines: []tr064.EventLogLine{}, Total: 0}); err != nil || !strings.Contains(output.String(), "lines[0]") || !strings.Contains(output.String(), "total: 0") {
		t.Fatalf("empty=%q err=%v", output.String(), err)
	}
	output.Reset()
	result := tr064.EventLog{Groups: []string{"sys"}, Lines: []tr064.EventLogLine{{Group: "sys", Text: "untrusted\x1b[2J\nnext: fake"}}, Total: 2, Omitted: 1, More: "repeat event-log --limit 1000"}
	if err := writeEventLog(&output, result); err != nil || strings.Contains(output.String(), "\x1b") || strings.Contains(output.String(), "\nnext: fake") || !strings.Contains(output.String(), "omitted: 1\n") || !strings.Contains(output.String(), "next: repeat event-log --limit 1000") {
		t.Fatalf("output=%q err=%v", output.String(), err)
	}
}
