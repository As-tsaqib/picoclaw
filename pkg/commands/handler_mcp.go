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
			detail := fmt.Sprintf("enabled=%s, deferred=%s, connected=%s", yesNo(server.Enabled), yesNo(server.Deferred), yesNo(server.Connected))
			if server.Connected {
				detail += fmt.Sprintf(", tools=%d", server.ToolCount)
			}
			rows = append(rows, []string{server.Name, detail})
			lines = append(lines, fmt.Sprintf("- %s — %s", server.Name, detail))
		}
		return req.replyStructured(tableContent("MCP Inventory", inventoryHeaderColumns(), rows, strings.Join(lines, "\n")))
	}
}

func showMCPToolsHandler() Handler {
	return func(ctx context.Context, req Request, rt *Runtime) error {
		if rt == nil || rt.ListMCPServers == nil || rt.ListMCPTools == nil {
			return req.Reply(unavailableMsg)
		}

		requested := strings.TrimSpace(nthToken(req.Text, 2))
		if requested == "" {
			return req.Reply("Usage: /show mcp <server>\nUse /list mcp for configured MCP inventory.")
		}
		server, ok := findMCPServer(rt.ListMCPServers(ctx), requested)
		if !ok {
			return req.Reply(fmt.Sprintf("MCP server %q is not configured. Use /list mcp to see configured servers.", requested))
		}

		rows := [][]string{
			{"Name", server.Name},
			{"Enabled", yesNo(server.Enabled)},
			{"Deferred", yesNo(server.Deferred)},
			{"Connected", yesNo(server.Connected)},
		}
		lines := []string{
			fmt.Sprintf("MCP server: %s", server.Name),
			fmt.Sprintf("Enabled: %s", yesNo(server.Enabled)),
			fmt.Sprintf("Deferred: %s", yesNo(server.Deferred)),
			fmt.Sprintf("Connected: %s", yesNo(server.Connected)),
		}
		if !server.Connected {
			rows = append(rows, []string{"Tools", "unavailable while disconnected"})
			lines = append(lines, "Tools: unavailable while disconnected")
			return req.replyStructured(tableContent("MCP Server", statusHeaderColumns(), rows, strings.Join(lines, "\n")))
		}

		tools, err := rt.ListMCPTools(ctx, server.Name)
		if err != nil {
			return req.Reply(UserFacingError(err, "MCP service is temporarily unavailable. Please try again."))
		}
		rows = append(rows, []string{"Active tools", fmt.Sprintf("%d", len(tools))})
		lines = append(lines, fmt.Sprintf("Active tools: %d", len(tools)))
		if len(tools) == 0 {
			lines = append(lines, "Tools: none")
			return req.replyStructured(tableContent("MCP Server", statusHeaderColumns(), rows, strings.Join(lines, "\n")))
		}
		sort.SliceStable(tools, func(i, j int) bool { return strings.ToLower(tools[i].Name) < strings.ToLower(tools[j].Name) })
		for _, tool := range tools {
			detail := strings.TrimSpace(tool.Description)
			if detail == "" {
				detail = "No description"
			}
			if len(tool.Parameters) > 0 {
				detail += fmt.Sprintf("; %d parameter(s)", len(tool.Parameters))
			}
			rows = append(rows, []string{"Tool: " + tool.Name, detail})
			lines = append(lines, fmt.Sprintf("- %s — %s", tool.Name, detail))
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
