package commands

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

func listMCPServersHandler() Handler {
	return func(ctx context.Context, req Request, rt *Runtime) error {
		if rt == nil || rt.ListMCPServers == nil {
			return req.Reply(unavailableMsg)
		}

		servers := sortedMCPServers(rt.ListMCPServers(ctx))
		if len(servers) == 0 {
			return req.Reply("No MCP servers configured.")
		}

		header := "Configured MCP servers:"
		if rt.Config != nil && !rt.Config.Tools.IsToolEnabled("mcp") {
			header = "Configured MCP servers (integration disabled):"
		}
		lines := []string{header}
		rows := make([][]string, 0, len(servers))
		for _, server := range servers {
			detail := fmt.Sprintf("enabled=%s, deferred=%s, connected=%s",
				yesNo(server.Enabled), yesNo(server.Deferred), yesNo(server.Connected))
			if server.Connected {
				detail += fmt.Sprintf(", tools=%d", server.ToolCount)
			}
			rows = append(rows, []string{server.Name, detail})
			lines = append(lines, fmt.Sprintf("- %s — %s", server.Name, detail))
		}
		return req.replyStructured(
			tableContent("MCP Inventory", inventoryHeaderColumns(), rows, strings.Join(lines, "\n")),
		)
	}
}

func showMCPToolsHandler() Handler {
	return func(ctx context.Context, req Request, rt *Runtime) error {
		if rt == nil || rt.ListMCPTools == nil {
			return req.Reply(unavailableMsg)
		}

		requested := strings.TrimSpace(nthToken(req.Text, 2))
		if requested == "" {
			return req.Reply("Usage: /show mcp <server>\nUse /list mcp for configured MCP inventory.")
		}

		server := MCPServerInfo{Name: requested}
		hasStatus := false
		if rt.ListMCPServers != nil {
			resolved, ok := findMCPServer(rt.ListMCPServers(ctx), requested)
			if !ok {
				return req.Reply(fmt.Sprintf(
					"MCP server %q is not configured. Use /list mcp to see configured servers.",
					requested,
				))
			}
			server = resolved
			hasStatus = true
		}

		rows := make([][]string, 0)
		lines := make([]string, 0)
		if hasStatus {
			rows = append(rows,
				[]string{"Name", server.Name},
				[]string{"Enabled", yesNo(server.Enabled)},
				[]string{"Deferred", yesNo(server.Deferred)},
				[]string{"Connected", yesNo(server.Connected)},
			)
			lines = append(lines,
				fmt.Sprintf("MCP server: %s", server.Name),
				fmt.Sprintf("Enabled: %s", yesNo(server.Enabled)),
				fmt.Sprintf("Deferred: %s", yesNo(server.Deferred)),
				fmt.Sprintf("Connected: %s", yesNo(server.Connected)),
			)
			if !server.Connected {
				rows = append(rows, []string{"Tools", "unavailable while disconnected"})
				lines = append(lines, "Tools: unavailable while disconnected")
				return req.replyStructured(
					tableContent("MCP Server", statusHeaderColumns(), rows, strings.Join(lines, "\n")),
				)
			}
		}

		tools, err := rt.ListMCPTools(ctx, server.Name)
		if err != nil {
			return req.Reply(UserFacingError(err, "MCP service is temporarily unavailable. Please try again."))
		}
		sort.SliceStable(tools, func(i, j int) bool {
			return strings.ToLower(tools[i].Name) < strings.ToLower(tools[j].Name)
		})
		if hasStatus {
			rows = append(rows, []string{"Active tools", fmt.Sprintf("%d", len(tools))})
			lines = append(lines, fmt.Sprintf("Active tools: %d", len(tools)), "")
		}
		lines = append(lines, fmt.Sprintf("Active MCP tools for `%s`:", server.Name))
		if len(tools) == 0 {
			lines = append(lines, "- (none)")
			if !hasStatus {
				rows = append(rows, []string{"Server", server.Name}, []string{"Active tools", "0"})
			}
			return req.replyStructured(tableContent("MCP Server", statusHeaderColumns(), rows, strings.Join(lines, "\n")))
		}

		for _, tool := range tools {
			detail := strings.TrimSpace(tool.Description)
			if detail == "" {
				detail = "No description"
			}
			rowDetail := detail
			if len(tool.Parameters) > 0 {
				rowDetail += fmt.Sprintf("; %d parameter(s)", len(tool.Parameters))
			}
			rows = append(rows, []string{"Tool: " + tool.Name, rowDetail})
			lines = append(lines, fmt.Sprintf("- `%s`", tool.Name), "  Description: "+detail)
			if len(tool.Parameters) == 0 {
				lines = append(lines, "  Parameters: none")
				continue
			}
			lines = append(lines, "  Parameters:")
			for _, param := range tool.Parameters {
				line := fmt.Sprintf("    - `%s`", param.Name)
				if param.Type != "" {
					line += " (" + param.Type
					if param.Required {
						line += ", required"
					}
					line += ")"
				} else if param.Required {
					line += " (required)"
				}
				if strings.TrimSpace(param.Description) != "" {
					line += ": " + strings.TrimSpace(param.Description)
				}
				lines = append(lines, line)
			}
		}
		return req.replyStructured(tableContent("MCP Server", statusHeaderColumns(), rows, strings.Join(lines, "\n")))
	}
}

func sortedMCPServers(servers []MCPServerInfo) []MCPServerInfo {
	out := append([]MCPServerInfo(nil), servers...)
	sort.SliceStable(out, func(i, j int) bool {
		left, right := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if left == right {
			return out[i].Name < out[j].Name
		}
		return left < right
	})
	return out
}

func findMCPServer(servers []MCPServerInfo, name string) (MCPServerInfo, bool) {
	for _, server := range servers {
		if strings.EqualFold(strings.TrimSpace(server.Name), strings.TrimSpace(name)) {
			return server, true
		}
	}
	return MCPServerInfo{}, false
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
