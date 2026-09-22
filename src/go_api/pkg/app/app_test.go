package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSuccess_InFlightRequestSurvivesGracefulShutdown(t *testing.T) {
	// Arrange
	ctx, cancel := context.WithCancel(t.Context())

	requestStarted := make(chan struct{})
	handlerCompleted := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/slow", func(w http.ResponseWriter, _ *http.Request) {
		close(requestStarted)
		// Simulate work that is still running when shutdown begins.
		time.Sleep(250 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		close(handlerCompleted)
	})

	sut := &http.Server{Handler: mux}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	url := "http://" + listener.Addr().String() + "/slow"

	runErr := make(chan error, 1)
	go func() {
		runErr <- serveWithGracefulShutdown(ctx, gracefulServer{
			server: sut,
			serve:  func() error { return sut.Serve(listener) },
		})
	}()

	client := &http.Client{Timeout: 5 * time.Second}
	status := make(chan int, 1)
	go func() {
		resp, err := client.Get(url)
		if err != nil {
			status <- 0
			return
		}
		defer resp.Body.Close()
		status <- resp.StatusCode
	}()

	// Act: cancel while the request is in flight (the SIGTERM equivalent).
	<-requestStarted
	cancel()

	// Assert: the in-flight request completed successfully.
	select {
	case code := <-status:
		assert.Equal(t, http.StatusOK, code)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the in-flight request to complete")
	}
	select {
	case <-handlerCompleted:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the handler to complete")
	}

	// Assert: the server exited and the helper returned nil (the filtered
	// http.ErrServerClosed is the clean-shutdown success path).
	var errResult error
	select {
	case errResult = <-runErr:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for serveWithGracefulShutdown to return")
	}
	require.NoError(t, errResult)

	resp, err := client.Get(url)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("expected the drained server to refuse new requests")
	}
}

func TestError_ServeWithGracefulShutdownReturnsServeError(t *testing.T) {
	// Arrange
	serveErr := errors.New("serve failed")
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	// The serve call fails without any cancellation: the group context must
	// unblock the shutdown watcher so Wait can return the real error.
	sut := &http.Server{}

	// Act
	err := serveWithGracefulShutdown(ctx, gracefulServer{
		server: sut,
		serve:  func() error { return serveErr },
	})

	// Assert
	require.ErrorIs(t, err, serveErr)
}
