package agent

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/commands"
)

const mcpStatusDetailPageOffset = 1_000_000

// configureSemanticMCPDiscoveryRuntime layers the v1 semantic distinction over
// the existing typed discovery API. /list mcp remains the inventory browser;
// /show -> MCP becomes a status selector and per-server detail surface. The
// numeric selector state remains process-local inside InteractionMenu entries;
// Telegram callback_data still contains only opaque interaction tokens.
func configureSemanticMCPDiscoveryRuntime(
	rt *commands.Runtime,
	agent *AgentInstance,
	opts *processOptions,
	al *AgentLoop,
) {
	if rt == nil || agent == nil || opts == nil || al == nil || rt.DiscoveryCommand == nil {
		return
	}
	base := rt.DiscoveryCommand
	rt.DiscoveryCommand = func(
		ctx context.Context,
		req commands.DiscoveryCommandRequest,
	) (*bus.StructuredContent, error) {
		if !strings.EqualFold(strings.TrimSpace(req.Domain), "show") ||
			!strings.EqualFold(strings.TrimSpace(req.Operation), "mcp") {
			return base(ctx, req)
		}
		if target := strings.TrimSpace(req.Argument); target != "" {
			return buildMCPServerStatusByName(ctx, agent, opts, rt, target)
		}
		if req.Page >= mcpStatusDetailPageOffset {
			return buildMCPServerStatusByIndex(ctx, agent, opts, rt, req.Page-mcpStatusDetailPageOffset)
		}
		return buildMCPStatusPicker(ctx, agent, opts, rt, req.Page), nil
	}
}

func buildMCPStatusPicker(
	ctx context.Context,
	agent *AgentInstance,
	opts *processOptions,
	rt *commands.Runtime,
	page int,
) *bus.StructuredContent {
	servers := sortedMCPServers(ctx, rt)
	page, pages, start, end := discoveryPageWindow(len(servers), page)
	entries := make([]bus.InteractionEntry, 0, end-start+5)
	lines := []string{"Choose a configured MCP server to inspect status and current tools."}
	if len(servers) == 0 {
		lines = append(lines, "No MCP servers configured.")
	}
	for i := start; i < end; i++ {
		server := servers[i]
		label := "🔌 " + server.Name
		if server.Connected {
			label = "🟢 " + server.Name
		} else if !server.Enabled {
			label = "⚪ " + server.Name
		}
		entries = append(entries, bus.InteractionEntry{
			Label:  label,
			Action: "show_mcp_page",
			Value:  strconv.Itoa(mcpStatusDetailPageOffset + i),
		})
		lines = append(lines, "- "+server.Name)
	}
	entries = append(entries, discoveryPagingEntries("show_mcp_page", page, pages, "show_dashboard")...)
	return &bus.StructuredContent{
		Kind:        "mcp_status_selector",
		Title:       "MCP Status",
		Paragraphs:  []string{"Choose a configured MCP server to inspect status and current tools."},
		Fallback:    strings.Join(lines, "\n"),
		Interaction: discoveryMenu(agent, opts, page, pages, "show:mcp_status", entries),
	}
}

func buildMCPServerStatusByIndex(
	ctx context.Context,
	agent *AgentInstance,
	opts *processOptions,
	rt *commands.Runtime,
	index int,
) (*bus.StructuredContent, error) {
	servers := sortedMCPServers(ctx, rt)
	if index < 0 || index >= len(servers) {
		return nil, fmt.Errorf("MCP status selection is no longer available")
	}
	return buildMCPServerStatus(ctx, agent, opts, rt, servers[index], index)
}

func buildMCPServerStatusByName(
	ctx context.Context,
	agent *AgentInstance,
	opts *processOptions,
	rt *commands.Runtime,
	name string,
) (*bus.StructuredContent, error) {
	servers := sortedMCPServers(ctx, rt)
	for index, server := range servers {
		if strings.EqualFold(strings.TrimSpace(server.Name), strings.TrimSpace(name)) {
			return buildMCPServerStatus(ctx, agent, opts, rt, server, index)
		}
	}
	return nil, commands.NewUserError(fmt.Sprintf("MCP server %q is not configured.", strings.TrimSpace(name)))
}

func buildMCPServerStatus(
	ctx context.Context,
	agent *AgentInstance,
	opts *processOptions,
	rt *commands.Runtime,
	server commands.MCPServerInfo,
	serverIndex int,
) (*bus.StructuredContent, error) {
	toolStatus := "unavailable"
	var tools []commands.MCPToolInfo
	if server.Enabled && server.Connected && rt != nil && rt.ListMCPTools != nil {
		resolved, err := rt.ListMCPTools(ctx, server.Name)
		if err == nil {
			tools = resolved
			if len(tools) == 0 {
				toolStatus = "none"
			} else {
				toolStatus = strconv.Itoa(len(tools))
			}
		}
	} else if server.Connected {
		toolStatus = strconv.Itoa(server.ToolCount)
	}

	statusRows := [][]string{
		{"Server", server.Name},
		{"Enabled", statusYesNo(server.Enabled)},
		{"Deferred", statusYesNo(server.Deferred)},
		{"Connected", statusYesNo(server.Connected)},
		{"Current tools", toolStatus},
	}
	tables := []bus.StructuredTable{{
		Columns: bus.CardHeaderColumns(bus.CardHeaderStatus, true),
		Rows:    statusRows,
		Border:  true,
		Striped: true,
		Header:  true,
	}}

	toolRows := make([][]string, 0, len(tools))
	for _, tool := range tools {
		detail := strings.TrimSpace(tool.Description)
		if detail == "" {
			detail = "Active MCP tool"
		}
		if len(tool.Parameters) > 0 {
			detail += fmt.Sprintf(" · %d parameter(s)", len(tool.Parameters))
		}
		toolRows = append(toolRows, []string{tool.Name, detail})
	}
	if len(toolRows) > 0 {
		tables = append(tables, bus.StructuredTable{
			Columns: bus.CardHeaderColumns(bus.CardHeaderInventory, true),
			Rows:    toolRows,
			Border:  true,
			Striped: true,
			Header:  true,
		})
	}

	fallback := mcpServerStatusFallback(server, toolStatus, tools)
	pickerPage := 0
	if serverIndex >= 0 {
		pickerPage = serverIndex / discoveryPageSize
	}
	encoded := strconv.Itoa(mcpStatusDetailPageOffset + serverIndex)
	return &bus.StructuredContent{
		Kind:     "mcp_server_status",
		Title:    "MCP Server · " + server.Name,
		Tables:   tables,
		Fallback: fallback,
		Interaction: discoveryMenu(agent, opts, pickerPage, 1, "show:mcp_server", []bus.InteractionEntry{
			{Label: "🔄 Refresh", Action: "show_mcp_page", Value: encoded},
			{Label: "↩️ Back", Action: "show_mcp", Value: strconv.Itoa(pickerPage)},
			{Label: "✖️ Close", Action: "close"},
		}),
	}, nil
}

func mcpServerStatusFallback(
	server commands.MCPServerInfo,
	toolStatus string,
	tools []commands.MCPToolInfo,
) string {
	lines := []string{
		"MCP Server: " + server.Name,
		bus.CardHeader(bus.CardHeaderStatus, false),
		"Server: " + server.Name,
		"Enabled: " + statusYesNo(server.Enabled),
		"Deferred: " + statusYesNo(server.Deferred),
		"Connected: " + statusYesNo(server.Connected),
		"Current tools: " + toolStatus,
	}
	if len(tools) > 0 {
		lines = append(lines, "", bus.CardHeader(bus.CardHeaderInventory, false))
		for _, tool := range tools {
			detail := strings.TrimSpace(tool.Description)
			if detail == "" {
				detail = "Active MCP tool"
			}
			lines = append(lines, tool.Name+": "+detail)
		}
	}
	return strings.Join(lines, "\n")
}
