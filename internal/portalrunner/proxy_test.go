package portalrunner

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCONNECTProxyAllowsOnlyManifestHosts(t *testing.T) {
	t.Parallel()
	proxy, err := NewCONNECTProxy(validManifest().Network)
	if err != nil {
		t.Fatal(err)
	}
	targetServer, targetClient := net.Pipe()
	t.Cleanup(func() {
		_ = targetServer.Close()
		_ = targetClient.Close()
	})
	proxy.dial = func(context.Context, string, string) (net.Conn, error) {
		return targetServer, nil
	}
	listener := newLoopbackListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- proxy.Serve(ctx, listener) }()

	connection, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	if _, err := io.WriteString(
		connection,
		"CONNECT github.com:443 HTTP/1.1\r\nHost: github.com:443\r\n\r\n",
	); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(connection), &http.Request{
		Method: http.MethodConnect,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT status = %s", response.Status)
	}

	const payload = "encrypted tunnel bytes"
	targetRead := make(chan string, 1)
	go func() {
		data := make([]byte, len(payload))
		_, _ = io.ReadFull(targetClient, data)
		targetRead <- string(data)
	}()
	if _, err := io.WriteString(connection, payload); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-targetRead:
		if got != payload {
			t.Fatalf("tunneled payload = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for tunneled payload")
	}
	_ = connection.Close()
	_ = targetClient.Close()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("proxy shutdown: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("proxy did not stop after cancellation")
	}
}

func TestCONNECTProxyDeniesUnsafeRequests(t *testing.T) {
	t.Parallel()
	proxy, err := NewCONNECTProxy(validManifest().Network)
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []*http.Request{
		{Method: http.MethodGet, Host: "github.com"},
		{Method: http.MethodConnect, Host: "example.org:443"},
		{Method: http.MethodConnect, Host: "127.0.0.1:443"},
		{Method: http.MethodConnect, Host: "github.com:22"},
	} {
		response := &recordingResponse{header: make(http.Header)}
		proxy.ServeHTTP(response, request)
		if response.status != http.StatusForbidden {
			t.Errorf("%s %s status = %d", request.Method, request.Host, response.status)
		}
		if !strings.Contains(response.body.String(), "denied") {
			t.Errorf("%s %s response disclosed unexpected detail", request.Method, request.Host)
		}
	}
}

func newLoopbackListener(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return listener
}

type recordingResponse struct {
	header http.Header
	body   strings.Builder
	status int
}

func (response *recordingResponse) Header() http.Header {
	return response.header
}

func (response *recordingResponse) Write(data []byte) (int, error) {
	if response.status == 0 {
		response.status = http.StatusOK
	}
	return response.body.Write(data)
}

func (response *recordingResponse) WriteHeader(status int) {
	response.status = status
}
