package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	mcpVersion  = "0.1.0"
	userAgent   = "foliopdf-mcp/" + mcpVersion
	retryDelay  = 1 * time.Second
	httpTimeout = 60 * time.Second
)

type apiClient struct {
	baseURL    string
	httpClient *http.Client
}

func newAPIClient() *apiClient {
	base := envOr("FOLIOPDF_API_URL", "https://api.foliopdf.dev/v1")
	return &apiClient{
		baseURL: strings.TrimRight(base, "/"),
		httpClient: &http.Client{
			Timeout: httpTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

// apiResponse holds the response from the FolioPDF API.
type apiResponse struct {
	Data       []byte
	Status     int
	RateLimit  *rateLimitInfo
}

// rateLimitInfo holds rate limit headers from the API response.
type rateLimitInfo struct {
	Limit     string // X-RateLimit-Limit
	Remaining string // X-RateLimit-Remaining
	Reset     string // X-RateLimit-Reset
	Plan      string // X-Plan
}

func parseRateLimit(h http.Header) *rateLimitInfo {
	remaining := h.Get("X-RateLimit-Remaining")
	if remaining == "" {
		return nil
	}
	return &rateLimitInfo{
		Limit:     h.Get("X-RateLimit-Limit"),
		Remaining: remaining,
		Reset:     h.Get("X-RateLimit-Reset"),
		Plan:      h.Get("X-Plan"),
	}
}

func (rl *rateLimitInfo) String() string {
	if rl == nil {
		return ""
	}
	return fmt.Sprintf("%s/%s renders remaining (resets %s)", rl.Remaining, rl.Limit, rl.Reset)
}

// doJSON sends a POST request with a JSON body and returns the API response.
// It retries once on 5xx errors after a 1-second delay.
func (a *apiClient) doJSON(ctx context.Context, path, apiKey string, body any) (*apiResponse, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := a.post(ctx, path, apiKey, payload)
	if err != nil {
		return nil, err
	}

	if resp.Status >= 500 {
		select {
		case <-time.After(retryDelay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		resp, err = a.post(ctx, path, apiKey, payload)
		if err != nil {
			return nil, err
		}
	}

	return resp, nil
}

func (a *apiClient) post(ctx context.Context, path, apiKey string, payload []byte) (*apiResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	if clientIP := clientIPFromCtx(ctx); clientIP != "" {
		req.Header.Set("X-Forwarded-For", clientIP)
	}

	httpResp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer httpResp.Body.Close()

	data, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return &apiResponse{
		Data:      data,
		Status:    httpResp.StatusCode,
		RateLimit: parseRateLimit(httpResp.Header),
	}, nil
}

type apiErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Docs    string `json:"docs"`
}

type apiErrorResponse struct {
	Error apiErrorDetail `json:"error"`
}

func parseAPIError(status int, data []byte) (apiErrorDetail, bool) {
	var resp apiErrorResponse
	if err := json.Unmarshal(data, &resp); err == nil && resp.Error.Code != "" {
		return resp.Error, true
	}
	return apiErrorDetail{
		Code:    "unknown_error",
		Message: fmt.Sprintf("unexpected status %d", status),
	}, true
}

func formatToolError(detail apiErrorDetail) *mcp.CallToolResult {
	msg := detail.Message

	switch detail.Code {
	case "quota_exceeded":
		msg = "Monthly render limit reached. Upgrade at foliopdf.dev/upgrade to continue."
	case "rate_limited":
		msg = detail.Message + " Register for a free account for higher limits at foliopdf.dev/keys"
	}

	return errorResult(msg)
}

func errorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{
			&mcp.TextContent{Text: msg},
		},
	}
}

func textResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: msg},
		},
	}
}
