package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	// --version / -v flag
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("foliopdf-mcp %s\n", mcpVersion)
		return
	}

	mode := detectMode()
	api := newAPIClient()

	// Use JSON logging in hosted mode, text in stdio (stderr only — stdout is MCP).
	var logger *slog.Logger
	if isHostedMode(mode) {
		logger = slog.New(slog.NewJSONHandler(os.Stderr, nil))
	} else {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}

	switch mode {
	case ModeStdio:
		apiKey := os.Getenv("FOLIOPDF_API_KEY")
		if apiKey == "" {
			logger.Warn("FOLIOPDF_API_KEY not set — running in free mode. render_template, manipulate_pdf, and extract_pdf will not work.")
		}
		server := buildServer(api, apiKey, mode, logger)
		if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			logger.Error("server exited", "error", err)
			os.Exit(1)
		}

	case ModeSSE:
		mcpHandler := mcp.NewSSEHandler(func(r *http.Request) *mcp.Server {
			return buildServer(api, extractBearerToken(r), ModeSSE, logger)
		}, nil)
		runHTTPServer(mcpHandler, api, logger, "sse")

	case ModeStreamableHTTP:
		mcpHandler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
			return buildServer(api, extractBearerToken(r), ModeStreamableHTTP, logger)
		}, nil)
		runHTTPServer(mcpHandler, api, logger, "streamable-http")
	}
}

func healthReadyHandler(api *apiClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		start := time.Now()
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		// HEAD request to the API base URL to check reachability
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, api.baseURL, nil)
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]any{
				"status": "unhealthy",
				"components": map[string]any{
					"api": map[string]any{"status": "unhealthy", "error": err.Error()},
				},
			})
			return
		}

		resp, err := api.httpClient.Do(req)
		latency := time.Since(start)
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]any{
				"status": "unhealthy",
				"components": map[string]any{
					"api": map[string]any{"status": "unhealthy", "latency": latency.String(), "error": err.Error()},
				},
			})
			return
		}
		resp.Body.Close()

		json.NewEncoder(w).Encode(map[string]any{
			"status":  "ok",
			"version": mcpVersion,
			"components": map[string]any{
				"api": map[string]any{"status": "healthy", "latency": latency.String()},
			},
		})
	}
}

func maxBodyMiddleware(next http.Handler, maxBytes int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
		}
		next.ServeHTTP(w, r)
	})
}

func runHTTPServer(mcpHandler http.Handler, api *apiClient, logger *slog.Logger, transport string) {
	addr := envOr("MCP_ADDR", ":8080")

	mux := http.NewServeMux()
	mux.Handle("/", mcpHandler)

	// Liveness probe — always returns 200. Use for k8s livenessProbe.
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
		})
	})

	// Readiness probe — checks upstream API reachability. Use for k8s readinessProbe and load balancers.
	mux.HandleFunc("GET /health/ready", healthReadyHandler(api))
	mux.HandleFunc("GET /health", healthReadyHandler(api))

	srv := &http.Server{
		Addr:           addr,
		Handler:        maxBodyMiddleware(clientIPMiddleware(mux), 10*1024*1024), // 10 MB max request body
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   120 * time.Second,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 1 << 20, // 1 MB max headers
	}

	// Graceful shutdown on SIGINT/SIGTERM
	done := make(chan struct{})
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		sig := <-sigCh
		logger.Info("shutting down", "signal", sig.String())

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			logger.Error("shutdown error", "error", err)
		}
		close(done)
	}()

	logger.Info("FolioPDF MCP server starting", "addr", addr, "transport", transport)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
	<-done
}
