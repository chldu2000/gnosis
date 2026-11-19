package gnosis

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestADBHandshakeUnblocksOnExecutionCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	entered := make(chan struct{})
	stopServer := make(chan struct{})
	defer close(stopServer)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var length [4]byte
		if _, err := io.ReadFull(conn, length[:]); err == nil {
			close(entered)
			<-stopServer
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	adb := adbTransport{address: listener.Addr().String(), parent: ctx}
	done := make(chan error, 1)
	go func() { _, err := adb.shell("echo ready"); done <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("ADB request did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("interrupted ADB call did not report an error")
		}
	case <-time.After(time.Second):
		t.Fatal("ADB handshake waited for its original timeout")
	}
}

type blockingAndroidRPC struct{ entered chan struct{} }

func (rpc blockingAndroidRPC) RoundTrip(req *http.Request) (*http.Response, error) {
	close(rpc.entered)
	<-req.Context().Done()
	return nil, req.Context().Err()
}

func TestAndroidActionCancelsItsHTTPCall(t *testing.T) {
	transport := blockingAndroidRPC{entered: make(chan struct{})}
	device := &AndroidDevice{http: &http.Client{Transport: transport}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	device.setRunContext(ctx)
	done := make(chan error, 1)
	go func() { done <- device.PressKey("back") }()
	select {
	case <-transport.entered:
	case <-time.After(time.Second):
		t.Fatal("device action did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("interrupted Android action did not report an error")
		}
	case <-time.After(time.Second):
		t.Fatal("device HTTP call did not observe execution cancellation")
	}
}
