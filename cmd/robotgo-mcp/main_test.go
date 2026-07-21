package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marang/robotgo/agent"
	"github.com/marang/robotgo/agent/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func writePolicy(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	return path
}

func TestDefaultPolicyIsDiagnosticsOnly(t *testing.T) {
	policy, err := policyFromFile("")
	if err != nil {
		t.Fatalf("policyFromFile: %v", err)
	}
	if len(policy.AllowedOperations) != 1 || policy.AllowedOperations[0] != agent.OperationObserve {
		t.Fatalf("allowed operations = %v", policy.AllowedOperations)
	}
	if policy.MaxObservations != defaultMaxObservations {
		t.Fatalf("max observations = %d", policy.MaxObservations)
	}
	if policy.MaxActions != 0 || policy.MaxCapturePixels != 0 || len(policy.AllowedDisplayIDs) != 0 {
		t.Fatalf("default policy permits capture or mutation: %+v", policy)
	}
}

func TestPolicyFileIsStrictAndBounded(t *testing.T) {
	valid := `{"allowed_operations":["desktop.observe"],"max_actions":0,"max_text_runes":0,"max_observations":2}`
	policy, err := policyFromFile(writePolicy(t, valid))
	if err != nil {
		t.Fatalf("valid policy: %v", err)
	}
	if policy.MaxObservations != 2 {
		t.Fatalf("max observations = %d", policy.MaxObservations)
	}

	for name, contents := range map[string]string{
		"unknown field":   `{"unknown":true}`,
		"multiple values": valid + ` {}`,
		"trailing data":   valid + ` nope`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := policyFromFile(writePolicy(t, contents)); err == nil {
				t.Fatal("invalid policy unexpectedly succeeded")
			}
		})
	}

	oversized := strings.Repeat(" ", maxPolicyBytes+1)
	if _, err := policyFromFile(writePolicy(t, oversized)); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("oversized policy error = %v", err)
	}
}

func TestPolicyNeverReadsStdin(t *testing.T) {
	if _, err := policyFromFile(policyStdinPath); err == nil || !strings.Contains(err.Error(), "not stdin") {
		t.Fatalf("stdin policy error = %v", err)
	}
}

func TestPolicyRejectsNonRegularFile(t *testing.T) {
	if _, err := policyFromFile(t.TempDir()); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("directory policy error = %v", err)
	}
}

type failingTransport struct{ err error }

func (t failingTransport) Connect(context.Context) (mcp.Connection, error) { return nil, t.err }

type commandSession struct{ closes int }

func (*commandSession) Catalog() agent.OperationCatalog { return agent.OperationCatalog{} }
func (*commandSession) Observe(context.Context, agent.ObserveRequest) (*agent.Observation, error) {
	return nil, errors.New("unused")
}
func (*commandSession) DryRun(context.Context, agent.ActionRequest) (agent.ActionResult, error) {
	return agent.ActionResult{}, errors.New("unused")
}
func (*commandSession) Execute(context.Context, agent.ActionRequest) (agent.ActionResult, error) {
	return agent.ActionResult{}, errors.New("unused")
}
func (s *commandSession) Close() error { s.closes++; return nil }

func TestRunUsesDefaultPolicyAndClosesOnTransportFailure(t *testing.T) {
	transportErr := errors.New("private transport failure")
	session := &commandSession{}
	var received agent.Policy
	err := run(t.Context(), nil, io.Discard, failingTransport{transportErr}, func(config agent.Config) (mcpserver.Session, error) {
		received = config.Policy
		return session, nil
	})
	if !errors.Is(err, transportErr) {
		t.Fatalf("run error = %v", err)
	}
	if len(received.AllowedOperations) != 1 || received.AllowedOperations[0] != agent.OperationObserve {
		t.Fatalf("factory policy = %+v", received)
	}
	if session.closes != 1 {
		t.Fatalf("Close calls = %d, want 1", session.closes)
	}
}

func TestRunRejectsArgumentsBeforeCreatingSession(t *testing.T) {
	for name, args := range map[string][]string{
		"positional":     {"unexpected"},
		"missing policy": {"-policy"},
	} {
		t.Run(name, func(t *testing.T) {
			created := false
			var stderr bytes.Buffer
			err := run(t.Context(), args, &stderr, failingTransport{}, func(agent.Config) (mcpserver.Session, error) {
				created = true
				return &commandSession{}, nil
			})
			if err == nil {
				t.Fatal("invalid arguments unexpectedly succeeded")
			}
			if created {
				t.Fatal("session was created for invalid arguments")
			}
		})
	}
}
