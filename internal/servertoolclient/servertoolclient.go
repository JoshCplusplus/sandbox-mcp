package servertoolclient

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// parseCommand splits a command string into command and arguments
func parseCommand(cmd string) []string {
	// This is a simple implementation that doesn't handle quotes or escapes
	// For a more robust solution, consider using a shell parser library
	var result []string
	var current string
	var inQuote bool
	var quoteChar rune

	for _, r := range cmd {
		switch {
		case r == ' ' && !inQuote:
			if current != "" {
				result = append(result, current)
				current = ""
			}
		case (r == '"' || r == '\''):
			if inQuote && r == quoteChar {
				inQuote = false
				quoteChar = 0
			} else if !inQuote {
				inQuote = true
				quoteChar = r
			} else {
				current += string(r)
			}
		default:
			current += string(r)
		}
	}

	if current != "" {
		result = append(result, current)
	}

	return result
}

func GetTools(filePath string) []mcp.Tool {

	// List available tools
	// Create a context with timeout
	var c *client.Client
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pythonCmd := "python3"
	stdioTransport := transport.NewStdio(pythonCmd, nil, filePath)

	// Create client with the transport
	c = client.NewClient(stdioTransport)

	// Start the client
	if err := c.Start(ctx); err != nil {
		log.Fatalf("Failed to start client: %v", err)
	}

	// Initialize the client
	fmt.Println("Initializing client...")
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "MCP-Go Simple Client Example",
		Version: "1.0.0",
	}
	initRequest.Params.Capabilities = mcp.ClientCapabilities{}

	serverInfo, err := c.Initialize(ctx, initRequest)
	if err != nil {
		log.Fatalf("Failed to initialize: %v", err)
	}

	// Display server information
	log.Printf("Connected to server: %s (version %s)\n",
		serverInfo.ServerInfo.Name,
		serverInfo.ServerInfo.Version)
	log.Printf("Server capabilities: %+v\n", serverInfo.Capabilities)

	// Perform health check using ping
	log.Println("Performing health check...")
	if err := c.Ping(ctx); err != nil {
		log.Fatalf("Health check failed: %v", err)
	}
	log.Println("Server is alive and responding")

	var tools []mcp.Tool
	// List available tools if the server supports them
	if serverInfo.Capabilities.Tools != nil {
		log.Println("Fetching available tools...")
		toolsRequest := mcp.ListToolsRequest{}
		toolsResult, err := c.ListTools(ctx, toolsRequest)
		if err != nil {
			log.Fatalf("Failed to list tools: %v", err)
		}
		log.Printf("Server has %d tools available\n", len(toolsResult.Tools))
		for i, tool := range toolsResult.Tools {
			log.Printf("  %d. %s - %s\n", i+1, tool.Name, tool.Description)
		}
		tools = toolsResult.Tools
	}
	log.Println("Found tools for server: ", filePath, tools)
	return tools
}
