package http

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/jrb/cuda-learning/src/go_api/pkg/infrastructure/logger"
)

type logsProxyHandler struct {
	enabled           bool
	collectorEndpoint string
}

func NewLogsProxyHandler(collectorEndpoint string, enabled bool) *logsProxyHandler {
	return &logsProxyHandler{
		enabled:           enabled,
		collectorEndpoint: collectorEndpoint,
	}
}

func (h *logsProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// All logging in this handler MUST use logger.LocalOnly() to avoid a feedback
	// loop: this handler forwards logs to the OTLP collector, so using the remote
	// logger here would cause those log entries to be re-forwarded indefinitely.
	log := logger.LocalOnly()

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Encoding")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if !h.enabled {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"success":true,"message":"logging disabled"}`)); err != nil {
			log.Error().Err(err).
				Str("remote_addr", r.RemoteAddr).
				Msg("Failed to write logging-disabled response")
		}
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Error().Err(err).
			Str("remote_addr", r.RemoteAddr).
			Str("content_type", r.Header.Get("Content-Type")).
			Msg("Failed to read logs request body")
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	log.Debug().
		Int("body_bytes", len(body)).
		Str("content_type", r.Header.Get("Content-Type")).
		Str("remote_addr", r.RemoteAddr).
		Msg("Received OTLP logs from frontend")

	if h.collectorEndpoint != "" {
		var collectorURL string
		if strings.HasPrefix(h.collectorEndpoint, "http://") || strings.HasPrefix(h.collectorEndpoint, "https://") {
			collectorURL = h.collectorEndpoint
		} else {
			collectorURL = fmt.Sprintf("http://%s/v1/logs", h.collectorEndpoint)
		}
		//nolint:gosec // collectorURL is from internal config, not user input
		resp, err := http.Post(collectorURL, "application/json", bytes.NewReader(body))
		if err != nil {
			log.Error().Err(err).
				Str("collector_url", collectorURL).
				Str("remote_addr", r.RemoteAddr).
				Int("body_bytes", len(body)).
				Msg("Failed to forward logs to collector")
		} else {
			defer resp.Body.Close()
			if resp.StatusCode >= 400 {
				respBody, _ := io.ReadAll(resp.Body) //nolint:errcheck // Best effort
				log.Warn().
					Int("status_code", resp.StatusCode).
					Str("collector_url", collectorURL).
					Str("collector_response", string(respBody)).
					Msg("Collector returned error status for logs")
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(`{"partialSuccess":{}}`)); err != nil {
		log.Error().Err(err).
			Str("remote_addr", r.RemoteAddr).
			Msg("Failed to write response")
	}
}
