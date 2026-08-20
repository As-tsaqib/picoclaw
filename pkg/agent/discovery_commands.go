package agent

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/commands"
)

const discoveryPageSize = 5

func configureDiscoveryCommandRuntime(
	rt *commands.Runtime,
	agent *AgentInstance,
	opts *processOptions,
	al *AgentLoop,
) {
	if rt == nil || agent == nil || opts == nil || al == nil {
		return
	}
	rt.CheckChannel = al.checkChannelStatus
	rt.DiscoveryCommand = func(
		ctx context.Context,
		req commands.DiscoveryCommandRequest,
	) (*bus.StructuredContent, error) {
		return al.executeDiscoveryCommand(ctx, agent, opts, rt, req)
	}
}

func (al *AgentLoop) executeDiscoveryCommand(
	ctx context.Context,
	agent *AgentInstance,
	opts *processOptions,
	rt *commands.Runtime,
	req commands.DiscoveryCommandRequest,
) (*bus.StructuredContent, error) {
	normalizeProcessOptionsInPlace(opts)
	if agent == nil || opts == nil || rt == nil || opts.Dispatch.SessionScope == nil ||
		strings.TrimSpace(opts.Dispatch.SessionKey) == "" || opts.Dispatch.InboundContext == nil {
		return nil, fmt.Errorf("discovery interaction context is unavailable")
	}

	domain := strings.ToLower(strings.TrimSpace(req.Domain))
	operation := strings.ToLower(strings.TrimSpace(req.Operation))
	switch domain {
	case "show":
		switch operation {
		case "", "dashboard":
			return al.buildShowDashboard(ctx, agent, opts, rt)
		case "model":
			return al.buildShowModel(ctx, agent, opts, rt)
		case "channel":
			return al.buildShowChannel(agent, opts), nil
		case "agents":
			return al.buildAgentsDiscovery(agent, opts, rt, req.Page, "show"), nil
		case "mcp":
			return al.buildMCPDiscovery(ctx, agent, opts, rt, req.Page, "show"), nil
		default:
			return nil, fmt.Errorf("unsupported show operation")
		}
	case "list":
		switch operation {
		case "", "dashboard":
			return al.buildListDashboard(agent, opts), nil
		case "models":
			return delegateModelCatalog(ctx, rt)
		case "skills":
			return delegateSkillCatalog(ctx, rt)
		case "channels":
			return al.buildChannelsDiscovery(agent, opts, rt, req.Page), nil
		case "agents":
			return al.buildAgentsDiscovery(agent, opts, rt, req.Page, "list"), nil
		case "mcp":
			return al.buildMCPDiscovery(ctx, agent, opts, rt, req.Page, "list"), nil
		default:
			return nil, fmt.Errorf("unsupported list operation")
		}
	case "check":
		if operation != "channel" {
			return nil, fmt.Errorf("unsupported check operation")
		}
		return al.buildChannelCheck(agent, opts, rt, req.Argument)
	default:
		return nil, fmt.Errorf("unsupported discovery domain")
	}
}

func (al *AgentLoop) buildShowDashboard(
	ctx context.Context,
	agent *AgentInstance,
	opts *processOptions,
	rt *commands.Runtime,
) (*bus.StructuredContent, error) {
	model, provider, err := currentModelSummary(ctx, rt)
	if err != nil {
		return nil, err
	}
	servers := sortedMCPServers(ctx, rt)
	connected := 0
	for _, server := range servers {
		if server.Connected {
			connected++
		}
	}
	channel := strings.TrimSpace(opts.Dispatch.InboundContext.Channel)
	rows := [][]string{
		{"Model", fallbackDash(model)},
		{"Provider", fallbackDash(provider)},
		{"Channel", fallbackDash(channel)},
		{"Agent", fallbackDash(agent.ID)},
		{"MCP", fmt.Sprintf("%d configured / %d connected", len(servers), connected)},
	}
	entries := []bus.InteractionEntry{
		{Label: "🤖 Model", Action: "show_model"},
		{Label: "📡 Channel", Action: "show_channel"},
		{Label: "🧠 Agents", Action: "show_agents"},
		{Label: "🔌 MCP", Action: "show_mcp"},
		{Label: "🔄 Refresh", Action: "show_dashboard"},
		{Label: "✖️ Close", Action: "close"},
	}
	return discoveryTableContent(
		"current_state",
		"Current State",
		[]string{"Properti", "Nilai"},
		rows,
		discoveryRowsFallback("Current State", rows),
		discoveryMenu(agent, opts, 0, 1, "show:dashboard", entries),
	), nil
}

func (al *AgentLoop) buildShowModel(
	ctx context.Context,
	agent *AgentInstance,
	opts *processOptions,
	rt *commands.Runtime,
) (*bus.StructuredContent, error) {
	if rt.ModelCommand == nil {
		return nil, fmt.Errorf("model semantics are unavailable")
	}
	content, err := rt.ModelCommand(ctx, commands.ModelCommandRequest{Operation: "current"})
	if err != nil {
		return nil, err
	}
	if content == nil {
		return nil, fmt.Errorf("model semantics returned no content")
	}
	content = content.Clone()
	content.Interaction = discoveryMenu(agent, opts, 0, 1, "show:model", []bus.InteractionEntry{
		{Label: "🔄 Refresh", Action: "show_model"},
		{Label: "↩️ Back", Action: "show_dashboard"},
		{Label: "✖️ Close", Action: "close"},
	})
	return content, nil
}

func (al *AgentLoop) buildShowChannel(agent *AgentInstance, opts *processOptions) *bus.StructuredContent {
	channel := strings.TrimSpace(opts.Dispatch.InboundContext.Channel)
	rows := [][]string{{"Current Channel", fallbackDash(channel)}}
	return discoveryTableContent(
		"current_channel",
		"Current Channel",
		[]string{"Properti", "Nilai"},
		rows,
		discoveryRowsFallback("Current Channel", rows),
		discoveryMenu(agent, opts, 0, 1, "show:channel", []bus.InteractionEntry{
			{Label: "🔄 Refresh", Action: "show_channel"},
			{Label: "↩️ Back", Action: "show_dashboard"},
			{Label: "✖️ Close", Action: "close"},
		}),
	)
}

func (al *AgentLoop) buildListDashboard(agent *AgentInstance, opts *processOptions) *bus.StructuredContent {
	entries := []bus.InteractionEntry{
		{Label: "🤖 Models", Action: "list_models"},
		{Label: "📡 Channels", Action: "list_channels"},
		{Label: "🧠 Agents", Action: "list_agents"},
		{Label: "🧩 Skills", Action: "list_skills"},
		{Label: "🔌 MCP", Action: "list_mcp"},
		{Label: "✖️ Close", Action: "close"},
	}
	fallback := strings.Join([]string{
		"Browse Resources",
		"- Models",
		"- Channels",
		"- Agents",
		"- Skills",
		"- MCP",
		"Commands: /list models · /list channels · /list agents · /list skills · /list mcp",
	}, "\n")
	return &bus.StructuredContent{
		Kind:        "resource_browser",
		Title:       "Browse Resources",
		Fallback:    fallback,
		Interaction: discoveryMenu(agent, opts, 0, 1, "list:dashboard", entries),
	}
}

func delegateModelCatalog(ctx context.Context, rt *commands.Runtime) (*bus.StructuredContent, error) {
	if rt == nil || rt.ModelCommand == nil {
		return nil, fmt.Errorf("model catalog is unavailable")
	}
	content, err := rt.ModelCommand(ctx, commands.ModelCommandRequest{Operation: "list"})
	if err != nil {
		return nil, err
	}
	if content == nil {
		return nil, fmt.Errorf("model catalog returned no content")
	}
	return content, nil
}

func delegateSkillCatalog(ctx context.Context, rt *commands.Runtime) (*bus.StructuredContent, error) {
	if rt == nil || rt.SkillCommand == nil {
		return nil, fmt.Errorf("skill catalog is unavailable")
	}
	content, err := rt.SkillCommand(ctx, commands.SkillCommandRequest{Operation: "dashboard"})
	if err != nil {
		return nil, err
	}
	if content == nil {
		return nil, fmt.Errorf("skill catalog returned no content")
	}
	return content, nil
}

func (al *AgentLoop) buildChannelsDiscovery(
	agent *AgentInstance,
	opts *processOptions,
	rt *commands.Runtime,
	page int,
) *bus.StructuredContent {
	channels := sortedStrings(runtimeChannels(rt))
	page, pages, start, end := discoveryPageWindow(len(channels), page)
	rows := make([][]string, 0, end-start)
	for _, name := range channels[start:end] {
		rows = append(rows, []string{name, "Enabled"})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"—", "No enabled channels"})
	}
	entries := discoveryPagingEntries("list_channels_page", page, pages, "list_dashboard")
	return discoveryTableContent(
		"channel_inventory",
		"Channels",
		[]string{"Channel", "Status"},
		rows,
		discoveryRowsFallback("Channels", rows),
		discoveryMenu(agent, opts, page, pages, "list:channels", entries),
	)
}

func (al *AgentLoop) buildAgentsDiscovery(
	agent *AgentInstance,
	opts *processOptions,
	rt *commands.Runtime,
	page int,
	domain string,
) *bus.StructuredContent {
	agents := sortedStrings(runtimeAgents(rt))
	page, pages, start, end := discoveryPageWindow(len(agents), page)
	rows := make([][]string, 0, end-start)
	for _, id := range agents[start:end] {
		status := "Registered"
		if strings.EqualFold(id, agent.ID) {
			status = "Current"
		}
		rows = append(rows, []string{id, status})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"—", "No registered agents"})
	}
	pageAction := "list_agents_page"
	backAction := "list_dashboard"
	current := "list:agents"
	if domain == "show" {
		pageAction = "show_agents_page"
		backAction = "show_dashboard"
		current = "show:agents"
	}
	entries := discoveryPagingEntries(pageAction, page, pages, backAction)
	if domain == "show" {
		entries = append([]bus.InteractionEntry{
			{Label: "🔄 Refresh", Action: "show_agents", Value: strconv.Itoa(page)},
		}, entries...)
	}
	return discoveryTableContent(
		"agent_inventory",
		"Agents",
		[]string{"Agent", "Status"},
		rows,
		discoveryRowsFallback("Agents", rows),
		discoveryMenu(agent, opts, page, pages, current, entries),
	)
}

func (al *AgentLoop) buildMCPDiscovery(
	ctx context.Context,
	agent *AgentInstance,
	opts *processOptions,
	rt *commands.Runtime,
	page int,
	domain string,
) *bus.StructuredContent {
	servers := sortedMCPServers(ctx, rt)
	page, pages, start, end := discoveryPageWindow(len(servers), page)
	rows := make([][]string, 0, end-start)
	for _, server := range servers[start:end] {
		rows = append(rows, []string{
			server.Name,
			statusYesNo(server.Enabled),
			statusYesNo(server.Deferred),
			statusYesNo(server.Connected),
			strconv.Itoa(server.ToolCount),
		})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"—", "no", "no", "no", "0"})
	}
	pageAction := "list_mcp_page"
	backAction := "list_dashboard"
	current := "list:mcp"
	if domain == "show" {
		pageAction = "show_mcp_page"
		backAction = "show_dashboard"
		current = "show:mcp"
	}
	entries := discoveryPagingEntries(pageAction, page, pages, backAction)
	if domain == "show" {
		entries = append([]bus.InteractionEntry{
			{Label: "🔄 Refresh", Action: "show_mcp", Value: strconv.Itoa(page)},
		}, entries...)
	}
	return discoveryTableContent(
		"mcp_inventory",
		"MCP",
		[]string{"Server", "Enabled", "Deferred", "Connected", "Tools"},
		rows,
		discoveryRowsFallback("MCP", rows),
		discoveryMenu(agent, opts, page, pages, current, entries),
	)
}

func (al *AgentLoop) buildChannelCheck(
	agent *AgentInstance,
	opts *processOptions,
	rt *commands.Runtime,
	target string,
) (*bus.StructuredContent, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, fmt.Errorf("channel name is required")
	}
	if rt == nil || rt.CheckChannel == nil {
		return nil, fmt.Errorf("channel status semantics are unavailable")
	}
	status, err := rt.CheckChannel(target)
	if err != nil {
		return nil, err
	}
	rows := [][]string{
		{"Channel", status.Name},
		{"Enabled", statusYesNo(status.Enabled)},
		{"Available", statusYesNo(status.Available)},
	}
	if reason := strings.TrimSpace(status.Reason); reason != "" {
		rows = append(rows, []string{"Reason", reason})
	}
	return discoveryTableContent(
		"channel_status",
		"Channel Status",
		[]string{"Properti", "Nilai"},
		rows,
		discoveryRowsFallback("Channel Status", rows),
		discoveryMenu(agent, opts, 0, 1, "check:channel", []bus.InteractionEntry{
			{Label: "🔄 Refresh", Action: "check_channel_refresh", Value: status.Name},
			{Label: "✖️ Close", Action: "close"},
		}),
	), nil
}

// checkChannelStatus is deliberately read-only. It never calls channel switch
// semantics and treats expected disabled/unavailable states as status data.
func (al *AgentLoop) checkChannelStatus(name string) (commands.ChannelStatus, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return commands.ChannelStatus{}, fmt.Errorf("channel name is required")
	}
	if strings.EqualFold(name, "cli") {
		return commands.ChannelStatus{
			Name: "cli", Enabled: true, Available: true, Reason: "built-in CLI channel",
		}, nil
	}
	if al == nil || al.channelManager == nil {
		return commands.ChannelStatus{}, fmt.Errorf("channel manager is unavailable")
	}
	for registered, raw := range al.channelManager.GetStatus() {
		if !strings.EqualFold(registered, name) {
			continue
		}
		enabled := true
		running := false
		if fields, ok := raw.(map[string]any); ok {
			if value, ok := fields["enabled"].(bool); ok {
				enabled = value
			}
			if value, ok := fields["running"].(bool); ok {
				running = value
			}
		}
		reason := "channel is enabled and running"
		if !enabled {
			reason = "channel is registered but disabled"
		} else if !running {
			reason = "channel is enabled but not running"
		}
		return commands.ChannelStatus{
			Name: registered, Enabled: enabled, Available: enabled && running, Reason: reason,
		}, nil
	}
	return commands.ChannelStatus{
		Name: name, Enabled: false, Available: false, Reason: "channel is not enabled or registered",
	}, nil
}

func (al *AgentLoop) handleInternalDiscoveryCallback(
	ctx context.Context,
	req bus.InternalCallbackRequest,
) (*bus.InternalCallbackResponse, error) {
	bound, err := al.resolveBoundInteraction(req)
	if err != nil {
		return nil, err
	}
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action == "close" {
		return &bus.InternalCallbackResponse{Close: true}, nil
	}
	if action == "noop" {
		return &bus.InternalCallbackResponse{Text: fmt.Sprintf("Page %d", req.Page+1)}, nil
	}

	opts := discoveryCallbackOptions(bound, req.SessionKey)
	rt := al.buildCommandsRuntime(ctx, bound.agent, opts)
	discoveryReq := commands.DiscoveryCommandRequest{}
	switch action {
	case "show_dashboard":
		discoveryReq = commands.DiscoveryCommandRequest{Domain: "show", Operation: "dashboard"}
	case "show_model":
		discoveryReq = commands.DiscoveryCommandRequest{Domain: "show", Operation: "model"}
	case "show_channel":
		discoveryReq = commands.DiscoveryCommandRequest{Domain: "show", Operation: "channel"}
	case "show_agents", "show_agents_page":
		page, parseErr := discoveryCallbackPage(action, req.Value, req.Page)
		if parseErr != nil {
			return nil, parseErr
		}
		discoveryReq = commands.DiscoveryCommandRequest{Domain: "show", Operation: "agents", Page: page}
	case "show_mcp", "show_mcp_page":
		page, parseErr := discoveryCallbackPage(action, req.Value, req.Page)
		if parseErr != nil {
			return nil, parseErr
		}
		discoveryReq = commands.DiscoveryCommandRequest{Domain: "show", Operation: "mcp", Page: page}
	case "list_dashboard":
		discoveryReq = commands.DiscoveryCommandRequest{Domain: "list", Operation: "dashboard"}
	case "list_models":
		discoveryReq = commands.DiscoveryCommandRequest{Domain: "list", Operation: "models"}
	case "list_skills":
		discoveryReq = commands.DiscoveryCommandRequest{Domain: "list", Operation: "skills"}
	case "list_channels", "list_channels_page":
		page, parseErr := discoveryCallbackPage(action, req.Value, req.Page)
		if parseErr != nil {
			return nil, parseErr
		}
		discoveryReq = commands.DiscoveryCommandRequest{Domain: "list", Operation: "channels", Page: page}
	case "list_agents", "list_agents_page":
		page, parseErr := discoveryCallbackPage(action, req.Value, req.Page)
		if parseErr != nil {
			return nil, parseErr
		}
		discoveryReq = commands.DiscoveryCommandRequest{Domain: "list", Operation: "agents", Page: page}
	case "list_mcp", "list_mcp_page":
		page, parseErr := discoveryCallbackPage(action, req.Value, req.Page)
		if parseErr != nil {
			return nil, parseErr
		}
		discoveryReq = commands.DiscoveryCommandRequest{Domain: "list", Operation: "mcp", Page: page}
	case "check_channel_refresh":
		target := strings.TrimSpace(req.Value)
		if target == "" {
			return nil, fmt.Errorf("channel status callback target is missing")
		}
		discoveryReq = commands.DiscoveryCommandRequest{
			Domain: "check", Operation: "channel", Argument: target,
		}
	default:
		return nil, fmt.Errorf("invalid discovery callback action")
	}

	content, err := al.executeDiscoveryCommand(ctx, bound.agent, opts, rt, discoveryReq)
	if err != nil {
		return nil, err
	}
	return &bus.InternalCallbackResponse{
		Content:    content,
		Transition: bus.InteractionReplaceCurrent,
	}, nil
}

func discoveryCallbackOptions(bound boundInteractionContext, sessionKey string) *processOptions {
	inbound := bound.inbound
	scope := bound.allocation.Scope
	aliases := append([]string(nil), bound.allocation.SessionAliases...)
	return &processOptions{
		Dispatch: DispatchRequest{
			SessionKey:     strings.TrimSpace(sessionKey),
			SessionAliases: aliases,
			InboundContext: &inbound,
			SessionScope:   &scope,
		},
		SessionKey:     strings.TrimSpace(sessionKey),
		SessionAliases: aliases,
		InboundContext: &inbound,
		SessionScope:   &scope,
	}
}

func discoveryCallbackPage(action, value string, current int) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		if strings.HasSuffix(action, "_page") {
			return 0, fmt.Errorf("discovery page callback is missing state")
		}
		return current, nil
	}
	page, err := strconv.Atoi(value)
	if err != nil || page < 0 {
		return 0, fmt.Errorf("invalid discovery page")
	}
	return page, nil
}

func discoveryMenu(
	agent *AgentInstance,
	opts *processOptions,
	page, pages int,
	current string,
	entries []bus.InteractionEntry,
) *bus.InteractionMenu {
	return newBoundInteractionMenu(
		"discovery",
		agent.ID,
		opts.Dispatch.SessionKey,
		opts.Dispatch.SessionScope,
		opts.Dispatch.InboundContext,
		page,
		pages,
		"",
		current,
		entries,
	)
}

func discoveryPagingEntries(action string, page, pages int, backAction string) []bus.InteractionEntry {
	entries := make([]bus.InteractionEntry, 0, 5)
	if page > 0 {
		entries = append(entries, bus.InteractionEntry{
			Label: "◀️", Action: action, Value: strconv.Itoa(page - 1),
		})
	}
	entries = append(entries, bus.InteractionEntry{
		Label: fmt.Sprintf("%d/%d", page+1, pages), Action: "noop",
	})
	if page+1 < pages {
		entries = append(entries, bus.InteractionEntry{
			Label: "▶️", Action: action, Value: strconv.Itoa(page + 1),
		})
	}
	entries = append(entries,
		bus.InteractionEntry{Label: "↩️ Back", Action: backAction},
		bus.InteractionEntry{Label: "✖️ Close", Action: "close"},
	)
	return entries
}

func discoveryPageWindow(total, page int) (int, int, int, int) {
	pages := 1
	if total > 0 {
		pages = (total + discoveryPageSize - 1) / discoveryPageSize
	}
	if page < 0 {
		page = 0
	}
	if page >= pages {
		page = pages - 1
	}
	start := page * discoveryPageSize
	end := start + discoveryPageSize
	if end > total {
		end = total
	}
	return page, pages, start, end
}

func currentModelSummary(ctx context.Context, rt *commands.Runtime) (string, string, error) {
	if rt == nil {
		return "", "", fmt.Errorf("model semantics are unavailable")
	}
	if rt.ModelCommand != nil {
		content, err := rt.ModelCommand(ctx, commands.ModelCommandRequest{Operation: "current"})
		if err != nil {
			return "", "", err
		}
		if content != nil {
			model := structuredProperty(content, "Model")
			if model == "" || model == "—" {
				model = structuredProperty(content, "Alias")
			}
			return model, structuredProperty(content, "Provider"), nil
		}
	}
	if rt.GetModelInfo != nil {
		model, provider := rt.GetModelInfo()
		return model, provider, nil
	}
	return "", "", fmt.Errorf("model semantics are unavailable")
}

func structuredProperty(content *bus.StructuredContent, key string) string {
	if content == nil {
		return ""
	}
	for _, table := range content.Tables {
		for _, row := range table.Rows {
			if len(row) >= 2 && strings.EqualFold(strings.TrimSpace(row[0]), key) {
				return strings.TrimSpace(row[1])
			}
		}
	}
	return ""
}

func runtimeChannels(rt *commands.Runtime) []string {
	if rt == nil || rt.GetEnabledChannels == nil {
		return nil
	}
	return append([]string(nil), rt.GetEnabledChannels()...)
}

func runtimeAgents(rt *commands.Runtime) []string {
	if rt == nil || rt.ListAgentIDs == nil {
		return nil
	}
	return append([]string(nil), rt.ListAgentIDs()...)
}

func sortedMCPServers(ctx context.Context, rt *commands.Runtime) []commands.MCPServerInfo {
	if rt == nil || rt.ListMCPServers == nil {
		return nil
	}
	servers := append([]commands.MCPServerInfo(nil), rt.ListMCPServers(ctx)...)
	sort.SliceStable(servers, func(i, j int) bool {
		left, right := strings.ToLower(servers[i].Name), strings.ToLower(servers[j].Name)
		if left == right {
			return servers[i].Name < servers[j].Name
		}
		return left < right
	})
	return servers
}

func sortedStrings(values []string) []string {
	values = append([]string(nil), values...)
	sort.SliceStable(values, func(i, j int) bool {
		left, right := strings.ToLower(values[i]), strings.ToLower(values[j])
		if left == right {
			return values[i] < values[j]
		}
		return left < right
	})
	return values
}

func statusYesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func discoveryTableContent(
	kind, title string,
	columns []string,
	rows [][]string,
	fallback string,
	menu *bus.InteractionMenu,
) *bus.StructuredContent {
	return &bus.StructuredContent{
		Kind:  kind,
		Title: title,
		Tables: []bus.StructuredTable{{
			Columns: columns,
			Rows:    rows,
			Border:  true,
			Striped: true,
			Header:  true,
		}},
		Fallback:    fallback,
		Interaction: menu,
	}
}

func discoveryRowsFallback(title string, rows [][]string) string {
	lines := []string{title}
	for _, row := range rows {
		if len(row) == 0 {
			continue
		}
		if len(row) == 1 {
			lines = append(lines, row[0])
			continue
		}
		lines = append(lines, row[0]+": "+strings.Join(row[1:], " · "))
	}
	return strings.Join(lines, "\n")
}
