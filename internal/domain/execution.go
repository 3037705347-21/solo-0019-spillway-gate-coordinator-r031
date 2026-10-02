package domain

import "strings"

// ExecutionReport is the operator-registered outcome of one execution attempt.
// A successful report carries no extra data and the gate settles at the command
// target; a faulted report must register the actual stop aperture and the fault
// basis so the aborted command and the faulted gate describe the same fact.
type ExecutionReport struct {
	Faulted       bool
	FinalAperture float64
	FaultReason   string
}

func NewExecutionReport(faulted bool, finalAperture *float64, faultReason string) (ExecutionReport, error) {
	if !faulted {
		if finalAperture != nil {
			return ExecutionReport{}, Invalid(
				"unexpected_final_aperture",
				"final aperture is only registered for faulted executions",
			)
		}
		if strings.TrimSpace(faultReason) != "" {
			return ExecutionReport{}, Invalid(
				"unexpected_fault_reason",
				"fault reason is only registered for faulted executions",
			)
		}
		return ExecutionReport{}, nil
	}

	if finalAperture == nil {
		return ExecutionReport{}, Invalid(
			"missing_final_aperture",
			"faulted executions require the actual stop aperture",
		)
	}
	faultReason = strings.TrimSpace(faultReason)
	if faultReason == "" {
		return ExecutionReport{}, Invalid(
			"missing_fault_reason",
			"faulted executions require a fault reason",
		)
	}
	return ExecutionReport{
		Faulted:       true,
		FinalAperture: *finalAperture,
		FaultReason:   faultReason,
	}, nil
}
