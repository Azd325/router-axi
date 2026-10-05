package app

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Azd325/router-axi/internal/tr064"
)

func (f fakeReader) DSLDetail(context.Context) (tr064.DSLDetail, error) {
	if f.err != nil {
		return tr064.DSLDetail{}, f.err
	}
	distance := uint64(120)
	return tr064.DSLDetail{
		Statistics: &tr064.DSLStatistics{ReceiveBlocks: 1001, TransmitBlocks: 1002, CellDelineation: 3, LinkRetrains: 4, InitErrors: 5, InitTimeouts: 6, LossOfFraming: 7, ErroredSeconds: 8, SeverelyErroredSeconds: 9, FECErrors: 10, ATUCFECErrors: 11, HECErrors: 12, ATUCHECErrors: 13, CRCErrors: 14, ATUCCRCErrors: 15},
		Diagnosis:  &tr064.DSLDiagnosis{State: "DONE_CABLE_NOK", CableFaultDistanceMeters: &distance, LastDiagnoseTimeSeconds: 45, SignalLossTimeSeconds: 900, Active: true},
	}, nil
}

type partialDSLDetailReader struct {
	fakeReader
	detail tr064.DSLDetail
}

func (r partialDSLDetailReader) DSLDetail(context.Context) (tr064.DSLDetail, error) {
	return r.detail, nil
}

func TestDSLDetailOutputContract(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"dsl", "detail"}, "dsl_detail:\n  statistics:\n    receive_blocks: 1001\n    transmit_blocks: 1002\n    cell_delineation: 3\n    link_retrains: 4\n    init_errors: 5\n    init_timeouts: 6\n    loss_of_framing: 7\n    errored_seconds: 8\n    severely_errored_seconds: 9\n    fec_errors: 10\n    atuc_fec_errors: 11\n    hec_errors: 12\n    atuc_hec_errors: 13\n    crc_errors: 14\n    atuc_crc_errors: 15\n  diagnosis:\n    state: DONE_CABLE_NOK\n    cable_fault_distance_meters: 120\n    last_diagnose_time_seconds: 45\n    signal_loss_time_seconds: 900\n    active: true\n    sync: false\n"},
		{[]string{"dsl", "detail", "--json"}, `{"statistics":{"receive_blocks":1001,"transmit_blocks":1002,"cell_delineation":3,"link_retrains":4,"init_errors":5,"init_timeouts":6,"loss_of_framing":7,"errored_seconds":8,"severely_errored_seconds":9,"fec_errors":10,"atuc_fec_errors":11,"hec_errors":12,"atuc_hec_errors":13,"crc_errors":14,"atuc_crc_errors":15},"diagnosis":{"state":"DONE_CABLE_NOK","cable_fault_distance_meters":120,"last_diagnose_time_seconds":45,"signal_loss_time_seconds":900,"active":true,"sync":false}}` + "\n"},
	} {
		var stdout, stderr bytes.Buffer
		code := New(func(Config) (Reader, error) { return fakeReader{}, nil }, func(string) string { return "" }).Run(t.Context(), test.args, &stdout, &stderr)
		if code != ExitOK || stdout.String() != test.want || stderr.Len() != 0 {
			t.Fatalf("args=%q code=%d stdout=%q stderr=%q", test.args, code, stdout.String(), stderr.String())
		}
	}
}

func TestDSLDetailUnsupportedPartOutput(t *testing.T) {
	statisticsOnly := tr064.DSLDetail{Statistics: &tr064.DSLStatistics{}}
	diagnosisOnly := tr064.DSLDetail{Diagnosis: &tr064.DSLDiagnosis{State: "NONE", Active: true, Sync: true}}
	for _, test := range []struct {
		detail tr064.DSLDetail
		args   []string
		want   string
	}{
		{statisticsOnly, []string{"dsl", "detail"}, "    atuc_crc_errors: 0\n  diagnosis: unsupported\n"},
		{statisticsOnly, []string{"dsl", "detail", "--json"}, `"atuc_crc_errors":0},"diagnosis":null}` + "\n"},
		{diagnosisOnly, []string{"dsl", "detail"}, "dsl_detail:\n  statistics: unsupported\n  diagnosis:\n    state: NONE\n    cable_fault_distance_meters: unknown\n    last_diagnose_time_seconds: 0\n    signal_loss_time_seconds: 0\n    active: true\n    sync: true\n"},
		{diagnosisOnly, []string{"dsl", "detail", "--json"}, `{"statistics":null,"diagnosis":{"state":"NONE","cable_fault_distance_meters":null,"last_diagnose_time_seconds":0,"signal_loss_time_seconds":0,"active":true,"sync":true}}` + "\n"},
	} {
		var stdout, stderr bytes.Buffer
		code := New(func(Config) (Reader, error) { return partialDSLDetailReader{detail: test.detail}, nil }, func(string) string { return "" }).Run(t.Context(), test.args, &stdout, &stderr)
		if code != ExitOK || !strings.HasSuffix(stdout.String(), test.want) || stderr.Len() != 0 {
			t.Fatalf("args=%q code=%d stdout=%q stderr=%q", test.args, code, stdout.String(), stderr.String())
		}
	}
}

func TestDSLDetailUsage(t *testing.T) {
	application := New(func(Config) (Reader, error) { t.Fatal("usage error contacted router"); return nil, nil }, func(string) string { return "" })
	for _, args := range [][]string{{"dsl", "detail", "detail"}, {"dsl", "statistics"}, {"dsl", "detail", "--all"}} {
		var stdout, stderr bytes.Buffer
		if code := application.Run(t.Context(), args, &stdout, &stderr); code != ExitUsage {
			t.Fatalf("args=%q code=%d stdout=%q stderr=%q", args, code, stdout.String(), stderr.String())
		}
	}
}

func TestDSLDetailHelp(t *testing.T) {
	application := New(func(Config) (Reader, error) { t.Fatal("help contacted router"); return nil, nil }, func(string) string { t.Fatal("help read credentials"); return "" })
	for _, test := range []struct {
		args []string
		want []string
	}{
		{[]string{"dsl", "detail", "--help"}, []string{"usage: router-axi dsl detail [--host ADDRESS] [--json] [--help]", "WANDSLInterfaceConfig:GetStatisticsTotal and X_AVM-DE_GetDSLDiagnoseInfo", "router-axi dsl detail --json"}},
		{[]string{"dsl", "--help"}, []string{"usage: router-axi dsl [detail]", "router-axi dsl detail"}},
		{[]string{"--help"}, []string{"dsl detail adds total error counters and the router's line-fault diagnosis"}},
	} {
		var stdout, stderr bytes.Buffer
		code := application.Run(t.Context(), test.args, &stdout, &stderr)
		if code != ExitOK || stderr.Len() != 0 {
			t.Fatalf("args=%q code=%d stdout=%q stderr=%q", test.args, code, stdout.String(), stderr.String())
		}
		for _, want := range test.want {
			if !strings.Contains(stdout.String(), want) {
				t.Fatalf("args=%q stdout=%q missing=%q", test.args, stdout.String(), want)
			}
		}
	}
}

func TestDSLDetailStructuredErrors(t *testing.T) {
	for _, test := range []struct {
		kind string
		exit int
		code string
	}{
		{"auth", ExitAuth, "authentication_failed"},
		{"network", ExitNetwork, "router_unreachable"},
		{"unsupported", ExitUnsupported, "unsupported_capability"},
		{"protocol", ExitRouter, "router_protocol_error"},
		{"router", ExitRouter, "router_protocol_error"},
	} {
		failure := &tr064.Error{Kind: test.kind, Operation: "dsl detail", Message: "DSL detail inspection failed"}
		var stdout, stderr bytes.Buffer
		code := New(func(Config) (Reader, error) { return fakeReader{err: failure}, nil }, func(string) string { return "" }).Run(t.Context(), []string{"dsl", "detail", "--json"}, &stdout, &stderr)
		if code != test.exit || !strings.Contains(stdout.String(), `"code":"`+test.code+`"`) || stderr.Len() != 0 {
			t.Fatalf("kind=%s code=%d stdout=%q stderr=%q", test.kind, code, stdout.String(), stderr.String())
		}
	}
}
