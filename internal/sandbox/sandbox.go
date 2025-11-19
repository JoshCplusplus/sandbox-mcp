package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/pottekkat/sandbox-mcp/internal/config"
)

// waitForContainer waits for a container to be in running state with a specified timeout
func waitForContainer(ctx context.Context, cli *client.Client, containerID string, timeout time.Duration) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	timeoutCh := time.After(timeout)

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for container to start")
		case <-timeoutCh:
			return fmt.Errorf("container did not reach running state within %v", timeout)
		case <-ticker.C:
			inspect, err := cli.ContainerInspect(ctx, containerID)
			if err != nil {
				return fmt.Errorf("failed to inspect container: %v", err)
			}
			if inspect.State != nil && inspect.State.Running {
				return nil
			}
		}
	}
}

func waitForContainerRunning(ctx context.Context, cli *client.Client, containerID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for {
		// Inspect container
		inspect, err := cli.ContainerInspect(ctx, containerID)
		if err != nil {
			return fmt.Errorf("failed to inspect container: %v", err)
		}

		if inspect.State != nil && inspect.State.Running {
			return nil // container is running
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for container %s to start", containerID)
		}

		time.Sleep(200 * time.Millisecond) // wait a bit before retry
	}
}

func waitForPort(ctx context.Context, host string, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	address := fmt.Sprintf("%s:%d", host, port)

	for {
		conn, err := net.DialTimeout("tcp", address, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil // port is open, service ready
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for port %s", address)
		}

		time.Sleep(200 * time.Millisecond)
	}
}

// NewSandboxTool creates a sandbox tool from a config
func NewSandboxTool(sandboxConfig *config.SandboxConfig) mcp.Tool {
	options := []mcp.ToolOption{
		// All tools have a description and an entrypoint
		mcp.WithDescription(generateSandboxDescription(sandboxConfig)),
		withEntrypoint(sandboxConfig.ParamEntrypoint(), fmt.Sprintf("Code to be stored in a file named `%s` and executed with the command `%s`.",
			sandboxConfig.Entrypoint,
			strings.Join(sandboxConfig.Command, " "))),

		mcp.WithTitleAnnotation(sandboxConfig.Name()),
		mcp.WithReadOnlyHintAnnotation(sandboxConfig.Hints.IsReadOnly(sandboxConfig.Mount.ReadOnly, sandboxConfig.Security.ReadOnly)),
		mcp.WithDestructiveHintAnnotation(sandboxConfig.Hints.IsDestructive()),
		mcp.WithIdempotentHintAnnotation(sandboxConfig.Hints.IsIdempotent()),
		mcp.WithOpenWorldHintAnnotation(sandboxConfig.Hints.IsExternalInteraction(sandboxConfig.Security.Network)),
	}

	// Add any specific additional files if provided in the config
	for _, file := range sandboxConfig.Parameters.Files {
		options = append(options, withFile(file.ParamName(), file.Description, true))
	}

	// Allow adding more files if enabled
	if sandboxConfig.Parameters.AdditionalFiles {
		options = append(options, withAdditionalFiles())
	}

	// Return a new tool with the tool name and provided options
	return mcp.NewTool(sandboxConfig.Id, options...)
}

// NewSandboxToolHandler creates a handler function for a sandbox tool
func NewSandboxToolHandler(sandboxConfig *config.SandboxConfig, tool mcp.Tool, serverFilePath string, clientFile []byte) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Return the handler function that will be run when the tool is called
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {

		serverFile, err := os.ReadFile(serverFilePath)
		if err != nil {
			log.Printf("Error reading server file: %v\n", err)
			return nil, fmt.Errorf("failed to read server file")
		}

		log.Println(request.Params.Arguments)

		// withEntrypoint ToolOption
		// Get the contents of the entrypoint file from the request
		/*
			entrypointFile := config.SandboxFile{Name: sandboxConfig.Entrypoint}
			entrypointParam := entrypointFile.ParamName()
			entrypointContent, ok := request.Params.Arguments[entrypointParam].(string)
			if !ok || entrypointContent == "" {
				return nil, fmt.Errorf("%s file is required", sandboxConfig.Entrypoint)
			}
		*/
		// Create a temporary directory for the entrypoint file
		dir, err := os.MkdirTemp("", sandboxConfig.Mount.TmpDirPrefix)
		if err != nil {
			return nil, fmt.Errorf("failed to create a temporary directory: %v", err)
		}
		defer os.RemoveAll(dir)

		log.Println("Directory made at " + dir)

		/*
			// Write the entrypoint to script file in the temp directory
			cmdFile := filepath.Join(dir, sandboxConfig.Entrypoint)
			if err := os.WriteFile(cmdFile, []byte(entrypointContent), sandboxConfig.Mount.ScriptPerms()); err != nil {
				return nil, fmt.Errorf("failed to write command file: %v", err)
			}
		*/

		dockerServerFilePath := filepath.Join(dir, "server.py")
		if err := os.WriteFile(dockerServerFilePath, []byte(serverFile), sandboxConfig.Mount.ScriptPerms()); err != nil {
			return nil, fmt.Errorf("failed to write server file: %v", err)
		}

		clientFilePath := filepath.Join(dir, "client.py")
		if err := os.WriteFile(clientFilePath, []byte(clientFile), sandboxConfig.Mount.ScriptPerms()); err != nil {
			return nil, fmt.Errorf("failed to write client file: %v", err)
		}

		// Read the directory contents
		entries, err := os.ReadDir(dir)
		if err != nil {
			log.Fatalf("Error reading directory: %v", err)
		}

		// Iterate through the entries and print file/directory names
		log.Printf("Contents of directory '%s':\n", dir)
		for _, entry := range entries {
			log.Println(entry.Name())
		}

		// withFile ToolOption
		// Get the contents of the required files from the request
		for _, file := range sandboxConfig.Parameters.Files {
			paramName := file.ParamName()
			content, ok := request.Params.Arguments[paramName].(string)
			if !ok || content == "" {
				return nil, fmt.Errorf("%s file is required", file.Name)
			}

			filePath := filepath.Join(dir, file.Name)
			if err := os.WriteFile(filePath, []byte(content), sandboxConfig.Mount.ScriptPerms()); err != nil {
				return nil, fmt.Errorf("failed to write file %s: %v", file.Name, err)
			}
		}

		// withAdditionalFiles ToolOption
		// Handle additional files if provided
		if files, ok := request.Params.Arguments["files"].([]any); ok {
			for _, file := range files {
				if fileMap, ok := file.(map[string]any); ok {
					filename := fileMap["filename"].(string)
					content := fileMap["content"].(string)

					filePath := filepath.Join(dir, filename)
					if err := os.WriteFile(filePath, []byte(content), sandboxConfig.Mount.ScriptPerms()); err != nil {
						return nil, fmt.Errorf("failed to write file %s: %v", filename, err)
					}
				}
			}
		}

		// Initialize Docker client
		cli, err := client.NewClientWithOpts(
			// Let the client be configured through environment variables
			client.FromEnv,
			// Try to support whatever version of the daemon is available
			client.WithAPIVersionNegotiation(),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create Docker client: %v", err)
		}
		defer cli.Close()

		// ----------------------------------------------
		// 1. Ensure Docker network exists (privoxy-net)
		// ----------------------------------------------
		networkName := "privoxy-net"

		_, err = cli.NetworkInspect(ctx, networkName, network.InspectOptions{})
		if err != nil {
			// network does not exist → create it
			_, err = cli.NetworkCreate(ctx, networkName, network.CreateOptions{
				Driver: "bridge",
			})
			if err != nil {
				return nil, fmt.Errorf("failed to create network %s: %v", networkName, err)
			}
		}

		// ----------------------------------------------------
		// 2. Start Privoxy container (if not already running)
		// ----------------------------------------------------
		privoxyName := "privoxy-proxy"
		privoxyImage := "vimagick/privoxy:latest"

		// Pull the image
		_, err = cli.ImagePull(ctx, privoxyImage, image.PullOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to pull privoxy image: %v", err)
		}

		// Define Privoxy container config
		privoxyConfig := &container.Config{
			Image: privoxyImage,
			ExposedPorts: nat.PortSet{
				"8118/tcp": {},
			},
		}

		// Attach to custom network
		privoxyHostConfig := &container.HostConfig{
			NetworkMode: container.NetworkMode(networkName),
			Mounts: []mount.Mount{
				{
					Type:     mount.TypeBind,
					Source:   "/Users/jc/UCSD/fa25/227/sandbox-mcp/user.action",
					Target:   "/etc/privoxy/user.action",
					ReadOnly: true,
				},
				{
					Type:     mount.TypeBind,
					Source:   "/Users/jc/UCSD/fa25/227/sandbox-mcp/config",
					Target:   "/etc/privoxy/config",
					ReadOnly: true,
				},
			},
		}

		// Create container
		privoxyResp, err := cli.ContainerCreate(ctx, privoxyConfig, privoxyHostConfig, nil, nil, privoxyName)
		if err != nil {
			return nil, fmt.Errorf("failed to create privoxy container: %v", err)
		}

		defer func() {
			killCtx, killCancel := context.WithTimeout(context.Background(), sandboxConfig.Timeout())
			defer killCancel()

			_ = cli.ContainerRemove(killCtx, privoxyResp.ID, container.RemoveOptions{
				Force:         true,
				RemoveVolumes: true,
			})
		}()

		// Start container
		err = cli.ContainerStart(ctx, privoxyResp.ID, container.StartOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to start privoxy container: %v", err)
		}

		// // Wait for container to be running
		// err = waitForContainerRunning(ctx, cli, privoxyResp.ID, 10*time.Second)
		// if err != nil {
		// 	return nil, err
		// }

		// // Get container IP
		// inspect, _ := cli.ContainerInspect(ctx, privoxyResp.ID)
		// ip := inspect.NetworkSettings.Networks[networkName].IPAddress

		// // Wait for Privoxy to accept connections
		// err = waitForPort(ctx, ip, 8118, 5*time.Minute)
		// if err != nil {
		// 	return nil, err
		// }

		//time.Sleep(30 * time.Second)
		log.Println("Config to be run is ", sandboxConfig)

		newCmd := []string{tool.Name}
		commandToRun := append(sandboxConfig.RunCommand(), tool.Name)
		if len(request.Params.Arguments) > 0 {
			jsonBytes, err := json.Marshal(request.Params.Arguments)
			if err != nil {
				log.Fatal(err)
			}
			commandToRun = append(commandToRun, string(jsonBytes))
			newCmd = append(newCmd, string(jsonBytes))
		}

		log.Println("Command to run is ", commandToRun)
		// Create container config
		containerConfig := &container.Config{
			Image:      sandboxConfig.Image,
			Cmd:        newCmd,
			WorkingDir: sandboxConfig.Mount.WorkDir,
			Tty:        sandboxConfig.Tty(),
			Env: []string{
				"HTTP_PROXY=http://privoxy-proxy:8118",
				"HTTPS_PROXY=http://privoxy-proxy:8118",
				"http_proxy=http://privoxy-proxy:8118",
				"https_proxy=http://privoxy-proxy:8118",
				"NO_PROXY=localhost,127.0.0.1",
			},
		}

		// Create host config
		hostConfig := &container.HostConfig{
			Resources: container.Resources{
				Memory:    sandboxConfig.Resources.Memory * 1024 * 1024,
				NanoCPUs:  int64(sandboxConfig.Resources.CPU * 1e9),
				PidsLimit: &sandboxConfig.Resources.Processes,
				Ulimits: []*container.Ulimit{
					{
						Name: "nofile",
						Soft: sandboxConfig.Resources.Files,
						Hard: sandboxConfig.Resources.Files,
					},
				},
			},
			NetworkMode:    container.NetworkMode(networkName),
			ReadonlyRootfs: sandboxConfig.Security.ReadOnly,
			Mounts: []mount.Mount{
				{
					Type:     mount.TypeBind,
					Source:   dir,
					Target:   sandboxConfig.Mount.WorkDir,
					ReadOnly: sandboxConfig.Mount.ReadOnly,
				},
			},
			CapDrop:     sandboxConfig.Security.CapDrop,
			SecurityOpt: sandboxConfig.Security.SecurityOpt,
			CapAdd:      []string{"NET_ADMIN"},
			Privileged:  true,
		}

		// Create execution context with timeout
		execCtx, cancel := context.WithTimeout(ctx, sandboxConfig.Timeout())
		defer cancel()

		// Create container
		resp, err := cli.ContainerCreate(execCtx, containerConfig, hostConfig, nil, nil, "")
		if err != nil {
			return nil, fmt.Errorf("failed to create container: %v", err)
		}

		// // Ensure container cleanup
		defer func() {
			killCtx, killCancel := context.WithTimeout(context.Background(), sandboxConfig.Timeout())
			defer killCancel()

			_ = cli.ContainerRemove(killCtx, resp.ID, container.RemoveOptions{
				Force:         true,
				RemoveVolumes: true,
			})
		}()

		// Start the container
		if err := cli.ContainerStart(execCtx, resp.ID, container.StartOptions{}); err != nil {
			return nil, fmt.Errorf("failed to start container: %v", err)
		}

		// Only exec Command if Before was used to start the container
		if sandboxConfig.ExecCommand() != nil {

			// Wait for container to be running
			if err := waitForContainer(execCtx, cli, resp.ID, 10*time.Second); err != nil {
				return nil, err
			}

			execConfig := container.ExecOptions{
				Cmd:          sandboxConfig.Command,
				AttachStdout: true,
				AttachStderr: true,
				User:         sandboxConfig.User,
			}

			execResp, err := cli.ContainerExecCreate(execCtx, resp.ID, execConfig)
			if err != nil {
				return nil, fmt.Errorf("failed to create exec: %v", err)
			}

			// Attach to the exec command to capture output
			response, err := cli.ContainerExecAttach(execCtx, execResp.ID, container.ExecStartOptions{})
			if err != nil {
				return nil, fmt.Errorf("failed to attach to exec: %v", err)
			}
			defer response.Close()

			// Read stdout and stderr from the exec command
			stdout := new(bytes.Buffer)
			stderr := new(bytes.Buffer)
			if _, err := stdcopy.StdCopy(stdout, stderr, response.Reader); err != nil {
				return nil, fmt.Errorf("failed to read exec output: %v", err)
			}

			// Wait for the exec command to complete
			for {
				inspectResp, err := cli.ContainerExecInspect(execCtx, execResp.ID)
				if err != nil {
					return nil, fmt.Errorf("failed to inspect exec: %v", err)
				}
				if !inspectResp.Running {
					// Return error if exec command failed
					if inspectResp.ExitCode != 0 {
						if stderr.Len() > 0 {
							return mcp.NewToolResultError(stderr.String()), nil
						}
						return mcp.NewToolResultError(fmt.Sprintf("Command failed with exit code %d", inspectResp.ExitCode)), nil
					}

					// Include stderr in stdout if present
					if stderr.Len() > 0 {
						stdout.WriteString("\nStderr:\n")
						stdout.Write(stderr.Bytes())
					}

					return mcp.NewToolResultText(stdout.String()), nil
				}
				time.Sleep(100 * time.Millisecond)
			}
		}

		// Wait for execution to finish
		statusCh, errCh := cli.ContainerWait(execCtx, resp.ID, container.WaitConditionNotRunning)
		select {
		case err := <-errCh:
			if err != nil {
				return nil, fmt.Errorf("error waiting for container: %v", err)
			}
		case status := <-statusCh:
			// Get container logs
			logs, err := cli.ContainerLogs(execCtx, resp.ID, container.LogsOptions{
				ShowStdout: true,
				ShowStderr: true,
				Timestamps: false,
				Follow:     false,
				Tail:       "1",
			})
			log.Println("Status returned was ", status)
			if err != nil {
				return nil, fmt.Errorf("failed to get logs: %v", err)
			}
			defer logs.Close()

			output, _ := io.ReadAll(logs)
			log.Println("output was ", string(output))

			// Read stdout and stderr
			stdout := new(bytes.Buffer)
			stderr := new(bytes.Buffer)
			if _, err := stdcopy.StdCopy(stdout, stderr, logs); err != nil {
				log.Println("Encountered error when reading logs: ", err)
				return nil, fmt.Errorf("failed to read logs: %v", err)
			}
			log.Println("Statuscode returned was ", status.StatusCode)
			// Return error if command failed
			if status.StatusCode != 0 {
				return mcp.NewToolResultError(stderr.String()), nil
			}

			// Include stderr in stdout if present
			if stderr.Len() > 0 {
				stdout.WriteString("\nStderr:\n")
				stdout.Write(stderr.Bytes())
			}
			log.Println("Stdout received was ", stdout.String())
			return mcp.NewToolResultText(string(output)), nil
		case <-execCtx.Done():
			return nil, fmt.Errorf("execution timeout after %d seconds", int(sandboxConfig.Timeout().Seconds()))
		}

		return nil, fmt.Errorf("unexpected error: container wait returned no result")
	}
}

// generateSandboxDescription creates a comprehensive description of the sandbox environment
func generateSandboxDescription(sandboxConfig *config.SandboxConfig) string {
	// Start with the base description from the config
	description := sandboxConfig.Description

	// Ensure the base description ends with a period if it doesn't already
	if !strings.HasSuffix(description, ".") {
		description += "."
	}

	// Add a space after the description
	description += " "

	// Create a more natural description of the sandbox environment with inline pluralization
	coreText := "cores"
	if sandboxConfig.Resources.CPU == 1 {
		coreText = "core"
	}

	description += fmt.Sprintf("This sandbox uses the `%s` Docker image, with %d CPU %s, %d MB RAM, and %d processes.",
		sandboxConfig.Image,
		sandboxConfig.Resources.CPU,
		coreText,
		sandboxConfig.Resources.Memory,
		sandboxConfig.Resources.Processes)

	// Add network and filesystem information
	if sandboxConfig.Security.Network == "none" {
		description += " It has no network access"
	} else {
		description += fmt.Sprintf(" It has %s network access", sandboxConfig.Security.Network)
	}

	if sandboxConfig.Mount.ReadOnly || sandboxConfig.Security.ReadOnly {
		description += " and read-only filesystem permissions."
	} else {
		description += " and read-write filesystem permissions."
	}

	// Add information about required files
	if len(sandboxConfig.Parameters.Files) > 0 {
		if len(sandboxConfig.Parameters.Files) == 1 {
			file := sandboxConfig.Parameters.Files[0]
			description += fmt.Sprintf(" It requires a `%s` file", file.Name)
			if file.Description != "" {
				description += fmt.Sprintf(" (%s)", file.Description)
			}
		} else {
			description += " It requires the following files:"
			for i, file := range sandboxConfig.Parameters.Files {
				if i > 0 {
					if i == len(sandboxConfig.Parameters.Files)-1 {
						description += " and"
					} else {
						description += ","
					}
				}
				description += fmt.Sprintf(" `%s`", file.Name)
				if file.Description != "" {
					description += fmt.Sprintf(" (%s)", file.Description)
				}
			}
		}

		if sandboxConfig.Parameters.AdditionalFiles {
			description += " and supports uploading additional files."
		} else {
			description += "."
		}
	} else if sandboxConfig.Parameters.AdditionalFiles {
		description += " It supports uploading additional files."
	}

	// Add timeout information
	description += fmt.Sprintf(" The execution is limited to %d seconds.", sandboxConfig.TimeoutRaw)

	return description
}
