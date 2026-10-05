package tr064

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

type DSLDetail struct {
	Statistics *DSLStatistics `json:"statistics"`
	Diagnosis  *DSLDiagnosis  `json:"diagnosis"`
}

type DSLStatistics struct {
	ReceiveBlocks          uint64 `json:"receive_blocks"`
	TransmitBlocks         uint64 `json:"transmit_blocks"`
	CellDelineation        uint64 `json:"cell_delineation"`
	LinkRetrains           uint64 `json:"link_retrains"`
	InitErrors             uint64 `json:"init_errors"`
	InitTimeouts           uint64 `json:"init_timeouts"`
	LossOfFraming          uint64 `json:"loss_of_framing"`
	ErroredSeconds         uint64 `json:"errored_seconds"`
	SeverelyErroredSeconds uint64 `json:"severely_errored_seconds"`
	FECErrors              uint64 `json:"fec_errors"`
	ATUCFECErrors          uint64 `json:"atuc_fec_errors"`
	HECErrors              uint64 `json:"hec_errors"`
	ATUCHECErrors          uint64 `json:"atuc_hec_errors"`
	CRCErrors              uint64 `json:"crc_errors"`
	ATUCCRCErrors          uint64 `json:"atuc_crc_errors"`
}

type DSLDiagnosis struct {
	State                    string  `json:"state"`
	CableFaultDistanceMeters *uint64 `json:"cable_fault_distance_meters"`
	LastDiagnoseTimeSeconds  uint64  `json:"last_diagnose_time_seconds"`
	SignalLossTimeSeconds    uint64  `json:"signal_loss_time_seconds"`
	Active                   bool    `json:"active"`
	Sync                     bool    `json:"sync"`
}

const (
	dslStatisticsAction  = "GetStatisticsTotal"
	dslDiagnosisAction   = "X_AVM-DE_GetDSLDiagnoseInfo"
	dslDetailRemediation = "use firmware that advertises WANDSLInterfaceConfig:" + dslStatisticsAction + " or " + dslDiagnosisAction
)

func (c *Client) DSLDetail(ctx context.Context) (DSLDetail, error) {
	client := *c
	httpClient := *c.http
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("DSL detail inspection refuses redirects")
	}
	client.http = &httpClient
	client.services, client.allServices, client.digestChallenge = nil, nil, nil
	if err := client.discover(ctx); err != nil {
		return DSLDetail{}, dslDetailError(err)
	}
	target, actions, err := client.dslServiceActions(ctx, "dsl detail", dslDetailRemediation, dslDetailError)
	if err != nil {
		return DSLDetail{}, err
	}
	if !actions[dslStatisticsAction] && !actions[dslDiagnosisAction] {
		return DSLDetail{}, &Error{Kind: "unsupported", Operation: "dsl detail", Message: "router advertises neither WANDSLInterfaceConfig:" + dslStatisticsAction + " nor " + dslDiagnosisAction + "; " + dslDetailRemediation}
	}
	var result DSLDetail
	if actions[dslStatisticsAction] {
		values, err := client.actionOnService(ctx, target, dslStatisticsAction)
		if err != nil {
			return DSLDetail{}, dslDetailError(err)
		}
		if result.Statistics, err = parseDSLStatistics(values); err != nil {
			return DSLDetail{}, err
		}
	}
	if actions[dslDiagnosisAction] {
		values, err := client.actionOnService(ctx, target, dslDiagnosisAction)
		if err != nil {
			return DSLDetail{}, dslDetailError(err)
		}
		if result.Diagnosis, err = parseDSLDiagnosis(values); err != nil {
			return DSLDetail{}, err
		}
	}
	return result, nil
}

func parseDSLStatistics(values soapValues) (*DSLStatistics, error) {
	var result DSLStatistics
	for _, counter := range []struct {
		value, field string
		target       *uint64
	}{
		{values.ReceiveBlocks, "receive blocks", &result.ReceiveBlocks},
		{values.TransmitBlocks, "transmit blocks", &result.TransmitBlocks},
		{values.CellDelin, "cell delineation", &result.CellDelineation},
		{values.LinkRetrain, "link retrains", &result.LinkRetrains},
		{values.InitErrors, "init errors", &result.InitErrors},
		{values.InitTimeouts, "init timeouts", &result.InitTimeouts},
		{values.LossOfFraming, "loss of framing", &result.LossOfFraming},
		{values.ErroredSecs, "errored seconds", &result.ErroredSeconds},
		{values.SeverelyErroredSecs, "severely errored seconds", &result.SeverelyErroredSeconds},
		{values.FECErrors, "FEC errors", &result.FECErrors},
		{values.ATUCFECErrors, "ATUC FEC errors", &result.ATUCFECErrors},
		{values.HECErrors, "HEC errors", &result.HECErrors},
		{values.ATUCHECErrors, "ATUC HEC errors", &result.ATUCHECErrors},
		{values.CRCErrors, "CRC errors", &result.CRCErrors},
		{values.ATUCCRCErrors, "ATUC CRC errors", &result.ATUCCRCErrors},
	} {
		n, err := dslDetailUint(counter.value, counter.field)
		if err != nil {
			return nil, err
		}
		*counter.target = n
	}
	return &result, nil
}

func parseDSLDiagnosis(values soapValues) (*DSLDiagnosis, error) {
	state := strings.TrimSpace(values.DSLDiagnoseState)
	switch state {
	case "":
		return nil, dslDetailFieldError("diagnosis state")
	case "NONE", "NO_CALIB", "RUNNING", "DONE", "DONE_CABLE_NOK", "DONE_CABLE_OK":
	default:
		state = "unknown"
	}
	result := DSLDiagnosis{State: state}
	distance, err := strconv.ParseInt(strings.TrimSpace(values.CableNokDistance), 10, 32)
	if err != nil || distance < -1 {
		return nil, dslDetailFieldError("cable fault distance")
	}
	if distance >= 0 {
		meters := uint64(distance)
		result.CableFaultDistanceMeters = &meters
	}
	if result.LastDiagnoseTimeSeconds, err = dslDetailUint(values.DSLLastDiagnoseTime, "last diagnose time"); err != nil {
		return nil, err
	}
	if result.SignalLossTimeSeconds, err = dslDetailUint(values.DSLSignalLossTime, "signal loss time"); err != nil {
		return nil, err
	}
	if result.Active, err = dslDetailBool(values.DSLActive, "active flag"); err != nil {
		return nil, err
	}
	if result.Sync, err = dslDetailBool(values.DSLSync, "sync flag"); err != nil {
		return nil, err
	}
	return &result, nil
}

func dslDetailUint(value, field string) (uint64, error) {
	n, err := strconv.ParseUint(strings.TrimSpace(value), 10, 32)
	if err != nil {
		return 0, dslDetailFieldError(field)
	}
	return n, nil
}

func dslDetailBool(value, field string) (bool, error) {
	switch strings.TrimSpace(value) {
	case "1", "true":
		return true, nil
	case "0", "false":
		return false, nil
	}
	return false, dslDetailFieldError(field)
}

func dslDetailFieldError(field string) *Error {
	return &Error{Kind: "protocol", Operation: "dsl detail", Message: "router returned an invalid DSL " + field}
}

func dslDetailError(err error) *Error {
	return dslOperationError("dsl detail", "DSL detail inspection failed", "router does not support the documented DSL detail reads; "+dslDetailRemediation, err)
}
