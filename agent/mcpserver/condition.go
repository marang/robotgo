package mcpserver

import (
	"context"

	"github.com/marang/robotgo/agent"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var _ VisualConditionSession = (*agent.Session)(nil)

const (
	// ToolFind evaluates a visual condition against one explicit observation.
	ToolFind = "robotgo_find"
	// ToolWait performs one policy-bounded visual wait over an explicit region.
	ToolWait = "robotgo_wait"
	// ToolReleaseObservation zeroes and removes one retained observation.
	ToolReleaseObservation = "robotgo_release_observation"
)

// FindOutput is the privacy-reduced output of robotgo_find.
type FindOutput struct {
	Result *agent.FindColorResult `json:"result,omitempty"`
	Error  *ToolError             `json:"error,omitempty"`
}

// WaitOutput is the privacy-reduced output of robotgo_wait.
type WaitOutput struct {
	Result *agent.WaitColorResult `json:"result,omitempty"`
	Error  *ToolError             `json:"error,omitempty"`
}

// ReleaseObservationInput identifies session-owned capture state to zero.
type ReleaseObservationInput struct {
	ObservationID string `json:"observation_id"`
}

// ReleaseObservationOutput confirms that no retained observation with the
// requested ID remains. Releasing the same valid ID repeatedly is successful.
type ReleaseObservationOutput struct {
	Released bool       `json:"released"`
	Error    *ToolError `json:"error,omitempty"`
}

func (s *Server) registerConditionTools() {
	closedWorld := false
	openWorld := true
	nondestructive := false

	mcp.AddTool(s.protocol, &mcp.Tool{
		Name:        ToolFind,
		Title:       "Find a color in an observation",
		Description: "Evaluate one typed color condition against an explicit live RobotGo observation. This never captures the desktop and returns no pixels, digest, or target color.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &closedWorld},
	}, s.find)

	mcp.AddTool(s.protocol, &mcp.Tool{
		Name:        ToolWait,
		Title:       "Wait for a color in a region",
		Description: "Perform a finite, policy-bounded visual wait over one explicit display region. A match retains one in-memory observation until it is released or the session closes.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &openWorld},
	}, s.wait)

	mcp.AddTool(s.protocol, &mcp.Tool{
		Name:        ToolReleaseObservation,
		Title:       "Release a RobotGo observation",
		Description: "Idempotently zero and remove one retained in-memory observation. No desktop backend is contacted.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &nondestructive, IdempotentHint: true, OpenWorldHint: &closedWorld},
	}, s.releaseObservation)
}

func (s *Server) find(ctx context.Context, _ *mcp.CallToolRequest, input agent.FindColorRequest) (*mcp.CallToolResult, FindOutput, error) {
	session, toolErr := s.adapter.begin()
	if toolErr != nil {
		return errorResult(), FindOutput{Error: toolErr}, nil
	}
	visual, toolErr := visualConditionSession(session)
	if toolErr != nil {
		return errorResult(), FindOutput{Error: toolErr}, nil
	}
	result, err := visual.FindColor(ctx, input)
	output := FindOutput{Result: &result}
	if err != nil {
		output.Error = safeToolError(err)
		return errorResult(), output, nil
	}
	return nil, output, nil
}

func (s *Server) wait(ctx context.Context, _ *mcp.CallToolRequest, input agent.WaitColorRequest) (*mcp.CallToolResult, WaitOutput, error) {
	session, toolErr := s.adapter.begin()
	if toolErr != nil {
		return errorResult(), WaitOutput{Error: toolErr}, nil
	}
	visual, toolErr := visualConditionSession(session)
	if toolErr != nil {
		return errorResult(), WaitOutput{Error: toolErr}, nil
	}
	result, err := visual.WaitColor(ctx, input)
	output := WaitOutput{Result: &result}
	if err != nil {
		output.Error = safeToolError(err)
		return errorResult(), output, nil
	}
	return nil, output, nil
}

func (s *Server) releaseObservation(_ context.Context, _ *mcp.CallToolRequest, input ReleaseObservationInput) (*mcp.CallToolResult, ReleaseObservationOutput, error) {
	session, toolErr := s.adapter.begin()
	if toolErr != nil {
		return errorResult(), ReleaseObservationOutput{Error: toolErr}, nil
	}
	visual, toolErr := visualConditionSession(session)
	if toolErr != nil {
		return errorResult(), ReleaseObservationOutput{Error: toolErr}, nil
	}
	if err := visual.ReleaseObservation(input.ObservationID); err != nil {
		return errorResult(), ReleaseObservationOutput{Error: safeToolError(err)}, nil
	}
	return nil, ReleaseObservationOutput{Released: true}, nil
}

func visualConditionSession(session Session) (VisualConditionSession, *ToolError) {
	visual, ok := session.(VisualConditionSession)
	if !ok {
		return nil, &ToolError{Code: agent.ErrorUnsupported, Message: "RobotGo visual conditions are unavailable for this session"}
	}
	return visual, nil
}
