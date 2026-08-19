package agent

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/commands"
	"github.com/As-tsaqib/picoclaw/pkg/memory"
)

const (
	memoryInteractionPageSize  = 5
	memoryInteractiveSearchMax = 100
)

type memoryCommandService struct {
	store       *memory.CuratedStore
	caller      memory.CallerScope
	includeUser bool
}

type memoryEntrySet struct {
	Workspace []memory.CuratedEntry
	User      []memory.CuratedEntry
}

type memoryPendingSet struct {
	Workspace []memory.PendingCuratedChange
	User      []memory.PendingCuratedChange
}

type memoryStatsSet struct {
	Workspace memory.CuratedStats
	User      *memory.CuratedStats
}

func newMemoryCommandService(store *memory.CuratedStore, caller memory.CallerScope) memoryCommandService {
	return memoryCommandService{store: store, caller: caller, includeUser: memory.AllowsPrivateUserMemory(caller)}
}

func (s memoryCommandService) stats() (memoryStatsSet, error) {
	if s.store == nil {
		return memoryStatsSet{}, fmt.Errorf("memory is not available")
	}
	workspace, err := s.store.Stats(memory.CuratedTargetWorkspace, s.caller)
	if err != nil {
		return memoryStatsSet{}, err
	}
	result := memoryStatsSet{Workspace: workspace}
	if !s.includeUser {
		return result, nil
	}
	user, err := s.store.Stats(memory.CuratedTargetCurrentUser, s.caller)
	if errors.Is(err, memory.ErrUserScopeUnavailable) {
		return result, nil
	}
	if err != nil {
		return memoryStatsSet{}, err
	}
	result.User = &user
	return result, nil
}

func (s memoryCommandService) profile(maxChars int, minConfidence float64) (memory.UserProfileSnapshot, error) {
	if !s.includeUser {
		return memory.UserProfileSnapshot{}, memory.ErrPrivateContextRequired
	}
	if s.store == nil {
		return memory.UserProfileSnapshot{}, fmt.Errorf("memory is not available")
	}
	return s.store.CompileUserProfile(s.caller, memory.UserProfileOptions{
		MaxChars: maxChars, MinConfidence: minConfidence,
	})
}

func (s memoryCommandService) list() (memoryEntrySet, error) {
	if s.store == nil {
		return memoryEntrySet{}, fmt.Errorf("memory is not available")
	}
	workspace, err := s.store.List(memory.CuratedTargetWorkspace, s.caller)
	if err != nil {
		return memoryEntrySet{}, err
	}
	result := memoryEntrySet{Workspace: workspace}
	if !s.includeUser {
		return result, nil
	}
	user, err := s.store.List(memory.CuratedTargetCurrentUser, s.caller)
	if errors.Is(err, memory.ErrUserScopeUnavailable) {
		return result, nil
	}
	if err != nil {
		return memoryEntrySet{}, err
	}
	result.User = user
	return result, nil
}

func (s memoryCommandService) search(query string, limit int) (memoryEntrySet, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return memoryEntrySet{}, fmt.Errorf("memory search query is empty")
	}
	if s.store == nil {
		return memoryEntrySet{}, fmt.Errorf("memory is not available")
	}
	if limit <= 0 {
		limit = 20
	}
	workspace, err := s.store.Search(memory.CuratedTargetWorkspace, s.caller, query, limit)
	if err != nil {
		return memoryEntrySet{}, err
	}
	result := memoryEntrySet{Workspace: workspace}
	if !s.includeUser {
		return result, nil
	}
	user, err := s.store.Search(memory.CuratedTargetCurrentUser, s.caller, query, limit)
	if errors.Is(err, memory.ErrUserScopeUnavailable) {
		return result, nil
	}
	if err != nil {
		return memoryEntrySet{}, err
	}
	result.User = user
	return result, nil
}

func (s memoryCommandService) detail(id string) (memory.CuratedEntry, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return memory.CuratedEntry{}, memory.ErrCuratedEntryNotFound
	}
	target, err := findMemoryEntryTarget(s.store, s.caller, id, s.includeUser)
	if err != nil {
		return memory.CuratedEntry{}, err
	}
	entries, err := s.store.List(target, s.caller)
	if err != nil {
		return memory.CuratedEntry{}, err
	}
	for _, entry := range entries {
		if entry.ID == id {
			return entry, nil
		}
	}
	return memory.CuratedEntry{}, memory.ErrCuratedEntryNotFound
}

func (s memoryCommandService) edit(id, content string) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return fmt.Errorf("memory content is empty")
	}
	target, err := findMemoryEntryTarget(s.store, s.caller, id, s.includeUser)
	if err != nil {
		return err
	}
	_, err = s.store.ApplyBatch(target, s.caller, []memory.CuratedMutation{{
		Action: memory.CuratedActionReplace, ID: id, Content: content,
		EvidenceKind: memory.CuratedEvidenceExplicit,
		Provenance:   memory.Provenance{Source: "user_command"},
	}}, false)
	return err
}

func (s memoryCommandService) entryAction(action, id string) error {
	action = strings.ToLower(strings.TrimSpace(action))
	switch action {
	case memory.CuratedActionPin, memory.CuratedActionUnpin,
		memory.CuratedActionArchive, memory.CuratedActionRestore:
	default:
		return memory.ErrCuratedInvalidAction
	}
	target, err := findMemoryEntryTarget(s.store, s.caller, id, s.includeUser)
	if err != nil {
		return err
	}
	mutation := memory.CuratedMutation{
		Action: action, ID: id, Provenance: memory.Provenance{Source: "user_command"},
	}
	if action == memory.CuratedActionRestore {
		mutation.EvidenceKind = memory.CuratedEvidenceExplicit
	}
	_, err = s.store.ApplyBatch(target, s.caller, []memory.CuratedMutation{mutation}, false)
	return err
}

func (s memoryCommandService) forget(id string) error {
	target, err := findMemoryEntryTarget(s.store, s.caller, id, s.includeUser)
	if err != nil {
		return err
	}
	_, err = s.store.ApplyBatch(target, s.caller, []memory.CuratedMutation{{
		Action: memory.CuratedActionRemove, ID: id,
		Provenance: memory.Provenance{Source: "user_command"},
	}}, false)
	return err
}

func (s memoryCommandService) pending() (memoryPendingSet, error) {
	if s.store == nil {
		return memoryPendingSet{}, fmt.Errorf("memory is not available")
	}
	workspace, err := s.store.Pending(memory.CuratedTargetWorkspace, s.caller)
	if err != nil {
		return memoryPendingSet{}, err
	}
	result := memoryPendingSet{Workspace: workspace}
	if !s.includeUser {
		return result, nil
	}
	user, err := s.store.Pending(memory.CuratedTargetCurrentUser, s.caller)
	if errors.Is(err, memory.ErrUserScopeUnavailable) {
		return result, nil
	}
	if err != nil {
		return memoryPendingSet{}, err
	}
	result.User = user
	return result, nil
}

func (s memoryCommandService) resolvePending(id string, approve bool) (int, error) {
	return resolvePendingMemory(s.store, s.caller, id, approve, s.includeUser)
}

func configureMemoryCommandRuntime(
	rt *commands.Runtime,
	agent *AgentInstance,
	opts *processOptions,
	al *AgentLoop,
) {
	if rt == nil || agent == nil || opts == nil || al == nil {
		return
	}
	caller := callerScopeForTurn(agent.ID, rt.Config, *opts)
	service := newMemoryCommandService(agent.CuratedMemory, caller)
	if agent.CuratedMemory != nil {
		rt.MemoryStatus = func() string {
			stats, err := service.stats()
			lines := []string{
				"Curated memory: enabled",
				fmt.Sprintf("Recall mode: %s", rt.Config.Memory.Recall.EffectiveMode()),
				fmt.Sprintf(
					"Background review: %t (interval %d)",
					rt.Config.Memory.BackgroundReview.Enabled,
					rt.Config.Memory.BackgroundReview.EffectiveInterval(),
				),
				fmt.Sprintf("Approval mode: %s", rt.Config.Memory.EffectiveApprovalMode()),
				fmt.Sprintf(
					"Query-aware retrieval: %t (%s, user share %.0f%%)",
					rt.Config.Memory.Retrieval.Enabled,
					rt.Config.Memory.Retrieval.EffectiveEngine(),
					rt.Config.Memory.Retrieval.EffectiveUserShare()*100,
				),
				fmt.Sprintf(
					"Compiled user profile: %t (max %d chars)",
					rt.Config.Memory.Profile.Enabled,
					rt.Config.Memory.Profile.EffectiveMaxChars(),
				),
				fmt.Sprintf("Notifications: %s", rt.Config.Memory.EffectiveNotificationMode()),
			}
			if err != nil {
				return strings.Join(append(lines, "Memory status unavailable: "+err.Error()), "\n")
			}
			lines = append(lines, formatMemoryStats(stats.Workspace))
			if !service.includeUser {
				lines = append(lines, "Current-user memory details are hidden in shared chats.")
			} else if stats.User == nil {
				lines = append(lines, "Current-user scope: unavailable on this request")
			} else {
				lines = append(lines, formatMemoryStats(*stats.User))
			}
			return strings.Join(lines, "\n")
		}
		rt.MemoryProfile = func() (string, error) {
			if !rt.Config.Memory.Profile.Enabled {
				return "Compiled user profile is disabled.", nil
			}
			profile, err := service.profile(
				rt.Config.Memory.Profile.EffectiveMaxChars(),
				rt.Config.Memory.Profile.EffectiveMinConfidence(),
			)
			if err != nil {
				return "", err
			}
			return formatUserProfile(profile), nil
		}
		rt.MemoryList = func() (string, error) {
			set, err := service.list()
			if err != nil {
				return "", err
			}
			text := formatMemoryEntries(set.Workspace, set.User)
			if !service.includeUser {
				text += "\nCurrent-user memory is hidden in shared chats; use a direct chat to list it."
			}
			return text, nil
		}
		rt.MemorySearch = func(query string) (string, error) {
			set, err := service.search(query, 20)
			if err != nil {
				return "", err
			}
			text := formatMemoryEntries(set.Workspace, set.User)
			if !service.includeUser {
				text += "\nCurrent-user memory search is available only in a direct chat."
			}
			return text, nil
		}
		rt.MemoryEdit = func(id, content string) (string, error) {
			if err := service.edit(id, content); err != nil {
				return "", err
			}
			return "Updated memory entry " + id + ".", nil
		}
		rt.MemoryEntryAction = func(action, id string) (string, error) {
			if err := service.entryAction(action, id); err != nil {
				return "", err
			}
			return fmt.Sprintf("Memory entry %s: %s.", id, strings.ToLower(strings.TrimSpace(action))), nil
		}
		rt.MemoryForget = func(id string) (string, error) {
			if err := service.forget(id); err != nil {
				return "", err
			}
			return "Forgot memory entry " + id + ".", nil
		}
		rt.MemoryPending = func() (string, error) {
			set, err := service.pending()
			if err != nil {
				return "", err
			}
			text := formatPendingMemory(set.Workspace, set.User)
			if !service.includeUser {
				text += "\nCurrent-user pending changes are hidden in shared chats; use a direct chat to manage them."
			}
			return text, nil
		}
		rt.MemoryApprove = func(id string) (string, error) {
			count, err := service.resolvePending(id, true)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Approved %d memory operation(s).", count), nil
		}
		rt.MemoryReject = func(id string) (string, error) {
			count, err := service.resolvePending(id, false)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Rejected %d pending memory change(s).", count), nil
		}
		rt.MemoryReview = func(_ context.Context) (string, error) {
			started, err := al.startMemoryReview(agent, caller, true, "")
			if err != nil {
				return "", err
			}
			if !started {
				return "A memory review is already running, or there is nothing eligible to start.", nil
			}
			return "Started a bounded memory review in the background.", nil
		}
	}

	if agent.Checkpoints != nil {
		rt.CheckpointList = func() (string, error) {
			checkpoints, err := agent.Checkpoints.List(caller, false)
			if err != nil {
				return "", err
			}
			return formatCheckpointList(checkpoints), nil
		}
		rt.CheckpointResume = func(id string) (string, error) {
			checkpoint, err := agent.Checkpoints.Apply(
				caller,
				"",
				memory.CheckpointMutation{Action: memory.CheckpointActionResume, ID: id},
			)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Resumed %s (%s). Next: %s", checkpoint.Title, checkpoint.ID, checkpoint.NextStep), nil
		}
		rt.CheckpointForget = func(id string) (string, error) {
			checkpoint, err := agent.Checkpoints.Apply(
				caller,
				"",
				memory.CheckpointMutation{Action: memory.CheckpointActionArchive, ID: id},
			)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Archived checkpoint %s (%s).", checkpoint.Title, checkpoint.ID), nil
		}
	}

	rt.MemoryCommand = func(ctx context.Context, req commands.MemoryCommandRequest) (*bus.StructuredContent, error) {
		return al.executeMemoryCommand(ctx, agent, opts, req)
	}
}

func (al *AgentLoop) executeMemoryCommand(
	_ context.Context,
	agent *AgentInstance,
	opts *processOptions,
	req commands.MemoryCommandRequest,
) (*bus.StructuredContent, error) {
	if agent == nil || opts == nil {
		return nil, fmt.Errorf("context is unavailable")
	}
	inbound := opts.Dispatch.InboundContext
	if inbound == nil {
		return nil, memory.ErrPrivateContextRequired
	}
	isPrivateDelivery := strings.EqualFold(strings.TrimSpace(inbound.ChatType), "direct") || inbound.PrivateResponse
	caller := callerScopeForTurn(agent.ID, al.cfg, *opts)
	if strings.ToLower(strings.TrimSpace(req.Operation)) != "dashboard" {
		return nil, fmt.Errorf("memory subcommand not recognized")
	}
	if !isPrivateDelivery && !memory.AllowsPrivateUserMemory(caller) {
		return &bus.StructuredContent{
			Title: "Personal Memory",
			Paragraphs: []string{
				"Personal memory is private and hidden in shared channels. " +
					"Please use a direct chat or an ephemeral private command to inspect and manage personal memory.",
			},
		}, nil
	}
	return buildMemoryDashboardContentE(agent, caller, inbound)
}

func normalizedMemoryMenuInbound(inbound *bus.InboundContext) (bus.InboundContext, error) {
	if inbound == nil {
		return bus.InboundContext{}, fmt.Errorf("memory interaction route is unavailable")
	}
	cloned := cloneInboundContext(inbound)
	if cloned == nil {
		return bus.InboundContext{}, fmt.Errorf("memory interaction route is unavailable")
	}
	cloned.Channel = strings.TrimSpace(cloned.Channel)
	cloned.Account = strings.TrimSpace(cloned.Account)
	cloned.ChatID = strings.TrimSpace(cloned.ChatID)
	cloned.TopicID = strings.TrimSpace(cloned.TopicID)
	cloned.SenderID = strings.TrimSpace(cloned.SenderID)
	if cloned.Channel == "" || cloned.Account == "" || cloned.ChatID == "" || cloned.SenderID == "" {
		return bus.InboundContext{}, fmt.Errorf("memory interaction route is incomplete")
	}
	return *cloned, nil
}

func newMemoryInteractionMenu(
	inbound *bus.InboundContext,
	agentID string,
	page int,
	pages int,
	current string,
	entries []bus.InteractionEntry,
) (*bus.InteractionMenu, error) {
	trusted, err := normalizedMemoryMenuInbound(inbound)
	if err != nil {
		return nil, err
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, fmt.Errorf("memory interaction agent is unavailable")
	}
	if page < 0 {
		page = 0
	}
	if pages <= 0 {
		pages = 1
	}
	return &bus.InteractionMenu{
		Kind: "memory", OwnerID: trusted.SenderID, Channel: trusted.Channel,
		Account: trusted.Account, ChatID: trusted.ChatID, TopicID: trusted.TopicID,
		AgentID: agentID, Inbound: trusted, Page: page, Pages: pages,
		Current: current, Entries: append([]bus.InteractionEntry(nil), entries...),
	}, nil
}

func buildMemoryDashboardContentE(
	agent *AgentInstance,
	caller memory.CallerScope,
	inbound *bus.InboundContext,
) (*bus.StructuredContent, error) {
	if agent == nil || agent.CuratedMemory == nil {
		return &bus.StructuredContent{
			Title:      "Personal Memory",
			Paragraphs: []string{"Curated memory is not configured."},
		}, nil
	}
	service := newMemoryCommandService(agent.CuratedMemory, caller)
	stats, err := service.stats()
	if err != nil {
		return nil, err
	}
	paragraphs := []string{fmt.Sprintf("Workspace entries: %d", stats.Workspace.Entries)}
	pendingCount := stats.Workspace.PendingCount
	if service.includeUser {
		if stats.User != nil {
			paragraphs = append(paragraphs, fmt.Sprintf("User entries: %d", stats.User.Entries))
			pendingCount += stats.User.PendingCount
		} else {
			paragraphs = append(paragraphs, "Current-user scope is unavailable on this request.")
		}
	}
	if pendingCount > 0 {
		paragraphs = append(paragraphs, fmt.Sprintf("Pending review: %d", pendingCount))
	}
	menu, err := newMemoryInteractionMenu(inbound, agent.ID, 0, 1, "", []bus.InteractionEntry{
		{Action: "profile", Label: "👤 My Profile"},
		{Action: "browse", Label: "📚 Browse"},
		{Action: "search", Label: "🔎 Search"},
		{Action: "pending", Label: "📝 Pending"},
		{Action: "close", Label: "✖️ Tutup"},
	})
	if err != nil {
		return nil, err
	}
	return &bus.StructuredContent{Title: "Personal Memory", Paragraphs: paragraphs, Interaction: menu}, nil
}

func memoryCallbackEnvelope(req bus.InternalCallbackRequest) (bus.InboundContext, error) {
	inbound := bus.NormalizeInboundMessage(bus.InboundMessage{Context: req.Inbound}).Context
	if strings.TrimSpace(req.OwnerID) == "" || inbound.SenderID != req.OwnerID ||
		inbound.Channel != req.Channel || inbound.Account != req.Account ||
		inbound.ChatID != req.ChatID || inbound.TopicID != req.TopicID {
		return bus.InboundContext{}, fmt.Errorf("callback scope validation failed")
	}
	return inbound, nil
}

func (al *AgentLoop) handleInternalMemoryCallback(
	ctx context.Context,
	req bus.InternalCallbackRequest,
) (response *bus.InternalCallbackResponse, err error) {
	inbound, err := memoryCallbackEnvelope(req)
	if err != nil {
		return nil, err
	}
	_, agent, routeErr := al.resolveMessageRoute(bus.InboundMessage{Context: inbound})
	if routeErr != nil || agent == nil || !strings.EqualFold(agent.ID, req.AgentID) {
		return nil, fmt.Errorf("callback agent validation failed")
	}
	if agent.CuratedMemory == nil {
		return nil, fmt.Errorf("memory is not available")
	}
	opts := processOptions{InboundContext: &inbound}
	caller := callerScopeForTurn(agent.ID, al.cfg, opts)
	service := newMemoryCommandService(agent.CuratedMemory, caller)
	action := strings.ToLower(strings.TrimSpace(req.Action))

	defer func() {
		if err != nil || response == nil || response.Content == nil || response.Content.Interaction == nil {
			return
		}
		menu := response.Content.Interaction
		bound, bindErr := newMemoryInteractionMenu(
			&inbound, agent.ID, menu.Page, menu.Pages, menu.Current, menu.Entries,
		)
		if bindErr != nil {
			response = nil
			err = bindErr
			return
		}
		response.Content.Interaction = bound
	}()

	switch action {
	case "close":
		return &bus.InternalCallbackResponse{Close: true}, nil
	case "dashboard":
		content, buildErr := buildMemoryDashboardContentE(agent, caller, &inbound)
		return &bus.InternalCallbackResponse{Content: content}, buildErr
	case "profile":
		if !al.cfg.Memory.Profile.Enabled {
			return &bus.InternalCallbackResponse{Content: memorySimpleView(
				"My Profile",
				"Compiled user profile is disabled.",
				[]bus.InteractionEntry{
					{Action: "dashboard", Label: "↩️ Kembali"},
					{Action: "close", Label: "✖️ Tutup"},
				},
			)}, nil
		}
		profile, profileErr := service.profile(
			al.cfg.Memory.Profile.EffectiveMaxChars(),
			al.cfg.Memory.Profile.EffectiveMinConfidence(),
		)
		if profileErr != nil {
			return nil, profileErr
		}
		return &bus.InternalCallbackResponse{Content: memorySimpleView(
			"My Profile",
			formatUserProfile(profile),
			[]bus.InteractionEntry{
				{Action: "dashboard", Label: "↩️ Kembali"},
				{Action: "close", Label: "✖️ Tutup"},
			},
		)}, nil
	case "browse", "browse_page", "page":
		page, pageErr := memoryRequestedPage(req, action)
		if pageErr != nil {
			return nil, pageErr
		}
		set, listErr := service.list()
		if listErr != nil {
			return nil, listErr
		}
		return &bus.InternalCallbackResponse{Content: renderMemoryEntryPage(
			"browse", "Memory", flattenMemoryEntries(set), page, "", true,
		)}, nil
	case "detail":
		entry, detailErr := service.detail(req.Value)
		if detailErr != nil {
			return nil, detailErr
		}
		return &bus.InternalCallbackResponse{Content: renderMemoryDetail(entry)}, nil
	case "pin", "unpin", "archive", "restore":
		if mutateErr := service.entryAction(action, req.Value); mutateErr != nil {
			return nil, mutateErr
		}
		entry, detailErr := service.detail(req.Value)
		if detailErr != nil {
			return nil, detailErr
		}
		return &bus.InternalCallbackResponse{Content: renderMemoryDetail(entry)}, nil
	case "forget_confirm":
		if _, detailErr := service.detail(req.Value); detailErr != nil {
			return nil, detailErr
		}
		return &bus.InternalCallbackResponse{Content: &bus.StructuredContent{
			Title:      "Lupakan Memori Ini?",
			Paragraphs: []string{"Tindakan ini akan menghapus entri memori yang dipilih. Lanjutkan?"},
			Interaction: &bus.InteractionMenu{Current: req.Value, Entries: []bus.InteractionEntry{
				{Action: "forget", Label: "✅ Konfirmasi", Value: req.Value},
				{Action: "detail", Label: "❌ Batal", Value: req.Value},
			}},
		}}, nil
	case "forget":
		if mutateErr := service.forget(req.Value); mutateErr != nil {
			return nil, mutateErr
		}
		set, listErr := service.list()
		if listErr != nil {
			return nil, listErr
		}
		return &bus.InternalCallbackResponse{Content: renderMemoryEntryPage(
			"browse", "Memory", flattenMemoryEntries(set), 0, "", true,
		)}, nil
	case "pending", "pending_page":
		page, pageErr := memoryRequestedPage(req, action)
		if pageErr != nil {
			return nil, pageErr
		}
		set, pendingErr := service.pending()
		if pendingErr != nil {
			return nil, pendingErr
		}
		return &bus.InternalCallbackResponse{Content: renderMemoryPendingPage(flattenPendingChanges(set), page)}, nil
	case "approve", "reject":
		if _, resolveErr := service.resolvePending(req.Value, action == "approve"); resolveErr != nil {
			return nil, resolveErr
		}
		set, pendingErr := service.pending()
		if pendingErr != nil {
			return nil, pendingErr
		}
		return &bus.InternalCallbackResponse{Content: renderMemoryPendingPage(
			flattenPendingChanges(set), req.Page,
		)}, nil
	case "search":
		query := strings.TrimSpace(req.Value)
		if query == "" {
			return &bus.InternalCallbackResponse{Text: "Balas pesan ini dengan kata kunci pencarian:"}, nil
		}
		set, searchErr := service.search(query, memoryInteractiveSearchMax)
		if searchErr != nil {
			return nil, searchErr
		}
		return &bus.InternalCallbackResponse{Content: renderMemoryEntryPage(
			"search", "Search Results", flattenMemoryEntries(set), 0, query, false,
		)}, nil
	case "search_page":
		page, pageErr := memoryRequestedPage(req, action)
		if pageErr != nil {
			return nil, pageErr
		}
		query := strings.TrimSpace(req.SessionKey)
		if query == "" {
			return nil, fmt.Errorf("memory search state is unavailable")
		}
		set, searchErr := service.search(query, memoryInteractiveSearchMax)
		if searchErr != nil {
			return nil, searchErr
		}
		return &bus.InternalCallbackResponse{Content: renderMemoryEntryPage(
			"search", "Search Results", flattenMemoryEntries(set), page, query, false,
		)}, nil
	case "edit":
		if strings.TrimSpace(req.Value) == "" {
			if strings.TrimSpace(req.SessionKey) == "" {
				return nil, fmt.Errorf("memory edit target is unavailable")
			}
			if _, detailErr := service.detail(req.SessionKey); detailErr != nil {
				return nil, detailErr
			}
			return &bus.InternalCallbackResponse{
				Text: "Balas pesan ini dengan konten baru untuk entri memori ini:",
			}, nil
		}
		id := strings.TrimSpace(req.SessionKey)
		if id == "" {
			return nil, fmt.Errorf("memory edit target is unavailable")
		}
		if editErr := service.edit(id, req.Value); editErr != nil {
			return nil, editErr
		}
		entry, detailErr := service.detail(id)
		if detailErr != nil {
			return nil, detailErr
		}
		return &bus.InternalCallbackResponse{Content: renderMemoryDetail(entry)}, nil
	case "noop":
		return &bus.InternalCallbackResponse{Text: fmt.Sprintf("Halaman %d", req.Page+1)}, nil
	default:
		return nil, fmt.Errorf("invalid memory callback action")
	}
}

func memorySimpleView(title, text string, entries []bus.InteractionEntry) *bus.StructuredContent {
	return &bus.StructuredContent{
		Title: title, Paragraphs: []string{text}, Interaction: &bus.InteractionMenu{Entries: entries},
	}
}

func memoryRequestedPage(req bus.InternalCallbackRequest, action string) (int, error) {
	if action == "browse" || action == "pending" {
		if req.Page < 0 {
			return 0, nil
		}
		return req.Page, nil
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(req.Value))
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("invalid memory page")
	}
	return parsed, nil
}

func flattenMemoryEntries(set memoryEntrySet) []memory.CuratedEntry {
	all := make([]memory.CuratedEntry, 0, len(set.Workspace)+len(set.User))
	all = append(all, set.Workspace...)
	all = append(all, set.User...)
	return all
}

func flattenPendingChanges(set memoryPendingSet) []memory.PendingCuratedChange {
	all := make([]memory.PendingCuratedChange, 0, len(set.Workspace)+len(set.User))
	all = append(all, set.Workspace...)
	all = append(all, set.User...)
	return all
}

func clampMemoryPage(total, page int) (int, int, int, int) {
	pages := (total + memoryInteractionPageSize - 1) / memoryInteractionPageSize
	if pages == 0 {
		pages = 1
	}
	if page < 0 {
		page = 0
	}
	if page >= pages {
		page = pages - 1
	}
	start := page * memoryInteractionPageSize
	end := start + memoryInteractionPageSize
	if end > total {
		end = total
	}
	return page, pages, start, end
}

func renderMemoryEntryPage(
	view, title string,
	all []memory.CuratedEntry,
	page int,
	current string,
	includeSearch bool,
) *bus.StructuredContent {
	page, pages, start, end := clampMemoryPage(len(all), page)
	lines := make([]string, 0, memoryInteractionPageSize)
	entries := make([]bus.InteractionEntry, 0, memoryInteractionPageSize+7)
	for i := start; i < end; i++ {
		entry := all[i]
		label := strconv.Itoa(i + 1)
		contentSnippet := truncateMemoryCommandText(memory.RedactMemoryText(entry.Content), 50)
		lines = append(lines, fmt.Sprintf("%s. [%s] %s", label, entry.EffectiveType(), contentSnippet))
		entries = append(entries, bus.InteractionEntry{Action: "detail", Label: label, Value: entry.ID})
	}
	if len(lines) == 0 {
		if view == "search" {
			lines = append(lines, "Tidak ditemukan entri yang cocok dengan kata kunci.")
		} else {
			lines = append(lines, "Belum ada entri memori.")
		}
	}
	pageAction := "browse_page"
	if view == "search" {
		pageAction = "search_page"
	}
	if page > 0 {
		entries = append(entries, bus.InteractionEntry{Action: pageAction, Label: "◀️", Value: strconv.Itoa(page - 1)})
	}
	entries = append(entries, bus.InteractionEntry{Action: "noop", Label: fmt.Sprintf("%d/%d", page+1, pages)})
	if page+1 < pages {
		entries = append(entries, bus.InteractionEntry{Action: pageAction, Label: "▶️", Value: strconv.Itoa(page + 1)})
	}
	if includeSearch || view == "search" {
		label := "🔎 Search"
		if view == "search" {
			label = "🔎 Search Lagi"
		}
		entries = append(entries, bus.InteractionEntry{Action: "search", Label: label})
	}
	entries = append(
		entries,
		bus.InteractionEntry{Action: "dashboard", Label: "↩️ Kembali"},
		bus.InteractionEntry{Action: "close", Label: "✖️ Tutup"},
	)
	return &bus.StructuredContent{
		Title:       fmt.Sprintf("%s · %d/%d", title, page+1, pages),
		Paragraphs:  []string{strings.Join(lines, "\n")},
		Interaction: &bus.InteractionMenu{Page: page, Pages: pages, Current: current, Entries: entries},
	}
}

func renderMemoryDetail(entry memory.CuratedEntry) *bus.StructuredContent {
	lines := []string{
		fmt.Sprintf("Type: %s", entry.EffectiveType()),
		fmt.Sprintf("Status: %s", entry.EffectiveStatus()),
		fmt.Sprintf("Pinned: %v", entry.Pinned),
		"",
		truncateMemoryCommandText(memory.RedactMemoryText(entry.Content), 480),
	}
	actions := []bus.InteractionEntry{{Action: "edit", Label: "✏️ Edit"}}
	if entry.Pinned {
		actions = append(actions, bus.InteractionEntry{Action: "unpin", Label: "📌 Unpin", Value: entry.ID})
	} else {
		actions = append(actions, bus.InteractionEntry{Action: "pin", Label: "📌 Pin", Value: entry.ID})
	}
	if entry.EffectiveStatus() == memory.CuratedStatusArchived {
		actions = append(actions, bus.InteractionEntry{Action: "restore", Label: "♻️ Restore", Value: entry.ID})
	} else {
		actions = append(actions, bus.InteractionEntry{Action: "archive", Label: "🗄 Archive", Value: entry.ID})
	}
	actions = append(actions,
		bus.InteractionEntry{Action: "forget_confirm", Label: "🗑 Forget", Value: entry.ID},
		bus.InteractionEntry{Action: "browse", Label: "↩️ Kembali"},
		bus.InteractionEntry{Action: "close", Label: "✖️ Tutup"},
	)
	return &bus.StructuredContent{
		Title:      "Memory Detail",
		Paragraphs: []string{strings.Join(lines, "\n")},
		Interaction: &bus.InteractionMenu{
			Current: entry.ID, Entries: actions,
		},
	}
}

func renderMemoryPendingPage(all []memory.PendingCuratedChange, page int) *bus.StructuredContent {
	page, pages, start, end := clampMemoryPage(len(all), page)
	lines := make([]string, 0, memoryInteractionPageSize)
	entries := make([]bus.InteractionEntry, 0, 2*memoryInteractionPageSize+5)
	for i := start; i < end; i++ {
		pending := all[i]
		label := strconv.Itoa(i + 1)
		lines = append(lines, fmt.Sprintf("%s. %d operation(s)", label, len(pending.Mutations)))
		entries = append(entries,
			bus.InteractionEntry{Action: "approve", Label: "✅ " + label, Value: pending.ID},
			bus.InteractionEntry{Action: "reject", Label: "❌ " + label, Value: pending.ID},
		)
	}
	if len(lines) == 0 {
		lines = append(lines, "Tidak ada perubahan memori yang tertunda.")
	}
	if page > 0 {
		entries = append(entries, bus.InteractionEntry{
			Action: "pending_page", Label: "◀️", Value: strconv.Itoa(page - 1),
		})
	}
	entries = append(entries, bus.InteractionEntry{Action: "noop", Label: fmt.Sprintf("%d/%d", page+1, pages)})
	if page+1 < pages {
		entries = append(entries, bus.InteractionEntry{
			Action: "pending_page", Label: "▶️", Value: strconv.Itoa(page + 1),
		})
	}
	entries = append(
		entries,
		bus.InteractionEntry{Action: "dashboard", Label: "↩️ Kembali"},
		bus.InteractionEntry{Action: "close", Label: "✖️ Tutup"},
	)
	return &bus.StructuredContent{
		Title:       fmt.Sprintf("Pending Memory · %d/%d", page+1, pages),
		Paragraphs:  []string{strings.Join(lines, "\n")},
		Interaction: &bus.InteractionMenu{Page: page, Pages: pages, Entries: entries},
	}
}

func formatUserProfile(profile memory.UserProfileSnapshot) string {
	lines := []string{"Current-user compiled profile:"}
	appendFields := func(title string, fields []memory.UserProfileField) {
		if len(fields) == 0 {
			return
		}
		lines = append(lines, title+":")
		for _, field := range fields {
			value := strings.TrimSpace(field.Value)
			if value == "" {
				value = strings.TrimSpace(field.Content)
			}
			value = truncateMemoryCommandText(memory.RedactMemoryText(value), 480)
			lines = append(lines, fmt.Sprintf(
				"- %s = %s [%s, confidence %.2f, source %s]",
				field.Key, value, field.EvidenceKind, field.Confidence, field.SourceID,
			))
		}
	}
	appendFields("Identity", profile.Identity)
	appendFields("Communication", profile.Communication)
	appendFields("Workflow", profile.Workflow)
	appendFields("Interaction", profile.Interaction)
	appendFields("Boundaries", profile.Boundaries)
	if len(lines) == 1 {
		lines = append(lines, "- (empty)")
	}
	lines = append(lines, fmt.Sprintf(
		"Profile size: %d characters; sources: %d", profile.Characters, len(profile.SourceIDs),
	))
	return strings.Join(lines, "\n")
}

func formatMemoryStats(stats memory.CuratedStats) string {
	return fmt.Sprintf(
		"%s: %d entries, %d/%d characters, %d pending",
		stats.Target, stats.Entries, stats.Characters, stats.Capacity, stats.PendingCount,
	)
}

func formatMemoryEntries(workspace, user []memory.CuratedEntry) string {
	var lines []string
	remainingChars := 6_000
	remainingEntries := 100
	appendEntries := func(title string, entries []memory.CuratedEntry) {
		lines = append(lines, title)
		if len(entries) == 0 {
			lines = append(lines, "- (empty)")
			return
		}
		for _, entry := range entries {
			if remainingEntries <= 0 || remainingChars <= 0 {
				lines = append(lines, "- … additional entries omitted")
				return
			}
			content := truncateMemoryCommandText(memory.RedactMemoryText(entry.Content), remainingChars)
			remainingChars -= len([]rune(content))
			remainingEntries--
			lines = append(lines, fmt.Sprintf(
				"- `%s` [%s/%s%s] — %s",
				entry.ID, entry.EffectiveType(), entry.EffectiveStatus(),
				map[bool]string{true: ", pinned"}[entry.Pinned], content,
			))
		}
	}
	appendEntries("Workspace memory:", workspace)
	if user != nil {
		appendEntries("Current-user memory:", user)
	}
	return strings.Join(lines, "\n")
}

func truncateMemoryCommandText(value string, maximum int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if maximum <= 0 {
		return ""
	}
	if len(runes) <= maximum {
		return value
	}
	if maximum == 1 {
		return "…"
	}
	return string(runes[:maximum-1]) + "…"
}

func findMemoryEntryTarget(
	store *memory.CuratedStore,
	caller memory.CallerScope,
	id string,
	includeCurrentUser bool,
) (string, error) {
	if store == nil {
		return "", fmt.Errorf("memory is not available")
	}
	if includeCurrentUser {
		entries, err := store.List(memory.CuratedTargetCurrentUser, caller)
		if err != nil && !errors.Is(err, memory.ErrUserScopeUnavailable) {
			return "", err
		}
		if err == nil {
			for _, entry := range entries {
				if entry.ID == id {
					return memory.CuratedTargetCurrentUser, nil
				}
			}
		}
	}
	entries, err := store.List(memory.CuratedTargetWorkspace, caller)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.ID == id {
			return memory.CuratedTargetWorkspace, nil
		}
	}
	return "", memory.ErrCuratedEntryNotFound
}

func formatPendingMemory(workspace, user []memory.PendingCuratedChange) string {
	var lines []string
	appendPending := func(target string, changes []memory.PendingCuratedChange) {
		for _, change := range changes {
			lines = append(lines, fmt.Sprintf(
				"- `%s` (%s, %d operation(s), %s)",
				change.ID, target, len(change.Mutations), change.CreatedAt.UTC().Format("2006-01-02 15:04Z"),
			))
		}
	}
	appendPending("workspace", workspace)
	appendPending("current_user", user)
	if len(lines) == 0 {
		return "No pending memory changes."
	}
	return "Pending memory changes:\n" + strings.Join(lines, "\n")
}

func resolvePendingMemory(
	store *memory.CuratedStore,
	caller memory.CallerScope,
	id string,
	approve bool,
	includeCurrentUser bool,
) (int, error) {
	count := 0
	found := false
	targets := []string{memory.CuratedTargetWorkspace}
	if includeCurrentUser {
		targets = append([]string{memory.CuratedTargetCurrentUser}, targets...)
	}
	for _, target := range targets {
		pending, err := store.Pending(target, caller)
		if errors.Is(err, memory.ErrUserScopeUnavailable) {
			continue
		}
		if err != nil {
			return 0, err
		}
		matches := false
		for _, change := range pending {
			if id == "all" || change.ID == id {
				matches = true
				found = true
				count += len(change.Mutations)
			}
		}
		if !matches {
			continue
		}
		if approve {
			if _, err := store.Approve(target, caller, id); err != nil {
				return 0, err
			}
		} else if _, err := store.Reject(target, caller, id); err != nil {
			return 0, err
		}
	}
	if !found {
		return 0, memory.ErrCuratedInvalidPending
	}
	return count, nil
}

func formatCheckpointList(checkpoints []memory.TaskCheckpoint) string {
	if len(checkpoints) == 0 {
		return "No active or suspended checkpoints in this session."
	}
	lines := []string{"Task checkpoints:"}
	for _, checkpoint := range checkpoints {
		line := fmt.Sprintf("- `%s` [%s] %s", checkpoint.ID, checkpoint.Status, checkpoint.Title)
		if strings.TrimSpace(checkpoint.NextStep) != "" {
			line += " — next: " + checkpoint.NextStep
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
