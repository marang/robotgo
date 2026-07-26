package portalrunner

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	proxyHeaderTimeout     = 5 * time.Second
	proxyDialTimeout       = 10 * time.Second
	proxyIdleTimeout       = 30 * time.Second
	proxyShutdownTimeout   = 5 * time.Second
	proxyMaximumTunnels    = 64
	proxyConnectionMessage = "HTTP/1.1 200 Connection Established\r\n\r\n"
)

// CONNECTProxy restricts a guest to TLS tunnels for manifest-allowlisted
// dependency hosts. It never parses or retains tunneled content.
type CONNECTProxy struct {
	network NetworkConfig
	dial    func(context.Context, string, string) (net.Conn, error)
	slots   chan struct{}
}

// NewCONNECTProxy constructs a bounded allowlist proxy.
func NewCONNECTProxy(network NetworkConfig) (*CONNECTProxy, error) {
	if len(network.AllowedHosts) == 0 {
		return nil, errors.New("portal runner proxy allowlist is empty")
	}
	dialer := &net.Dialer{Timeout: proxyDialTimeout, KeepAlive: proxyIdleTimeout}
	return &CONNECTProxy{
		network: network,
		dial:    dialer.DialContext,
		slots:   make(chan struct{}, proxyMaximumTunnels),
	}, nil
}

// Serve handles proxy traffic until context cancellation or a listener error.
func (proxy *CONNECTProxy) Serve(ctx context.Context, listener net.Listener) error {
	if proxy == nil || proxy.dial == nil || proxy.slots == nil {
		return errors.New("portal runner proxy is not initialized")
	}
	if listener == nil {
		return errors.New("portal runner proxy listener is nil")
	}
	server := &http.Server{
		Handler:           proxy,
		ReadHeaderTimeout: proxyHeaderTimeout,
		IdleTimeout:       proxyIdleTimeout,
		MaxHeaderBytes:    16 * 1024,
	}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownContext, cancel := context.WithTimeout(
			context.Background(),
			proxyShutdownTimeout,
		)
		defer cancel()
		_ = server.Shutdown(shutdownContext)
	}()

	err := server.Serve(listener)
	if !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve portal runner proxy: %w", err)
	}
	<-shutdownDone
	return nil
}

// ServeHTTP accepts only allowlisted HTTPS CONNECT requests.
func (proxy *CONNECTProxy) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodConnect ||
		!proxy.network.HostAllowed(request.Host) {
		http.Error(response, "proxy destination denied", http.StatusForbidden)
		return
	}
	select {
	case proxy.slots <- struct{}{}:
		defer func() { <-proxy.slots }()
	default:
		http.Error(response, "proxy capacity exceeded", http.StatusServiceUnavailable)
		return
	}

	target, err := proxy.dial(request.Context(), "tcp", request.Host)
	if err != nil {
		http.Error(response, "proxy destination unavailable", http.StatusBadGateway)
		return
	}
	hijacker, ok := response.(http.Hijacker)
	if !ok {
		_ = target.Close()
		http.Error(response, "proxy transport unavailable", http.StatusInternalServerError)
		return
	}
	client, buffered, err := hijacker.Hijack()
	if err != nil {
		_ = target.Close()
		return
	}
	defer func() { _ = client.Close() }()
	defer func() { _ = target.Close() }()

	if _, err := buffered.WriteString(proxyConnectionMessage); err != nil {
		return
	}
	if err := buffered.Flush(); err != nil {
		return
	}
	proxyTunnel(client, buffered.Reader, target)
}

func proxyTunnel(client net.Conn, clientReader *bufio.Reader, target net.Conn) {
	var closeOnce sync.Once
	closeConnections := func() {
		_ = client.Close()
		_ = target.Close()
	}
	done := make(chan struct{}, 2)
	copyStream := func(destination io.Writer, source io.Reader) {
		_, _ = io.Copy(destination, source)
		closeOnce.Do(closeConnections)
		done <- struct{}{}
	}
	go copyStream(target, clientReader)
	go copyStream(client, target)
	<-done
	<-done
}
