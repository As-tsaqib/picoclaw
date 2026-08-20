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
	configureModelCommandRuntime(rt, agent, opts, al)
	configureDiscoveryCommandRuntime(rt, agent, opts, al)
	configureSemanticMCPDiscoveryRuntime(rt, agent, opts, al)
}

func (al *AgentLoop) handleInternalSessionCallback(
	_ context.Context,
	req bus.InternalCallbackRequest,
) (*bus.InternalCallbackResponse, error) {
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
	aliases := allocation.SessionAliases
	active := catalog.ActiveScopedSession(&allocation.Scope, aliases)
	page := req.Page

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
	case "remove":
		target := strings.TrimSpace(req.SessionKey)
		if target == "" || target != active || !catalogSessionInScope(catalog, &allocation.Scope, aliases, target) {
			return nil, fmt.Errorf("session removal was rejected")
		}
		if err := catalog.RemoveScopedSession(&allocation.Scope, aliases, target); err != nil {
			return nil, fmt.Errorf("session could not be removed")
		}
		active = catalog.ActiveScopedSession(&allocation.Scope, aliases)
	case "rename":
		target := strings.TrimSpace(req.SessionKey)
		if target == "" || target != active || !catalogSessionInScope(catalog, &allocation.Scope, aliases, target) {
			return nil, fmt.Errorf("session rename was rejected")
		}
		if strings.TrimSpace(req.Value) == "" {
			return &bus.InternalCallbackResponse{
				Text: "Balas pesan ini dengan nama baru untuk session aktif.",
			}, nil
		}
		if err := catalog.RenameScopedSession(&allocation.Scope, aliases, target, req.Value); err != nil {
			return nil, fmt.Errorf("session could not be renamed")
		}
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
	active := catalog.ActiveScopedSession(scope, aliases)
	if strings.TrimSpace(opts.Dispatch.SessionKey) != "" &&
		catalogSessionInScope(catalog, scope, aliases, opts.Dispatch.SessionKey) {
		active = opts.Dispatch.SessionKey
	}

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
	case "remove":
		removedName := active
		if records, err := catalog.ListScopedSessions(scope, aliases); err == nil {
			for _, record := range records {
				if record.Key == active {
					removedName = record.Name
					break
				}
			}
		}
		if err := catalog.RemoveScopedSession(scope, aliases, active); err != nil {
			return nil, err
		}
		return paragraphContent("Session dihapus: " + removedName + "."), nil
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
	rows := make([][]string, 0, end-start)
	entries := make([]bus.InteractionEntry, 0, end-start+5)
	for i := start; i < end; i++ {
		record := records[i]
		no := strconv.Itoa(i + 1)
		if record.Key == active {
			no = "✅" + no
		}
		rows = append(
			rows,
			[]string{no, record.Name, strconv.Itoa(record.MessageCount), compactSessionTime(record.UpdatedAt)},
		)
		entries = append(entries, bus.InteractionEntry{Label: strconv.Itoa(i + 1), Action: "select", Value: record.Key})
	}
	if page > 0 {
		entries = append(entries, bus.InteractionEntry{Label: "◀️", Action: "page", Value: strconv.Itoa(page - 1)})
	}
	entries = append(
		entries,
		bus.InteractionEntry{Label: fmt.Sprintf("Halaman %d/%d", page+1, pages), Action: "noop", Value: ""},
	)
	if page+1 < pages {
		entries = append(entries, bus.InteractionEntry{Label: "▶️", Action: "page", Value: strconv.Itoa(page + 1)})
	}
	entries = append(entries,
		bus.InteractionEntry{Label: "➕ New", Action: "new", Value: ""},
		bus.InteractionEntry{Label: "🗑️ Remove", Action: "remove", Value: ""},
		bus.InteractionEntry{Label: "✏️ Rename", Action: "rename", Value: ""},
		bus.InteractionEntry{Label: "✖️ Tutup", Action: "close", Value: ""},
	)

	fallback := sessionTableFallback(records[start:end], active, start)
	menuInbound := bus.InboundContext{}
	if cloned := cloneInboundContext(inbound); cloned != nil {
		menuInbound = *cloned
	}
	menu := &bus.InteractionMenu{
		Kind:       "session",
		OwnerID:    sessionMenuOwner(inbound),
		Channel:    inboundChannel(inbound),
		Account:    inboundAccount(inbound),
		ChatID:     inboundChatID(inbound),
		TopicID:    inboundTopicID(inbound),
		AgentID:    agentID,
		Scope:      session.CanonicalScopeSignature(*scope),
		Inbound:    menuInbound,
		Page:       page,
		Pages:      pages,
		Entries:    entries,
		Current:    active,
		SessionKey: active,
	}
	return &bus.StructuredContent{
		Kind: "session_list", Title: "Session", Tables: []bus.StructuredTable{{
			Columns: []string{"No", "Nama Session", "Pesan", "Terakhir"}, Rows: rows,
			Border: true, Striped: true, Header: true,
		}}, Fallback: fallback, Interaction: menu,
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
	for _, record := range records {
		if record.Key != active {
			continue
		}
		fallback := fmt.Sprintf(
			"Session aktif: %s\nID: %s\nPesan: %d\nTerakhir: %s",
			record.Name,
			record.ShortID,
			record.MessageCount,
			compactSessionTime(record.UpdatedAt),
		)
		return &bus.StructuredContent{Kind: "session_current", Title: "Session aktif", Tables: []bus.StructuredTable{
			{
				Columns: []string{
					"Properti",
					"Nilai",
				},
				Rows: [][]string{
					{"Nama", record.Name},
					{"Short-ID", record.ShortID},
					{"Pesan", strconv.Itoa(record.MessageCount)},
					{"Terakhir", compactSessionTime(record.UpdatedAt)},
				},
				Border:  true,
				Striped: true,
				Header:  true,
			},
		}, Fallback: fallback}
	}
	return paragraphContent("Belum ada session aktif dalam scope aman ini.")
}

func sessionTableFallback(records []session.SessionRecord, active string, offset int) string {
	lines := make([]string, 0, 2+len(records))
	lines = append(lines, "| No | Nama Session | Pesan | Terakhir |", "|---|---|---:|---|")
	for i, record := range records {
		no := strconv.Itoa(offset + i + 1)
		if record.Key == active {
			no = "✅" + no
		}
		lines = append(
			lines,
			fmt.Sprintf(
				"| %s | %s | %d | %s |",
				escapeTableCell(no),
				escapeTableCell(record.Name),
				record.MessageCount,
				escapeTableCell(compactSessionTime(record.UpdatedAt)),
			),
		)
	}
	return strings.Join(lines, "\n")
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
