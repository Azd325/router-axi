package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Azd325/router-axi/internal/tr064"
)

func watchPointer(value uint64) *uint64 { return &value }

func watchFixture(at time.Time, downloaded, uploaded uint64) tr064.WatchSnapshot {
	return tr064.WatchSnapshot{ObservedAt: at, WANStatus: tr064.WANStatusConnected, WANUptimeSeconds: watchPointer(100),
		TotalDownloadBytes: watchPointer(downloaded), TotalUploadBytes: watchPointer(uploaded),
		DownloadAt: at.Add(-time.Second), UploadAt: at, Source: "synthetic-source"}
}

type watchReader struct {
	fakeReader
	calls int
	read  func(context.Context, int) (tr064.WatchSnapshot, error)
}

func (r *watchReader) WatchSnapshot(ctx context.Context) (tr064.WatchSnapshot, error) {
	r.calls++
	if r.read != nil {
		return r.read(ctx, r.calls)
	}
	return watchFixture(time.Now(), uint64(r.calls)*100, uint64(r.calls)*40), nil
}

func watchApp(reader Reader) *App {
	application := New(func(Config) (Reader, error) { return reader, nil }, func(string) string { return "" })
	application.watchSignals = false
	return application
}

func TestWatchDefaultsAndCompletion(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reader := &watchReader{}
		start := time.Now()
		var out, stderr bytes.Buffer
		code := watchApp(reader).Run(t.Context(), []string{"watch", "--json"}, &out, &stderr)
		if code != ExitOK || reader.calls != 6 || time.Since(start) != 25*time.Second || stderr.Len() != 0 {
			t.Fatalf("code=%d calls=%d elapsed=%s error=%s", code, reader.calls, time.Since(start), stderr.String())
		}
		lines := strings.Split(strings.TrimSpace(out.String()), "\n")
		if len(lines) != 6 {
			t.Fatalf("got %d lines", len(lines))
		}
		for index, line := range lines {
			var sample watchSample
			if err := json.Unmarshal([]byte(line), &sample); err != nil || sample.Sample != index+1 {
				t.Fatalf("invalid sample: %s", line)
			}
			if index == 0 && sample.DownloadDeltaBytes != nil {
				t.Fatal("first sample has delta")
			}
			if index > 0 && (sample.DownloadBytesPerSecond == nil || *sample.DownloadBytesPerSecond != 20) {
				t.Fatal("wrong observed rate")
			}
		}
		time.Sleep(time.Minute)
		if reader.calls != 6 {
			t.Fatal("polling continued after return")
		}
	})
}

func TestWatchJSONLAndCompactContract(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, jsonOutput := range []bool{false, true} {
		reader := &watchReader{read: func(context.Context, int) (tr064.WatchSnapshot, error) { return watchFixture(at, 100, 40), nil }}
		args := []string{"watch", "--count", "1"}
		if jsonOutput {
			args = append(args, "--json")
		}
		var out, stderr bytes.Buffer
		code := watchApp(reader).Run(t.Context(), args, &out, &stderr)
		want := "sample_1[1]{observed_at,wan_status,wan_uptime_seconds,total_download_bytes,total_upload_bytes,download_delta_bytes,upload_delta_bytes,download_bytes_per_second,upload_bytes_per_second}:\n  \"2026-01-02T03:04:05Z\",connected,100,100,40,unknown,unknown,unknown,unknown\n"
		if jsonOutput {
			want = "{\"sample\":1,\"observed_at\":\"2026-01-02T03:04:05Z\",\"wan_status\":\"connected\",\"wan_uptime_seconds\":100,\"total_download_bytes\":100,\"total_upload_bytes\":40,\"download_delta_bytes\":null,\"upload_delta_bytes\":null,\"download_bytes_per_second\":null,\"upload_bytes_per_second\":null}\n"
		}
		if code != 0 || out.String() != want || stderr.Len() != 0 {
			t.Fatalf("code=%d out=%s err=%s", code, out.String(), stderr.String())
		}
	}
}

func TestWatchUnknownValues(t *testing.T) {
	for _, jsonOutput := range []bool{false, true} {
		reader := &watchReader{read: func(context.Context, int) (tr064.WatchSnapshot, error) { return tr064.WatchSnapshot{}, nil }}
		args := []string{"watch", "--count", "1"}
		want := "sample_1[1]{observed_at,wan_status,wan_uptime_seconds,total_download_bytes,total_upload_bytes,download_delta_bytes,upload_delta_bytes,download_bytes_per_second,upload_bytes_per_second}:\n  \"unknown\",unknown,unknown,unknown,unknown,unknown,unknown,unknown,unknown\n"
		if jsonOutput {
			args = append(args, "--json")
			want = "{\"sample\":1,\"observed_at\":\"unknown\",\"wan_status\":\"unknown\",\"wan_uptime_seconds\":null,\"total_download_bytes\":null,\"total_upload_bytes\":null,\"download_delta_bytes\":null,\"upload_delta_bytes\":null,\"download_bytes_per_second\":null,\"upload_bytes_per_second\":null}\n"
		}
		var out, stderr bytes.Buffer
		if code := watchApp(reader).Run(t.Context(), args, &out, &stderr); code != 0 {
			t.Fatal(code)
		}
		if out.String() != want {
			t.Fatal(out.String())
		}
	}
}

func TestWatchBoundsBeforeFactory(t *testing.T) {
	cases := [][]string{
		{"watch", "--count", "0"}, {"watch", "--count", "-1"}, {"watch", "--count", "3601"}, {"watch", "--count", "99999999999999999999"},
		{"watch", "--count", "1.5"}, {"watch", "--count"}, {"watch", "--count", "1", "--count", "2"},
		{"watch", "--interval", "0s"}, {"watch", "--interval", "999ms"}, {"watch", "--interval", "61s"}, {"watch", "--interval", "-1s"},
		{"watch", "--interval", "bad"}, {"watch", "--interval"}, {"watch", "--interval", "1s", "--interval", "2s"},
		{"traffic", "--count", "1"}, {"wan", "--interval", "1s"}, {"watch", "--all"}, {"watch", "--confirm"}, {"watch", "--force"},
		{"watch", "--output", "no-file"}, {"watch", "--instance", "1"}, {"watch", "--forever"}, {"watch", "extra"},
	}
	application := New(func(Config) (Reader, error) { t.Fatal("factory called for invalid grammar"); return nil, nil }, func(string) string { return "" })
	for _, args := range cases {
		var out, stderr bytes.Buffer
		if code := application.Run(t.Context(), append(args, "--json"), &out, &stderr); code != ExitUsage || out.Len() != 0 || !json.Valid(bytes.TrimSpace(stderr.Bytes())) {
			t.Fatalf("args=%v code=%d out=%s err=%s", args, code, out.String(), stderr.String())
		}
	}
	for _, args := range [][]string{{"watch"}, {"watch", "--interval", "1s", "--count", "1"}, {"watch", "--interval", "1m", "--count", "3600"}} {
		if _, err := parse(args); err != nil {
			t.Fatalf("valid boundary rejected: %v", err)
		}
	}
	var out, stderr bytes.Buffer
	if code := application.Run(t.Context(), []string{"watch", "--help"}, &out, &stderr); code != 0 || !strings.Contains(out.String(), "JSONL") {
		t.Fatal("help is not offline/complete")
	}
}

func TestWatchDeltaMath(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name              string
		previous, current *uint64
		before, after     time.Time
		delta             *uint64
		rate              float64
	}{
		{"fractional", watchPointer(100), watchPointer(400), at, at.Add(1500 * time.Millisecond), watchPointer(300), 200},
		{"zero growth", watchPointer(100), watchPointer(100), at, at.Add(time.Second), watchPointer(0), 0},
		{"large exact delta", watchPointer(math.MaxUint64 - 10), watchPointer(math.MaxUint64), at, at.Add(time.Second), watchPointer(10), 10},
		{"decrease", watchPointer(400), watchPointer(100), at, at.Add(time.Second), nil, 0},
		{"wrap", watchPointer(math.MaxUint64), watchPointer(0), at, at.Add(time.Second), nil, 0},
		{"missing previous", nil, watchPointer(1), at, at.Add(time.Second), nil, 0},
		{"missing current", watchPointer(1), nil, at, at.Add(time.Second), nil, 0},
		{"missing time", watchPointer(1), watchPointer(2), time.Time{}, at, nil, 0},
		{"same time", watchPointer(1), watchPointer(2), at, at, nil, 0},
		{"backward time", watchPointer(1), watchPointer(2), at, at.Add(-time.Second), nil, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			delta, rate := watchDelta(test.previous, test.current, test.before, test.after)
			if test.delta == nil {
				if delta != nil || rate != nil {
					t.Fatal("invented delta/rate")
				}
				return
			}
			if delta == nil || rate == nil || *delta != *test.delta || *rate != test.rate {
				t.Fatalf("wrong delta/rate: %v %v", delta, rate)
			}
		})
	}
}

func TestWatchDerivesDeltasForNormalizedConnectedStatus(t *testing.T) {
	at := time.Now()
	previous := watchFixture(at, 100, 40)
	current := watchFixture(at.Add(5*time.Second), 200, 80)
	previous.WANStatus, current.WANStatus = "connected", "connected"
	sample := makeWatchSample(2, current, &previous)
	if sample.DownloadDeltaBytes == nil || *sample.DownloadDeltaBytes != 100 || sample.UploadDeltaBytes == nil || *sample.UploadDeltaBytes != 40 {
		t.Fatal("deltas suppressed for the status tr064 reports when connected")
	}
}

func TestWatchDiscontinuity(t *testing.T) {
	at := time.Now()
	previous := watchFixture(at, 100, 40)
	for _, change := range []func(*tr064.WatchSnapshot){
		func(s *tr064.WatchSnapshot) { s.Source = "other" },
		func(s *tr064.WatchSnapshot) { s.Source = "" },
		func(s *tr064.WatchSnapshot) { s.WANStatus = "disconnected" },
		func(s *tr064.WatchSnapshot) { s.WANStatus = "unknown" },
		func(s *tr064.WatchSnapshot) { s.WANUptimeSeconds = nil },
		func(s *tr064.WatchSnapshot) { s.WANUptimeSeconds = watchPointer(10) },
	} {
		current := watchFixture(at.Add(5*time.Second), 200, 80)
		change(&current)
		sample := makeWatchSample(2, current, &previous)
		if sample.DownloadDeltaBytes != nil || sample.UploadDeltaBytes != nil {
			t.Fatal("derived values across discontinuity")
		}
	}
	current := watchFixture(at.Add(5*time.Second), 200, 80)
	current.DownloadAt = previous.DownloadAt.Add(2 * time.Second)
	sample := makeWatchSample(2, current, &previous)
	if *sample.DownloadBytesPerSecond != 50 || *sample.UploadBytesPerSecond != 8 {
		t.Fatal("directional read times ignored")
	}
	current.TotalDownloadBytes = nil
	sample = makeWatchSample(2, current, &previous)
	if sample.DownloadDeltaBytes != nil || sample.UploadDeltaBytes == nil {
		t.Fatal("directions not independent")
	}
}

func TestWatchReadFailures(t *testing.T) {
	for _, jsonOutput := range []bool{false, true} {
		for _, failAt := range []int{1, 2} {
			for _, test := range []struct {
				err    error
				code   int
				marker string
			}{
				{errors.New("private response token"), ExitInternal, "internal_error"},
				{&tr064.Error{Kind: "network", Message: "private response token"}, ExitNetwork, "router_unreachable"},
				{&tr064.Error{Kind: "network", Code: "tls_untrusted", Message: "private response token"}, ExitNetwork, "tls_untrusted"},
				{&tr064.Error{Kind: "auth", Message: "private response token"}, ExitAuth, "authentication_failed"},
				{&tr064.Error{Kind: "unsupported", Message: "private response token"}, ExitUnsupported, "unsupported_capability"},
				{&tr064.Error{Kind: "protocol", Message: "private response token"}, ExitRouter, "router_protocol_error"},
			} {
				synctest.Test(t, func(t *testing.T) {
					reader := &watchReader{read: func(_ context.Context, n int) (tr064.WatchSnapshot, error) {
						if n == failAt {
							return tr064.WatchSnapshot{}, test.err
						}
						return watchFixture(time.Now(), 100, 40), nil
					}}
					args := []string{"watch", "--count", "3", "--interval", "1s"}
					if jsonOutput {
						args = append(args, "--json")
					}
					var out, stderr bytes.Buffer
					code := watchApp(reader).Run(t.Context(), args, &out, &stderr)
					if code != test.code || reader.calls != failAt || !strings.Contains(stderr.String(), test.marker) || strings.Contains(out.String()+stderr.String(), "private") {
						t.Fatalf("code=%d calls=%d error=%s", code, reader.calls, stderr.String())
					}
					if (failAt == 1) != (out.Len() == 0) {
						t.Fatal("partial sample emitted or previous output lost")
					}
					if jsonOutput && !json.Valid(bytes.TrimSpace(stderr.Bytes())) {
						t.Fatal("error is not JSON")
					}
					time.Sleep(time.Minute)
					if reader.calls != failAt {
						t.Fatal("polling after failure")
					}
				})
			}
		}
	}
}

func TestWatchCancellationAndDeadline(t *testing.T) {
	for _, phase := range []string{"before", "wait", "read", "deadline"} {
		t.Run(phase, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				reader := &watchReader{}
				if phase == "before" {
					cancel()
				}
				reader.read = func(ctx context.Context, _ int) (tr064.WatchSnapshot, error) {
					if phase == "read" || phase == "deadline" {
						<-ctx.Done()
						return tr064.WatchSnapshot{}, ctx.Err()
					}
					return watchFixture(time.Now(), 100, 40), nil
				}
				if phase == "wait" || phase == "read" {
					go func() { time.Sleep(time.Second); cancel() }()
				}
				var out, stderr bytes.Buffer
				code := watchApp(reader).Run(ctx, []string{"watch", "--json"}, &out, &stderr)
				want, marker := ExitInterrupted, "interrupted"
				if phase == "deadline" {
					want, marker = ExitNetwork, "watch_timeout"
				}
				if code != want || !strings.Contains(stderr.String(), marker) {
					t.Fatalf("code=%d error=%s", code, stderr.String())
				}
				wantCalls := 1
				if phase == "before" {
					wantCalls = 0
				}
				if reader.calls != wantCalls {
					t.Fatal("unexpected read count")
				}
				time.Sleep(time.Minute)
				if reader.calls != wantCalls {
					t.Fatal("polling after cancellation")
				}
			})
		})
	}
}

type watchBrokenWriter struct{}

func (watchBrokenWriter) Write([]byte) (int, error) { return 0, errors.New("private output detail") }

func TestWatchOutputFailureStops(t *testing.T) {
	for _, jsonOutput := range []bool{false, true} {
		reader := &watchReader{}
		var stderr bytes.Buffer
		args := []string{"watch"}
		if jsonOutput {
			args = append(args, "--json")
		}
		code := watchApp(reader).Run(t.Context(), args, watchBrokenWriter{}, &stderr)
		if code != ExitInternal || reader.calls != 1 || !strings.Contains(stderr.String(), "output_failed") || strings.Contains(stderr.String(), "private") {
			t.Fatalf("code=%d calls=%d error=%s", code, reader.calls, stderr.String())
		}
	}
}

func TestWatchInvalidHostRedacted(t *testing.T) {
	application := New(func(Config) (Reader, error) { return nil, errors.New("private endpoint") }, func(string) string { return "" })
	var out, stderr bytes.Buffer
	code := application.Run(t.Context(), []string{"watch", "--host", "http://synthetic.invalid", "--json"}, &out, &stderr)
	if code != ExitUsage || out.Len() != 0 || strings.Contains(stderr.String(), "private") {
		t.Fatal("configuration error leaked")
	}
}

func TestWatchSignalHelper(t *testing.T) {
	mode := os.Getenv("ROUTER_AXI_TEST_WATCH_SIGNAL")
	if mode == "" {
		return
	}
	reader := &watchReader{}
	if mode == "read" {
		reader.read = func(ctx context.Context, _ int) (tr064.WatchSnapshot, error) {
			_, _ = io.WriteString(os.Stdout, "ready\n")
			<-ctx.Done()
			return tr064.WatchSnapshot{}, ctx.Err()
		}
	}
	application := New(func(Config) (Reader, error) { return reader, nil }, func(string) string { return "" })
	os.Exit(application.Run(context.Background(), []string{"watch", "--interval", "1m", "--json"}, os.Stdout, os.Stderr))
}

func TestWatchSignals(t *testing.T) {
	for _, signal := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		for _, phase := range []string{"wait", "read"} {
			t.Run(signal.String()+"/"+phase, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWatchSignalHelper$")
				cmd.Env = append(os.Environ(), "ROUTER_AXI_TEST_WATCH_SIGNAL="+phase)
				var stderr bytes.Buffer
				cmd.Stderr = &stderr
				stdout, err := cmd.StdoutPipe()
				if err != nil {
					t.Fatal(err)
				}
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = cmd.Process.Kill() })
				input := bufio.NewReader(stdout)
				line, err := input.ReadString('\n')
				if err != nil {
					t.Fatal("child failed before readiness")
				}
				if phase == "wait" && !json.Valid([]byte(line)) {
					t.Fatal("child did not emit a JSON sample")
				}
				if err := cmd.Process.Signal(signal); err != nil {
					t.Fatal(err)
				}
				rest, err := io.ReadAll(input)
				if err != nil {
					t.Fatal(err)
				}
				err = cmd.Wait()
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != ExitInterrupted || len(rest) != 0 || !strings.Contains(stderr.String(), "interrupted") {
					t.Fatalf("exit=%v trailing=%q err=%s", err, rest, stderr.String())
				}
			})
		}
	}
}
