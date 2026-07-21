package mcpserver

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/marang/robotgo/agent"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestFindReturnsOnlySanitizedConditionResult(t *testing.T) {
	request := agent.FindColorRequest{
		ObservationID: "observation-17",
		Condition: agent.ColorCondition{
			Red: 19, Green: 83, Blue: 241, Tolerance: 0.123456789,
		},
		Confirmed: true,
	}
	fake := &fakeSession{findFunc: func(_ context.Context, got agent.FindColorRequest) (agent.FindColorResult, error) {
		if got != request {
			t.Fatalf("FindColor request = %+v, want %+v", got, request)
		}
		return agent.FindColorResult{
			SchemaVersion: agent.ConditionSchemaVersion,
			ConditionID:   "condition-11",
			ObservationID: got.ObservationID,
			Status:        agent.ConditionMatched,
			Match:         &agent.VisualMatch{X: 501, Y: 702, DisplayID: 3},
		}, nil
	}}
	client := connectProtocol(t, newProtocolServer(t, fake))

	result := callTool(t, client, ToolFind, request)
	if result.IsError {
		t.Fatalf("find returned tool error: %s", serializedResult(t, result))
	}
	output := decodeOutput[FindOutput](t, result)
	if output.Result == nil || output.Result.Status != agent.ConditionMatched || output.Result.Match == nil {
		t.Fatalf("find output = %+v", output)
	}
	if output.Result.Match.X != 501 || output.Result.ObservationID != request.ObservationID {
		t.Fatalf("find result = %+v", output.Result)
	}
	assertNoConditionPayload(t, result)
}

func TestFindNoMatchIsARegularSanitizedResult(t *testing.T) {
	fake := &fakeSession{findFunc: func(_ context.Context, request agent.FindColorRequest) (agent.FindColorResult, error) {
		return agent.FindColorResult{
			SchemaVersion: agent.ConditionSchemaVersion,
			ConditionID:   "condition-21",
			ObservationID: request.ObservationID,
			Status:        agent.ConditionNotMatched,
		}, nil
	}}
	client := connectProtocol(t, newProtocolServer(t, fake))

	result := callTool(t, client, ToolFind, agent.FindColorRequest{
		ObservationID: "observation-31",
		Condition:     agent.ColorCondition{Red: 7, Green: 71, Blue: 171, Tolerance: 0.345678912},
	})
	if result.IsError {
		t.Fatalf("no-match find returned tool error: %s", serializedResult(t, result))
	}
	output := decodeOutput[FindOutput](t, result)
	if output.Result == nil || output.Result.Status != agent.ConditionNotMatched || output.Result.Match != nil {
		t.Fatalf("no-match find output = %+v", output)
	}
	assertNoConditionPayload(t, result)
}

func TestWaitReturnsSanitizedResultAndObservationCanBeReleased(t *testing.T) {
	request := agent.WaitColorRequest{
		Region: agent.CaptureRegion{X: 100, Y: 200, Width: 30, Height: 40, DisplayID: 2},
		Condition: agent.ColorCondition{
			Red: 37, Green: 91, Blue: 213, Tolerance: 0.234567891,
		},
		Confirmed: true,
	}
	var released []string
	fake := &fakeSession{
		waitFunc: func(_ context.Context, got agent.WaitColorRequest) (agent.WaitColorResult, error) {
			if got != request {
				t.Fatalf("WaitColor request = %+v, want %+v", got, request)
			}
			return agent.WaitColorResult{
				SchemaVersion: agent.ConditionSchemaVersion,
				ConditionID:   "condition-12",
				Status:        agent.ConditionMatched,
				Attempts:      3,
				ObservationID: "observation-23",
				Match:         &agent.VisualMatch{X: 108, Y: 209, DisplayID: 2},
			}, nil
		},
		releaseFunc: func(id string) error {
			released = append(released, id)
			return nil
		},
	}
	client := connectProtocol(t, newProtocolServer(t, fake))

	result := callTool(t, client, ToolWait, request)
	if result.IsError {
		t.Fatalf("wait returned tool error: %s", serializedResult(t, result))
	}
	output := decodeOutput[WaitOutput](t, result)
	if output.Result == nil || output.Result.ObservationID != "observation-23" || output.Result.Attempts != 3 {
		t.Fatalf("wait output = %+v", output)
	}
	assertNoConditionPayload(t, result)

	for range 2 {
		releaseResult := callTool(t, client, ToolReleaseObservation, ReleaseObservationInput{
			ObservationID: output.Result.ObservationID,
		})
		if releaseResult.IsError {
			t.Fatalf("release returned tool error: %s", serializedResult(t, releaseResult))
		}
		if releaseOutput := decodeOutput[ReleaseObservationOutput](t, releaseResult); !releaseOutput.Released {
			t.Fatalf("release output = %+v", releaseOutput)
		}
	}
	if len(released) != 2 || released[0] != "observation-23" || released[1] != released[0] {
		t.Fatalf("released IDs = %v", released)
	}
}

func TestConditionFailureKeepsSafePartialResultAndSanitizesBackendError(t *testing.T) {
	const privateBackendError = "private pixels and backend path"
	fake := &fakeSession{waitFunc: func(context.Context, agent.WaitColorRequest) (agent.WaitColorResult, error) {
		return agent.WaitColorResult{
			SchemaVersion: agent.ConditionSchemaVersion,
			ConditionID:   "condition-13",
			Status:        agent.ConditionNotMatched,
			Attempts:      2,
		}, errors.New(privateBackendError)
	}}
	client := connectProtocol(t, newProtocolServer(t, fake))

	result := callTool(t, client, ToolWait, agent.WaitColorRequest{
		Region:    agent.CaptureRegion{Width: 1, Height: 1},
		Condition: agent.ColorCondition{},
	})
	if !result.IsError {
		t.Fatal("backend failure unexpectedly succeeded")
	}
	serialized := serializedResult(t, result)
	if strings.Contains(serialized, privateBackendError) {
		t.Fatal("raw condition backend error crossed MCP boundary")
	}
	output := decodeOutput[WaitOutput](t, result)
	if output.Result == nil || output.Result.Attempts != 2 {
		t.Fatalf("partial wait result = %+v", output.Result)
	}
	if output.Error == nil || output.Error.Code != agent.ErrorBackendFailure || output.Error.Message != errorMessageFailed {
		t.Fatalf("safe wait error = %+v", output.Error)
	}
}

func TestConditionStructuredErrorPreservesSafeCode(t *testing.T) {
	fake := &fakeSession{waitFunc: func(context.Context, agent.WaitColorRequest) (agent.WaitColorResult, error) {
		return agent.WaitColorResult{
				SchemaVersion: agent.ConditionSchemaVersion,
				ConditionID:   "condition-22",
				Status:        agent.ConditionNotMatched,
				Attempts:      4,
			}, &agent.ActionError{
				Code: agent.ErrorConditionNotMet, Operation: agent.OperationWaitColor,
				Message: "visual condition was not met",
			}
	}}
	client := connectProtocol(t, newProtocolServer(t, fake))

	result := callTool(t, client, ToolWait, agent.WaitColorRequest{
		Region: agent.CaptureRegion{Width: 1, Height: 1},
	})
	if !result.IsError {
		t.Fatal("condition error unexpectedly succeeded")
	}
	output := decodeOutput[WaitOutput](t, result)
	if output.Error == nil || output.Error.Code != agent.ErrorConditionNotMet || output.Error.Message != "visual condition was not met" {
		t.Fatalf("condition error = %+v", output.Error)
	}
	if output.Result == nil || output.Result.Attempts != 4 || output.Result.Status != agent.ConditionNotMatched {
		t.Fatalf("condition partial result = %+v", output.Result)
	}
}

func TestWaitErrorPreservesReleaseHandleForRetainedObservation(t *testing.T) {
	var released string
	fake := &fakeSession{
		waitFunc: func(context.Context, agent.WaitColorRequest) (agent.WaitColorResult, error) {
			return agent.WaitColorResult{
					SchemaVersion: agent.ConditionSchemaVersion,
					ConditionID:   "condition-23",
					Status:        agent.ConditionMatched,
					Attempts:      1,
					ObservationID: "observation-41",
					Match:         &agent.VisualMatch{X: 9, Y: 10},
				}, &agent.ActionError{
					Code: agent.ErrorAuditDelivery, Operation: agent.OperationWaitColor,
					Message: "color wait completed but audit delivery failed",
				}
		},
		releaseFunc: func(id string) error {
			released = id
			return nil
		},
	}
	client := connectProtocol(t, newProtocolServer(t, fake))

	result := callTool(t, client, ToolWait, agent.WaitColorRequest{
		Region: agent.CaptureRegion{Width: 1, Height: 1},
	})
	if !result.IsError {
		t.Fatal("audit failure unexpectedly succeeded")
	}
	output := decodeOutput[WaitOutput](t, result)
	if output.Error == nil || output.Error.Code != agent.ErrorAuditDelivery ||
		output.Result == nil || output.Result.ObservationID != "observation-41" {
		t.Fatalf("audit failure output = %+v", output)
	}

	releaseResult := callTool(t, client, ToolReleaseObservation, ReleaseObservationInput{
		ObservationID: output.Result.ObservationID,
	})
	if releaseResult.IsError {
		t.Fatalf("release after wait error failed: %s", serializedResult(t, releaseResult))
	}
	if released != "observation-41" {
		t.Fatalf("released observation = %q", released)
	}
}

func TestReleaseObservationReturnsStructuredValidationError(t *testing.T) {
	fake := &fakeSession{releaseFunc: func(string) error {
		return &agent.ActionError{Code: agent.ErrorInvalidInput, Message: "invalid RobotGo observation ID"}
	}}
	client := connectProtocol(t, newProtocolServer(t, fake))

	result := callTool(t, client, ToolReleaseObservation, ReleaseObservationInput{ObservationID: "invalid"})
	if !result.IsError {
		t.Fatal("invalid release unexpectedly succeeded")
	}
	output := decodeOutput[ReleaseObservationOutput](t, result)
	if output.Released || output.Error == nil || output.Error.Code != agent.ErrorInvalidInput {
		t.Fatalf("release error output = %+v", output)
	}
}

func TestLegacySessionKeepsOriginalToolSurface(t *testing.T) {
	legacy := struct{ Session }{Session: &fakeSession{}}
	client := connectProtocol(t, newProtocolServer(t, legacy))

	result, err := client.clientSession.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var names []string
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	want := []string{ToolAct, ToolCapabilities, ToolClose, ToolObserve}
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Fatalf("legacy tools = %v, want %v", names, want)
	}
}

func TestWaitCancellationReachesSessionOperation(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	fake := &fakeSession{waitFunc: func(ctx context.Context, _ agent.WaitColorRequest) (agent.WaitColorResult, error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		return agent.WaitColorResult{}, ctx.Err()
	}}
	client := connectProtocol(t, newProtocolServer(t, fake))

	ctx, cancel := context.WithCancel(t.Context())
	resultDone := make(chan error, 1)
	go func() {
		_, err := client.clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name: ToolWait,
			Arguments: agent.WaitColorRequest{
				Region: agent.CaptureRegion{Width: 1, Height: 1}, Condition: agent.ColorCondition{},
			},
		})
		resultDone <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("wait did not start")
	}
	cancel()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("cancellation did not reach WaitColor")
	}
	select {
	case err := <-resultDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("CallTool error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled wait call did not return")
	}
}

func TestConditionSchemaRejectsUnknownInputWithoutCallingSession(t *testing.T) {
	const privateValue = "private-condition-payload"
	fake := &fakeSession{}
	client := connectProtocol(t, newProtocolServer(t, fake))

	result := callTool(t, client, ToolFind, map[string]any{
		"observation_id": "observation-1",
		"condition":      map[string]any{"red": 1, "green": 2, "blue": 3, "tolerance": 0},
		"unknown":        privateValue,
	})
	if !result.IsError {
		t.Fatal("unknown condition field unexpectedly succeeded")
	}
	if strings.Contains(serializedResult(t, result), privateValue) {
		t.Fatal("invalid condition input was echoed")
	}
	finds, waits, releases := fake.conditionCounts()
	if finds != 0 || waits != 0 || releases != 0 {
		t.Fatalf("schema-invalid input reached session: find=%d wait=%d release=%d", finds, waits, releases)
	}
}

func assertNoConditionPayload(t *testing.T, result *mcp.CallToolResult) {
	t.Helper()
	serialized := strings.ToLower(serializedResult(t, result))
	for _, forbidden := range []string{"\"red\"", "\"green\"", "\"blue\"", "\"tolerance\"", "sha256", "pixels", "image"} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("condition result leaked forbidden field %q: %s", forbidden, serialized)
		}
	}
}
