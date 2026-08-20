package agent

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/commands"
	"github.com/As-tsaqib/picoclaw/pkg/session"
)

const (
	skillInteractionPageSize  = 5
	skillInteractiveSearchMax = 50
	skillSearchQueryMaxRunes  = 128
)

func configureSkillCommandRuntime(
	rt *commands.Runtime,
	agent *AgentInstance,
	opts *processOptions,
	al *AgentLoop,
) {
	if rt == nil || agent == nil || opts == nil || al == nil {
		return
	}
	rt.SkillCommand = func(
		ctx context.Context,
		req commands.SkillCommandRequest,
	) (*bus.StructuredContent, error) {
		return al.executeSkillCommand(ctx, agent, opts, req)
	}
}

func (al *AgentLoop) executeSkillCommand(
	_ context.Context,
	agent *AgentInstance,
	opts *processOptions,
	req commands.SkillCommandRequest,
) (*bus.StructuredContent, error) {
	if agent == nil || agent.ContextBuilder == nil || opts == nil || opts.Dispatch.SessionScope == nil ||
		strings.TrimSpace(opts.Dispatch.SessionKey) == "" || opts.Dispatch.InboundContext == nil {
		return nil, fmt.Errorf("skill interaction context is unavailable")
	}
	operation := strings.ToLower(strings.TrimSpace(req.Operation))
	switch operation {
	case "dashboard":
		return buildSkillPickerContent(agent, al, opts.Dispatch.SessionKey, opts.Dispatch.SessionScope,
			opts.Dispatch.InboundContext, 0, ""), nil
	case "page":
		return buildSkillPickerContent(agent, al, opts.Dispatch.SessionKey, opts.Dispatch.SessionScope,
			opts.Dispatch.InboundContext, req.Page, req.Query), nil
	default:
		return nil, fmt.Errorf("skill command operation is not supported")
	}
}

func sortedSkillNames(agent *AgentInstance) []string {
	if agent == nil || agent.ContextBuilder == nil {
		return nil
	}
	names := append([]string(nil), agent.ContextBuilder.ListSkillNames()...)
	sort.SliceStable(names, func(i, j int) bool {
		li, lj := strings.ToLower(names[i]), strings.ToLower(names[j])
		if li == lj {
			return names[i] < names[j]
		}
		return li < lj
	})
	return names
}

func filterSkillNames(names []string, query string) []string {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return names
	}
	result := make([]string, 0, minInt(len(names), skillInteractiveSearchMax))
	for _, name := range names {
		if strings.Contains(strings.ToLower(name), query) {
			result = append(result, name)
			if len(result) == skillInteractiveSearchMax {
				break
			}
		}
	}
	return result
}

func buildSkillPickerFallback(names []string, query, pending string) string {
	lines := []string{"Skill Picker"}
	if query = strings.TrimSpace(query); query != "" {
		lines = append(lines, fmt.Sprintf("Search: %q", query))
	}
	if len(names) == 0 {
		lines = append(lines, "No installed skills match this view.")
	} else {
		lines = append(lines, "Available skills:")
		for _, name := range names {
			lines = append(lines, "- "+name)
		}
	}
	if pending = strings.TrimSpace(pending); pending != "" {
		lines = append(lines, "Pending override: "+pending)
	}
	lines = append(lines,
		"Use /use <skill> to arm a skill for your next message.",
		"Use /use <skill> <message> to force it for the current request.",
		"Use /use clear or /use off to cancel a pending override.",
	)
	return strings.Join(lines, "\n")
}

func buildSkillPickerContent(
	agent *AgentInstance,
	al *AgentLoop,
	sessionKey string,
	scope *session.SessionScope,
	inbound *bus.InboundContext,
	page int,
	query string,
) *bus.StructuredContent {
	names := filterSkillNames(sortedSkillNames(agent), query)
	pages := 1
	if len(names) > 0 {
		pages = (len(names) + skillInteractionPageSize - 1) / skillInteractionPageSize
	}
	if page < 0 {
		page = 0
	}
	if page >= pages {
		page = pages - 1
	}
	start := page * skillInteractionPageSize
	end := start + skillInteractionPageSize
	if end > len(names) {
		end = len(names)
	}
	entries := make([]bus.InteractionEntry, 0, 12)
	lines := make([]string, 0, skillInteractionPageSize+2)
	if strings.TrimSpace(query) != "" {
		lines = append(lines, fmt.Sprintf("Search: %q", strings.TrimSpace(query)))
	}
	if len(names) == 0 {
		lines = append(lines, "No installed skills match this view.")
	}
	for i := start; i < end; i++ {
		label := strconv.Itoa(i - start + 1)
		lines = append(lines, fmt.Sprintf("%s. %s", label, names[i]))
		entries = append(entries, bus.InteractionEntry{Label: label, Action: "detail", Value: names[i]})
	}
	pageAction := "page"
	if strings.TrimSpace(query) != "" {
		pageAction = "search_page"
	}
	if page > 0 {
		entries = append(entries, bus.InteractionEntry{Label: "◀️", Action: pageAction, Value: strconv.Itoa(page - 1)})
	}
	entries = append(entries, bus.InteractionEntry{Label: fmt.Sprintf("%d/%d", page+1, pages), Action: "noop"})
	if page+1 < pages {
		entries = append(entries, bus.InteractionEntry{Label: "▶️", Action: pageAction, Value: strconv.Itoa(page + 1)})
	}
	entries = append(entries, bus.InteractionEntry{Label: "🔎 Search", Action: "search"})
	pending := al.pendingSkillForSession(sessionKey)
	if pending != "" {
		lines = append(lines, "Pending override: "+pending)
		entries = append(entries, bus.InteractionEntry{Label: "🧹 Clear Pending", Action: "clear"})
	}
	entries = append(entries, bus.InteractionEntry{Label: "✖️ Close", Action: "close"})
	return &bus.StructuredContent{
		Title:      "Skill Picker",
		Paragraphs: lines,
		Fallback:   buildSkillPickerFallback(names, query, pending),
		Interaction: newBoundInteractionMenu(
			"skill", agent.ID, sessionKey, scope, inbound, page, pages, query, "", entries,
		),
	}
}

func buildSkillDetailContent(
	agent *AgentInstance,
	sessionKey string,
	scope *session.SessionScope,
	inbound *bus.InboundContext,
	skillName string,
	page int,
	query string,
) *bus.StructuredContent {
	return &bus.StructuredContent{
		Title: "Skill",
		Paragraphs: []string{
			skillName,
			"Arm this skill for the next normal message in this exact session.",
		},
		Interaction: newBoundInteractionMenu(
			"skill",
			agent.ID,
			sessionKey,
			scope,
			inbound,
			page,
			maxInt(page+1, 1),
			query,
			skillName,
			[]bus.InteractionEntry{
				{Label: "✅ Arm for Next Message", Action: "arm", Value: skillName},
				{Label: "↩️ Back", Action: "back"},
				{Label: "✖️ Close", Action: "close"},
			},
		),
	}
}

func newBoundInteractionMenu(
	kind, agentID, sessionKey string,
	scope *session.SessionScope,
	inbound *bus.InboundContext,
	page, pages int,
	query, current string,
	entries []bus.InteractionEntry,
) *bus.InteractionMenu {
	menuInbound := bus.InboundContext{}
	if cloned := cloneInboundContext(inbound); cloned != nil {
		menuInbound = *cloned
	}
	scopeSig := ""
	if scope != nil {
		scopeSig = session.CanonicalScopeSignature(*scope)
	}
	return &bus.InteractionMenu{
		Kind:       kind,
		OwnerID:    sessionMenuOwner(inbound),
		Channel:    inboundChannel(inbound),
		Account:    inboundAccount(inbound),
		ChatID:     inboundChatID(inbound),
		TopicID:    inboundTopicID(inbound),
		AgentID:    agentID,
		Scope:      scopeSig,
		Inbound:    menuInbound,
		Page:       page,
		Pages:      pages,
		Entries:    append([]bus.InteractionEntry(nil), entries...),
		Current:    current,
		SessionKey: strings.TrimSpace(sessionKey),
		Query:      query,
	}
}

func (al *AgentLoop) handleInternalSkillCallback(
	_ context.Context,
	req bus.InternalCallbackRequest,
) (*bus.InternalCallbackResponse, error) {
	bound, err := al.resolveBoundInteraction(req)
	if err != nil {
		return nil, err
	}
	if bound.agent.ContextBuilder == nil {
		return nil, fmt.Errorf("skills are unavailable")
	}
	action := strings.ToLower(strings.TrimSpace(req.Action))
	switch action {
	case "close":
		return &bus.InternalCallbackResponse{Close: true}, nil
	case "noop":
		return &bus.InternalCallbackResponse{Text: fmt.Sprintf("Page %d/%d", req.Page+1, maxInt(req.Page+1, 1))}, nil
	case "dashboard":
		return &bus.InternalCallbackResponse{Content: buildSkillPickerContent(
			bound.agent, al, req.SessionKey, &bound.allocation.Scope, &bound.inbound, 0, "",
		)}, nil
	case "back":
		return &bus.InternalCallbackResponse{Content: buildSkillPickerContent(
			bound.agent, al, req.SessionKey, &bound.allocation.Scope, &bound.inbound, req.Page, req.Query,
		)}, nil
	case "page", "search_page":
		page, parseErr := strconv.Atoi(strings.TrimSpace(req.Value))
		if parseErr != nil || page < 0 {
			return nil, fmt.Errorf("invalid skill page")
		}
		query := ""
		if action == "search_page" {
			query = req.Query
		}
		return &bus.InternalCallbackResponse{Content: buildSkillPickerContent(
			bound.agent, al, req.SessionKey, &bound.allocation.Scope, &bound.inbound, page, query,
		)}, nil
	case "detail":
		canonical, ok := bound.agent.ContextBuilder.ResolveSkillName(req.Value)
		if !ok {
			return nil, fmt.Errorf("skill is no longer installed")
		}
		return &bus.InternalCallbackResponse{Content: buildSkillDetailContent(
			bound.agent, req.SessionKey, &bound.allocation.Scope, &bound.inbound, canonical, req.Page, req.Query,
		)}, nil
	case "arm":
		canonical, ok := bound.agent.ContextBuilder.ResolveSkillName(req.Value)
		if !ok {
			return nil, fmt.Errorf("skill is no longer installed")
		}
		al.setPendingSkills(req.SessionKey, []string{canonical})
		content := buildSkillPickerContent(
			bound.agent, al, req.SessionKey, &bound.allocation.Scope, &bound.inbound, req.Page, req.Query,
		)
		content.Title = "Skill Armed"
		content.Paragraphs = append(
			[]string{fmt.Sprintf("%s is armed for the next normal message in this session.", canonical)},
			content.Paragraphs...,
		)
		return &bus.InternalCallbackResponse{Content: content}, nil
	case "clear":
		al.clearPendingSkills(req.SessionKey)
		content := buildSkillPickerContent(
			bound.agent, al, req.SessionKey, &bound.allocation.Scope, &bound.inbound, req.Page, req.Query,
		)
		content.Title = "Pending Skill Cleared"
		return &bus.InternalCallbackResponse{Content: content}, nil
	case "search":
		query := strings.TrimSpace(req.Value)
		if query == "" {
			return &bus.InternalCallbackResponse{Text: "Reply to this prompt with a skill name to search:"}, nil
		}
		if len([]rune(query)) > skillSearchQueryMaxRunes {
			return nil, fmt.Errorf("skill search query is too long")
		}
		return &bus.InternalCallbackResponse{
			Content: buildSkillPickerContent(
				bound.agent, al, req.SessionKey, &bound.allocation.Scope, &bound.inbound, 0, query,
			),
			Transition: bus.InteractionAppendContinuation,
		}, nil
	default:
		return nil, fmt.Errorf("invalid skill callback action")
	}
}

func (al *AgentLoop) pendingSkillForSession(sessionKey string) string {
	value, ok := al.pendingSkills.Load(strings.TrimSpace(sessionKey))
	if !ok {
		return ""
	}
	names, ok := value.([]string)
	if !ok || len(names) == 0 {
		return ""
	}
	return strings.TrimSpace(names[0])
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
