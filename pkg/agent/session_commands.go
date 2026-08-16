package agent

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/commands"
	"github.com/As-tsaqib/picoclaw/pkg/session"
)

const sessionsPerPage = 5

func configureSessionCommandRuntime(rt *commands.Runtime, agent *AgentInstance, opts *processOptions, al *AgentLoop) {
	if rt == nil || agent == nil || opts == nil || al == nil {
		return
	}
	rt.SessionCommand = func(ctx context.Context, req commands.SessionCommandRequest) (*bus.StructuredContent, error) {
		return al.executeSessionCommand(ctx, agent, opts, req)
	}
}

func (al *AgentLoop) handleInternalCallback(
	_ context.Context,
	req bus.InternalCallbackRequest,
) (*bus.InternalCallbackResponse, error) {
	if !strings.EqualFold(strings.TrimSpace(req.Kind), "session") {
		return nil, fmt.Errorf("unsupported internal callback")
	}
	inbound := bus.NormalizeInboundMessage(bus.InboundMessage{Context: req.Inbound}).Context
	if strings.TrimSpace(req.OwnerID) == "" || inbound.SenderID != req.OwnerID ||
		inbound.Channel != req.Channel || inbound.Account != req.Account ||
		inbound.ChatID != req.ChatID || inbound.TopicID != req.TopicID {
		return nil, fmt.Errorf("callback scope validation failed")
	}
	route, agent, routeErr := al.resolveMessageRoute(bus.InboundMessage{Context: inbound})
	if routeErr != nil || agent == nil || !strings.EqualFold(agent.ID, req.AgentID) {
		return nil, fmt.Errorf("callback agent validation failed")
	}
	allocation := session.AllocateRouteSession(session.AllocationInput{
		AgentID: route.AgentID, Context: inbound, SessionPolicy: route.SessionPolicy,
	})
	if session.CanonicalScopeSignature(allocation.Scope) != req.Scope {
		return nil, fmt.Errorf("callback session scope validation failed")
	}
	catalog, ok := agent.Sessions.(session.ScopedSessionStore)
	if !ok {
		return nil, session.ErrSessionCatalogUnavailable
	}

	routeActive := catalog.ActiveScopedSession(&allocation.Scope, allocation.SessionAliases)
	mode, query, dashboard := al.telegramSessionDashboard(
		&inbound,
		agent.ID,
		routeActive,
		&allocation.Scope,
		allocation.SessionAliases,
	)
	reqMode := session.DashboardMode(strings.ToLower(strings.TrimSpace(req.DashboardMode)))
	if reqMode == "" {
		reqMode = session.DashboardModeRoute
	}
	if !dashboard {
		mode = session.DashboardModeRoute
	}
	if reqMode != mode {
		return nil, fmt.Errorf("callback dashboard mode validation failed")
	}

	page := req.Page
	if dashboard {
		dashboardCatalog, ok := agent.Sessions.(session.DashboardSessionStore)
		if !ok {
			return nil, session.ErrSessionCatalogUnavailable
		}
		active := dashboardCatalog.ActiveDashboardSession(query)
		switch strings.ToLower(strings.TrimSpace(req.Action)) {
		case "select":
			if err := dashboardCatalog.SetActiveDashboardSession(query, req.Value); err != nil {
				return nil, fmt.Errorf("session selection was rejected")
			}
			active = req.Value
		case "page":
			parsed, err := strconv.Atoi(strings.TrimSpace(req.Value))
			if err != nil || parsed < 0 {
				return nil, fmt.Errorf("invalid session page")
			}
			page = parsed
		case "new":
			record, err := catalog.CreateScopedSession(&allocation.Scope, "")
			if err != nil {
				return nil, fmt.Errorf("session could not be created")
			}
			if err := dashboardCatalog.SetActiveDashboardSession(query, record.Key); err != nil {
				return nil, fmt.Errorf("new session could not be activated")
			}
			active = record.Key
			page = 0
		case "rename":
			return &bus.InternalCallbackResponse{
				Text: "Gunakan /session rename <nama baru> untuk mengganti nama session aktif.",
			}, nil
		case "noop":
			return &bus.InternalCallbackResponse{Text: fmt.Sprintf("Halaman %d", page+1)}, nil
		case "close":
			return &bus.InternalCallbackResponse{Close: true}, nil
		default:
			return nil, fmt.Errorf("invalid session callback action")
		}
		records, err := dashboardCatalog.ListDashboardSessions(query)
		if err != nil {
			return nil, fmt.Errorf("session catalog unavailable")
		}
		return &bus.InternalCallbackResponse{Content: buildSessionListFromRecords(
			records, active, page, &inbound, agent.ID, &allocation.Scope, mode,
		)}, nil
	}

	aliases := allocation.SessionAliases
	active := catalog.ActiveScopedSession(&allocation.Scope, aliases)
	switch strings.ToLower(strings.TrimSpace(req.Action)) {
	case "select":
		if err := catalog.SetActiveScopedSession(&allocation.Scope, aliases, req.Value); err != nil {
			return nil, fmt.Errorf("session selection was rejected")
		}
		active = req.Value
	case "page":
		parsed, err := strconv.Atoi(strings.TrimSpace(req.Value))
		if err != nil || parsed < 0 {
			return nil, fmt.Errorf("invalid session page")
		}
		page = parsed
	case "new":
		record, err := catalog.CreateScopedSession(&allocation.Scope, "")
		if err != nil {
			return nil, fmt.Errorf("session could not be created")
		}
		if err := catalog.SetActiveScopedSession(&allocation.Scope, aliases, record.Key); err != nil {
			return nil, fmt.Errorf("new session could not be activated")
		}
		active = record.Key
		page = 0
	case "rename":
		return &bus.InternalCallbackResponse{
			Text: "Gunakan /session rename <nama baru> untuk mengganti nama session aktif.",
		}, nil
	case "noop":
		return &bus.InternalCallbackResponse{Text: fmt.Sprintf("Halaman %d", page+1)}, nil
	case "close":
		return &bus.InternalCallbackResponse{Close: true}, nil
	default:
		return nil, fmt.Errorf("invalid session callback action")
	}

	content := buildSessionListContent(catalog, &allocation.Scope, aliases, active, page, &inbound, agent.ID)
	return &bus.InternalCallbackResponse{Content: content}, nil
}

func (al *AgentLoop) executeSessionCommand(
	_ context.Context,
	agent *AgentInstance,
	opts *processOptions,
	req commands.SessionCommandRequest,
) (*bus.StructuredContent, error) {
	if agent == nil || opts == nil || opts.Dispatch.SessionScope == nil {
		return nil, fmt.Errorf("session scope is unavailable")
	}
	catalog, ok := agent.Sessions.(session.ScopedSessionStore)
	if !ok {
		return nil, session.ErrSessionCatalogUnavailable
	}
	scope := opts.Dispatch.SessionScope
	aliases := append([]string(nil), opts.Dispatch.SessionAliases...)
	routeActive := catalog.ActiveScopedSession(scope, aliases)
	if routeKey := strings.TrimSpace(opts.Dispatch.RouteSessionKey); routeKey != "" &&
		catalogSessionInScope(catalog, scope, aliases, routeKey) {
		routeActive = routeKey
	} else if strings.TrimSpace(opts.Dispatch.SessionKey) != "" &&
		!opts.Dispatch.SessionDashboard &&
		catalogSessionInScope(catalog, scope, aliases, opts.Dispatch.SessionKey) {
		routeActive = opts.Dispatch.SessionKey
	}

	mode, query, dashboard := al.telegramSessionDashboard(
		opts.Dispatch.InboundContext,
		agent.ID,
		routeActive,
		scope,
		aliases,
	)
	if dashboard {
		dashboardCatalog, ok := agent.Sessions.(session.DashboardSessionStore)
		if !ok {
			return nil, session.ErrSessionCatalogUnavailable
		}
		active := dashboardCatalog.ActiveDashboardSession(query)
		switch strings.ToLower(strings.TrimSpace(req.Operation)) {
		case "list":
			records, err := dashboardCatalog.ListDashboardSessions(query)
			if err != nil {
				return nil, err
			}
			return buildSessionListFromRecords(
				records,
				active,
				0,
				opts.Dispatch.InboundContext,
				agent.ID,
				scope,
				mode,
			), nil
		case "current":
			records, err := dashboardCatalog.ListDashboardSessions(query)
			if err != nil {
				return nil, err
			}
			return buildCurrentSessionFromRecords(records, active, mode), nil
		case "new":
			record, err := catalog.CreateScopedSession(scope, req.Argument)
			if err != nil {
				return nil, err
			}
			if err := dashboardCatalog.SetActiveDashboardSession(query, record.Key); err != nil {
				return nil, err
			}
			return paragraphContent(
				fmt.Sprintf("Session aktif dashboard: %s (ID %s).", record.Name, record.ShortID),
			), nil
		case "rename":
			if active == "" {
				return nil, session.ErrSessionNotInScope
			}
			if err := dashboardCatalog.RenameDashboardSession(query, active, req.Argument); err != nil {
				return nil, err
			}
			return paragraphContent(
				"Nama session berhasil diubah menjadi: " + session.SanitizeSessionName(req.Argument),
			), nil
		case "use":
			record, err := dashboardCatalog.ResolveDashboardSelector(query, req.Argument)
			if err != nil {
				return nil, fmt.Errorf("session tidak ditemukan dalam dashboard aman ini")
			}
			if err := dashboardCatalog.SetActiveDashboardSession(query, record.Key); err != nil {
				return nil, fmt.Errorf("session tidak dapat diaktifkan")
			}
			return paragraphContent(
				fmt.Sprintf("Session dashboard diganti ke %s (ID %s).", record.Name, record.ShortID),
			), nil
		default:
			return nil, fmt.Errorf("subcommand session tidak dikenal")
		}
	}

	active := routeActive
	switch strings.ToLower(strings.TrimSpace(req.Operation)) {
	case "list":
		return buildSessionListContent(catalog, scope, aliases, active, 0, opts.Dispatch.InboundContext, agent.ID), nil
	case "current":
		return buildCurrentSessionContent(catalog, scope, aliases, active), nil
	case "new":
		record, err := catalog.CreateScopedSession(scope, req.Argument)
		if err != nil {
			return nil, err
		}
		if err := catalog.SetActiveScopedSession(scope, aliases, record.Key); err != nil {
			return nil, err
		}
		return paragraphContent(fmt.Sprintf("Session aktif: %s (ID %s).", record.Name, record.ShortID)), nil
	case "rename":
		if err := catalog.RenameScopedSession(scope, aliases, active, req.Argument); err != nil {
			return nil, err
		}
		return paragraphContent(
			"Nama session berhasil diubah menjadi: " + session.SanitizeSessionName(req.Argument),
		), nil
	case "use":
		record, err := catalog.ResolveScopedSelector(scope, aliases, req.Argument)
		if err != nil {
			return nil, fmt.Errorf("session tidak ditemukan dalam scope aman ini")
		}
		if err := catalog.SetActiveScopedSession(scope, aliases, record.Key); err != nil {
			return nil, fmt.Errorf("session tidak dapat diaktifkan")
		}
		return paragraphContent(fmt.Sprintf("Session aktif diganti ke %s (ID %s).", record.Name, record.ShortID)), nil
	default:
		return nil, fmt.Errorf("subcommand session tidak dikenal")
	}
}

func catalogSessionInScope(
	catalog session.ScopedSessionStore,
	scope *session.SessionScope,
	aliases []string,
	key string,
) bool {
	records, err := catalog.ListScopedSessions(scope, aliases)
	if err != nil {
		return false
	}
	for _, record := range records {
		if record.Key == key {
			return true
		}
	}
	return false
}

func buildSessionListContent(
	catalog session.ScopedSessionStore,
	scope *session.SessionScope,
	aliases []string,
	active string,
	page int,
	inbound *bus.InboundContext,
	agentID string,
) *bus.StructuredContent {
	records, err := catalog.ListScopedSessions(scope, aliases)
	if err != nil {
		return paragraphContent("Daftar session belum tersedia.")
	}
	return buildSessionListFromRecords(records, active, page, inbound, agentID, scope, session.DashboardModeRoute)
}

func buildSessionListFromRecords(
	records []session.SessionRecord,
	active string,
	page int,
	inbound *bus.InboundContext,
	agentID string,
	menuScope *session.SessionScope,
	mode session.DashboardMode,
) *bus.StructuredContent {
	if len(records) == 0 {
		return paragraphContent("Belum ada session dalam scope aman ini.")
	}
	pages := (len(records) + sessionsPerPage - 1) / sessionsPerPage
	if page < 0 {
		page = 0
	}
	if page >= pages {
		page = pages - 1
	}
	start := page * sessionsPerPage
	end := start + sessionsPerPage
	if end > len(records) {
		end = len(records)
	}

	columns, rows := sessionTableRows(records[start:end], active, start, mode)
	entries := make([]bus.InteractionEntry, 0, end-start+5)
	for i := start; i < end; i++ {
		entries = append(
			entries,
			bus.InteractionEntry{Label: strconv.Itoa(i + 1), Action: "select", Value: records[i].Key},
		)
	}
	if page > 0 {
		entries = append(entries, bus.InteractionEntry{Label: "◀️", Action: "page", Value: strconv.Itoa(page - 1)})
	}
	entries = append(entries, bus.InteractionEntry{Label: fmt.Sprintf("Halaman %d/%d", page+1, pages), Action: "noop"})
	if page+1 < pages {
		entries = append(entries, bus.InteractionEntry{Label: "▶️", Action: "page", Value: strconv.Itoa(page + 1)})
	}
	entries = append(entries,
		bus.InteractionEntry{Label: "➕ Baru", Action: "new"},
		bus.InteractionEntry{Label: "✏️ Rename", Action: "rename"},
		bus.InteractionEntry{Label: "✖️ Tutup", Action: "close"},
	)

	menuInbound := bus.InboundContext{}
	if cloned := cloneInboundContext(inbound); cloned != nil {
		menuInbound = *cloned
	}
	scopeSignature := ""
	if menuScope != nil {
		scopeSignature = session.CanonicalScopeSignature(*menuScope)
	}
	menu := &bus.InteractionMenu{
		Kind:          "session",
		OwnerID:       sessionMenuOwner(inbound),
		Channel:       inboundChannel(inbound),
		Account:       inboundAccount(inbound),
		ChatID:        inboundChatID(inbound),
		TopicID:       inboundTopicID(inbound),
		AgentID:       agentID,
		Scope:         scopeSignature,
		DashboardMode: string(mode),
		Inbound:       menuInbound,
		Page:          page,
		Pages:         pages,
		Entries:       entries,
		Current:       active,
	}
	fallback := genericSessionTableFallback(columns, rows)
	return &bus.StructuredContent{
		Kind: "session_list", Title: sessionListTitle(mode), Tables: []bus.StructuredTable{{
			Columns: columns, Rows: rows, Border: true, Striped: true, Header: true,
		}}, Fallback: fallback, Interaction: menu,
	}
}

func sessionTableRows(
	records []session.SessionRecord,
	active string,
	offset int,
	mode session.DashboardMode,
) ([]string, [][]string) {
	switch mode {
	case session.DashboardModeSuperadmin:
		columns := []string{
			"No",
			"Nama Session",
			"Channel",
			"Account/Bot",
			"Agent",
			"Chat/Topic",
			"Owner",
			"Pesan",
			"Terakhir",
		}
		rows := make([][]string, 0, len(records))
		for i, record := range records {
			no := sessionRowNumber(record, active, offset+i+1)
			channel, account, agentID, chatTopic, owner := sessionRecordOrigin(record)
			rows = append(
				rows,
				[]string{
					no,
					record.Name,
					channel,
					account,
					agentID,
					chatTopic,
					owner,
					strconv.Itoa(record.MessageCount),
					compactSessionTime(record.UpdatedAt),
				},
			)
		}
		return columns, rows
	case session.DashboardModePersonal:
		columns := []string{"No", "Nama Session", "Asal", "Pesan", "Terakhir"}
		rows := make([][]string, 0, len(records))
		for i, record := range records {
			no := sessionRowNumber(record, active, offset+i+1)
			channel, _, _, chatTopic, _ := sessionRecordOrigin(record)
			origin := channel
			if chatTopic != "-" {
				origin += " / " + chatTopic
			}
			rows = append(
				rows,
				[]string{
					no,
					record.Name,
					origin,
					strconv.Itoa(record.MessageCount),
					compactSessionTime(record.UpdatedAt),
				},
			)
		}
		return columns, rows
	default:
		columns := []string{"No", "Nama Session", "Pesan", "Terakhir"}
		rows := make([][]string, 0, len(records))
		for i, record := range records {
			rows = append(rows, []string{
				sessionRowNumber(record, active, offset+i+1), record.Name,
				strconv.Itoa(record.MessageCount), compactSessionTime(record.UpdatedAt),
			})
		}
		return columns, rows
	}
}

func sessionRowNumber(record session.SessionRecord, active string, n int) string {
	no := strconv.Itoa(n)
	if record.Key == active {
		return "✅" + no
	}
	return no
}

func sessionListTitle(mode session.DashboardMode) string {
	switch mode {
	case session.DashboardModePersonal:
		return "👤 Session Saya"
	case session.DashboardModeSuperadmin:
		return "🌐 Mode Global Superadmin"
	default:
		return "Session"
	}
}

func buildCurrentSessionContent(
	catalog session.ScopedSessionStore,
	scope *session.SessionScope,
	aliases []string,
	active string,
) *bus.StructuredContent {
	records, err := catalog.ListScopedSessions(scope, aliases)
	if err != nil {
		return paragraphContent("Session aktif belum tersedia.")
	}
	return buildCurrentSessionFromRecords(records, active, session.DashboardModeRoute)
}

func buildCurrentSessionFromRecords(
	records []session.SessionRecord,
	active string,
	mode session.DashboardMode,
) *bus.StructuredContent {
	for _, record := range records {
		if record.Key != active {
			continue
		}
		channel, _, _, chatTopic, owner := sessionRecordOrigin(record)
		origin := channel
		if chatTopic != "-" {
			origin += " / " + chatTopic
		}
		modeText := sessionModeLabel(mode)
		rows := [][]string{
			{"Nama", record.Name},
			{"Short-ID", record.ShortID},
			{"Asal", origin},
			{"Owner", owner},
			{"Mode", modeText},
			{"Pesan", strconv.Itoa(record.MessageCount)},
			{"Terakhir", compactSessionTime(record.UpdatedAt)},
		}
		fallback := fmt.Sprintf(
			"Session aktif: %s\nShort-ID: %s\nAsal: %s\nOwner: %s\nMode: %s\nPesan: %d\nTerakhir: %s",
			record.Name,
			record.ShortID,
			origin,
			owner,
			modeText,
			record.MessageCount,
			compactSessionTime(record.UpdatedAt),
		)
		return &bus.StructuredContent{Kind: "session_current", Title: "Session aktif", Tables: []bus.StructuredTable{{
			Columns: []string{"Properti", "Nilai"}, Rows: rows, Border: true, Striped: true, Header: true,
		}}, Fallback: fallback}
	}
	return paragraphContent("Belum ada session aktif dalam scope aman ini.")
}

func sessionModeLabel(mode session.DashboardMode) string {
	switch mode {
	case session.DashboardModePersonal:
		return "Personal"
	case session.DashboardModeSuperadmin:
		return "Superadmin"
	default:
		return "Route"
	}
}

func sessionRecordOrigin(record session.SessionRecord) (channel, account, agentID, chatTopic, owner string) {
	if record.LegacyUnknown || record.Scope == nil {
		return "Legacy/Unknown", "Legacy/Unknown", "Legacy/Unknown", "Legacy/Unknown", "Legacy/Unknown"
	}
	scope := record.Scope
	channel = strings.TrimSpace(scope.OriginChannel)
	if channel == "" {
		channel = strings.TrimSpace(scope.Platform)
	}
	if channel == "" {
		channel = strings.TrimSpace(scope.Channel)
	}
	if channel == "" {
		channel = "-"
	}
	account = strings.TrimSpace(scope.BotAccount)
	routingAccount := strings.TrimSpace(scope.Account)
	if account == "" {
		account = routingAccount
	} else if routingAccount != "" && !strings.EqualFold(routingAccount, account) {
		account += " / " + routingAccount
	}
	if account == "" {
		account = "-"
	}
	agentID = strings.TrimSpace(scope.AgentID)
	if agentID == "" {
		agentID = "-"
	}
	chatTopic = strings.TrimSpace(scope.OriginChatID)
	if topic := strings.TrimSpace(scope.OriginTopicID); topic != "" {
		if chatTopic == "" {
			chatTopic = "topic " + topic
		} else {
			chatTopic += " / " + topic
		}
	}
	if chatTopic == "" {
		chatTopic = "-"
	}
	owner = "-"
	if verified, ok := session.VerifiedTelegramOwner(scope, session.SessionBotAccount(scope)); ok {
		owner = verified
	}
	return channel, account, agentID, chatTopic, owner
}

func sessionTableFallback(records []session.SessionRecord, active string, offset int) string {
	lines := make([]string, 0, 2+len(records))
	lines = append(lines,
		"| No | Nama Session | Pesan | Terakhir |",
		"|---|---|---:|---|",
	)
	for i, record := range records {
		no := strconv.Itoa(offset + i + 1)
		if record.Key == active {
			no = "✅" + no
		}
		lines = append(lines, fmt.Sprintf(
			"| %s | %s | %d | %s |",
			escapeTableCell(no),
			escapeTableCell(record.Name),
			record.MessageCount,
			escapeTableCell(compactSessionTime(record.UpdatedAt)),
		))
	}
	return strings.Join(lines, "\n")
}

func genericSessionTableFallback(columns []string, rows [][]string) string {
	if len(columns) == 0 {
		return ""
	}
	lines := []string{"| " + strings.Join(escapedTableCells(columns), " | ") + " |"}
	separator := make([]string, len(columns))
	for i := range separator {
		separator[i] = "---"
	}
	lines = append(lines, "| "+strings.Join(separator, " | ")+" |")
	for _, row := range rows {
		cells := append([]string(nil), row...)
		for len(cells) < len(columns) {
			cells = append(cells, "")
		}
		lines = append(lines, "| "+strings.Join(escapedTableCells(cells[:len(columns)]), " | ")+" |")
	}
	return strings.Join(lines, "\n")
}

func escapedTableCells(values []string) []string {
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = escapeTableCell(value)
	}
	return out
}

func escapeTableCell(value string) string {
	value = strings.ReplaceAll(strings.ReplaceAll(value, "\n", " "), "\r", " ")
	return strings.NewReplacer(
		"\\", "\\\\",
		"|", "\\|",
		"`", "\\`",
		"*", "\\*",
		"_", "\\_",
		"~", "\\~",
		"@", "\\@",
		"[", "\\[",
		"]", "\\]",
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
	).Replace(value)
}

func compactSessionTime(ts time.Time) string {
	if ts.IsZero() {
		return "-"
	}
	now := time.Now().In(time.Local)
	local := ts.In(time.Local)
	if local.Year() == now.Year() && local.YearDay() == now.YearDay() {
		return local.Format("15:04")
	}
	yesterday := now.AddDate(0, 0, -1)
	if local.Year() == yesterday.Year() && local.YearDay() == yesterday.YearDay() {
		return "Kemarin"
	}
	if local.Year() == now.Year() {
		return local.Format("02 Jan 15:04")
	}
	return local.Format("02 Jan 2006")
}

func paragraphContent(text string) *bus.StructuredContent {
	return &bus.StructuredContent{Kind: "paragraph", Paragraphs: []string{text}, Fallback: text}
}

func sessionMenuOwner(inbound *bus.InboundContext) string {
	if inbound == nil {
		return ""
	}
	return strings.TrimSpace(inbound.SenderID)
}

func inboundChannel(inbound *bus.InboundContext) string {
	if inbound == nil {
		return ""
	}
	return inbound.Channel
}

func inboundAccount(inbound *bus.InboundContext) string {
	if inbound == nil {
		return ""
	}
	return inbound.Account
}

func inboundChatID(inbound *bus.InboundContext) string {
	if inbound == nil {
		return ""
	}
	return inbound.ChatID
}

func inboundTopicID(inbound *bus.InboundContext) string {
	if inbound == nil {
		return ""
	}
	return inbound.TopicID
}
