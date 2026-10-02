package domain

// ExecutionOutcome describes how an executing command ended.
//
// A normal completion settles at the target aperture and carries no operator
// reading. A fault carries the operator-registered actual stop aperture and the
// basis for that reading, which become the single fact recorded on both the
// aborted command and the faulted gate.
type ExecutionOutcome struct {
	Fault          bool
	ActualAperture float64
	FaultBasis     string
}

// CompletionOutcome is the normal end state: the gate lands on the target.
func CompletionOutcome() ExecutionOutcome {
	return ExecutionOutcome{Fault: false}
}

// FaultOutcome registers a fault at the actual stop aperture with the basis
// that supports the reading. Fields are normalized here; the per-entity
// invariants still enforce them during the locked transition.
func FaultOutcome(actualAperture float64, basis string) ExecutionOutcome {
	return ExecutionOutcome{
		Fault:          true,
		ActualAperture: actualAperture,
		FaultBasis:     basis,
	}
}
