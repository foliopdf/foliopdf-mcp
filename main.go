package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const mcpVersion = "0.2.0"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("foliopdf-mcp %s\n", mcpVersion)
		return
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	server := buildServer(logger)
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		logger.Error("server exited", "error", err)
		os.Exit(1)
	}
}
