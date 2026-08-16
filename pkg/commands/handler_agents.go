package commands

import (
	"context"
	"fmt"
	"strings"
)

// agentsHandler returns a shared handler for both /show agents and /list agents.
func agentsHandler() Handler {
	return func(_ context.Context, req Request, rt *Runtime) error {
		if rt == nil || rt.ListAgentIDs == nil {
			return req.Reply(unavailableMsg)
		}
		ids := rt.ListAgentIDs()
		if len(ids) == 0 {
			return req.Reply("No agents registered")
		}
		fallback := fmt.Sprintf("Registered agents: %s", strings.Join(ids, ", "))
		return req.replyStructured(numberedListContent("Agents", "Agent", ids, fallback))
	}
}
