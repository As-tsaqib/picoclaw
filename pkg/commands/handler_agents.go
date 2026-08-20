package commands

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// agentsHandler returns a shared handler for both /show agents and /list agents.
func agentsHandler() Handler {
	return func(_ context.Context, req Request, rt *Runtime) error {
		if rt == nil || rt.ListAgentIDs == nil {
			return req.Reply(unavailableMsg)
		}
		ids := append([]string(nil), rt.ListAgentIDs()...)
		sort.SliceStable(ids, func(i, j int) bool {
			left, right := strings.ToLower(ids[i]), strings.ToLower(ids[j])
			if left == right {
				return ids[i] < ids[j]
			}
			return left < right
		})
		if len(ids) == 0 {
			return req.Reply("No agents registered")
		}
		fallback := fmt.Sprintf("Registered agents: %s", strings.Join(ids, ", "))
		return req.replyStructured(numberedListContent("Agents", "Agent", ids, fallback))
	}
}
