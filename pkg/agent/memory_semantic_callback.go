package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/commands"
)

// handleInternalMemorySemanticCallback preserves the existing opaque callback
// envelope and owner/route validation while translating allowlisted actions to
// the same typed MemoryCommand request used by textual /memory operations.
func (al *AgentLoop) handleInternalMemorySemanticCallback(
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
	normalizeProcessOptionsInPlace(&opts)
	action := strings.ToLower(strings.TrimSpace(req.Action))

	defer func() {
		if err != nil || response == nil || response.Content == nil || response.Content.Interaction == nil {
			return
		}
		menu := response.Content.Interaction
		bound, bindErr := newMemoryInteractionMenu(
			&inbound,
			agent.ID,
			menu.Page,
			menu.Pages,
			menu.Current,
			menu.Entries,
		)
		if bindErr != nil {
			response = nil
			err = bindErr
			return
		}
		bound.Query = menu.Query
		bound.SessionKey = menu.SessionKey
		response.Content.Interaction = bound
	}()

	if action == "close" {
		return &bus.InternalCallbackResponse{Close: true}, nil
	}
	if action == "noop" {
		return &bus.InternalCallbackResponse{Text: fmt.Sprintf("Halaman %d", req.Page+1)}, nil
	}

	semantic := commands.MemoryCommandRequest{Interactive: true, Page: req.Page}
	switch action {
	case "dashboard", "profile":
		semantic.Operation = action
	case "browse", "browse_page", "page":
		page, pageErr := memoryRequestedPage(req, action)
		if pageErr != nil {
			return nil, pageErr
		}
		semantic.Operation = "list"
		semantic.Page = page
	case "detail":
		semantic.Operation = "detail"
		semantic.ID = strings.TrimSpace(req.Value)
	case "pin", "unpin", "archive", "restore", "forget":
		semantic.Operation = action
		semantic.ID = strings.TrimSpace(req.Value)
	case "forget_confirm":
		semantic.Operation = "detail"
		semantic.ID = strings.TrimSpace(req.Value)
		if _, semanticErr := al.executeMemorySemanticCommand(ctx, agent, &opts, semantic); semanticErr != nil {
			return nil, semanticErr
		}
		return &bus.InternalCallbackResponse{Content: &bus.StructuredContent{
			Title:      "Lupakan Memori Ini?",
			Paragraphs: []string{"Tindakan ini akan menghapus entri memori yang dipilih. Lanjutkan?"},
			Interaction: &bus.InteractionMenu{Current: semantic.ID, Entries: []bus.InteractionEntry{
				{Action: "forget", Label: "✅ Konfirmasi", Value: semantic.ID},
				{Action: "detail", Label: "❌ Batal", Value: semantic.ID},
			}},
		}}, nil
	case "pending", "pending_page":
		page, pageErr := memoryRequestedPage(req, action)
		if pageErr != nil {
			return nil, pageErr
		}
		semantic.Operation = "pending"
		semantic.Page = page
	case "approve", "reject":
		semantic.Operation = action
		semantic.ID = strings.TrimSpace(req.Value)
	case "search":
		query := strings.TrimSpace(req.Value)
		if query == "" {
			return &bus.InternalCallbackResponse{Text: "Balas pesan ini dengan kata kunci pencarian:"}, nil
		}
		semantic.Operation = "search"
		semantic.Query = query
		semantic.Page = 0
	case "search_page":
		page, pageErr := memoryRequestedPage(req, action)
		if pageErr != nil {
			return nil, pageErr
		}
		query := strings.TrimSpace(req.Query)
		if query == "" {
			return nil, fmt.Errorf("memory search state is unavailable")
		}
		semantic.Operation = "search"
		semantic.Query = query
		semantic.Page = page
	case "edit":
		content := strings.TrimSpace(req.Value)
		id := strings.TrimSpace(req.SessionKey)
		if content == "" {
			if id == "" {
				return nil, fmt.Errorf("memory edit target is unavailable")
			}
			if _, semanticErr := al.executeMemorySemanticCommand(ctx, agent, &opts, commands.MemoryCommandRequest{
				Operation: "detail", ID: id, Interactive: true,
			}); semanticErr != nil {
				return nil, semanticErr
			}
			return &bus.InternalCallbackResponse{Text: "Balas pesan ini dengan konten baru untuk entri memori ini:"}, nil
		}
		if id == "" {
			return nil, fmt.Errorf("memory edit target is unavailable")
		}
		semantic.Operation = "edit"
		semantic.ID = id
		semantic.Content = content
	default:
		return nil, fmt.Errorf("invalid memory callback action")
	}

	content, semanticErr := al.executeMemorySemanticCommand(ctx, agent, &opts, semantic)
	if semanticErr != nil {
		return nil, semanticErr
	}
	result := &bus.InternalCallbackResponse{Content: content}
	if action == "search" && strings.TrimSpace(req.Value) != "" {
		result.Transition = bus.InteractionAppendContinuation
	}
	return result, nil
}
