package main

import (
	"fmt"
	"math"
	"net/http"
)

func runExecuteGateCommand(client *apiClient) error {
	if err := verifyNormalCompletion(client); err != nil {
		return err
	}
	if err := verifyFaultRegistration(client); err != nil {
		return err
	}
	return nil
}

func verifyNormalCompletion(client *apiClient) error {
	if _, err := fixtureGate(client, "gate-west", "西侧泄洪闸", 4, 0.25); err != nil {
		return err
	}
	command, err := fixtureCommand(client, "gate-west", 2.0, "平衡下游水位", "dispatcher-zhao")
	if err != nil {
		return err
	}
	commandID, err := stringField(command, "id")
	if err != nil {
		return err
	}

	approved, err := fixtureClearReview(client, commandID, "safety-he")
	if err != nil {
		return err
	}
	token, err := stringField(approved, "execution_token")
	if err != nil {
		return err
	}

	executed, err := client.post("/commands/"+commandID+"/execute", map[string]any{
		"operator":       "operator-chen",
		"token":          token,
		"simulate_fault": false,
	})
	if err != nil {
		return err
	}
	if err := requireStatus(executed, http.StatusOK, "execute approved command"); err != nil {
		return err
	}
	executedCommand, err := objectField(executed.Body, "command")
	if err != nil {
		return err
	}
	completedStatus, err := stringField(executedCommand, "status")
	if err != nil {
		return err
	}
	if completedStatus != "completed" {
		return fmt.Errorf("execution status: expected completed, got %q", completedStatus)
	}
	if _, exists := executedCommand["actual_aperture"]; exists {
		return fmt.Errorf("completed command must not carry a fault actual_aperture: %s", prettyBody(executedCommand))
	}

	executedGate, err := objectField(executed.Body, "gate")
	if err != nil {
		return err
	}
	gateStatus, err := stringField(executedGate, "status")
	if err != nil {
		return err
	}
	aperture, err := floatField(executedGate, "aperture")
	if err != nil {
		return err
	}
	if gateStatus != "idle" || aperture != 2.0 {
		return fmt.Errorf("settled gate mismatch: status=%q aperture=%v", gateStatus, aperture)
	}
	if _, exists := executedGate["fault_basis"]; exists {
		return fmt.Errorf("idle gate must not carry a fault basis: %s", prettyBody(executedGate))
	}

	reused, err := client.post("/commands/"+commandID+"/execute", map[string]any{
		"operator":       "operator-chen",
		"token":          token,
		"simulate_fault": false,
	})
	if err != nil {
		return err
	}
	if err := requireStatus(reused, http.StatusConflict, "reject reused execution token"); err != nil {
		return err
	}

	fmt.Println("execute-gate-command: normal completion settled at target and token reuse rejected")
	return nil
}

func verifyFaultRegistration(client *apiClient) error {
	if _, err := fixtureGate(client, "gate-jam", "卡阻泄洪闸", 4, 0.25); err != nil {
		return err
	}
	command, err := fixtureCommand(client, "gate-jam", 2.0, "落闸过程卡阻", "dispatcher-zhao")
	if err != nil {
		return err
	}
	commandID, err := stringField(command, "id")
	if err != nil {
		return err
	}
	approved, err := fixtureClearReview(client, commandID, "safety-he")
	if err != nil {
		return err
	}
	token, err := stringField(approved, "execution_token")
	if err != nil {
		return err
	}

	// A fault without the operator's actual stop aperture is invalid input.
	missingAperture, err := client.post("/commands/"+commandID+"/execute", map[string]any{
		"operator":       "operator-chen",
		"token":          token,
		"simulate_fault": true,
		"fault_basis":    "现场标尺读数",
	})
	if err != nil {
		return err
	}
	if err := requireStatus(missingAperture, http.StatusBadRequest, "reject fault without actual aperture"); err != nil {
		return err
	}

	// A fault without the basis for the reading is invalid input.
	missingBasis, err := client.post("/commands/"+commandID+"/execute", map[string]any{
		"operator":        "operator-chen",
		"token":           token,
		"simulate_fault":  true,
		"actual_aperture": 1.25,
	})
	if err != nil {
		return err
	}
	if err := requireStatus(missingBasis, http.StatusBadRequest, "reject fault without basis"); err != nil {
		return err
	}

	// A stop beyond the travel allowed by this command (starting 0.25 -> target
	// 2.0) must fail explicitly and must not write a partial state.
	outOfTravel, err := client.post("/commands/"+commandID+"/execute", map[string]any{
		"operator":        "operator-chen",
		"token":           token,
		"simulate_fault":  true,
		"actual_aperture": 2.75,
		"fault_basis":     "现场标尺读数",
	})
	if err != nil {
		return err
	}
	if err := requireStatus(outOfTravel, http.StatusBadRequest, "reject out-of-travel fault stop"); err != nil {
		return err
	}
	if err := requireStillExecutable(client, commandID); err != nil {
		return err
	}

	// A corrected, in-travel fault report registers the actual stop and basis.
	const stopAperture = 1.25
	const basis = "闸门卡阻，现场标尺复测停在1.25m"
	faulted, err := client.post("/commands/"+commandID+"/execute", map[string]any{
		"operator":        "operator-chen",
		"token":           token,
		"simulate_fault":  true,
		"actual_aperture": stopAperture,
		"fault_basis":     basis,
	})
	if err != nil {
		return err
	}
	if err := requireStatus(faulted, http.StatusOK, "register actual fault stop"); err != nil {
		return err
	}

	faultCommand, err := objectField(faulted.Body, "command")
	if err != nil {
		return err
	}
	commandStatus, err := stringField(faultCommand, "status")
	if err != nil {
		return err
	}
	if commandStatus != "aborted" {
		return fmt.Errorf("fault command status: expected aborted, got %q", commandStatus)
	}
	if err := requireAperture(faultCommand, "actual_aperture", stopAperture); err != nil {
		return fmt.Errorf("fault command actual aperture: %w", err)
	}
	commandBasis, err := stringField(faultCommand, "fault_basis")
	if err != nil {
		return err
	}
	if commandBasis != basis {
		return fmt.Errorf("fault command basis: expected %q, got %q", basis, commandBasis)
	}
	if _, exists := faultCommand["execution_token"]; exists && faultCommand["execution_token"] != "" {
		return fmt.Errorf("aborted command must not retain its execution token: %s", prettyBody(faultCommand))
	}

	faultGate, err := objectField(faulted.Body, "gate")
	if err != nil {
		return err
	}
	gateStatus, err := stringField(faultGate, "status")
	if err != nil {
		return err
	}
	if gateStatus != "faulted" {
		return fmt.Errorf("fault gate status: expected faulted, got %q", gateStatus)
	}
	if err := requireAperture(faultGate, "aperture", stopAperture); err != nil {
		return fmt.Errorf("fault gate aperture: %w", err)
	}
	gateBasis, err := stringField(faultGate, "fault_basis")
	if err != nil {
		return err
	}
	if gateBasis != basis {
		return fmt.Errorf("fault gate basis: expected %q, got %q", basis, gateBasis)
	}
	if active, exists := faultGate["active_command_id"]; exists && active != "" {
		return fmt.Errorf("faulted gate must release its active command, got %v", active)
	}

	// The aborted command, the faulted gate and subsequent reads must agree on
	// the single registered stop fact.
	if err := requireReadingsAgree(client, commandID, "gate-jam", stopAperture, basis); err != nil {
		return err
	}

	// The execution permission cannot be reused after the abort.
	reused, err := client.post("/commands/"+commandID+"/execute", map[string]any{
		"operator":        "operator-chen",
		"token":           token,
		"simulate_fault":  true,
		"actual_aperture": stopAperture,
		"fault_basis":     basis,
	})
	if err != nil {
		return err
	}
	if err := requireStatus(reused, http.StatusConflict, "reject token reuse after abort"); err != nil {
		return err
	}

	fmt.Println("execute-gate-command: fault stop registered; out-of-travel failed and token not reusable")
	return nil
}

// requireStillExecutable proves an invalid fault report committed no partial
// state: the command stays approved with its token and the gate stays reserved.
func requireStillExecutable(client *apiClient, commandID string) error {
	commandResult, err := client.get("/commands/" + commandID)
	if err != nil {
		return err
	}
	if err := requireStatus(commandResult, http.StatusOK, "read command after rejected fault"); err != nil {
		return err
	}
	storedCommand, err := objectField(commandResult.Body, "command")
	if err != nil {
		return err
	}
	status, err := stringField(storedCommand, "status")
	if err != nil {
		return err
	}
	if status != "approved" {
		return fmt.Errorf("rejected fault must leave command approved, got %q", status)
	}

	gateID, err := stringField(storedCommand, "gate_id")
	if err != nil {
		return err
	}
	gateResult, err := client.get("/gates/" + gateID)
	if err != nil {
		return err
	}
	if err := requireStatus(gateResult, http.StatusOK, "read gate after rejected fault"); err != nil {
		return err
	}
	storedGate, err := objectField(gateResult.Body, "gate")
	if err != nil {
		return err
	}
	gateStatus, err := stringField(storedGate, "status")
	if err != nil {
		return err
	}
	activeCommandID, err := stringField(storedGate, "active_command_id")
	if err != nil {
		return err
	}
	if gateStatus != "reserved" || activeCommandID != commandID {
		return fmt.Errorf(
			"rejected fault must leave gate reserved by command: status=%q active=%q",
			gateStatus,
			activeCommandID,
		)
	}
	return nil
}

func requireReadingsAgree(
	client *apiClient,
	commandID string,
	gateID string,
	expectedAperture float64,
	expectedBasis string,
) error {
	commandResult, err := client.get("/commands/" + commandID)
	if err != nil {
		return err
	}
	if err := requireStatus(commandResult, http.StatusOK, "read aborted command"); err != nil {
		return err
	}
	storedCommand, err := objectField(commandResult.Body, "command")
	if err != nil {
		return err
	}
	if err := requireAperture(storedCommand, "actual_aperture", expectedAperture); err != nil {
		return fmt.Errorf("aborted command readback: %w", err)
	}
	basis, err := stringField(storedCommand, "fault_basis")
	if err != nil {
		return err
	}
	if basis != expectedBasis {
		return fmt.Errorf("aborted command readback basis: expected %q, got %q", expectedBasis, basis)
	}

	gateResult, err := client.get("/gates/" + gateID)
	if err != nil {
		return err
	}
	if err := requireStatus(gateResult, http.StatusOK, "read faulted gate"); err != nil {
		return err
	}
	storedGate, err := objectField(gateResult.Body, "gate")
	if err != nil {
		return err
	}
	if err := requireAperture(storedGate, "aperture", expectedAperture); err != nil {
		return fmt.Errorf("faulted gate readback: %w", err)
	}
	gateBasis, err := stringField(storedGate, "fault_basis")
	if err != nil {
		return err
	}
	if gateBasis != expectedBasis {
		return fmt.Errorf("faulted gate readback basis: expected %q, got %q", expectedBasis, gateBasis)
	}
	return nil
}

func requireAperture(object map[string]any, name string, expected float64) error {
	value, err := floatField(object, name)
	if err != nil {
		return err
	}
	if math.Abs(value-expected) > 1e-9 {
		return fmt.Errorf("field %q: expected %v, got %v", name, expected, value)
	}
	return nil
}
