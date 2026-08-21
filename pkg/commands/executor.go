package commands

import (
	"context"
	"fmt"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
)

type Outcome int

const (
	// OutcomePassthrough means this input should continue through normal agent flow.
	OutcomePassthrough Outcome = iota
	// OutcomeHandled means a command handler executed (with or without handler error).
	OutcomeHandled
)

type ExecuteResult struct {
	Outcome Outcome
	Command string
	Err     error
}

type Executor struct {
	reg *Registry
	rt  *Runtime
}

func NewExecutor(reg *Registry, rt *Runtime) *Executor {
	return &Executor{reg: reg, rt: rt}
}

// Execute implements a two-state command decision:
// 1) handled: execute command immediately;
// 2) passthrough: normal non-command text or a registered command intentionally
// deferred to agent logic.
//
// Any syntactically valid slash/bang-prefixed command that is not registered is
// handled here and therefore cannot fall through into the normal LLM pipeline.
func (e *Executor) Execute(ctx context.Context, req Request) ExecuteResult {
	cmdName, ok := parseCommandName(req.Text)
	if !ok {
		return ExecuteResult{Outcome: OutcomePassthrough}
	}
	req = ensureReply(req)

	if e == nil || e.reg == nil {
		err := req.Reply("Command routing is unavailable. Use /help to see available commands.")
		return ExecuteResult{Outcome: OutcomeHandled, Command: cmdName, Err: err}
	}

	def, found := e.reg.Lookup(cmdName)
	if !found {
		err := req.Reply(fmt.Sprintf("Unknown command: /%s. Use /help to see available commands.", cmdName))
		return ExecuteResult{Outcome: OutcomeHandled, Command: cmdName, Err: err}
	}

	return e.executeDefinition(ctx, req, def)
}

func ensureReply(req Request) Request {
	if req.Reply == nil {
		req.Reply = func(string) error { return nil }
	}
	if req.ReplyStructured == nil {
		req.ReplyStructured = func(content bus.StructuredContent) error {
			return req.Reply(content.FallbackText())
		}
	}
	return req
}

func (e *Executor) executeDefinition(ctx context.Context, req Request, def Definition) ExecuteResult {
	req = ensureReply(req)

	// Simple command — no sub-commands
	if len(def.SubCommands) == 0 {
		if def.Handler == nil {
			return ExecuteResult{Outcome: OutcomePassthrough, Command: def.Name}
		}
		err := def.Handler(ctx, req, e.rt)
		return ExecuteResult{Outcome: OutcomeHandled, Command: def.Name, Err: err}
	}

	// Sub-command routing
	subName := nthToken(req.Text, 1)
	if subName == "" {
		if def.Handler != nil {
			err := def.Handler(ctx, req, e.rt)
			return ExecuteResult{Outcome: OutcomeHandled, Command: def.Name, Err: err}
		}
		err := req.Reply("Usage: " + def.EffectiveUsage())
		return ExecuteResult{Outcome: OutcomeHandled, Command: def.Name, Err: err}
	}

	for _, sc := range def.SubCommands {
		if sc.matches(subName) {
			if sc.Handler == nil {
				return ExecuteResult{Outcome: OutcomePassthrough, Command: def.Name}
			}
			err := sc.Handler(ctx, req, e.rt)
			return ExecuteResult{Outcome: OutcomeHandled, Command: def.Name, Err: err}
		}
	}

	// Unknown sub-command is also terminal and never becomes prompt text.
	err := req.Reply(fmt.Sprintf("Unknown option: %s. Use /help %s for valid options.", subName, def.Name))
	return ExecuteResult{Outcome: OutcomeHandled, Command: def.Name, Err: err}
}
