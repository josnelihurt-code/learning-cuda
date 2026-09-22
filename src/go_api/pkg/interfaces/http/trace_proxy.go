package http

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/jrb/cuda-learning/src/go_api/pkg/infrastructure/logger"
)

type traceProxyHandler struct {
	collectorEndpoint string
	enabled           bool
}

func NewTraceProxyHandler(collectorEndpoint string, enabled bool) *traceProxyHandler {
	return &traceProxyHandler{
		collectorEndpoint: collectorEndpoint,
		enabled:           enabled,
	}
}

func (h *traceProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if !h.enabled {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"success":true,"message":"tracing disabled"}`)); err != nil {
			logger.FromContext(ctx).Error().Err(err).
				Str("remote_addr", r.RemoteAddr).
				Msg("Failed to write tracing-disabled response")
		}
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.FromContext(ctx).Error().Err(err).
			Str("remote_addr", r.RemoteAddr).
			Str("content_type", r.Header.Get("Content-Type")).
			Msg("Failed to read trace request body")
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var collectorURL string
	if strings.HasPrefix(h.collectorEndpoint, "http://") || strings.HasPrefix(h.collectorEndpoint, "https://") {
		collectorURL = strings.TrimSuffix(h.collectorEndpoint, "/")
		if !strings.HasSuffix(collectorURL, "/v1/traces") {
			collectorURL += "/v1/traces"
		}
	} else {
		collectorURL = fmt.Sprintf("http://%s/v1/traces", h.collectorEndpoint)
	}

	logger.FromContext(ctx).Debug().
		Str("collector_url", collectorURL).
		Int("body_bytes", len(body)).
		Str("remote_addr", r.RemoteAddr).
		Msg("Forwarding browser traces")

	proxyReq, err := http.NewRequest(http.MethodPost, collectorURL, bytes.NewReader(body))
	if err != nil {
		logger.FromContext(ctx).Error().Err(err).
			Str("collector_url", collectorURL).
			Str("remote_addr", r.RemoteAddr).
			Msg("Failed to create proxy request to collector")
		http.Error(w, "Failed to create proxy request", http.StatusInternalServerError)
		return
	}

	proxyReq.Header.Set("Content-Type", "application/json")
	for key, values := range r.Header {
		for _, value := range values {
			proxyReq.Header.Add(key, value)
		}
	}

	client := &http.Client{}
	resp, err := client.Do(proxyReq)
	if err != nil {
		logger.FromContext(ctx).Error().Err(err).
			Str("collector_url", collectorURL).
			Str("remote_addr", r.RemoteAddr).
			Int("body_bytes", len(body)).
			Msg("Failed to forward traces to collector")
		http.Error(w, "Failed to forward traces", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body) //nolint:errcheck // Best effort response logging

	if resp.StatusCode >= 400 {
		logger.FromContext(ctx).Warn().
			Str("collector_url", collectorURL).
			Str("remote_addr", r.RemoteAddr).
			Int("status_code", resp.StatusCode).
			Str("collector_response", string(respBody)).
			Msg("Collector returned error status for traces")
	} else {
		logger.FromContext(ctx).Debug().
			Str("collector_url", collectorURL).
			Int("status_code", resp.StatusCode).
			Int("body_bytes", len(body)).
			Msg("Traces forwarded successfully")
	}

	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(respBody) //nolint:errcheck // Best effort response write
}
