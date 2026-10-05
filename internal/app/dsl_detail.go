package app

import (
	"fmt"
	"io"

	"github.com/Azd325/router-axi/internal/tr064"
)

func writeDSLDetail(w io.Writer, value tr064.DSLDetail) error {
	if _, err := io.WriteString(w, "dsl_detail:\n"); err != nil {
		return err
	}
	if s := value.Statistics; s == nil {
		if _, err := io.WriteString(w, "  statistics: unsupported\n"); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintf(w, "  statistics:\n    receive_blocks: %d\n    transmit_blocks: %d\n    cell_delineation: %d\n    link_retrains: %d\n    init_errors: %d\n    init_timeouts: %d\n    loss_of_framing: %d\n    errored_seconds: %d\n    severely_errored_seconds: %d\n    fec_errors: %d\n    atuc_fec_errors: %d\n    hec_errors: %d\n    atuc_hec_errors: %d\n    crc_errors: %d\n    atuc_crc_errors: %d\n", s.ReceiveBlocks, s.TransmitBlocks, s.CellDelineation, s.LinkRetrains, s.InitErrors, s.InitTimeouts, s.LossOfFraming, s.ErroredSeconds, s.SeverelyErroredSeconds, s.FECErrors, s.ATUCFECErrors, s.HECErrors, s.ATUCHECErrors, s.CRCErrors, s.ATUCCRCErrors); err != nil {
		return err
	}
	d := value.Diagnosis
	if d == nil {
		_, err := io.WriteString(w, "  diagnosis: unsupported\n")
		return err
	}
	_, err := fmt.Fprintf(w, "  diagnosis:\n    state: %s\n    cable_fault_distance_meters: %s\n    last_diagnose_time_seconds: %d\n    signal_loss_time_seconds: %d\n    active: %t\n    sync: %t\n", scalar(d.State), optionalUint(d.CableFaultDistanceMeters), d.LastDiagnoseTimeSeconds, d.SignalLossTimeSeconds, d.Active, d.Sync)
	return err
}
