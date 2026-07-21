// Package mcpserver exposes an agent Session through a small, local-only MCP
// tool surface. Policy, validation, execution, and sensitive capture ownership
// remain in package agent.
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	robotgo "github.com/marang/robotgo"
	"github.com/marang/robotgo/agent"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	// ToolCapabilities reports the immutable operation catalog.
	ToolCapabilities = "robotgo_capabilities"
	// ToolObserve performs one policy-gated diagnostics or capture observation.
	ToolObserve = "robotgo_observe"
	// ToolAct plans or executes one typed action.
	ToolAct = "robotgo_act"
	// ToolClose closes the underlying RobotGo agent session.
	ToolClose = "robotgo_close"

	serverName = "robotgo"

	errorMessageClosed = "RobotGo agent session is closed"
	errorMessageFailed = "RobotGo agent operation failed"
)

// Session is the protocol-independent behavior consumed by the adapter.
// *agent.Session implements Session.
type Session interface {
	Catalog() agent.OperationCatalog
	Observe(context.Context, agent.ObserveRequest) (*agent.Observation, error)
	DryRun(context.Context, agent.ActionRequest) (agent.ActionResult, error)
	Execute(context.Context, agent.ActionRequest) (agent.ActionResult, error)
	Close() error
}

// Server binds one process-exclusive agent session to one MCP connection.
type Server struct {
	adapter    *adapter
	protocol   *mcp.Server
	runStarted atomic.Bool
}

// New constructs a server without opening a transport or touching the desktop.
func New(session Session) (*Server, error) {
	if nilSession(session) {
		return nil, fmt.Errorf("mcpserver: nil agent session")
	}
	a := &adapter{session: session, closeDone: make(chan struct{})}
	s := &Server{
		adapter: a,
		protocol: mcp.NewServer(&mcp.Implementation{
			Name:    serverName,
			Version: robotgo.Version,
		}, nil),
	}
	s.registerTools()
	return s, nil
}

// Run serves one persistent transport until the peer disconnects or ctx is
// canceled. The agent session is always closed before Run returns.
func (s *Server) Run(ctx context.Context, transport mcp.Transport) (runErr error) {
	if s == nil || s.protocol == nil || s.adapter == nil {
		return fmt.Errorf("mcpserver: uninitialized server")
	}
	if !s.runStarted.CompareAndSwap(false, true) {
		return fmt.Errorf("mcpserver: server already run")
	}
	defer func() {
		runErr = errors.Join(runErr, s.Close())
	}()
	if ctx == nil {
		return fmt.Errorf("mcpserver: nil context")
	}
	if transport == nil {
		return fmt.Errorf("mcpserver: nil transport")
	}
	return s.protocol.Run(ctx, transport)
}

// Close closes the underlying agent session. It is safe to call repeatedly
// and concurrently. Calls that start after close receive a stable error.
func (s *Server) Close() error {
	if s == nil || s.adapter == nil {
		return nil
	}
	return s.adapter.close()
}

type adapter struct {
	session Session

	mu        sync.Mutex
	closed    bool
	closeOne  sync.Once
	closeErr  error
	closeDone chan struct{}
}

func (a *adapter) begin() (Session, *ToolError) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return nil, closedToolError()
	}
	return a.session, nil
}

func (a *adapter) close() error {
	if a == nil {
		return nil
	}
	a.closeOne.Do(func() {
		a.mu.Lock()
		a.closed = true
		a.mu.Unlock()

		a.closeErr = a.session.Close()
		close(a.closeDone)
	})
	<-a.closeDone
	return a.closeErr
}

func nilSession(session Session) bool {
	if session == nil {
		return true
	}
	value := reflect.ValueOf(session)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// ToolError is a stable, payload-free error returned in structured tool
// output. Unclassified backend error strings are never forwarded.
type ToolError struct {
	Code    agent.ErrorCode `json:"code"`
	Message string          `json:"message"`
}

type emptyInput struct{}

// CapabilitiesOutput is the structured output of robotgo_capabilities.
type CapabilitiesOutput struct {
	Catalog agent.OperationCatalog `json:"catalog"`
	Error   *ToolError             `json:"error,omitempty"`
}

// CaptureOutput reports only geometry. The agent observation's pixels and
// lineage digest deliberately remain private to the in-process session.
type CaptureOutput struct {
	Region agent.CaptureRegion `json:"region"`
	Width  int                 `json:"width"`
	Height int                 `json:"height"`
}

// ObservationOutput is the privacy-reduced MCP projection of an observation.
type ObservationOutput struct {
	SchemaVersion string                   `json:"schema_version"`
	ObservationID string                   `json:"observation_id"`
	CreatedAt     time.Time                `json:"created_at"`
	Diagnostics   agent.RuntimeDiagnostics `json:"diagnostics"`
	Capture       *CaptureOutput           `json:"capture,omitempty"`
}

// ObserveOutput is the structured output of robotgo_observe.
type ObserveOutput struct {
	Observation *ObservationOutput `json:"observation,omitempty"`
	Error       *ToolError         `json:"error,omitempty"`
}

// ActMode controls whether robotgo_act only plans or actually executes.
type ActMode string

const (
	// ActModeDryRun performs full preflight without injecting input.
	ActModeDryRun ActMode = "dry-run"
	// ActModeExecute permits execution, still subject to session policy and
	// per-action confirmation.
	ActModeExecute ActMode = "execute"
)

// ActInput is the strict input of robotgo_act. An omitted mode is dry-run.
type ActInput struct {
	Mode    ActMode             `json:"mode,omitempty"`
	Request agent.ActionRequest `json:"request"`
}

// ActOutput is the structured output of robotgo_act.
type ActOutput struct {
	Result *agent.ActionResult `json:"result,omitempty"`
	Error  *ToolError          `json:"error,omitempty"`
}

// CloseOutput is the structured output of robotgo_close.
type CloseOutput struct {
	Closed bool       `json:"closed"`
	Error  *ToolError `json:"error,omitempty"`
}

func (s *Server) registerTools() {
	closedWorld := false
	openWorld := true
	destructive := true
	nondestructive := false

	mcp.AddTool(s.protocol, &mcp.Tool{
		Name:        ToolCapabilities,
		Title:       "RobotGo capabilities",
		Description: "Report the immutable, policy-filtered RobotGo operation catalog without touching the desktop.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &closedWorld},
	}, s.capabilities)

	mcp.AddTool(s.protocol, &mcp.Tool{
		Name:        ToolObserve,
		Title:       "Observe desktop state",
		Description: "Return sanitized runtime diagnostics and optional bounded capture metadata. Pixels and capture digests never cross MCP.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &openWorld},
	}, s.observe)

	mcp.AddTool(s.protocol, &mcp.Tool{
		Name:        ToolAct,
		Title:       "Plan or execute a RobotGo action",
		Description: "Dry-run a typed action by default. Execution requires mode=execute and remains subject to policy and confirmation.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &destructive, OpenWorldHint: &openWorld},
	}, s.act)

	mcp.AddTool(s.protocol, &mcp.Tool{
		Name:        ToolClose,
		Title:       "Close RobotGo agent session",
		Description: "Idempotently close the process-exclusive RobotGo agent session and zero retained capture buffers.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &nondestructive, IdempotentHint: true, OpenWorldHint: &closedWorld},
	}, s.closeTool)
}

func (s *Server) capabilities(_ context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, CapabilitiesOutput, error) {
	session, toolErr := s.adapter.begin()
	if toolErr != nil {
		return errorResult(), CapabilitiesOutput{Error: toolErr}, nil
	}
	return nil, CapabilitiesOutput{Catalog: session.Catalog()}, nil
}

func (s *Server) observe(ctx context.Context, _ *mcp.CallToolRequest, input agent.ObserveRequest) (*mcp.CallToolResult, ObserveOutput, error) {
	session, toolErr := s.adapter.begin()
	if toolErr != nil {
		return errorResult(), ObserveOutput{Error: toolErr}, nil
	}
	observation, err := session.Observe(ctx, input)
	if err != nil {
		return errorResult(), ObserveOutput{Error: safeToolError(err)}, nil
	}
	if observation == nil {
		return errorResult(), ObserveOutput{Error: safeToolError(errors.New("nil observation"))}, nil
	}
	return nil, ObserveOutput{Observation: projectObservation(observation)}, nil
}

func (s *Server) act(ctx context.Context, _ *mcp.CallToolRequest, input ActInput) (*mcp.CallToolResult, ActOutput, error) {
	session, toolErr := s.adapter.begin()
	if toolErr != nil {
		return errorResult(), ActOutput{Error: toolErr}, nil
	}
	mode := input.Mode
	if mode == "" {
		mode = ActModeDryRun
	}
	var (
		result agent.ActionResult
		err    error
	)
	switch mode {
	case ActModeDryRun:
		result, err = session.DryRun(ctx, input.Request)
	case ActModeExecute:
		result, err = session.Execute(ctx, input.Request)
	default:
		return errorResult(), ActOutput{Error: &ToolError{
			Code:    agent.ErrorInvalidInput,
			Message: "mode must be dry-run or execute",
		}}, nil
	}
	output := ActOutput{Result: &result}
	if err != nil {
		output.Error = safeToolError(err)
		return errorResult(), output, nil
	}
	return nil, output, nil
}

func (s *Server) closeTool(_ context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, CloseOutput, error) {
	if err := s.Close(); err != nil {
		return errorResult(), CloseOutput{Error: safeToolError(err)}, nil
	}
	return nil, CloseOutput{Closed: true}, nil
}

func projectObservation(observation *agent.Observation) *ObservationOutput {
	if observation == nil {
		return nil
	}
	output := &ObservationOutput{
		SchemaVersion: observation.SchemaVersion,
		ObservationID: observation.ObservationID,
		CreatedAt:     observation.CreatedAt,
		Diagnostics:   observation.Diagnostics,
	}
	if observation.Capture != nil {
		output.Capture = &CaptureOutput{
			Region: observation.Capture.Region,
			Width:  observation.Capture.Width,
			Height: observation.Capture.Height,
		}
	}
	return output
}

func errorResult() *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true}
}

func closedToolError() *ToolError {
	return &ToolError{Code: agent.ErrorSessionClosed, Message: errorMessageClosed}
}

func safeToolError(err error) *ToolError {
	if err == nil {
		return nil
	}
	var actionErr *agent.ActionError
	if errors.As(err, &actionErr) {
		return &ToolError{Code: actionErr.Code, Message: actionErr.Message}
	}
	switch {
	case errors.Is(err, context.Canceled):
		return &ToolError{Code: agent.ErrorCanceled, Message: "RobotGo agent operation was canceled"}
	case errors.Is(err, context.DeadlineExceeded):
		return &ToolError{Code: agent.ErrorTimedOut, Message: "RobotGo agent operation timed out"}
	case errors.Is(err, agent.ErrSessionClosed):
		return closedToolError()
	default:
		return &ToolError{Code: agent.ErrorBackendFailure, Message: errorMessageFailed}
	}
}
