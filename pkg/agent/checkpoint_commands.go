package agent

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/commands"
	"github.com/As-tsaqib/picoclaw/pkg/memory"
	"github.com/As-tsaqib/picoclaw/pkg/session"
)

const checkpointInteractionPageSize = 5

type checkpointStore interface {
	List(memory.CallerScope, bool) ([]memory.TaskCheckpoint, error)
	Get(memory.CallerScope, string) (memory.TaskCheckpoint, error)
	Apply(memory.CallerScope, string, memory.CheckpointMutation) (memory.TaskCheckpoint, error)
}

type checkpointCommandService struct {
	store  checkpointStore
	caller memory.CallerScope
}

func newCheckpointCommandService(store checkpointStore, caller memory.CallerScope) checkpointCommandService {
	return checkpointCommandService{store: store, caller: caller}
}

func (s checkpointCommandService) list() ([]memory.TaskCheckpoint, error) {
	if s.store == nil {
		return nil, fmt.Errorf("checkpoints are unavailable")
	}
	return s.store.List(s.caller, false)
}

func (s checkpointCommandService) detail(id string) (memory.TaskCheckpoint, error) {
	if s.store == nil {
		return memory.TaskCheckpoint{}, fmt.Errorf("checkpoints are unavailable")
	}
	checkpoint, err := s.store.Get(s.caller, strings.TrimSpace(id))
	if err != nil {
		return memory.TaskCheckpoint{}, err
	}
	if checkpoint.Status == memory.CheckpointStatusArchived || checkpoint.Status == memory.CheckpointStatusCompleted {
		return memory.TaskCheckpoint{}, fmt.Errorf("checkpoint is not available in the active dashboard")
	}
	return checkpoint, nil
}

func (s checkpointCommandService) resume(id string) (memory.TaskCheckpoint, error) {
	if s.store == nil {
		return memory.TaskCheckpoint{}, fmt.Errorf("checkpoints are unavailable")
	}
	return s.store.Apply(s.caller, "", memory.CheckpointMutation{
		Action: memory.CheckpointActionResume, ID: strings.TrimSpace(id),
	})
}

func (s checkpointCommandService) archive(id string) (memory.TaskCheckpoint, error) {
	if s.store == nil {
		return memory.TaskCheckpoint{}, fmt.Errorf("checkpoints are unavailable")
	}
	return s.store.Apply(s.caller, "", memory.CheckpointMutation{
		Action: memory.CheckpointActionArchive, ID: strings.TrimSpace(id),
	})
}

func configureCheckpointCommandRuntime(
	rt *commands.Runtime,
	agent *AgentInstance,
	opts *processOptions,
	al *AgentLoop,
) {
	if rt == nil || agent == nil || opts == nil || al == nil {
		return
	}
	rt.CheckpointCommand = func(
		ctx context.Context,
		req commands.CheckpointCommandRequest,
	) (*bus.StructuredContent, error) {
		return al.executeCheckpointCommand(ctx, agent, opts, req)
	}
}

func (al *AgentLoop) executeCheckpointCommand(
	_ context.Context,
	agent *AgentInstance,
	opts *processOptions,
	req commands.CheckpointCommandRequest,
) (*bus.StructuredContent, error) {
	if agent == nil || agent.Checkpoints == nil || opts == nil {
		return nil, fmt.Errorf("checkpoints are unavailable")
	}
	caller := callerScopeForTurn(agent.ID, al.cfg, *opts)
	service := newCheckpointCommandService(agent.Checkpoints, caller)
	op := strings.ToLower(strings.TrimSpace(req.Operation))
	switch op {
	case "list":
		checkpoints, err := service.list()
		if err != nil {
			return nil, err
		}
		text := formatCheckpointList(checkpoints)
		return &bus.StructuredContent{Title: "Task Checkpoints", Paragraphs: []string{text}, Fallback: text}, nil
	case "resume":
		checkpoint, err := service.resume(req.ID)
		if err != nil {
			return nil, err
		}
		text := fmt.Sprintf("Resumed %s (%s). Next: %s", checkpoint.Title, checkpoint.ID, checkpoint.NextStep)
		return paragraphContent(text), nil
	case "archive":
		checkpoint, err := service.archive(req.ID)
		if err != nil {
			return nil, err
		}
		text := fmt.Sprintf("Archived checkpoint %s (%s).", checkpoint.Title, checkpoint.ID)
		return paragraphContent(text), nil
	case "dashboard":
		inbound := opts.Dispatch.InboundContext
		if inbound == nil || !memoryInteractionRouteIsPrivate(inbound) {
			return &bus.StructuredContent{
				Title: "Task Checkpoints",
				Paragraphs: []string{
					"Checkpoint details are private. Use a direct chat or a verified private/ephemeral command route.",
				},
			}, nil
		}
		if opts.Dispatch.SessionScope == nil || strings.TrimSpace(opts.Dispatch.SessionKey) == "" {
			return nil, fmt.Errorf("checkpoint session binding is unavailable")
		}
		return buildCheckpointPage(agent, caller, opts.Dispatch.SessionKey, opts.Dispatch.SessionScope,
			inbound, req.Page)
	default:
		return nil, fmt.Errorf("checkpoint operation is not supported")
	}
}

func buildCheckpointPage(
	agent *AgentInstance,
	caller memory.CallerScope,
	sessionKey string,
	scope *session.SessionScope,
	inbound *bus.InboundContext,
	page int,
) (*bus.StructuredContent, error) {
	checkpoints, err := newCheckpointCommandService(agent.Checkpoints, caller).list()
	if err != nil {
		return nil, err
	}
	pages := 1
	if len(checkpoints) > 0 {
		pages = (len(checkpoints) + checkpointInteractionPageSize - 1) / checkpointInteractionPageSize
	}
	if page < 0 {
		page = 0
	}
	if page >= pages {
		page = pages - 1
	}
	start := page * checkpointInteractionPageSize
	end := start + checkpointInteractionPageSize
	if end > len(checkpoints) {
		end = len(checkpoints)
	}
	entries := make([]bus.InteractionEntry, 0, 10)
	lines := make([]string, 0, checkpointInteractionPageSize+1)
	if len(checkpoints) == 0 {
		lines = append(lines, "No active or suspended checkpoints in this session.")
	}
	for i := start; i < end; i++ {
		cp := checkpoints[i]
		label := strconv.Itoa(i - start + 1)
		lines = append(lines, fmt.Sprintf("%s. %s — %s", label, compactCheckpointText(cp.Title, 96), cp.Status))
		entries = append(entries, bus.InteractionEntry{Label: label, Action: "detail", Value: cp.ID})
	}
	if page > 0 {
		entries = append(entries, bus.InteractionEntry{Label: "◀️", Action: "page", Value: strconv.Itoa(page - 1)})
	}
	entries = append(entries, bus.InteractionEntry{Label: fmt.Sprintf("%d/%d", page+1, pages), Action: "noop"})
	if page+1 < pages {
		entries = append(entries, bus.InteractionEntry{Label: "▶️", Action: "page", Value: strconv.Itoa(page + 1)})
	}
	entries = append(entries, bus.InteractionEntry{Label: "✖️ Close", Action: "close"})
	return &bus.StructuredContent{
		Title: "Task Checkpoints", Paragraphs: lines,
		Interaction: newBoundInteractionMenu(
			"checkpoint", agent.ID, sessionKey, scope, inbound, page, pages, "", "", entries,
		),
	}, nil
}

func buildCheckpointDetail(
	agent *AgentInstance,
	checkpoint memory.TaskCheckpoint,
	sessionKey string,
	scope *session.SessionScope,
	inbound *bus.InboundContext,
	page int,
) *bus.StructuredContent {
	paragraphs := []string{
		"Status: " + checkpoint.Status,
		"Objective: " + compactCheckpointText(checkpoint.Objective, 600),
	}
	if current := compactCheckpointText(checkpoint.CurrentStep, 500); current != "" {
		paragraphs = append(paragraphs, "Current: "+current)
	}
	if next := compactCheckpointText(checkpoint.NextStep, 500); next != "" {
		paragraphs = append(paragraphs, "Next: "+next)
	}
	if len(checkpoint.CompletedItems) > 0 {
		limit := len(checkpoint.CompletedItems)
		if limit > 5 {
			limit = 5
		}
		items := make([]string, 0, limit)
		for _, item := range checkpoint.CompletedItems[:limit] {
			items = append(items, compactCheckpointText(item, 160))
		}
		paragraphs = append(paragraphs, "Completed: "+strings.Join(items, "; "))
	}
	paragraphs = append(paragraphs, "Updated: "+checkpoint.UpdatedAt.Local().Format("02 Jan 2006 15:04"))
	entries := make([]bus.InteractionEntry, 0, 4)
	if checkpoint.Status == memory.CheckpointStatusActive || checkpoint.Status == memory.CheckpointStatusSuspended {
		entries = append(entries, bus.InteractionEntry{Label: "▶️ Resume", Action: "resume", Value: checkpoint.ID})
	}
	entries = append(entries,
		bus.InteractionEntry{Label: "🗄️ Archive", Action: "archive", Value: checkpoint.ID},
		bus.InteractionEntry{Label: "↩️ Back", Action: "dashboard"},
		bus.InteractionEntry{Label: "✖️ Close", Action: "close"},
	)
	return &bus.StructuredContent{
		Title: compactCheckpointText(checkpoint.Title, 180), Paragraphs: paragraphs,
		Interaction: newBoundInteractionMenu(
			"checkpoint", agent.ID, sessionKey, scope, inbound, page, maxInt(page+1, 1), "", checkpoint.ID, entries,
		),
	}
}

func buildCheckpointArchiveConfirm(
	agent *AgentInstance,
	checkpoint memory.TaskCheckpoint,
	sessionKey string,
	scope *session.SessionScope,
	inbound *bus.InboundContext,
	page int,
) *bus.StructuredContent {
	return &bus.StructuredContent{
		Title: "Archive Checkpoint?",
		Paragraphs: []string{
			compactCheckpointText(checkpoint.Title, 180),
			"This hides the checkpoint from the active dashboard. Continue?",
		},
		Interaction: newBoundInteractionMenu(
			"checkpoint", agent.ID, sessionKey, scope, inbound, page, maxInt(page+1, 1), "", checkpoint.ID, []bus.InteractionEntry{
				{Label: "✅ Confirm Archive", Action: "archive_confirm", Value: checkpoint.ID},
				{Label: "❌ Cancel", Action: "detail", Value: checkpoint.ID},
			},
		),
	}
}

func (al *AgentLoop) handleInternalCheckpointCallback(
	_ context.Context,
	req bus.InternalCallbackRequest,
) (*bus.InternalCallbackResponse, error) {
	bound, err := al.resolveBoundInteraction(req)
	if err != nil {
		return nil, err
	}
	if bound.agent.Checkpoints == nil {
		return nil, fmt.Errorf("checkpoints are unavailable")
	}
	if !memoryInteractionRouteIsPrivate(&bound.inbound) {
		return nil, fmt.Errorf("checkpoint callback requires a private route")
	}
	caller := callerScopeFromInbound(bound.agent.ID, req.SessionKey, &bound.inbound, &bound.allocation.Scope, al.cfg)
	service := newCheckpointCommandService(bound.agent.Checkpoints, caller)
	action := strings.ToLower(strings.TrimSpace(req.Action))
	switch action {
	case "close":
		return &bus.InternalCallbackResponse{Close: true}, nil
	case "noop":
		return &bus.InternalCallbackResponse{Text: fmt.Sprintf("Page %d", req.Page+1)}, nil
	case "dashboard":
		content, buildErr := buildCheckpointPage(
			bound.agent, caller, req.SessionKey, &bound.allocation.Scope, &bound.inbound, 0,
		)
		return &bus.InternalCallbackResponse{Content: content}, buildErr
	case "back":
		content, buildErr := buildCheckpointPage(
			bound.agent, caller, req.SessionKey, &bound.allocation.Scope, &bound.inbound, req.Page,
		)
		return &bus.InternalCallbackResponse{Content: content}, buildErr
	case "page":
		page, parseErr := strconv.Atoi(strings.TrimSpace(req.Value))
		if parseErr != nil || page < 0 {
			return nil, fmt.Errorf("invalid checkpoint page")
		}
		content, buildErr := buildCheckpointPage(
			bound.agent, caller, req.SessionKey, &bound.allocation.Scope, &bound.inbound, page,
		)
		return &bus.InternalCallbackResponse{Content: content}, buildErr
	case "detail":
		checkpoint, getErr := service.detail(req.Value)
		if getErr != nil {
			return nil, getErr
		}
		return &bus.InternalCallbackResponse{Content: buildCheckpointDetail(
			bound.agent, checkpoint, req.SessionKey, &bound.allocation.Scope, &bound.inbound, req.Page,
		)}, nil
	case "resume":
		checkpoint, applyErr := service.resume(req.Value)
		if applyErr != nil {
			return nil, applyErr
		}
		return &bus.InternalCallbackResponse{Content: buildCheckpointDetail(
			bound.agent, checkpoint, req.SessionKey, &bound.allocation.Scope, &bound.inbound, req.Page,
		)}, nil
	case "archive":
		checkpoint, getErr := service.detail(req.Value)
		if getErr != nil {
			return nil, getErr
		}
		return &bus.InternalCallbackResponse{Content: buildCheckpointArchiveConfirm(
			bound.agent, checkpoint, req.SessionKey, &bound.allocation.Scope, &bound.inbound, req.Page,
		)}, nil
	case "archive_confirm":
		if _, applyErr := service.archive(req.Value); applyErr != nil {
			return nil, applyErr
		}
		content, buildErr := buildCheckpointPage(
			bound.agent, caller, req.SessionKey, &bound.allocation.Scope, &bound.inbound, req.Page,
		)
		if buildErr == nil {
			content.Title = "Checkpoint Archived"
		}
		return &bus.InternalCallbackResponse{Content: content}, buildErr
	default:
		return nil, fmt.Errorf("invalid checkpoint callback action")
	}
}

func compactCheckpointText(value string, maxRunes int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes-1]) + "…"
}
