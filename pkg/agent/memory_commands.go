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
	if agent.CuratedMemory != nil {
		rt.MemoryStatus = func() string {
			workspace, workspaceErr := agent.CuratedMemory.Stats(memory.CuratedTargetWorkspace, caller)
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
			if workspaceErr == nil {
				lines = append(lines, formatMemoryStats(workspace))
			}
			if !memory.AllowsPrivateUserMemory(caller) {
				lines = append(lines, "Current-user memory details are hidden in shared chats.")
			} else {
				user, userErr := agent.CuratedMemory.Stats(memory.CuratedTargetCurrentUser, caller)
				if userErr == nil {
					lines = append(lines, formatMemoryStats(user))
				} else if errors.Is(userErr, memory.ErrUserScopeUnavailable) {
					lines = append(lines, "Current-user scope: unavailable on this request")
				}
			}
			return strings.Join(lines, "\n")
		}
		rt.MemoryProfile = func() (string, error) {
			if !memory.AllowsPrivateUserMemory(caller) {
				return "", memory.ErrPrivateContextRequired
			}
			if !rt.Config.Memory.Profile.Enabled {
				return "Compiled user profile is disabled.", nil
			}
			profile, err := agent.CuratedMemory.CompileUserProfile(caller, memory.UserProfileOptions{
				MaxChars:      rt.Config.Memory.Profile.EffectiveMaxChars(),
				MinConfidence: rt.Config.Memory.Profile.EffectiveMinConfidence(),
			})
			if err != nil {
				return "", err
			}
			return formatUserProfile(profile), nil
		}
		rt.MemoryList = func() (string, error) {
			workspace, err := agent.CuratedMemory.List(memory.CuratedTargetWorkspace, caller)
			if err != nil {
				return "", err
			}
			if !memory.AllowsPrivateUserMemory(caller) {
				return formatMemoryEntries(workspace, nil) +
					"\nCurrent-user memory is hidden in shared chats; use a direct chat to list it.", nil
			}
			user, userErr := agent.CuratedMemory.List(memory.CuratedTargetCurrentUser, caller)
			if userErr != nil && !errors.Is(userErr, memory.ErrUserScopeUnavailable) {
				return "", userErr
			}
			return formatMemoryEntries(workspace, user), nil
		}
		rt.MemorySearch = func(query string) (string, error) {
			workspace, err := agent.CuratedMemory.Search(memory.CuratedTargetWorkspace, caller, query, 20)
			if err != nil {
				return "", err
			}
			if !memory.AllowsPrivateUserMemory(caller) {
				return formatMemoryEntries(workspace, nil) +
					"\nCurrent-user memory search is available only in a direct chat.", nil
			}
			user, userErr := agent.CuratedMemory.Search(memory.CuratedTargetCurrentUser, caller, query, 20)
			if userErr != nil && !errors.Is(userErr, memory.ErrUserScopeUnavailable) {
				return "", userErr
			}
			return formatMemoryEntries(workspace, user), nil
		}
		rt.MemoryEdit = func(id, content string) (string, error) {
			target, err := findMemoryEntryTarget(
				agent.CuratedMemory,
				caller,
				id,
				memory.AllowsPrivateUserMemory(caller),
			)
			if err != nil {
				return "", err
			}
			_, err = agent.CuratedMemory.ApplyBatch(target, caller, []memory.CuratedMutation{{
				Action: memory.CuratedActionReplace, ID: id, Content: content,
				EvidenceKind: memory.CuratedEvidenceExplicit,
				Provenance:   memory.Provenance{Source: "user_command"},
			}}, false)
			if err != nil {
				return "", err
			}
			return "Updated memory entry " + id + ".", nil
		}
		rt.MemoryEntryAction = func(action, id string) (string, error) {
			target, err := findMemoryEntryTarget(
				agent.CuratedMemory,
				caller,
				id,
				memory.AllowsPrivateUserMemory(caller),
			)
			if err != nil {
				return "", err
			}
			action = strings.ToLower(strings.TrimSpace(action))
			switch action {
			case memory.CuratedActionPin, memory.CuratedActionUnpin,
				memory.CuratedActionArchive, memory.CuratedActionRestore:
			default:
				return "", memory.ErrCuratedInvalidAction
			}
			mutation := memory.CuratedMutation{
				Action: action, ID: id, Provenance: memory.Provenance{Source: "user_command"},
			}
			if action == memory.CuratedActionRestore {
				// A direct restore is an explicit user reaffirmation for structured
				// preference entries. The store ignores this evidence override for
				// non-preference entries.
				mutation.EvidenceKind = memory.CuratedEvidenceExplicit
			}
			_, err = agent.CuratedMemory.ApplyBatch(
				target,
				caller,
				[]memory.CuratedMutation{mutation},
				false,
			)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Memory entry %s: %s.", id, action), nil
		}
		rt.MemoryForget = func(id string) (string, error) {
			target, err := findMemoryEntryTarget(
				agent.CuratedMemory,
				caller,
				id,
				memory.AllowsPrivateUserMemory(caller),
			)
			if err != nil {
				return "", err
			}
			_, err = agent.CuratedMemory.ApplyBatch(target, caller, []memory.CuratedMutation{{
				Action: memory.CuratedActionRemove, ID: id,
				Provenance: memory.Provenance{Source: "user_command"},
			}}, false)
			if err != nil {
				return "", err
			}
			return "Forgot memory entry " + id + ".", nil
		}
		rt.MemoryPending = func() (string, error) {
			workspace, err := agent.CuratedMemory.Pending(memory.CuratedTargetWorkspace, caller)
			if err != nil {
				return "", err
			}
			if !memory.AllowsPrivateUserMemory(caller) {
				return formatPendingMemory(workspace, nil) +
					"\nCurrent-user pending changes are hidden in shared chats; use a direct chat to manage them.", nil
			}
			user, userErr := agent.CuratedMemory.Pending(memory.CuratedTargetCurrentUser, caller)
			if userErr != nil && !errors.Is(userErr, memory.ErrUserScopeUnavailable) {
				return "", userErr
			}
			return formatPendingMemory(workspace, user), nil
		}
		rt.MemoryApprove = func(id string) (string, error) {
			count, err := resolvePendingMemory(
				agent.CuratedMemory,
				caller,
				id,
				true,
				memory.AllowsPrivateUserMemory(caller),
			)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Approved %d memory operation(s).", count), nil
		}
		rt.MemoryReject = func(id string) (string, error) {
			count, err := resolvePendingMemory(
				agent.CuratedMemory,
				caller,
				id,
				false,
				memory.AllowsPrivateUserMemory(caller),
			)
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
			checkpoint, err := agent.Checkpoints.Apply(caller, "", memory.CheckpointMutation{
				Action: memory.CheckpointActionResume, ID: id,
			})
			if err != nil {
				return "", err
			}
			return fmt.Sprintf(
				"Resumed %s (%s). Next: %s",
				checkpoint.Title,
				checkpoint.ID,
				checkpoint.NextStep,
			), nil
		}
		rt.CheckpointForget = func(id string) (string, error) {
			checkpoint, err := agent.Checkpoints.Apply(caller, "", memory.CheckpointMutation{
				Action: memory.CheckpointActionArchive, ID: id,
			})
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

	caller := callerScopeForTurn(agent.ID, al.cfg, *opts)

	switch strings.ToLower(strings.TrimSpace(req.Operation)) {
	case "dashboard":
		return buildMemoryDashboardContent(agent, caller, opts.Dispatch.InboundContext), nil
	default:
		return nil, fmt.Errorf("memory subcommand not recognized")
	}
}

func buildMemoryDashboardContent(agent *AgentInstance, caller memory.CallerScope, inbound *bus.InboundContext) *bus.StructuredContent {
	content := &bus.StructuredContent{
		Title: "Personal Memory",
	}

	workspace, _ := agent.CuratedMemory.Stats(memory.CuratedTargetWorkspace, caller)
	status := fmt.Sprintf("Workspace entries: %d", workspace.Entries)
	if memory.AllowsPrivateUserMemory(caller) {
		user, err := agent.CuratedMemory.Stats(memory.CuratedTargetCurrentUser, caller)
		if err == nil {
			status += fmt.Sprintf("\nUser entries: %d", user.Entries)
		}
	} else {
		status += "\nUser entries hidden in shared scope."
	}
	
	content.Paragraphs = []string{
		status,
		"Choose an action below to manage memory.",
	}

	content.Interaction = &bus.InteractionMenu{
		Kind:    "memory",
		OwnerID: inbound.SenderID,
		AgentID: agent.ID,
		Channel: inbound.Channel,
		Entries: []bus.InteractionEntry{
			{Action: "profile", Label: "My Profile"},
			{Action: "browse", Label: "Browse"},
			{Action: "search", Label: "Search"},
			{Action: "pending", Label: "Pending"},
			{Action: "close", Label: "Close"},
		},
	}
	return content
}

func (al *AgentLoop) handleInternalMemoryCallback(
	ctx context.Context,
	req bus.InternalCallbackRequest,
) (*bus.InternalCallbackResponse, error) {
	inbound := bus.NormalizeInboundMessage(bus.InboundMessage{Context: req.Inbound}).Context
	if strings.TrimSpace(req.OwnerID) == "" || inbound.SenderID != req.OwnerID ||
		inbound.Channel != req.Channel || inbound.Account != req.Account ||
		inbound.ChatID != req.ChatID || inbound.TopicID != req.TopicID {
		return nil, fmt.Errorf("callback scope validation failed")
	}
	_, agent, routeErr := al.resolveMessageRoute(bus.InboundMessage{Context: inbound})
	if routeErr != nil || agent == nil || !strings.EqualFold(agent.ID, req.AgentID) {
		return nil, fmt.Errorf("callback agent validation failed")
	}
	
	action := strings.ToLower(strings.TrimSpace(req.Action))
	
	opts := processOptions{InboundContext: &inbound}
	caller := callerScopeForTurn(agent.ID, al.cfg, opts)

	switch action {
	case "close":
		return &bus.InternalCallbackResponse{Close: true}, nil
	case "dashboard":
		return &bus.InternalCallbackResponse{Content: buildMemoryDashboardContent(agent, caller, &inbound)}, nil
	case "profile":
		if !memory.AllowsPrivateUserMemory(caller) {
			return nil, memory.ErrPrivateContextRequired
		}
		profile, err := agent.CuratedMemory.CompileUserProfile(caller, memory.UserProfileOptions{
			MaxChars:      al.cfg.Memory.Profile.EffectiveMaxChars(),
			MinConfidence: al.cfg.Memory.Profile.EffectiveMinConfidence(),
		})
		var text string
		if err != nil {
			text = "Failed to compile profile: " + err.Error()
		} else {
			text = formatUserProfile(profile)
		}
		
		content := &bus.StructuredContent{
			Title:      "My Profile",
			Paragraphs: []string{text},
			Interaction: &bus.InteractionMenu{
				Kind:    "memory",
				OwnerID: inbound.SenderID,
				AgentID: agent.ID,
				Channel: inbound.Channel,
				Entries: []bus.InteractionEntry{
					{Action: "dashboard", Label: "Back to Dashboard"},
					{Action: "close", Label: "Close"},
				},
			},
		}
		return &bus.InternalCallbackResponse{Content: content}, nil
	case "browse":
		workspace, _ := agent.CuratedMemory.List(memory.CuratedTargetWorkspace, caller)
		user, _ := agent.CuratedMemory.List(memory.CuratedTargetCurrentUser, caller)
		
		allEntries := append(workspace, user...)
		
		// Simple pagination
		page := req.Page
		if page < 0 {
			page = 0
		}
		perPage := 5
		pages := (len(allEntries) + perPage - 1) / perPage
		if page >= pages && pages > 0 {
			page = pages - 1
		}
		
		start := page * perPage
		end := start + perPage
		if end > len(allEntries) {
			end = len(allEntries)
		}
		
		pageEntries := allEntries[start:end]
		
		var lines []string
		var entries []bus.InteractionEntry
		
		for i, entry := range pageEntries {
			content := truncateMemoryCommandText(memory.RedactMemoryText(entry.Content), 50)
			lines = append(lines, fmt.Sprintf("%d. %s — %s", i+1, entry.ID, content))
			entries = append(entries, bus.InteractionEntry{
				Action: "detail",
				Label:  fmt.Sprintf("%d. %s", i+1, entry.ID),
				Value:  entry.ID,
			})
		}
		
		if len(lines) == 0 {
			lines = append(lines, "No memory entries found.")
		}
		
		if page > 0 {
			entries = append(entries, bus.InteractionEntry{Action: "page", Label: "◀️ Prev", Value: fmt.Sprintf("%d", page-1)})
		}
		if page < pages-1 {
			entries = append(entries, bus.InteractionEntry{Action: "page", Label: "Next ▶️", Value: fmt.Sprintf("%d", page+1)})
		}
		
		entries = append(entries, bus.InteractionEntry{Action: "dashboard", Label: "Back to Dashboard"})
		entries = append(entries, bus.InteractionEntry{Action: "close", Label: "Close"})
		
		content := &bus.StructuredContent{
			Title:      "Memory Browse",
			Paragraphs: []string{strings.Join(lines, "\n")},
			Interaction: &bus.InteractionMenu{
				Kind:    "memory",
				OwnerID: inbound.SenderID,
				AgentID: agent.ID,
				Channel: inbound.Channel,
				Page:    page,
				Pages:   pages,
				Entries: entries,
			},
		}
		return &bus.InternalCallbackResponse{Content: content}, nil
	case "detail":
		id := req.Value
		target, err := findMemoryEntryTarget(agent.CuratedMemory, caller, id, memory.AllowsPrivateUserMemory(caller))
		if err != nil {
			return nil, err
		}
		list, _ := agent.CuratedMemory.List(target, caller)
		var matched *memory.CuratedEntry
		for _, e := range list {
			if e.ID == id {
				matched = &e
				break
			}
		}
		if matched == nil {
			return nil, fmt.Errorf("entry not found")
		}
		
		content := &bus.StructuredContent{
			Title: "Memory Detail: " + matched.ID,
			Paragraphs: []string{
				fmt.Sprintf("Type: %s", matched.EffectiveType()),
				fmt.Sprintf("Status: %s", matched.EffectiveStatus()),
				fmt.Sprintf("Pinned: %v", matched.Pinned),
				"\n" + matched.Content,
			},
			Interaction: &bus.InteractionMenu{
				Kind:    "memory",
				OwnerID: inbound.SenderID,
				AgentID: agent.ID,
				Channel: inbound.Channel,
				Entries: []bus.InteractionEntry{
					{Action: "edit", Label: "Edit", Value: ""},
					{Action: "forget_confirm", Label: "Forget", Value: matched.ID},
					{Action: "browse", Label: "Back to List"},
					{Action: "close", Label: "Close"},
				},
			},
		}
		return &bus.InternalCallbackResponse{Content: content}, nil
	case "forget_confirm":
		id := req.Value
		content := &bus.StructuredContent{
			Title: "Confirm Forget: " + id,
			Paragraphs: []string{"Are you sure you want to forget this entry? This action cannot be undone."},
			Interaction: &bus.InteractionMenu{
				Kind:    "memory",
				OwnerID: inbound.SenderID,
				AgentID: agent.ID,
				Channel: inbound.Channel,
				Entries: []bus.InteractionEntry{
					{Action: "forget", Label: "Confirm Forget", Value: id},
					{Action: "detail", Label: "Cancel", Value: id},
				},
			},
		}
		return &bus.InternalCallbackResponse{Content: content}, nil
	case "forget":
		id := req.Value
		target, err := findMemoryEntryTarget(agent.CuratedMemory, caller, id, memory.AllowsPrivateUserMemory(caller))
		if err != nil {
			return nil, err
		}
		_, err = agent.CuratedMemory.ApplyBatch(target, caller, []memory.CuratedMutation{{
			Action: memory.CuratedActionRemove, ID: id,
			Provenance: memory.Provenance{Source: "user_command"},
		}}, false)
		if err != nil {
			return nil, err
		}
		return &bus.InternalCallbackResponse{Text: "Forgot memory entry " + id}, nil
	case "page":
		parsed, _ := strconv.Atoi(req.Value)
		return al.handleInternalMemoryCallback(ctx, bus.InternalCallbackRequest{
			Kind: req.Kind, Action: "browse", Value: "", Page: parsed,
			OwnerID: req.OwnerID, Channel: req.Channel, Account: req.Account,
			ChatID: req.ChatID, TopicID: req.TopicID, MessageID: req.MessageID,
			AgentID: req.AgentID, Scope: req.Scope, Inbound: req.Inbound,
		})
	case "pending":
		workspace, _ := agent.CuratedMemory.Pending(memory.CuratedTargetWorkspace, caller)
		user, _ := agent.CuratedMemory.Pending(memory.CuratedTargetCurrentUser, caller)
		
		text := formatPendingMemory(workspace, user)
		content := &bus.StructuredContent{
			Title:      "Pending Memory",
			Paragraphs: []string{text},
			Interaction: &bus.InteractionMenu{
				Kind:    "memory",
				OwnerID: inbound.SenderID,
				AgentID: agent.ID,
				Channel: inbound.Channel,
				Entries: []bus.InteractionEntry{
					{Action: "dashboard", Label: "Back to Dashboard"},
					{Action: "close", Label: "Close"},
				},
			},
		}
		return &bus.InternalCallbackResponse{Content: content}, nil
	case "search":
		if strings.TrimSpace(req.Value) == "" {
			return &bus.InternalCallbackResponse{Text: "Balas pesan ini dengan kata kunci pencarian:"}, nil
		}
		
		workspace, _ := agent.CuratedMemory.Search(memory.CuratedTargetWorkspace, caller, req.Value, 20)
		user, _ := agent.CuratedMemory.Search(memory.CuratedTargetCurrentUser, caller, req.Value, 20)
		
		text := formatMemoryEntries(workspace, user)
		if text == "" || text == "Workspace memory:\n- (empty)\nCurrent-user memory:\n- (empty)" {
			text = "No results found for query: " + req.Value
		}
		
		content := &bus.StructuredContent{
			Title:      "Search Results",
			Paragraphs: []string{text},
			Interaction: &bus.InteractionMenu{
				Kind:    "memory",
				OwnerID: inbound.SenderID,
				AgentID: agent.ID,
				Channel: inbound.Channel,
				Entries: []bus.InteractionEntry{
					{Action: "search", Label: "Search Again"},
					{Action: "dashboard", Label: "Back to Dashboard"},
					{Action: "close", Label: "Close"},
				},
			},
		}
		return &bus.InternalCallbackResponse{Content: content}, nil
	case "edit":
		if strings.TrimSpace(req.Value) == "" {
			return &bus.InternalCallbackResponse{Text: "Balas pesan ini dengan format `<id> <konten baru>`:"}, nil
		}
		parts := strings.SplitN(strings.TrimSpace(req.Value), " ", 2)
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid edit format")
		}
		id, editedContent := parts[0], parts[1]
		
		target, err := findMemoryEntryTarget(
			agent.CuratedMemory,
			caller,
			id,
			memory.AllowsPrivateUserMemory(caller),
		)
		if err != nil {
			return nil, err
		}
		_, err = agent.CuratedMemory.ApplyBatch(target, caller, []memory.CuratedMutation{{
			Action: memory.CuratedActionReplace, ID: id, Content: editedContent,
			EvidenceKind: memory.CuratedEvidenceExplicit,
			Provenance:   memory.Provenance{Source: "user_command"},
		}}, false)
		if err != nil {
			return nil, err
		}
		
		return &bus.InternalCallbackResponse{Text: "Updated memory entry " + id}, nil
	default:
		return nil, fmt.Errorf("invalid memory callback action")
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
	lines = append(
		lines,
		fmt.Sprintf("Profile size: %d characters; sources: %d", profile.Characters, len(profile.SourceIDs)),
	)
	return strings.Join(lines, "\n")
}

func formatMemoryStats(stats memory.CuratedStats) string {
	return fmt.Sprintf(
		"%s: %d entries, %d/%d characters, %d pending",
		stats.Target,
		stats.Entries,
		stats.Characters,
		stats.Capacity,
		stats.PendingCount,
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
			content := memory.RedactMemoryText(entry.Content)
			content = truncateMemoryCommandText(content, remainingChars)
			remainingChars -= len([]rune(content))
			remainingEntries--
			lines = append(
				lines,
				fmt.Sprintf(
					"- `%s` [%s/%s%s] — %s",
					entry.ID,
					entry.EffectiveType(),
					entry.EffectiveStatus(),
					map[bool]string{true: ", pinned"}[entry.Pinned],
					content,
				),
			)
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
	if includeCurrentUser {
		entries, err := store.List(memory.CuratedTargetCurrentUser, caller)
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
				change.ID,
				target,
				len(change.Mutations),
				change.CreatedAt.UTC().Format("2006-01-02 15:04Z"),
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
