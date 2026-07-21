package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marang/robotgo/agent"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type fakeSession struct {
	mu sync.Mutex

	catalog     agent.OperationCatalog
	observation *agent.Observation
	observeFunc func(context.Context, agent.ObserveRequest) (*agent.Observation, error)
	dryRunFunc  func(context.Context, agent.ActionRequest) (agent.ActionResult, error)
	executeFunc func(context.Context, agent.ActionRequest) (agent.ActionResult, error)
	closeFunc   func() error

	dryRuns  int
	executes int
	closes   int
}

func (f *fakeSession) Catalog() agent.OperationCatalog { return f.catalog }

func (f *fakeSession) Observe(ctx context.Context, request agent.ObserveRequest) (*agent.Observation, error) {
	if f.observeFunc != nil {
		return f.observeFunc(ctx, request)
	}
	return f.observation, nil
}

func (f *fakeSession) DryRun(ctx context.Context, request agent.ActionRequest) (agent.ActionResult, error) {
	f.mu.Lock()
	f.dryRuns++
	f.mu.Unlock()
	if f.dryRunFunc != nil {
		return f.dryRunFunc(ctx, request)
	}
	return agent.ActionResult{ActionID: "planned-1", Operation: request.Operation, Status: agent.ActionPlanned}, nil
}

func (f *fakeSession) Execute(ctx context.Context, request agent.ActionRequest) (agent.ActionResult, error) {
	f.mu.Lock()
	f.executes++
	f.mu.Unlock()
	if f.executeFunc != nil {
		return f.executeFunc(ctx, request)
	}
	return agent.ActionResult{ActionID: "executed-1", Operation: request.Operation, Status: agent.ActionSucceeded}, nil
}

func (f *fakeSession) Close() error {
	f.mu.Lock()
	f.closes++
	f.mu.Unlock()
	if f.closeFunc != nil {
		return f.closeFunc()
	}
	return nil
}

func (f *fakeSession) counts() (dryRuns, executes, closes int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dryRuns, f.executes, f.closes
}

type protocolClient struct {
	clientSession *mcp.ClientSession
	serverSession *mcp.ServerSession
}

func connectProtocol(t *testing.T, server *Server) *protocolClient {
	t.Helper()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.protocol.Connect(t.Context(), serverTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "robotgo-test", Version: "1"}, nil)
	clientSession, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		_ = serverSession.Close()
		t.Fatalf("connect client: %v", err)
	}
	t.Cleanup(func() {
		_ = clientSession.Close()
		_ = serverSession.Close()
		_ = server.Close()
	})
	return &protocolClient{clientSession: clientSession, serverSession: serverSession}
}

func newProtocolServer(t *testing.T, session Session) *Server {
	t.Helper()
	server, err := New(session)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return server
}

func callTool(t *testing.T, client *protocolClient, name string, arguments any) *mcp.CallToolResult {
	t.Helper()
	result, err := client.clientSession.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	return result
}

func decodeOutput[T any](t *testing.T, result *mcp.CallToolResult) T {
	t.Helper()
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured output: %v", err)
	}
	var output T
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatalf("decode structured output: %v", err)
	}
	return output
}

func serializedResult(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	return string(data)
}

func TestProtocolInitializesAndListsOnlyFourTools(t *testing.T) {
	fake := &fakeSession{catalog: agent.OperationCatalog{SchemaVersion: agent.CatalogSchemaVersion}}
	server := newProtocolServer(t, fake)
	client := connectProtocol(t, server)

	result, err := client.clientSession.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var names []string
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
		if tool.InputSchema == nil || tool.OutputSchema == nil {
			t.Errorf("tool %q has incomplete schemas", tool.Name)
		}
	}
	slices.Sort(names)
	want := []string{ToolAct, ToolCapabilities, ToolClose, ToolObserve}
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Fatalf("tools = %v, want %v", names, want)
	}
}

func TestCapabilitiesReturnsCatalog(t *testing.T) {
	fake := &fakeSession{catalog: agent.OperationCatalog{
		SchemaVersion: agent.CatalogSchemaVersion,
		Operations:    []agent.OperationCapability{{Operation: agent.OperationObserve, Available: true}},
	}}
	client := connectProtocol(t, newProtocolServer(t, fake))

	result := callTool(t, client, ToolCapabilities, map[string]any{})
	if result.IsError {
		t.Fatalf("capabilities returned tool error: %s", serializedResult(t, result))
	}
	output := decodeOutput[CapabilitiesOutput](t, result)
	if output.Catalog.SchemaVersion != agent.CatalogSchemaVersion || len(output.Catalog.Operations) != 1 {
		t.Fatalf("unexpected catalog: %+v", output.Catalog)
	}
}

func TestObserveReturnsPrivacyReducedMetadata(t *testing.T) {
	const secretDigest = "secret-capture-digest"
	fake := &fakeSession{observation: &agent.Observation{
		SchemaVersion: agent.ObservationSchemaVersion,
		ObservationID: "observation-7",
		CreatedAt:     time.Unix(100, 0).UTC(),
		Diagnostics: agent.RuntimeDiagnostics{
			GOOS: "linux", GOARCH: "amd64", Implementation: "cgo", DisplayServer: "wayland",
		},
		Capture: &agent.CaptureMetadata{
			Region: agent.CaptureRegion{X: 1, Y: 2, Width: 3, Height: 4, DisplayID: 0},
			SHA256: secretDigest,
			Width:  3,
			Height: 4,
		},
	}}
	client := connectProtocol(t, newProtocolServer(t, fake))

	result := callTool(t, client, ToolObserve, map[string]any{})
	if result.IsError {
		t.Fatalf("observe returned tool error: %s", serializedResult(t, result))
	}
	output := decodeOutput[ObserveOutput](t, result)
	if output.Observation == nil || output.Observation.Capture == nil {
		t.Fatalf("missing observation metadata: %+v", output)
	}
	if output.Observation.Capture.Width != 3 || output.Observation.Capture.Height != 4 {
		t.Fatalf("capture dimensions = %+v", output.Observation.Capture)
	}
	serialized := serializedResult(t, result)
	for _, forbidden := range []string{secretDigest, "sha256", "pixels", "image"} {
		if strings.Contains(strings.ToLower(serialized), strings.ToLower(forbidden)) {
			t.Fatalf("observe result leaked forbidden value %q: %s", forbidden, serialized)
		}
	}
}

func TestActDefaultsToDryRunAndRequiresExplicitExecute(t *testing.T) {
	const typedSecret = "do-not-echo-this-text"
	fake := &fakeSession{}
	client := connectProtocol(t, newProtocolServer(t, fake))
	request := agent.ActionRequest{
		Operation: agent.OperationTypeText,
		TypeText:  &agent.TypeTextAction{Text: typedSecret},
	}

	dryResult := callTool(t, client, ToolAct, ActInput{Request: request})
	if dryResult.IsError {
		t.Fatalf("default act returned error: %s", serializedResult(t, dryResult))
	}
	if strings.Contains(serializedResult(t, dryResult), typedSecret) {
		t.Fatal("typed text was copied into dry-run output")
	}
	dryOutput := decodeOutput[ActOutput](t, dryResult)
	if dryOutput.Result == nil || dryOutput.Result.Status != agent.ActionPlanned {
		t.Fatalf("dry-run result = %+v", dryOutput)
	}

	executeResult := callTool(t, client, ToolAct, ActInput{Mode: ActModeExecute, Request: request})
	if executeResult.IsError {
		t.Fatalf("execute returned error: %s", serializedResult(t, executeResult))
	}
	if strings.Contains(serializedResult(t, executeResult), typedSecret) {
		t.Fatal("typed text was copied into execute output")
	}
	dryRuns, executes, _ := fake.counts()
	if dryRuns != 1 || executes != 1 {
		t.Fatalf("dry runs = %d, executes = %d", dryRuns, executes)
	}
}

func TestActRejectsUnknownModeWithoutCallingSession(t *testing.T) {
	fake := &fakeSession{}
	client := connectProtocol(t, newProtocolServer(t, fake))

	result := callTool(t, client, ToolAct, ActInput{Mode: "surprise"})
	if !result.IsError {
		t.Fatal("unknown mode unexpectedly succeeded")
	}
	output := decodeOutput[ActOutput](t, result)
	if output.Error == nil || output.Error.Code != agent.ErrorInvalidInput {
		t.Fatalf("unexpected error: %+v", output.Error)
	}
	dryRuns, executes, _ := fake.counts()
	if dryRuns != 0 || executes != 0 {
		t.Fatalf("unknown mode reached session: dry=%d execute=%d", dryRuns, executes)
	}
}

func TestProtocolSchemaRejectsUnknownInputWithoutEchoingValue(t *testing.T) {
	const privateValue = "private-value-that-must-not-be-echoed"
	fake := &fakeSession{}
	client := connectProtocol(t, newProtocolServer(t, fake))

	result := callTool(t, client, ToolAct, map[string]any{
		"request": map[string]any{
			"operation": "pointer.click",
			"click":     map[string]any{"button": "left"},
			"unknown":   privateValue,
		},
	})
	if !result.IsError {
		t.Fatal("unknown input field unexpectedly succeeded")
	}
	if strings.Contains(serializedResult(t, result), privateValue) {
		t.Fatal("invalid input value was echoed in schema error")
	}
	dryRuns, executes, _ := fake.counts()
	if dryRuns != 0 || executes != 0 {
		t.Fatalf("schema-invalid input reached session: dry=%d execute=%d", dryRuns, executes)
	}
}

func TestBackendErrorsAreSanitized(t *testing.T) {
	const privateBackendError = "private backend path and payload"
	fake := &fakeSession{observeFunc: func(context.Context, agent.ObserveRequest) (*agent.Observation, error) {
		return nil, errors.New(privateBackendError)
	}}
	client := connectProtocol(t, newProtocolServer(t, fake))

	result := callTool(t, client, ToolObserve, map[string]any{})
	if !result.IsError {
		t.Fatal("backend failure unexpectedly succeeded")
	}
	if strings.Contains(serializedResult(t, result), privateBackendError) {
		t.Fatal("raw backend error crossed MCP boundary")
	}
	output := decodeOutput[ObserveOutput](t, result)
	if output.Error == nil || output.Error.Code != agent.ErrorBackendFailure || output.Error.Message != errorMessageFailed {
		t.Fatalf("unexpected safe error: %+v", output.Error)
	}
}

func TestNilObservationFailsWithSanitizedError(t *testing.T) {
	fake := &fakeSession{}
	client := connectProtocol(t, newProtocolServer(t, fake))

	result := callTool(t, client, ToolObserve, map[string]any{})
	if !result.IsError {
		t.Fatal("nil observation unexpectedly succeeded")
	}
	output := decodeOutput[ObserveOutput](t, result)
	if output.Error == nil || output.Error.Code != agent.ErrorBackendFailure || output.Error.Message != errorMessageFailed {
		t.Fatalf("unexpected safe error: %+v", output.Error)
	}
}

func TestCanceledToolCallCancelsSessionOperation(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	fake := &fakeSession{observeFunc: func(ctx context.Context, _ agent.ObserveRequest) (*agent.Observation, error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		return nil, ctx.Err()
	}}
	client := connectProtocol(t, newProtocolServer(t, fake))

	ctx, cancel := context.WithCancel(t.Context())
	resultDone := make(chan error, 1)
	go func() {
		_, err := client.clientSession.CallTool(ctx, &mcp.CallToolParams{Name: ToolObserve, Arguments: map[string]any{}})
		resultDone <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("observe did not start")
	}
	cancel()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("cancellation did not reach session operation")
	}
	select {
	case err := <-resultDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("CallTool error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled CallTool did not return")
	}
}

func TestCloseIsIdempotentAndLaterCallsFailClosed(t *testing.T) {
	fake := &fakeSession{}
	client := connectProtocol(t, newProtocolServer(t, fake))

	for range 2 {
		result := callTool(t, client, ToolClose, map[string]any{})
		if result.IsError {
			t.Fatalf("close returned error: %s", serializedResult(t, result))
		}
		if output := decodeOutput[CloseOutput](t, result); !output.Closed {
			t.Fatalf("close output = %+v", output)
		}
	}

	for _, tool := range []string{ToolCapabilities, ToolObserve, ToolAct} {
		arguments := any(map[string]any{})
		if tool == ToolAct {
			arguments = ActInput{}
		}
		result := callTool(t, client, tool, arguments)
		if !result.IsError {
			t.Fatalf("%s unexpectedly succeeded after close", tool)
		}
		serialized := serializedResult(t, result)
		if !strings.Contains(serialized, string(agent.ErrorSessionClosed)) || !strings.Contains(serialized, errorMessageClosed) {
			t.Fatalf("%s returned unstable close error: %s", tool, serialized)
		}
	}
	_, _, closes := fake.counts()
	if closes != 1 {
		t.Fatalf("session Close calls = %d, want 1", closes)
	}
}

func TestCloseInterruptsConcurrentOperationAndRejectsNewCalls(t *testing.T) {
	started := make(chan struct{})
	closed := make(chan struct{})
	var closeOnce sync.Once
	fake := &fakeSession{
		dryRunFunc: func(context.Context, agent.ActionRequest) (agent.ActionResult, error) {
			close(started)
			<-closed
			return agent.ActionResult{}, agent.ErrSessionClosed
		},
		closeFunc: func() error {
			closeOnce.Do(func() { close(closed) })
			return nil
		},
	}
	client := connectProtocol(t, newProtocolServer(t, fake))

	actionDone := make(chan *mcp.CallToolResult, 1)
	go func() {
		result, _ := client.clientSession.CallTool(t.Context(), &mcp.CallToolParams{
			Name: ToolAct, Arguments: ActInput{Request: agent.ActionRequest{Operation: agent.OperationClick}},
		})
		actionDone <- result
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("action did not start")
	}

	closeResult := callTool(t, client, ToolClose, map[string]any{})
	if closeResult.IsError {
		t.Fatalf("close failed: %s", serializedResult(t, closeResult))
	}
	select {
	case result := <-actionDone:
		if result == nil || !result.IsError {
			t.Fatalf("concurrent action result = %+v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("close did not release concurrent action")
	}

	result := callTool(t, client, ToolCapabilities, map[string]any{})
	if !result.IsError {
		t.Fatal("new call started after close")
	}
}

func TestRunClosesSessionOnCancellation(t *testing.T) {
	fake := &fakeSession{}
	server := newProtocolServer(t, fake)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(t.Context())
	runDone := make(chan error, 1)
	go func() { runDone <- server.Run(ctx, serverTransport) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "robotgo-test", Version: "1"}, nil)
	clientSession, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })
	cancel()
	select {
	case err := <-runDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not stop after cancellation")
	}
	_, _, closes := fake.counts()
	if closes != 1 {
		t.Fatalf("session Close calls = %d, want 1", closes)
	}
}

func TestRunClosesSessionWhenTransportIsNil(t *testing.T) {
	fake := &fakeSession{}
	server := newProtocolServer(t, fake)
	if err := server.Run(t.Context(), nil); err == nil {
		t.Fatal("Run with nil transport unexpectedly succeeded")
	}
	_, _, closes := fake.counts()
	if closes != 1 {
		t.Fatalf("session Close calls = %d, want 1", closes)
	}
}

func TestRunClosesSessionWhenContextIsNilAndCannotRunTwice(t *testing.T) {
	fake := &fakeSession{}
	server := newProtocolServer(t, fake)
	serverTransport, _ := mcp.NewInMemoryTransports()
	if err := server.Run(nil, serverTransport); err == nil { //nolint:staticcheck // Verify that the exported boundary fails safely.
		t.Fatal("Run with nil context unexpectedly succeeded")
	}
	_, _, closes := fake.counts()
	if closes != 1 {
		t.Fatalf("session Close calls = %d, want 1", closes)
	}
	if err := server.Run(t.Context(), serverTransport); err == nil || !strings.Contains(err.Error(), "already run") {
		t.Fatalf("second Run error = %v", err)
	}
	_, _, closes = fake.counts()
	if closes != 1 {
		t.Fatalf("second Run changed Close calls to %d", closes)
	}
}

func TestNewRejectsNilSession(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Fatal("New(nil) unexpectedly succeeded")
	}
	var typedNil *fakeSession
	if _, err := New(typedNil); err == nil {
		t.Fatal("New(typed nil) unexpectedly succeeded")
	}
}
