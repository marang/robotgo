package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
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
	findFunc    func(context.Context, agent.FindColorRequest) (agent.FindColorResult, error)
	waitFunc    func(context.Context, agent.WaitColorRequest) (agent.WaitColorResult, error)
	releaseFunc func(string) error
	inspectFunc func(context.Context, agent.InspectUIRequest) (agent.UIObservation, error)
	viewFunc    func(context.Context, agent.ViewRequest) (*agent.View, error)
	dryRunFunc  func(context.Context, agent.ActionRequest) (agent.ActionResult, error)
	executeFunc func(context.Context, agent.ActionRequest) (agent.ActionResult, error)
	closeFunc   func() error

	dryRuns  int
	executes int
	finds    int
	waits    int
	releases int
	inspects int
	views    int
	closes   int
}

// imageOnlySession verifies that observation cleanup is not coupled to the
// optional visual-condition or accessibility extensions.
type imageOnlySession struct {
	viewFunc    func(context.Context, agent.ViewRequest) (*agent.View, error)
	releasedIDs []string
}

func (*imageOnlySession) Catalog() agent.OperationCatalog { return agent.OperationCatalog{} }
func (*imageOnlySession) Observe(context.Context, agent.ObserveRequest) (*agent.Observation, error) {
	return nil, errors.New("unused")
}
func (*imageOnlySession) DryRun(context.Context, agent.ActionRequest) (agent.ActionResult, error) {
	return agent.ActionResult{}, errors.New("unused")
}
func (*imageOnlySession) Execute(context.Context, agent.ActionRequest) (agent.ActionResult, error) {
	return agent.ActionResult{}, errors.New("unused")
}
func (*imageOnlySession) Close() error { return nil }
func (session *imageOnlySession) View(ctx context.Context, request agent.ViewRequest) (*agent.View, error) {
	return session.viewFunc(ctx, request)
}
func (session *imageOnlySession) ReleaseObservation(id string) error {
	session.releasedIDs = append(session.releasedIDs, id)
	return nil
}

func (f *fakeSession) Catalog() agent.OperationCatalog { return f.catalog }

func (f *fakeSession) Observe(ctx context.Context, request agent.ObserveRequest) (*agent.Observation, error) {
	if f.observeFunc != nil {
		return f.observeFunc(ctx, request)
	}
	return f.observation, nil
}

func (f *fakeSession) FindColor(ctx context.Context, request agent.FindColorRequest) (agent.FindColorResult, error) {
	f.mu.Lock()
	f.finds++
	f.mu.Unlock()
	if f.findFunc != nil {
		return f.findFunc(ctx, request)
	}
	return agent.FindColorResult{}, errors.New("unused")
}

func (f *fakeSession) WaitColor(ctx context.Context, request agent.WaitColorRequest) (agent.WaitColorResult, error) {
	f.mu.Lock()
	f.waits++
	f.mu.Unlock()
	if f.waitFunc != nil {
		return f.waitFunc(ctx, request)
	}
	return agent.WaitColorResult{}, errors.New("unused")
}

func (f *fakeSession) ReleaseObservation(id string) error {
	f.mu.Lock()
	f.releases++
	f.mu.Unlock()
	if f.releaseFunc != nil {
		return f.releaseFunc(id)
	}
	return nil
}

func (f *fakeSession) InspectUI(ctx context.Context, request agent.InspectUIRequest) (agent.UIObservation, error) {
	f.mu.Lock()
	f.inspects++
	f.mu.Unlock()
	if f.inspectFunc != nil {
		return f.inspectFunc(ctx, request)
	}
	return agent.UIObservation{}, errors.New("unused")
}

func (f *fakeSession) View(ctx context.Context, request agent.ViewRequest) (*agent.View, error) {
	f.mu.Lock()
	f.views++
	f.mu.Unlock()
	if f.viewFunc != nil {
		return f.viewFunc(ctx, request)
	}
	return nil, errors.New("unused")
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

func (f *fakeSession) conditionCounts() (finds, waits, releases int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.finds, f.waits, f.releases
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

func newImageProtocolServer(t *testing.T, session Session) *Server {
	t.Helper()
	server, err := NewWithOptions(session, Options{AllowImageContent: true})
	if err != nil {
		t.Fatalf("NewWithOptions: %v", err)
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

func TestProtocolInitializesAndListsFocusedTools(t *testing.T) {
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
		if tool.Name == ToolAct {
			schema, marshalErr := json.Marshal(tool.InputSchema)
			if marshalErr != nil {
				t.Fatalf("marshal act schema: %v", marshalErr)
			}
			for _, field := range []string{
				"scroll", "drag", "key_chord", "activate",
				"target_x", "duration_ms", "modifiers", "target_pid", "kind",
			} {
				if !strings.Contains(string(schema), `"`+field+`"`) {
					t.Errorf("robotgo_act schema omitted %q: %s", field, schema)
				}
			}
		}
		switch tool.Name {
		case ToolFind, ToolWait, ToolInspectUI, ToolView:
			if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
				t.Errorf("tool %q is not marked read-only", tool.Name)
			}
		case ToolReleaseObservation:
			if tool.Annotations == nil || !tool.Annotations.IdempotentHint ||
				tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint {
				t.Errorf("release annotations = %+v", tool.Annotations)
			}
		}
	}
	slices.Sort(names)
	want := []string{
		ToolAct, ToolCapabilities, ToolClose, ToolFind, ToolInspectUI, ToolObserve,
		ToolReleaseObservation, ToolWait,
	}
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Fatalf("tools = %v, want %v", names, want)
	}
}

func TestImageViewToolRequiresAdapterStartupGrant(t *testing.T) {
	fake := &fakeSession{}
	for name, server := range map[string]*Server{
		"default": newProtocolServer(t, fake),
		"enabled": newImageProtocolServer(t, fake),
	} {
		t.Run(name, func(t *testing.T) {
			client := connectProtocol(t, server)
			result, err := client.clientSession.ListTools(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, tool := range result.Tools {
				if tool.Name == ToolView {
					found = true
					if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
						t.Fatalf("view annotations = %+v", tool.Annotations)
					}
				}
			}
			if found != (name == "enabled") {
				t.Fatalf("view tool visible = %v", found)
			}
		})
	}
}

func TestImageOnlySessionExposesViewAndIndependentReleaseTool(t *testing.T) {
	metadata := agent.ViewMetadata{
		SchemaVersion: agent.ViewSchemaVersion, ObservationID: "observation-93",
		CreatedAt: time.Unix(499, 0).UTC(),
		Region:    agent.CaptureRegion{Width: 1, Height: 1, DisplayID: 0},
		Width:     1, Height: 1, Backend: "fixture-capture", MIMEType: agent.ViewMIMEType,
	}
	session := &imageOnlySession{viewFunc: func(context.Context, agent.ViewRequest) (*agent.View, error) {
		return agent.NewImageView(metadata, testPNG(t, 1, 1))
	}}
	client := connectProtocol(t, newImageProtocolServer(t, session))
	listed, err := client.clientSession.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	want := []string{ToolAct, ToolCapabilities, ToolClose, ToolObserve, ToolReleaseObservation, ToolView}
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Fatalf("image-only tools = %v, want %v", names, want)
	}
	viewResult := callTool(t, client, ToolView, agent.ViewRequest{Region: &metadata.Region})
	if viewResult.IsError {
		t.Fatalf("image-only view failed: %s", serializedResult(t, viewResult))
	}
	releaseResult := callTool(t, client, ToolReleaseObservation, ReleaseObservationInput{
		ObservationID: metadata.ObservationID,
	})
	if releaseResult.IsError || !slices.Equal(session.releasedIDs, []string{metadata.ObservationID}) {
		t.Fatalf("image-only release = %s, calls=%v", serializedResult(t, releaseResult), session.releasedIDs)
	}
}

func TestOfficialMCPClientReceivesImageContentWithoutStructuredDuplication(t *testing.T) {
	pngData := testPNG(t, 2, 1)
	expectedPNG := append([]byte(nil), pngData...)
	serverOwned := pngData
	metadata := agent.ViewMetadata{
		SchemaVersion: agent.ViewSchemaVersion, ObservationID: "observation-91",
		CreatedAt: time.Unix(500, 0).UTC(),
		Region:    agent.CaptureRegion{X: 10, Y: 20, Width: 2, Height: 1, DisplayID: 0},
		Width:     2, Height: 1, Backend: "fixture-capture", MIMEType: agent.ViewMIMEType,
		CaptureDurationMillis: 3,
	}
	fake := &fakeSession{viewFunc: func(_ context.Context, request agent.ViewRequest) (*agent.View, error) {
		if request.Region == nil || request.Region.X != 10 || !request.Confirmed {
			return nil, errors.New("unexpected view request")
		}
		return agent.NewImageView(metadata, serverOwned)
	}}
	client := connectProtocol(t, newImageProtocolServer(t, fake))
	result := callTool(t, client, ToolView, agent.ViewRequest{
		Region: &metadata.Region, Confirmed: true,
	})
	if result.IsError || len(result.Content) != 1 {
		t.Fatalf("view result = %s", serializedResult(t, result))
	}
	imageContent, ok := result.Content[0].(*mcp.ImageContent)
	if !ok || imageContent.MIMEType != agent.ViewMIMEType || !bytes.Equal(imageContent.Data, expectedPNG) {
		t.Fatalf("image content = %#v", result.Content[0])
	}
	output := decodeOutput[ViewOutput](t, result)
	if output.View == nil || output.View.ObservationID != metadata.ObservationID || output.Error != nil {
		t.Fatalf("view output = %+v", output)
	}
	structured, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(structured, pngData) || strings.Contains(string(structured), `"data"`) {
		t.Fatalf("structured output duplicated image bytes: %s", structured)
	}
	deadline := time.Now().Add(time.Second)
	for !allZero(serverOwned) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !allZero(serverOwned) {
		t.Fatal("server-owned encoded image was not zeroed after MCP serialization")
	}
	if _, _, releases := fake.conditionCounts(); releases != 0 {
		t.Fatalf("successful view unexpectedly released lineage: %d", releases)
	}
	release := callTool(t, client, ToolReleaseObservation, ReleaseObservationInput{ObservationID: metadata.ObservationID})
	if release.IsError {
		t.Fatalf("release view lineage = %s", serializedResult(t, release))
	}
	if _, _, releases := fake.conditionCounts(); releases != 1 {
		t.Fatalf("view lineage release calls = %d", releases)
	}
}

func TestImageViewFailureZeroesPayloadAndReleasesCompletedLineage(t *testing.T) {
	data := testPNG(t, 1, 1)
	metadata := agent.ViewMetadata{
		SchemaVersion: agent.ViewSchemaVersion, ObservationID: "observation-92",
		CreatedAt: time.Unix(501, 0).UTC(),
		Region:    agent.CaptureRegion{Width: 1, Height: 1, DisplayID: 0},
		Width:     1, Height: 1, Backend: "fixture-capture", MIMEType: agent.ViewMIMEType,
	}
	fake := &fakeSession{viewFunc: func(context.Context, agent.ViewRequest) (*agent.View, error) {
		view, err := agent.NewImageView(metadata, data)
		if err != nil {
			return nil, err
		}
		return view, &agent.ActionError{
			Code: agent.ErrorAuditDelivery, Operation: agent.OperationView,
			Message: "image view completed but audit delivery failed",
		}
	}}
	client := connectProtocol(t, newImageProtocolServer(t, fake))
	result := callTool(t, client, ToolView, agent.ViewRequest{
		Region: &metadata.Region,
	})
	output := decodeOutput[ViewOutput](t, result)
	if !result.IsError || output.View != nil || output.Error == nil ||
		output.Error.Code != agent.ErrorAuditDelivery {
		t.Fatalf("failed view output = %+v", output)
	}
	if !allZero(data) {
		t.Fatal("failed view retained encoded image bytes")
	}
	if _, _, releases := fake.conditionCounts(); releases != 1 {
		t.Fatalf("failed completed view releases = %d", releases)
	}
}

func TestServerCloseClearsImageContentBeforeSerialization(t *testing.T) {
	server := newImageProtocolServer(t, &fakeSession{})
	data := testPNG(t, 1, 1)
	content := server.newClearingImageContent(data, agent.ViewMIMEType)
	if content == nil || allZero(data) {
		t.Fatal("pending image content was not retained for serialization")
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	if !allZero(data) {
		t.Fatal("server close retained image content that was never serialized")
	}
	lateData := testPNG(t, 1, 1)
	server.newClearingImageContent(lateData, agent.ViewMIMEType)
	if !allZero(lateData) {
		t.Fatal("closed server accepted new pending image content")
	}
}

func TestServerClosePreventsImageTransferAfterPendingRegistration(t *testing.T) {
	server := newImageProtocolServer(t, &fakeSession{})
	content := server.newClearingImageContent(nil, agent.ViewMIMEType)
	data := testPNG(t, 1, 1)
	view, err := agent.NewImageView(agent.ViewMetadata{
		SchemaVersion: agent.ViewSchemaVersion, ObservationID: "observation-93",
		CreatedAt: time.Unix(502, 0).UTC(),
		Region:    agent.CaptureRegion{Width: 1, Height: 1, DisplayID: 0},
		Width:     1, Height: 1, Backend: "fixture-capture", MIMEType: agent.ViewMIMEType,
	}, data)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	if err := content.take(view); !errors.Is(err, agent.ErrObservationClosed) {
		t.Fatalf("content transfer after close error = %v", err)
	}
	if err := view.Close(); err != nil {
		t.Fatal(err)
	}
	if !allZero(data) {
		t.Fatal("server close race retained image bytes in the untransferred view")
	}
}

func TestServerCloseWaitsForAtomicImageTransfer(t *testing.T) {
	server := newImageProtocolServer(t, &fakeSession{})
	content := server.newClearingImageContent(nil, agent.ViewMIMEType)
	data := testPNG(t, 1, 1)
	view, err := agent.NewImageView(agent.ViewMetadata{
		SchemaVersion: agent.ViewSchemaVersion, ObservationID: "observation-94",
		CreatedAt: time.Unix(503, 0).UTC(),
		Region:    agent.CaptureRegion{Width: 1, Height: 1, DisplayID: 0},
		Width:     1, Height: 1, Backend: "fixture-capture", MIMEType: agent.ViewMIMEType,
	}, data)
	if err != nil {
		t.Fatal(err)
	}
	transferred := make(chan struct{})
	resume := make(chan struct{})
	takeDone := make(chan error, 1)
	go func() {
		takeDone <- content.takeWith(func() ([]byte, error) {
			encoded, takeErr := view.TakePNG()
			close(transferred)
			<-resume
			return encoded, takeErr
		})
	}()
	<-transferred

	closeDone := make(chan error, 1)
	go func() { closeDone <- server.Close() }()
	select {
	case err := <-closeDone:
		t.Fatalf("server close returned before image handoff completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(resume)
	if err := <-takeDone; err != nil {
		t.Fatal(err)
	}
	if err := <-closeDone; err != nil {
		t.Fatal(err)
	}
	if !allZero(data) {
		t.Fatal("server close retained bytes after waiting for atomic image handoff")
	}
}

func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(20 + x), G: uint8(40 + y), B: 60, A: 255})
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatal(err)
	}
	clear(img.Pix)
	return buffer.Bytes()
}

func allZero(data []byte) bool {
	for _, value := range data {
		if value != 0 {
			return false
		}
	}
	return true
}

func TestInspectUIReturnsOnlyPrivacyReducedSemanticContract(t *testing.T) {
	fake := &fakeSession{inspectFunc: func(_ context.Context, request agent.InspectUIRequest) (agent.UIObservation, error) {
		if request.Target != 42 || request.Kind != agent.WindowTargetProcess {
			return agent.UIObservation{}, errors.New("unexpected target")
		}
		return agent.UIObservation{
			SchemaVersion: agent.UISchemaVersion,
			ObservationID: "observation-9",
			CreatedAt:     time.Unix(200, 0).UTC(),
			Backend:       "fake-accessibility",
			Elements: []agent.UIElement{{
				ElementID: "observation-9-element-1", Role: agent.UIRoleButton,
				Name: "Save", Actions: []agent.UIAction{agent.UIActionPress},
			}},
		}, nil
	}}
	client := connectProtocol(t, newProtocolServer(t, fake))
	result := callTool(t, client, ToolInspectUI, agent.InspectUIRequest{
		Target: 42, Kind: agent.WindowTargetProcess,
	})
	if result.IsError {
		t.Fatalf("inspect UI returned tool error: %s", serializedResult(t, result))
	}
	output := decodeOutput[InspectUIOutput](t, result)
	if output.Observation == nil || len(output.Observation.Elements) != 1 ||
		output.Observation.Elements[0].Name != "Save" {
		t.Fatalf("inspect UI output = %+v", output)
	}
	serialized := serializedResult(t, result)
	for _, forbidden := range []string{"native_handle", "object_path", "password_value", "raw_error"} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("inspect UI leaked %q: %s", forbidden, serialized)
		}
	}
}

func TestInspectUIErrorReleasesCompletedObservationBeforeOmittingIt(t *testing.T) {
	fake := &fakeSession{inspectFunc: func(context.Context, agent.InspectUIRequest) (agent.UIObservation, error) {
		return agent.UIObservation{ObservationID: "observation-9"}, &agent.ActionError{
			Code: agent.ErrorAuditDelivery, Operation: agent.OperationInspectUI,
			Message: "UI inspection completed but audit delivery failed",
		}
	}}
	client := connectProtocol(t, newProtocolServer(t, fake))
	result := callTool(t, client, ToolInspectUI, agent.InspectUIRequest{
		Target: 42, Kind: agent.WindowTargetProcess,
	})
	if !result.IsError {
		t.Fatal("failed UI inspection returned success")
	}
	output := decodeOutput[InspectUIOutput](t, result)
	if output.Observation != nil || output.Error == nil || output.Error.Code != agent.ErrorAuditDelivery {
		t.Fatalf("failed inspection output = %+v", output)
	}
	_, _, releases := fake.conditionCounts()
	if releases != 1 {
		t.Fatalf("completed failed observation releases = %d", releases)
	}
}

func TestInspectUIReleaseFailureReportsCleanupFailure(t *testing.T) {
	fake := &fakeSession{
		inspectFunc: func(context.Context, agent.InspectUIRequest) (agent.UIObservation, error) {
			return agent.UIObservation{ObservationID: "observation-9"}, errors.New("private inspect failure")
		},
		releaseFunc: func(string) error { return errors.New("private release failure") },
	}
	client := connectProtocol(t, newProtocolServer(t, fake))
	result := callTool(t, client, ToolInspectUI, agent.InspectUIRequest{
		Target: 42, Kind: agent.WindowTargetProcess,
	})
	output := decodeOutput[InspectUIOutput](t, result)
	if !result.IsError || output.Observation != nil || output.Error == nil ||
		output.Error.Code != agent.ErrorCleanupFailed {
		t.Fatalf("failed inspection cleanup output = %+v", output)
	}
	serialized := serializedResult(t, result)
	if strings.Contains(serialized, "private inspect") || strings.Contains(serialized, "private release") {
		t.Fatalf("failed inspection cleanup leaked details: %s", serialized)
	}
}

func TestActProjectsExtendedTypedActionWithoutChangingDryRunDefault(t *testing.T) {
	var received agent.ActionRequest
	fake := &fakeSession{
		dryRunFunc: func(_ context.Context, request agent.ActionRequest) (agent.ActionResult, error) {
			received = request
			return agent.ActionResult{
				ActionID: "planned-drag", Operation: request.Operation,
				Status: agent.ActionPlanned,
			}, nil
		},
	}
	client := connectProtocol(t, newProtocolServer(t, fake))
	request := agent.ActionRequest{
		Operation: agent.OperationDrag,
		Confirmed: true,
		Drag: &agent.DragAction{
			StartX: 1, StartY: 2, EndX: 3, EndY: 4,
			DisplayID: 0, Button: agent.MouseButtonLeft, DurationMillis: 50,
		},
	}
	result := callTool(t, client, ToolAct, ActInput{Request: request})
	if result.IsError {
		t.Fatalf("extended dry-run returned error: %s", serializedResult(t, result))
	}
	output := decodeOutput[ActOutput](t, result)
	if output.Result == nil || output.Result.Status != agent.ActionPlanned {
		t.Fatalf("extended dry-run output = %+v", output)
	}
	if received.Drag == nil || *received.Drag != *request.Drag {
		t.Fatalf("projected request = %+v", received)
	}
	dryRuns, executes, _ := fake.counts()
	if dryRuns != 1 || executes != 0 {
		t.Fatalf("dry runs = %d, executes = %d", dryRuns, executes)
	}
}

func TestCloseProjectsInputCleanupFailureWithoutBackendDetails(t *testing.T) {
	const privateDetail = "private-release-backend-detail"
	fake := &fakeSession{closeFunc: func() error {
		return errors.Join(agent.ErrInputCleanup, errors.New(privateDetail))
	}}
	client := connectProtocol(t, newProtocolServer(t, fake))
	result := callTool(t, client, ToolClose, map[string]any{})
	if !result.IsError {
		t.Fatal("cleanup failure unexpectedly succeeded")
	}
	output := decodeOutput[CloseOutput](t, result)
	if output.Error == nil || output.Error.Code != agent.ErrorCleanupFailed {
		t.Fatalf("cleanup output = %+v", output)
	}
	if strings.Contains(serializedResult(t, result), privateDetail) {
		t.Fatal("cleanup output leaked backend detail")
	}
}

func TestCloseCanRetryInputCleanupBeforeReleasingAdapter(t *testing.T) {
	attempts := 0
	fake := &fakeSession{closeFunc: func() error {
		attempts++
		if attempts == 1 {
			return agent.ErrInputCleanup
		}
		return nil
	}}
	client := connectProtocol(t, newProtocolServer(t, fake))
	first := callTool(t, client, ToolClose, map[string]any{})
	if !first.IsError {
		t.Fatal("first cleanup attempt unexpectedly succeeded")
	}
	second := callTool(t, client, ToolClose, map[string]any{})
	if second.IsError {
		t.Fatalf("cleanup retry failed: %s", serializedResult(t, second))
	}
	if output := decodeOutput[CloseOutput](t, second); !output.Closed {
		t.Fatalf("cleanup retry output = %+v", output)
	}
	if attempts != 2 {
		t.Fatalf("close attempts = %d", attempts)
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

func TestSafeToolErrorPrioritizesInputCleanupOverCancellation(t *testing.T) {
	toolErr := safeToolError(errors.Join(
		&agent.ActionError{
			Code: agent.ErrorCanceled, Message: "canceled action",
		},
		agent.ErrInputCleanup,
	))
	if toolErr == nil || toolErr.Code != agent.ErrorCleanupFailed {
		t.Fatalf("joined cleanup error = %+v", toolErr)
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

	for _, tool := range []string{ToolCapabilities, ToolObserve, ToolInspectUI, ToolFind, ToolWait, ToolReleaseObservation, ToolAct} {
		arguments := any(map[string]any{})
		switch tool {
		case ToolInspectUI:
			arguments = agent.InspectUIRequest{Target: 1, Kind: agent.WindowTargetProcess}
		case ToolFind:
			arguments = agent.FindColorRequest{ObservationID: "observation-1"}
		case ToolWait:
			arguments = agent.WaitColorRequest{Region: agent.CaptureRegion{Width: 1, Height: 1}}
		case ToolReleaseObservation:
			arguments = ReleaseObservationInput{ObservationID: "observation-1"}
		case ToolAct:
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
