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

	robotgo "github.com/marang/robotgo"
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

func TestRunImageFlagEmitsOnlyExplicitPrivacyNotice(t *testing.T) {
	transportErr := errors.New("transport stopped")
	session := &commandSession{}
	var stderr bytes.Buffer
	err := run(t.Context(), []string{"-allow-image-content"}, &stderr,
		failingTransport{transportErr}, func(agent.Config) (mcpserver.Session, error) {
			return session, nil
		})
	if !errors.Is(err, transportErr) {
		t.Fatalf("run error = %v", err)
	}
	if stderr.String() != imageStartupNotice {
		t.Fatalf("startup notice = %q", stderr.String())
	}
	for _, forbidden := range []string{"data:", "base64", "pixel bytes", "iVBOR"} {
		if strings.Contains(stderr.String(), forbidden) {
			t.Fatalf("startup notice contains sensitive payload marker %q", forbidden)
		}
	}
	if session.closes != 1 {
		t.Fatalf("Close calls = %d", session.closes)
	}

	session = &commandSession{}
	stderr.Reset()
	err = run(t.Context(), nil, &stderr, failingTransport{transportErr}, func(agent.Config) (mcpserver.Session, error) {
		return session, nil
	})
	if !errors.Is(err, transportErr) || stderr.Len() != 0 {
		t.Fatalf("default startup = err %v, stderr %q", err, stderr.String())
	}
}

func TestRunPortalViewRequiresBothImageAndValidatedPolicyGrantsBeforeConsent(t *testing.T) {
	validPolicy := `{
		"allowed_operations":["desktop.view"],
		"allowed_display_ids":[0],
		"allowed_view_regions":[{"x":0,"y":0,"width":64,"height":64,"display_id":0}],
		"allow_portal_view":true,
		"max_actions":0,
		"max_text_runes":0,
		"max_observations":2,
		"max_view_source_pixels":4096,
		"max_view_encoded_bytes":65536,
		"max_view_width":64,
		"max_view_height":64,
		"max_views":1,
		"max_concurrent_views":1,
		"min_view_interval_ms":100,
		"view_timeout_ms":5000,
		"session_timeout_ms":10000
	}`
	policyPath := writePolicy(t, validPolicy)

	for name, args := range map[string][]string{
		"missing image grant": {"-start-portal-view"},
		"missing view policy": {"-allow-image-content", "-start-portal-view"},
	} {
		t.Run(name, func(t *testing.T) {
			started := false
			created := false
			var stderr bytes.Buffer
			err := runWithScreenCast(t.Context(), args, &stderr, failingTransport{}, func(agent.Config) (mcpserver.Session, error) {
				created = true
				return &commandSession{}, nil
			}, screenCastLifecycle{
				start: func(context.Context, robotgo.ScreenCastCaptureOptions, ...int) error {
					started = true
					return nil
				},
				close: func() error { return nil },
			})
			if err == nil {
				t.Fatal("unsafe portal startup unexpectedly succeeded")
			}
			if started || created {
				t.Fatalf("denied portal startup reached consent=%v session=%v", started, created)
			}
		})
	}

	t.Run("explicit grants start and close one session", func(t *testing.T) {
		transportErr := errors.New("transport stopped")
		session := &commandSession{}
		starts, closes := 0, 0
		var stderr bytes.Buffer
		err := runWithScreenCast(t.Context(), []string{
			"-policy", policyPath, "-allow-image-content", "-start-portal-view",
		}, &stderr, failingTransport{transportErr}, func(agent.Config) (mcpserver.Session, error) {
			return session, nil
		}, screenCastLifecycle{
			start: func(_ context.Context, options robotgo.ScreenCastCaptureOptions, streamIndex ...int) error {
				starts++
				if options.Sources != robotgo.ScreenCastSourceMonitor ||
					options.Cursor != robotgo.ScreenCastCursorHidden ||
					options.Persist != robotgo.ScreenCastPersistNone || len(streamIndex) != 0 {
					t.Fatalf("portal options = %+v, stream=%v", options, streamIndex)
				}
				return nil
			},
			close: func() error { closes++; return nil },
		})
		if !errors.Is(err, transportErr) {
			t.Fatalf("run error = %v", err)
		}
		if starts != 1 || closes != 1 || session.closes != 1 {
			t.Fatalf("lifecycle starts=%d closes=%d session-closes=%d", starts, closes, session.closes)
		}
		if stderr.String() != imageStartupNotice+portalStartupNotice {
			t.Fatalf("startup notice = %q", stderr.String())
		}
	})
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
