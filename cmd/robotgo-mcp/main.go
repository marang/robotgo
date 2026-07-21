// Command robotgo-mcp serves one policy-gated RobotGo agent session over MCP
// stdio. It deliberately exposes no network transport.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/marang/robotgo/agent"
	"github.com/marang/robotgo/agent/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	maxPolicyBytes          = 1 << 20
	defaultMaxObservations  = 100
	policyStdinPath         = "-"
	policyFlagDescription   = "strict RobotGo agent policy JSON file (never read from stdin)"
	commandErrorMessageBase = "robotgo-mcp"
)

type sessionFactory func(agent.Config) (mcpserver.Session, error)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := run(ctx, os.Args[1:], os.Stderr, &mcp.StdioTransport{}, func(config agent.Config) (mcpserver.Session, error) {
		return agent.NewSession(config)
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		_, _ = fmt.Fprintf(os.Stderr, "%s: %v\n", commandErrorMessageBase, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stderr io.Writer, transport mcp.Transport, newSession sessionFactory) error {
	flags := flag.NewFlagSet(commandErrorMessageBase, flag.ContinueOnError)
	flags.SetOutput(stderr)
	policyPath := flags.String("policy", "", policyFlagDescription)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	if transport == nil {
		return fmt.Errorf("nil MCP transport")
	}
	if newSession == nil {
		return fmt.Errorf("nil agent session factory")
	}

	policy, err := policyFromFile(*policyPath)
	if err != nil {
		return err
	}
	session, err := newSession(agent.Config{Policy: policy})
	if err != nil {
		return fmt.Errorf("create agent session: %w", err)
	}
	server, err := mcpserver.New(session)
	if err != nil {
		_ = session.Close()
		return fmt.Errorf("create MCP server: %w", err)
	}
	return server.Run(ctx, transport)
}

func defaultPolicy() agent.Policy {
	return agent.Policy{
		AllowedOperations: []agent.Operation{agent.OperationObserve},
		MaxObservations:   defaultMaxObservations,
	}
}

func policyFromFile(path string) (policy agent.Policy, returnErr error) {
	if path == "" {
		return defaultPolicy(), nil
	}
	if path == policyStdinPath {
		return agent.Policy{}, fmt.Errorf("policy must be a file path, not stdin")
	}
	file, err := os.Open(path)
	if err != nil {
		return agent.Policy{}, fmt.Errorf("open policy: %w", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("close policy: %w", err))
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return agent.Policy{}, fmt.Errorf("inspect policy: %w", err)
	}
	if !info.Mode().IsRegular() {
		return agent.Policy{}, fmt.Errorf("policy must be a regular file")
	}

	data, err := io.ReadAll(io.LimitReader(file, maxPolicyBytes+1))
	if err != nil {
		return agent.Policy{}, fmt.Errorf("read policy: %w", err)
	}
	if len(data) > maxPolicyBytes {
		return agent.Policy{}, fmt.Errorf("policy exceeds %d-byte limit", maxPolicyBytes)
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil {
		return agent.Policy{}, fmt.Errorf("decode policy: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return agent.Policy{}, err
	}
	return policy, nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("decode policy: multiple JSON values")
		}
		return fmt.Errorf("decode policy: trailing data: %w", err)
	}
	return nil
}
