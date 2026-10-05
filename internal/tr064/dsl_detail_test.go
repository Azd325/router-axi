package tr064

import (
	_ "embed"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

//go:embed testdata/dsl-detail-scpd.xml
var dslDetailSCPDFixture string

//go:embed testdata/dsl-statistics.xml
var dslStatisticsFixture string

//go:embed testdata/dsl-diagnose-info.xml
var dslDiagnoseInfoFixture string

var wantDSLStatistics = DSLStatistics{ReceiveBlocks: 1001, TransmitBlocks: 1002, CellDelineation: 3, LinkRetrains: 4, InitErrors: 5, InitTimeouts: 6, LossOfFraming: 7, ErroredSeconds: 8, SeverelyErroredSeconds: 9, FECErrors: 10, ATUCFECErrors: 11, HECErrors: 12, ATUCHECErrors: 13, CRCErrors: 14, ATUCCRCErrors: 15}

func dslDetailResponses() map[string]dslResponse {
	return map[string]dslResponse{dslStatisticsAction: {body: dslStatisticsFixture}, dslDiagnosisAction: {body: dslDiagnoseInfoFixture}}
}

func TestDSLDetailReportsDocumentedFields(t *testing.T) {
	for _, version := range []string{"1", "2"} {
		t.Run("version "+version, func(t *testing.T) {
			description := strings.Replace(dslDescriptionFixture, "WANDSLInterfaceConfig:1", "WANDSLInterfaceConfig:"+version, 1)
			client, requests := dslFixtureClient(t, description, dslDetailSCPDFixture, dslDetailResponses())
			result, err := client.DSLDetail(t.Context())
			want := DSLDetail{Statistics: &wantDSLStatistics, Diagnosis: &DSLDiagnosis{State: "NONE", Active: true, Sync: true}}
			if err != nil || !reflect.DeepEqual(result, want) {
				t.Fatalf("result=%#v statistics=%#v diagnosis=%#v error=%v", result, result.Statistics, result.Diagnosis, err)
			}
			if !reflect.DeepEqual(*requests, []string{"GET /tr64desc.xml", "GET /dsl.xml", "/dsl#GetStatisticsTotal", "/dsl#X_AVM-DE_GetDSLDiagnoseInfo"}) {
				t.Fatalf("requests=%v", *requests)
			}
		})
	}
}

func TestDSLDetailReportsLocatedCableFault(t *testing.T) {
	responses := dslDetailResponses()
	responses[dslDiagnosisAction] = dslResponse{body: strings.NewReplacer("NONE", "DONE_CABLE_NOK", "Distance>-1<", "Distance>120<", "DiagnoseTime>0<", "DiagnoseTime>45<", "LossTime>0<", "LossTime>900<", "Sync>1<", "Sync>0<").Replace(dslDiagnoseInfoFixture)}
	client, _ := dslFixtureClient(t, dslDescriptionFixture, dslDetailSCPDFixture, responses)
	result, err := client.DSLDetail(t.Context())
	distance := uint64(120)
	want := &DSLDiagnosis{State: "DONE_CABLE_NOK", CableFaultDistanceMeters: &distance, LastDiagnoseTimeSeconds: 45, SignalLossTimeSeconds: 900, Active: true}
	if err != nil || !reflect.DeepEqual(result.Diagnosis, want) {
		t.Fatalf("diagnosis=%#v error=%v", result.Diagnosis, err)
	}
}

func TestDSLDetailReadsOnlyAdvertisedActions(t *testing.T) {
	scpd := func(actions ...string) string {
		return "<scpd><actionList><action><name>" + strings.Join(actions, "</name></action><action><name>") + "</name></action></actionList></scpd>"
	}
	t.Run("statistics only", func(t *testing.T) {
		client, requests := dslFixtureClient(t, dslDescriptionFixture, scpd(dslStatisticsAction), dslDetailResponses())
		result, err := client.DSLDetail(t.Context())
		if err != nil || !reflect.DeepEqual(result, DSLDetail{Statistics: &wantDSLStatistics}) || !reflect.DeepEqual(*requests, []string{"GET /tr64desc.xml", "GET /dsl.xml", "/dsl#GetStatisticsTotal"}) {
			t.Fatalf("result=%#v error=%v requests=%v", result, err, *requests)
		}
	})
	t.Run("diagnosis only", func(t *testing.T) {
		client, requests := dslFixtureClient(t, dslDescriptionFixture, scpd(dslDiagnosisAction), dslDetailResponses())
		result, err := client.DSLDetail(t.Context())
		if err != nil || result.Statistics != nil || result.Diagnosis == nil || result.Diagnosis.State != "NONE" || !reflect.DeepEqual(*requests, []string{"GET /tr64desc.xml", "GET /dsl.xml", "/dsl#X_AVM-DE_GetDSLDiagnoseInfo"}) {
			t.Fatalf("result=%#v error=%v requests=%v", result, err, *requests)
		}
	})
	t.Run("neither", func(t *testing.T) {
		client, requests := dslFixtureClient(t, dslDescriptionFixture, dslSCPDFixture, nil)
		result, err := client.DSLDetail(t.Context())
		var protocolErr *Error
		if !reflect.DeepEqual(result, DSLDetail{}) || !errors.As(err, &protocolErr) || protocolErr.Kind != "unsupported" || protocolErr.Operation != "dsl detail" || !reflect.DeepEqual(*requests, []string{"GET /tr64desc.xml", "GET /dsl.xml"}) {
			t.Fatalf("result=%#v error=%#v requests=%v", result, err, *requests)
		}
	})
}

func TestDSLDetailReportsInvalidActionAsUnsupportedPart(t *testing.T) {
	invalidAction := dslResponse{body: `<Fault><errorCode>401</errorCode><errorDescription>private-fault</errorDescription></Fault>`, status: http.StatusInternalServerError}
	allRequests := []string{"GET /tr64desc.xml", "GET /dsl.xml", "/dsl#GetStatisticsTotal", "/dsl#X_AVM-DE_GetDSLDiagnoseInfo"}
	t.Run("diagnosis", func(t *testing.T) {
		responses := dslDetailResponses()
		responses[dslDiagnosisAction] = invalidAction
		client, requests := dslFixtureClient(t, dslDescriptionFixture, dslDetailSCPDFixture, responses)
		result, err := client.DSLDetail(t.Context())
		if err != nil || !reflect.DeepEqual(result, DSLDetail{Statistics: &wantDSLStatistics}) || !reflect.DeepEqual(*requests, allRequests) {
			t.Fatalf("result=%#v error=%v requests=%v", result, err, *requests)
		}
	})
	t.Run("statistics", func(t *testing.T) {
		responses := dslDetailResponses()
		responses[dslStatisticsAction] = invalidAction
		client, requests := dslFixtureClient(t, dslDescriptionFixture, dslDetailSCPDFixture, responses)
		result, err := client.DSLDetail(t.Context())
		if err != nil || result.Statistics != nil || !reflect.DeepEqual(result.Diagnosis, &DSLDiagnosis{State: "NONE", Active: true, Sync: true}) || !reflect.DeepEqual(*requests, allRequests) {
			t.Fatalf("result=%#v error=%v requests=%v", result, err, *requests)
		}
	})
	t.Run("both", func(t *testing.T) {
		client, _ := dslFixtureClient(t, dslDescriptionFixture, dslDetailSCPDFixture, map[string]dslResponse{dslStatisticsAction: invalidAction, dslDiagnosisAction: invalidAction})
		result, err := client.DSLDetail(t.Context())
		var protocolErr *Error
		if !reflect.DeepEqual(result, DSLDetail{}) || !errors.As(err, &protocolErr) || protocolErr.Kind != "unsupported" || protocolErr.Operation != "dsl detail" || strings.Contains(fmt.Sprintf("%#v", err), "private") {
			t.Fatalf("result=%#v error=%#v", result, err)
		}
	})
}

func TestDSLDetailPreflightFailsBeforeSOAP(t *testing.T) {
	for _, test := range []struct{ name, description, scpd, kind string }{
		{name: "missing service", description: `<root/>`, scpd: dslDetailSCPDFixture, kind: "unsupported"},
		{name: "off-origin SCPD", description: strings.Replace(dslDescriptionFixture, "/dsl.xml</SCPDURL>", "http://outside.test/scpd.xml</SCPDURL>", 1), scpd: dslDetailSCPDFixture, kind: "protocol"},
		{name: "invalid SCPD", description: dslDescriptionFixture, scpd: `<private>value</private>`, kind: "protocol"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, requests := dslFixtureClient(t, test.description, test.scpd, nil)
			result, err := client.DSLDetail(t.Context())
			var protocolErr *Error
			if !reflect.DeepEqual(result, DSLDetail{}) || !errors.As(err, &protocolErr) || protocolErr.Kind != test.kind || protocolErr.Operation != "dsl detail" || strings.Contains(fmt.Sprintf("%#v", err), "outside.test") {
				t.Fatalf("result=%#v error=%#v", result, err)
			}
			for _, request := range *requests {
				if strings.Contains(request, "#") {
					t.Fatalf("SOAP request sent after failed preflight: %v", *requests)
				}
			}
		})
	}
}

func TestDSLDetailValidatesFields(t *testing.T) {
	statistics := soapValues{ReceiveBlocks: "1", TransmitBlocks: "2", CellDelin: "3", LinkRetrain: "4", InitErrors: "5", InitTimeouts: "6", LossOfFraming: "7", ErroredSecs: "8", SeverelyErroredSecs: "9", FECErrors: "10", ATUCFECErrors: "11", HECErrors: "12", ATUCHECErrors: "13", CRCErrors: "14", ATUCCRCErrors: "15"}
	diagnosis := soapValues{DSLDiagnoseState: "NONE", CableNokDistance: "-1", DSLLastDiagnoseTime: "0", DSLSignalLossTime: "0", DSLActive: "1", DSLSync: "1"}
	if _, err := parseDSLStatistics(statistics); err != nil {
		t.Fatal(err)
	}
	if _, err := parseDSLDiagnosis(diagnosis); err != nil {
		t.Fatal(err)
	}
	unknown := diagnosis
	unknown.DSLDiagnoseState = "private-value"
	if result, err := parseDSLDiagnosis(unknown); err != nil || result.State != "unknown" {
		t.Fatalf("result=%#v error=%v", result, err)
	}
	for _, test := range []struct {
		name   string
		mutate func(statistics, diagnosis *soapValues)
	}{
		{"missing counter", func(s, _ *soapValues) { s.LinkRetrain = "" }},
		{"negative counter", func(s, _ *soapValues) { s.CRCErrors = "-1" }},
		{"counter above ui4", func(s, _ *soapValues) { s.ErroredSecs = "4294967296" }},
		{"missing state", func(_, d *soapValues) { d.DSLDiagnoseState = "" }},
		{"missing distance", func(_, d *soapValues) { d.CableNokDistance = "" }},
		{"distance below -1", func(_, d *soapValues) { d.CableNokDistance = "-2" }},
		{"invalid loss time", func(_, d *soapValues) { d.DSLSignalLossTime = "private-value" }},
		{"invalid last diagnose time", func(_, d *soapValues) { d.DSLLastDiagnoseTime = "-1" }},
		{"invalid active flag", func(_, d *soapValues) { d.DSLActive = "2" }},
		{"missing sync flag", func(_, d *soapValues) { d.DSLSync = "" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, d := statistics, diagnosis
			test.mutate(&s, &d)
			_, statisticsErr := parseDSLStatistics(s)
			_, diagnosisErr := parseDSLDiagnosis(d)
			err := errors.Join(statisticsErr, diagnosisErr)
			var protocolErr *Error
			if !errors.As(err, &protocolErr) || protocolErr.Kind != "protocol" || protocolErr.Operation != "dsl detail" || strings.Contains(fmt.Sprintf("%#v", protocolErr), "private-value") {
				t.Fatalf("error=%#v", err)
			}
		})
	}
}

func TestDSLDetailFailuresAreSanitized(t *testing.T) {
	for _, test := range []struct {
		name, action, body, kind string
		status                   int
	}{
		{name: "internal error", action: dslDiagnosisAction, body: `<Fault><errorCode>820</errorCode><errorDescription>private-fault</errorDescription></Fault>`, kind: "router", status: http.StatusInternalServerError},
		{name: "invalid XML", action: dslStatisticsAction, body: `<private-value`, kind: "protocol", status: http.StatusOK},
		{name: "unauthorized", action: dslStatisticsAction, kind: "auth", status: http.StatusUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			responses := dslDetailResponses()
			responses[test.action] = dslResponse{body: test.body, status: test.status}
			client, _ := dslFixtureClient(t, dslDescriptionFixture, dslDetailSCPDFixture, responses)
			result, err := client.DSLDetail(t.Context())
			var protocolErr *Error
			if !reflect.DeepEqual(result, DSLDetail{}) || !errors.As(err, &protocolErr) || protocolErr.Kind != test.kind || protocolErr.Operation != "dsl detail" || strings.Contains(fmt.Sprintf("%#v", err), "private") || strings.Contains(fmt.Sprintf("%#v", err), client.base.Host) {
				t.Fatalf("result=%#v error=%#v", result, err)
			}
		})
	}
}

func TestDSLDetailRefusesSCPDRedirect(t *testing.T) {
	client, requests := dslFixtureClient(t, dslDescriptionFixture, dslDetailSCPDFixture, map[string]dslResponse{"GET /dsl.xml": {status: http.StatusFound, location: "__ORIGIN__/other.xml"}})
	result, err := client.DSLDetail(t.Context())
	var protocolErr *Error
	if !reflect.DeepEqual(result, DSLDetail{}) || !errors.As(err, &protocolErr) || protocolErr.Operation != "dsl detail" || !reflect.DeepEqual(*requests, []string{"GET /tr64desc.xml", "GET /dsl.xml"}) {
		t.Fatalf("result=%#v error=%#v requests=%v", result, err, *requests)
	}
}
