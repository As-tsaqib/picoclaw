package agent

import "context"

// applyExplicitSkillCommand is retained as a narrow internal compatibility
// shim for existing tests and callers while all semantics are owned by
// applyUseIntent and commands.ParseUseIntent.
func (al *AgentLoop) applyExplicitSkillCommand(
	raw string,
	agent *AgentInstance,
	opts *processOptions,
) (matched bool, handled bool, reply string) {
	matched, handled, reply, _ = al.applyUseIntent(context.Background(), raw, agent, opts)
	return matched, handled, reply
}
