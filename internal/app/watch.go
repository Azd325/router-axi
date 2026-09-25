package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/Azd325/router-axi/internal/tr064"
)

const (
	defaultWatchInterval = 5 * time.Second
	defaultWatchCount    = 6
	maxWatchCount        = 3600
	watchSampleTimeout   = 30 * time.Second
	ExitInterrupted      = 130
)

type watchSample struct {
	Sample                 int      `json:"sample"`
	ObservedAt             string   `json:"observed_at"`
	WANStatus              string   `json:"wan_status"`
	WANUptimeSeconds       *uint64  `json:"wan_uptime_seconds"`
	TotalDownloadBytes     *uint64  `json:"total_download_bytes"`
	TotalUploadBytes       *uint64  `json:"total_upload_bytes"`
	DownloadDeltaBytes     *uint64  `json:"download_delta_bytes"`
	UploadDeltaBytes       *uint64  `json:"upload_delta_bytes"`
	DownloadBytesPerSecond *float64 `json:"download_bytes_per_second"`
	UploadBytesPerSecond   *float64 `json:"upload_bytes_per_second"`
}

func runWatch(ctx context.Context, reader Reader, opts options, stdout, stderr io.Writer, registerSignals bool) int {
	if registerSignals {
		var stop context.CancelFunc
		ctx, stop = watchSignalContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer stop()
	}
	var previous *tr064.WatchSnapshot
	for index := 1; index <= opts.count; index++ {
		if ctx.Err() != nil {
			return watchInterrupted(stderr, opts.json)
		}
		if index > 1 {
			if err := waitWatch(ctx, opts.interval); err != nil {
				return watchInterrupted(stderr, opts.json)
			}
		}
		if ctx.Err() != nil {
			return watchInterrupted(stderr, opts.json)
		}
		sampleCtx, cancel := context.WithTimeout(ctx, watchSampleTimeout)
		current, err := reader.WatchSnapshot(sampleCtx)
		timedOut := sampleCtx.Err() != nil
		cancel()
		if ctx.Err() != nil {
			return watchInterrupted(stderr, opts.json)
		}
		if timedOut {
			return writeError(stderr, opts.json, ExitNetwork, "watch_timeout", "watch sample exceeded its 30s deadline; polling stopped", "check router responsiveness before running watch again")
		}
		if err != nil {
			return watchReadError(stderr, opts.json, index, err)
		}
		sample := makeWatchSample(index, current, previous)
		if opts.json {
			if writeJSON(stdout, sample) != ExitOK {
				return writeError(stderr, true, ExitInternal, "output_failed", "watch output could not be written; polling stopped", "")
			}
		} else if err := writeWatchSample(stdout, sample); err != nil {
			return writeError(stderr, false, ExitInternal, "output_failed", "watch output could not be written; polling stopped", "")
		}
		previous = &current
	}
	return ExitOK
}

// watchSignalContext installs signal-driven cancellation for watch. It is a
// variable so deterministic tests can substitute a no-op instead of
// registering real OS signal delivery inside a synthetic-time bubble.
var watchSignalContext = signal.NotifyContext

func waitWatch(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func watchInterrupted(w io.Writer, jsonOutput bool) int {
	return writeError(w, jsonOutput, ExitInterrupted, "interrupted", "watch interrupted; polling stopped", "")
}

func watchReadError(w io.Writer, jsonOutput bool, index int, err error) int {
	result := &tr064.Error{Kind: "internal", Operation: "watch", Message: fmt.Sprintf("watch sample %d failed; polling stopped", index)}
	var protocolErr *tr064.Error
	if !errors.As(err, &protocolErr) {
		return writeError(w, jsonOutput, ExitInternal, "internal_error", result.Message, "")
	}
	result.Kind, result.Code = protocolErr.Kind, protocolErr.Code
	if result.Kind == "unsupported" {
		result.Message += "; enable documented Layer3Forwarding, WAN connection and WANCommonInterfaceConfig reads or use supported firmware"
	}
	if result.Kind == "usage" {
		return writeError(w, jsonOutput, ExitUsage, "invalid_configuration", result.Message, "router-axi watch --help")
	}
	return renderProtocolError(w, jsonOutput, result)
}

func makeWatchSample(index int, current tr064.WatchSnapshot, previous *tr064.WatchSnapshot) watchSample {
	observed := "unknown"
	if !current.ObservedAt.IsZero() {
		observed = current.ObservedAt.UTC().Format(time.RFC3339Nano)
	}
	sample := watchSample{Sample: index, ObservedAt: observed, WANStatus: current.WANStatus,
		WANUptimeSeconds: current.WANUptimeSeconds, TotalDownloadBytes: current.TotalDownloadBytes, TotalUploadBytes: current.TotalUploadBytes}
	if sample.WANStatus == "" {
		sample.WANStatus = "unknown"
	}
	if previous == nil || current.Source == "" || current.Source != previous.Source ||
		current.WANStatus != "Connected" || previous.WANStatus != "Connected" ||
		current.WANUptimeSeconds == nil || previous.WANUptimeSeconds == nil ||
		*current.WANUptimeSeconds < *previous.WANUptimeSeconds {
		return sample
	}
	sample.DownloadDeltaBytes, sample.DownloadBytesPerSecond = watchDelta(previous.TotalDownloadBytes, current.TotalDownloadBytes, previous.DownloadAt, current.DownloadAt)
	sample.UploadDeltaBytes, sample.UploadBytesPerSecond = watchDelta(previous.TotalUploadBytes, current.TotalUploadBytes, previous.UploadAt, current.UploadAt)
	return sample
}

func watchDelta(previous, current *uint64, before, after time.Time) (*uint64, *float64) {
	if previous == nil || current == nil || before.IsZero() || after.IsZero() || *current < *previous {
		return nil, nil
	}
	elapsed := after.Sub(before).Seconds()
	if elapsed <= 0 {
		return nil, nil
	}
	delta := *current - *previous
	rate := float64(delta) / elapsed
	return &delta, &rate
}

func writeWatchSample(w io.Writer, sample watchSample) error {
	_, err := fmt.Fprintf(w, "sample_%d[1]{observed_at,wan_status,wan_uptime_seconds,total_download_bytes,total_upload_bytes,download_delta_bytes,upload_delta_bytes,download_bytes_per_second,upload_bytes_per_second}:\n  %s,%s,%s,%s,%s,%s,%s,%s,%s\n",
		sample.Sample, strconv.Quote(sample.ObservedAt), sample.WANStatus, watchUint(sample.WANUptimeSeconds),
		watchUint(sample.TotalDownloadBytes), watchUint(sample.TotalUploadBytes), watchUint(sample.DownloadDeltaBytes), watchUint(sample.UploadDeltaBytes),
		watchRate(sample.DownloadBytesPerSecond), watchRate(sample.UploadBytesPerSecond))
	return err
}

func watchUint(value *uint64) string {
	if value == nil {
		return "null"
	}
	return strconv.FormatUint(*value, 10)
}

func watchRate(value *float64) string {
	if value == nil {
		return "null"
	}
	return strconv.FormatFloat(*value, 'f', -1, 64)
}
