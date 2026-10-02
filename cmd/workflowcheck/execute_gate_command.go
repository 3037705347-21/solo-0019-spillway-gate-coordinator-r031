package main

import (
	"fmt"
	"net/http"
)

func runExecuteGateCommand(client *apiClient) error {
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
	completedFinal, err := floatField(executedCommand, "final_aperture")
	if err != nil {
		return err
	}
	if completedFinal != 2.0 {
		return fmt.Errorf("completed final aperture: expected 2.0, got %v", completedFinal)
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

	if err := runFaultExecution(client, "gate-west"); err != nil {
		return err
	}

	fmt.Println("execute-gate-command: command completed, fault stop registered consistently, token reuse rejected")
	return nil
}

func runFaultExecution(client *apiClient, gateID string) error {
	command, err := fixtureCommand(client, gateID, 3.5, "继续抬升泄流", "dispatcher-zhao")
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

	missingStop, err := client.post("/commands/"+commandID+"/execute", map[string]any{
		"operator":       "operator-chen",
		"token":          token,
		"simulate_fault": true,
		"fault_reason":   "门体卡阻",
	})
	if err != nil {
		return err
	}
	if err := requireStatus(missingStop, http.StatusBadRequest, "reject fault without stop aperture"); err != nil {
		return err
	}

	missingReason, err := client.post("/commands/"+commandID+"/execute", map[string]any{
		"operator":       "operator-chen",
		"token":          token,
		"simulate_fault": true,
		"final_aperture": 3.0,
	})
	if err != nil {
		return err
	}
	if err := requireStatus(missingReason, http.StatusBadRequest, "reject fault without reason"); err != nil {
		return err
	}

	for _, stop := range []float64{1.5, 3.8} {
		outOfTravel, err := client.post("/commands/"+commandID+"/execute", map[string]any{
			"operator":       "operator-chen",
			"token":          token,
			"simulate_fault": true,
			"final_aperture": stop,
			"fault_reason":   "停位越出允许行程",
		})
		if err != nil {
			return err
		}
		if err := requireStatus(outOfTravel, http.StatusBadRequest, "reject stop outside allowed travel"); err != nil {
			return err
		}
	}

	successWithStop, err := client.post("/commands/"+commandID+"/execute", map[string]any{
		"operator":       "operator-chen",
		"token":          token,
		"simulate_fault": false,
		"final_aperture": 3.0,
	})
	if err != nil {
		return err
	}
	if err := requireStatus(successWithStop, http.StatusBadRequest, "reject stop aperture on successful execution"); err != nil {
		return err
	}

	faulted, err := client.post("/commands/"+commandID+"/execute", map[string]any{
		"operator":       "operator-chen",
		"token":          token,
		"simulate_fault": true,
		"final_aperture": 3.0,
		"fault_reason":   "闸门卡阻于3.0米",
	})
	if err != nil {
		return err
	}
	if err := requireStatus(faulted, http.StatusOK, "register fault stop"); err != nil {
		return err
	}
	faultedCommand, err := objectField(faulted.Body, "command")
	if err != nil {
		return err
	}
	if err := requireFaultStop(faultedCommand, 3.0, "闸门卡阻于3.0米"); err != nil {
		return err
	}
	faultedGate, err := objectField(faulted.Body, "gate")
	if err != nil {
		return err
	}
	if err := requireFaultedGate(faultedGate, 3.0); err != nil {
		return err
	}

	readCommand, err := client.get("/commands/" + commandID)
	if err != nil {
		return err
	}
	if err := requireStatus(readCommand, http.StatusOK, "read aborted command"); err != nil {
		return err
	}
	storedCommand, err := objectField(readCommand.Body, "command")
	if err != nil {
		return err
	}
	if err := requireFaultStop(storedCommand, 3.0, "闸门卡阻于3.0米"); err != nil {
		return err
	}

	readGate, err := client.get("/gates/" + gateID)
	if err != nil {
		return err
	}
	if err := requireStatus(readGate, http.StatusOK, "read faulted gate"); err != nil {
		return err
	}
	storedGate, err := objectField(readGate.Body, "gate")
	if err != nil {
		return err
	}
	if err := requireFaultedGate(storedGate, 3.0); err != nil {
		return err
	}

	reused, err := client.post("/commands/"+commandID+"/execute", map[string]any{
		"operator":       "operator-chen",
		"token":          token,
		"simulate_fault": true,
		"final_aperture": 3.0,
		"fault_reason":   "复用令牌重试",
	})
	if err != nil {
		return err
	}
	if err := requireStatus(reused, http.StatusConflict, "reject token reuse after abort"); err != nil {
		return err
	}
	return nil
}

func requireFaultStop(command map[string]any, wantAperture float64, wantReason string) error {
	status, err := stringField(command, "status")
	if err != nil {
		return err
	}
	if status != "aborted" {
		return fmt.Errorf("faulted command status: expected aborted, got %q", status)
	}
	finalAperture, err := floatField(command, "final_aperture")
	if err != nil {
		return err
	}
	if finalAperture != wantAperture {
		return fmt.Errorf("fault stop aperture: expected %v, got %v", wantAperture, finalAperture)
	}
	reason, err := stringField(command, "terminal_reason")
	if err != nil {
		return err
	}
	if reason != wantReason {
		return fmt.Errorf("fault reason: expected %q, got %q", wantReason, reason)
	}
	return nil
}

func requireFaultedGate(gate map[string]any, wantAperture float64) error {
	status, err := stringField(gate, "status")
	if err != nil {
		return err
	}
	aperture, err := floatField(gate, "aperture")
	if err != nil {
		return err
	}
	if status != "faulted" || aperture != wantAperture {
		return fmt.Errorf("faulted gate mismatch: status=%q aperture=%v", status, aperture)
	}
	return nil
}
