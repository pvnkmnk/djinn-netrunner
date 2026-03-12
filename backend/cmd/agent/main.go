package main

import (
	"fmt"
	"os"

	"github.com/mark3labs/mcp-go/server"
)

func main() {
	// Create a new MCP server
	s := server.NewMCPServer(
		"NetRunner Agent Interface",
		"1.0.0",
	)

	// In the future, we will register tools here
	// s.AddTool(server.NewTool("probe_system", ...))

	// Run the server on stdio
	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintf(os.Stderr, "Error serving MCP: %v\n", err)
		os.Exit(1)
	}
}
