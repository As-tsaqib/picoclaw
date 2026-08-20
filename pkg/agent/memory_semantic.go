package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/commands"
	"github.com/As-tsaqib/picoclaw/pkg/memory"
)

// executeMemorySemanticCommand is the single semantic owner for textual and
// interactive memory operations. The channel/callback layer remains responsible
// only for trusted interaction state, continuation establishment, and rendering
// transitions; all memory reads and mutations pass through this typed request.
func (al *AgentLoop) executeMemorySemanticCommand(
	ctx context.Context,
	agent *AgentInstance,
	opts *processOptions,
	req commands.MemoryCommandRequest,
) (*bus.StructuredContent, error) {
	if al == nil || agent == nil || opts == nil {
		return nil, fmt.Errorf("memory command context is unavailable")
	}
	if agent.CuratedMemory == nil {
		return nil, fmt.Errorf("memory service is unavailable")
	}
	cfg := al.GetConfig()
	if cfg == nil {
		return nil, fmt.Errorf("memory configuration is unavailable")
	}

	inbound := opts.Dispatch.InboundContext
	if inbound == nil {
		inbound = opts.InboundContext
	}
	caller := callerScopeForTurn(agent.ID, cfg, *opts)
	if inbound != nil {
		caller = memoryInteractionCallerScope(caller, inbound)
	}
	service := newMemoryCommandService(agent.CuratedMemory, caller)
	operation := strings.ToLower(strings.TrimSpace(req.Operation))

	switch operation {
	case "dashboard":
		if inbound == nil || !memoryInteractionRouteIsPrivate(inbound) {
			return &bus.StructuredContent{
				Title: "Personal Memory",
				Paragraphs: []string{
					"Personal memory is private and hidden in shared channels. Please use a direct chat or an ephemeral private command to inspect and manage personal memory.",
				},
			}, nil
		}
		return buildMemoryDashboardContentE(agent, caller, inbound)

	case "status":
		stats, err := service.stats()
		if err != nil {
			return nil, err
		}
		rows := [][]string{
			{"Curated memory", "enabled"},
			{"Recall mode", cfg.Memory.Recall.EffectiveMode()},
			{"Approval mode", cfg.Memory.EffectiveApprovalMode()},
			{"Workspace", formatMemoryStats(stats.Workspace)},
		}
		if !service.includeUser {
			rows = append(rows, []string{"Current-user memory", "hidden in shared chats"})
		} else if stats.User == nil {
			rows = append(rows, []string{"Current-user memory", "unavailable on this request"})
		} else {
			rows = append(rows, []string{"Current-user", formatMemoryStats(*stats.User)})
		}
		return memorySemanticTable("Memory Status", bus.CardHeaderStatus, rows), nil

	case "profile":
		if !cfg.Memory.Profile.Enabled {
			return memorySemanticText("My Profile", "Compiled user profile is disabled."), nil
		}
		profile, err := service.profile(
			cfg.Memory.Profile.EffectiveMaxChars(),
			cfg.Memory.Profile.EffectiveMinConfidence(),
		)
		if err != nil {
			if err == memory.ErrPrivateContextRequired || err == memory.ErrUserScopeUnavailable {
				return nil, commands.NewUserError("Current-user memory is available only in a private context.")
			}
			return nil, err
		}
		content := memorySemanticText("My Profile", formatUserProfile(profile))
		if req.Interactive {
			content.Interaction = &bus.InteractionMenu{Entries: []bus.InteractionEntry{
				{Action: "dashboard", Label: "↩️ Kembali"},
				{Action: "close", Label: "✖️ Tutup"},
			}}
		}
		return content, nil

	case "list", "browse":
		set, err := service.list()
		if err != nil {
			return nil, err
		}
		if req.Interactive {
			return renderMemoryEntryPage("browse", "Memory", flattenMemoryEntries(set), req.Page, "", true), nil
		}
		text := formatMemoryEntries(set.Workspace, set.User)
		if !service.includeUser {
			text += "\nCurrent-user memory is hidden in shared chats; use a direct chat to list it."
		}
		return memorySemanticText("Memory", text), nil

	case "detail":
		entry, err := service.detail(req.ID)
		if err != nil {
			return nil, memoryDomainError(err)
		}
		return renderMemoryDetail(entry), nil

	case "search":
		query := strings.TrimSpace(req.Query)
		if query == "" {
			query = strings.TrimSpace(req.Argument)
		}
		limit := 20
		if req.Interactive {
			limit = memoryInteractiveSearchMax
		}
		set, err := service.search(query, limit)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "query is empty") {
				return nil, commands.NewUserError("Memory search needs a query.")
			}
			if strings.Contains(strings.ToLower(err.Error()), "too long") {
				return nil, commands.NewUserError(fmt.Sprintf(
					"Memory search queries are limited to %d characters.",
					memorySearchQueryMaxRunes,
				))
			}
			return nil, err
		}
		if req.Interactive {
			return renderMemoryEntryPage(
				"search",
				"Search Results",
				flattenMemoryEntries(set),
				req.Page,
				query,
				false,
			), nil
		}
		text := formatMemoryEntries(set.Workspace, set.User)
		if !service.includeUser {
			text += "\nCurrent-user memory search is available only in a direct chat."
		}
		return memorySemanticText("Memory Search", text), nil

	case "edit":
		if strings.TrimSpace(req.ID) == "" || strings.TrimSpace(req.Content) == "" {
			return nil, commands.NewUserError("Memory edit needs an entry ID and replacement content.")
		}
		if err := service.edit(req.ID, req.Content); err != nil {
			return nil, memoryDomainError(err)
		}
		if req.Interactive {
			entry, err := service.detail(req.ID)
			if err != nil {
				return nil, memoryDomainError(err)
			}
			return renderMemoryDetail(entry), nil
		}
		return memorySemanticText("Memory", "Updated memory entry "+req.ID+"."), nil

	case "pin", "unpin", "archive", "restore":
		if strings.TrimSpace(req.ID) == "" {
			return nil, commands.NewUserError("Memory operation needs an entry ID.")
		}
		if err := service.entryAction(operation, req.ID); err != nil {
			return nil, memoryDomainError(err)
		}
		if req.Interactive {
			entry, err := service.detail(req.ID)
			if err != nil {
				return nil, memoryDomainError(err)
			}
			return renderMemoryDetail(entry), nil
		}
		return memorySemanticText("Memory", fmt.Sprintf("Memory entry %s: %s.", req.ID, operation)), nil

	case "forget":
		if strings.TrimSpace(req.ID) == "" {
			return nil, commands.NewUserError("Memory forget needs an entry ID.")
		}
		if err := service.forget(req.ID); err != nil {
			return nil, memoryDomainError(err)
		}
		if req.Interactive {
			set, err := service.list()
			if err != nil {
				return nil, err
			}
			return renderMemoryEntryPage("browse", "Memory", flattenMemoryEntries(set), 0, "", true), nil
		}
		return memorySemanticText("Memory", "Forgot memory entry "+req.ID+"."), nil

	case "pending":
		set, err := service.pending()
		if err != nil {
			return nil, err
		}
		if req.Interactive {
			return renderMemoryPendingPage(flattenPendingChanges(set), req.Page), nil
		}
		text := formatPendingMemory(set.Workspace, set.User)
		if !service.includeUser {
			text += "\nCurrent-user pending changes are hidden in shared chats; use a direct chat to manage them."
		}
		return memorySemanticText("Pending Memory", text), nil

	case "approve", "reject":
		id := strings.TrimSpace(req.ID)
		if id == "" {
			return nil, commands.NewUserError("Pending-memory operation needs an ID or all.")
		}
		approve := operation == "approve"
		count, err := service.resolvePending(id, approve)
		if err != nil {
			return nil, memoryDomainError(err)
		}
		if req.Interactive {
			set, err := service.pending()
			if err != nil {
				return nil, err
			}
			return renderMemoryPendingPage(flattenPendingChanges(set), req.Page), nil
		}
		verb := "Rejected"
		if approve {
			verb = "Approved"
		}
		return memorySemanticText("Pending Memory", fmt.Sprintf("%s %d memory operation(s).", verb, count)), nil

	case "review":
		started, err := al.startMemoryReview(agent, caller, true, "")
		if err != nil {
			return nil, err
		}
		if !started {
			return memorySemanticText("Memory Review", "A memory review is already running, or there is nothing eligible to start."), nil
		}
		return memorySemanticText("Memory Review", "Started a bounded memory review."), nil

	default:
		return nil, commands.NewUserError("Unknown memory operation. Use /help memory for supported options.")
	}
}

func memorySemanticText(title, text string) *bus.StructuredContent {
	return &bus.StructuredContent{Title: title, Paragraphs: []string{text}, Fallback: text}
}

func memorySemanticTable(title string, kind bus.CardHeaderKind, rows [][]string) *bus.StructuredContent {
	fallbackLines := []string{bus.CardHeader(kind, false)}
	for _, row := range rows {
		if len(row) < 2 {
			continue
		}
		fallbackLines = append(fallbackLines, row[0]+": "+row[1])
	}
	return &bus.StructuredContent{
		Kind:  "table",
		Title: title,
		Tables: []bus.StructuredTable{{
			Columns: bus.CardHeaderColumns(kind, true),
			Rows:    rows,
			Border:  true,
			Striped: true,
			Header:  true,
		}},
		Fallback: strings.Join(fallbackLines, "\n"),
	}
}

func memoryDomainError(err error) error {
	if err == nil {
		return nil
	}
	if err == memory.ErrCuratedEntryNotFound {
		return commands.NewUserError("Memory entry was not found in the current safe scope.")
	}
	if err == memory.ErrCuratedInvalidPending {
		return commands.NewUserError("Pending memory change was not found in the current safe scope.")
	}
	if err == memory.ErrPrivateContextRequired || err == memory.ErrUserScopeUnavailable {
		return commands.NewUserError("Current-user memory is available only in a private context.")
	}
	return err
}
