package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TransportMode determines how the MCP server communicates.
type TransportMode string

const (
	ModeStdio          TransportMode = "stdio"
	ModeSSE            TransportMode = "sse"
	ModeStreamableHTTP TransportMode = "streamable"
)

func detectMode() TransportMode {
	switch strings.ToLower(os.Getenv("MCP_TRANSPORT")) {
	case "sse":
		return ModeSSE
	case "streamable":
		return ModeStreamableHTTP
	default:
		return ModeStdio
	}
}

func isHostedMode(mode TransportMode) bool {
	return mode == ModeSSE || mode == ModeStreamableHTTP
}

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	if key := r.URL.Query().Get("api_key"); key != "" {
		return key
	}
	return ""
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// --- Client IP forwarding ---

type ctxKey int

const clientIPKey ctxKey = iota

// clientIPMiddleware extracts the real client IP from the HTTP request and
// stores it in context. For SSE, this runs on the initial GET — the context
// propagates to all tool calls in that session. For Streamable HTTP, it runs
// on each POST.
func clientIPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := extractClientIP(r)
		ctx := context.WithValue(r.Context(), clientIPKey, ip)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// resolveClientIP returns the end-user IP, preferring the context value
// (set by HTTP middleware) and falling back to req.Extra.Header
// (available in Streamable HTTP mode).
func resolveClientIP(ctx context.Context, req *mcp.CallToolRequest) string {
	if ip, ok := ctx.Value(clientIPKey).(string); ok && ip != "" {
		return ip
	}
	if req != nil && req.Extra != nil && req.Extra.Header != nil {
		return firstIPFromHeaders(req.Extra.Header)
	}
	return ""
}

// ctxWithClientIP enriches the context with the resolved client IP so it
// flows through to api.post() which reads it via clientIPFromCtx().
func ctxWithClientIP(ctx context.Context, req *mcp.CallToolRequest) context.Context {
	if ip := resolveClientIP(ctx, req); ip != "" {
		return context.WithValue(ctx, clientIPKey, ip)
	}
	return ctx
}

// clientIPFromCtx retrieves the client IP from context. Called by api.post().
func clientIPFromCtx(ctx context.Context) string {
	if ip, ok := ctx.Value(clientIPKey).(string); ok {
		return ip
	}
	return ""
}

// extractClientIP gets the real client IP from an HTTP request, checking
// proxy headers first, then falling back to RemoteAddr.
func extractClientIP(r *http.Request) string {
	if ip := firstIPFromHeaders(r.Header); ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func firstIPFromHeaders(h http.Header) string {
	if xff := h.Get("X-Forwarded-For"); xff != "" {
		if ip, _, ok := strings.Cut(xff, ","); ok {
			return strings.TrimSpace(ip)
		}
		return strings.TrimSpace(xff)
	}
	if xri := h.Get("X-Real-Ip"); xri != "" {
		return xri
	}
	return ""
}
