package commands

import (
	"context"
	"strings"
	"testing"
)

func TestBuiltinHelpHandler_ReturnsFormattedMessage(t *testing.T) {
	ex := NewExecutor(NewRegistry(BuiltinDefinitions()), nil)

	var reply string
	res := ex.Execute(context.Background(), Request{
		Text: "/help",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	})
	if res.Outcome != OutcomeHandled {
		t.Fatalf("outcome=%v, want=%v", res.Outcome, OutcomeHandled)
	}

	if !strings.Contains(reply, "/help") || !strings.Contains(reply, "/model") {
		t.Fatalf("reply=%q, expected to contain standard commands", reply)
	}
}

func TestBuiltinStop_UsesRuntimeStopper(t *testing.T) {
	called := false
	rt := &Runtime{
		StopActiveTurn: func() (StopResult, error) {
			called = true
			return StopResult{Stopped: true, TaskName: "some-task"}, nil
		},
	}
	ex := NewExecutor(NewRegistry(BuiltinDefinitions()), rt)

	var reply string
	res := ex.Execute(context.Background(), Request{
		Text: "/stop",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	})
	if res.Outcome != OutcomeHandled {
		t.Fatalf("outcome=%v, want=%v", res.Outcome, OutcomeHandled)
	}
	if !called {
		t.Fatal("StopActiveTurn was not called")
	}
	if reply != "Task stopped. \"some-task\" was canceled." {
		t.Fatalf("reply=%q, want 'Task stopped. \"some-task\" was canceled.'", reply)
	}
}

func TestBuiltinStop_NoActiveTask(t *testing.T) {
	rt := &Runtime{
		StopActiveTurn: func() (StopResult, error) {
			return StopResult{Stopped: false}, nil
		},
	}
	ex := NewExecutor(NewRegistry(BuiltinDefinitions()), rt)

	var reply string
	res := ex.Execute(context.Background(), Request{
		Text: "/stop",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	})
	if res.Outcome != OutcomeHandled {
		t.Fatalf("outcome=%v, want=%v", res.Outcome, OutcomeHandled)
	}
	if reply != "No active task to stop." {
		t.Fatalf("reply=%q, want 'No active task to stop.'", reply)
	}
}

func TestBuiltinShowChannel_PreservesUserVisibleBehavior(t *testing.T) {
	ex := NewExecutor(NewRegistry(BuiltinDefinitions()), nil)

	var reply string
	res := ex.Execute(context.Background(), Request{
		Channel: "slack",
		Text:    "/show channel",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	})
	if res.Outcome != OutcomeHandled {
		t.Fatalf("outcome=%v, want=%v", res.Outcome, OutcomeHandled)
	}

	if !strings.Contains(reply, "Current Channel: slack") {
		t.Fatalf("reply=%q, expected channel display format", reply)
	}
}

func TestBuiltinListChannels_UsesGetEnabledChannels(t *testing.T) {
	rt := &Runtime{
		GetEnabledChannels: func() []string {
			return []string{"telegram", "cli"}
		},
	}
	ex := NewExecutor(NewRegistry(BuiltinDefinitions()), rt)

	var reply string
	res := ex.Execute(context.Background(), Request{
		Text: "/list channels",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	})
	if res.Outcome != OutcomeHandled {
		t.Fatalf("outcome=%v, want=%v", res.Outcome, OutcomeHandled)
	}
	if !strings.Contains(reply, "telegram") || !strings.Contains(reply, "cli") {
		t.Fatalf("reply=%q, expected channel list output", reply)
	}
}

func TestBuiltinShowAgents_RestoresOldBehavior(t *testing.T) {
	rt := &Runtime{
		ListAgentIDs: func() []string {
			return []string{"default", "research"}
		},
	}
	ex := NewExecutor(NewRegistry(BuiltinDefinitions()), rt)

	var reply string
	res := ex.Execute(context.Background(), Request{
		Text: "/show agents",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	})
	if res.Outcome != OutcomeHandled {
		t.Fatalf("outcome=%v, want=%v", res.Outcome, OutcomeHandled)
	}
	if !strings.Contains(reply, "default") || !strings.Contains(reply, "research") {
		t.Fatalf("reply=%q, expected agent list output", reply)
	}
}

func TestBuiltinListAgents_RestoresOldBehavior(t *testing.T) {
	rt := &Runtime{
		ListAgentIDs: func() []string {
			return []string{"default", "research"}
		},
	}
	ex := NewExecutor(NewRegistry(BuiltinDefinitions()), rt)

	var reply string
	res := ex.Execute(context.Background(), Request{
		Text: "/list agents",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	})
	if res.Outcome != OutcomeHandled {
		t.Fatalf("outcome=%v, want=%v", res.Outcome, OutcomeHandled)
	}
	if !strings.Contains(reply, "default") || !strings.Contains(reply, "research") {
		t.Fatalf("reply=%q, expected agent list output", reply)
	}
}

func TestBuiltinListSkills_UsesRuntimeSkillNames(t *testing.T) {
	rt := &Runtime{
		ListSkillNames: func() []string {
			return []string{"shell", "weather"}
		},
	}
	ex := NewExecutor(NewRegistry(BuiltinDefinitions()), rt)

	var reply string
	res := ex.Execute(context.Background(), Request{
		Text: "/list skills",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	})
	if res.Outcome != OutcomeHandled {
		t.Fatalf("outcome=%v, want=%v", res.Outcome, OutcomeHandled)
	}
	if !strings.Contains(reply, "shell") || !strings.Contains(reply, "weather") {
		t.Fatalf("reply=%q, expected skill list output", reply)
	}
}

func TestBuiltinListMCP_UsesRuntimeServerStatus(t *testing.T) {
	rt := &Runtime{
		ListMCPServers: func(_ context.Context) []MCPServerInfo {
			return []MCPServerInfo{
				{Name: "filesystem", Enabled: true, Deferred: true, Connected: false},
				{Name: "github", Enabled: true, Deferred: false, Connected: true, ToolCount: 3},
			}
		},
	}
	ex := NewExecutor(NewRegistry(BuiltinDefinitions()), rt)

	var reply string
	res := ex.Execute(context.Background(), Request{
		Text: "/list mcp",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	})
	if res.Outcome != OutcomeHandled {
		t.Fatalf("/list mcp: outcome=%v, want=%v", res.Outcome, OutcomeHandled)
	}
	if !strings.Contains(reply, "- filesystem — enabled=yes, deferred=yes, connected=no") {
		t.Fatalf("/list mcp reply=%q, want formatted filesystem block", reply)
	}
	if !strings.Contains(reply, "- github — enabled=yes, deferred=no, connected=yes, tools=3") {
		t.Fatalf("/list mcp reply=%q, want formatted github block", reply)
	}
}

func TestBuiltinShowMCP_UsesRuntimeToolNames(t *testing.T) {
	rt := &Runtime{
		ListMCPTools: func(_ context.Context, serverName string) ([]MCPToolInfo, error) {
			if serverName != "github" {
				t.Fatalf("serverName=%q, want github", serverName)
			}
			return []MCPToolInfo{
				{
					Name:        "create_issue",
					Description: "Create a GitHub issue",
					Parameters: []MCPToolParameterInfo{
						{Name: "body", Type: "string", Description: "Issue body"},
						{Name: "title", Type: "string", Description: "Issue title", Required: true},
					},
				},
				{
					Name:        "list_prs",
					Description: "List open pull requests",
				},
			}, nil
		},
	}
	ex := NewExecutor(NewRegistry(BuiltinDefinitions()), rt)

	var reply string
	res := ex.Execute(context.Background(), Request{
		Text: "/show mcp github",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	})
	if res.Outcome != OutcomeHandled {
		t.Fatalf("/show mcp <server>: outcome=%v, want=%v", res.Outcome, OutcomeHandled)
	}
	if !strings.Contains(reply, "- `create_issue`") || !strings.Contains(reply, "- `list_prs`") {
		t.Fatalf("/show mcp <server> reply=%q, want tool names", reply)
	}
}

func TestBuiltinUseCommand_PassthroughsToAgentLogic(t *testing.T) {
	ex := NewExecutor(NewRegistry(BuiltinDefinitions()), nil)

	res := ex.Execute(context.Background(), Request{
		Text: "/use summarize",
	})
	// /use operates purely as agent routing context.
	if res.Outcome != OutcomePassthrough {
		t.Fatalf("outcome=%v, want=%v", res.Outcome, OutcomePassthrough)
	}
}

func TestBuiltinBtwCommand_UsesSideQuestionRuntime(t *testing.T) {
	called := false
	rt := &Runtime{
		AskSideQuestion: func(ctx context.Context, question string) (string, error) {
			called = true
			if question != "what is the time?" {
				t.Fatalf("question=%q, want 'what is the time?'", question)
			}
			return "it is 12:00", nil
		},
	}
	ex := NewExecutor(NewRegistry(BuiltinDefinitions()), rt)

	var reply string
	res := ex.Execute(context.Background(), Request{
		Text: "/btw what is the time?",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	})
	if res.Outcome != OutcomeHandled {
		t.Fatalf("outcome=%v, want=%v", res.Outcome, OutcomeHandled)
	}
	if !called {
		t.Fatal("AskSideQuestion was not called")
	}
	if reply != "it is 12:00" {
		t.Fatalf("reply=%q, want 'it is 12:00'", reply)
	}
}

func TestBuiltinBtwCommand_MissingQuestion(t *testing.T) {
	rt := &Runtime{
		AskSideQuestion: func(ctx context.Context, question string) (string, error) {
			return "", nil
		},
	}
	ex := NewExecutor(NewRegistry(BuiltinDefinitions()), rt)

	var reply string
	res := ex.Execute(context.Background(), Request{
		Text: "/btw",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	})
	if res.Outcome != OutcomeHandled {
		t.Fatalf("outcome=%v, want=%v", res.Outcome, OutcomeHandled)
	}
	if reply != "Usage: /btw <question>" {
		t.Fatalf("reply=%q, want usage message", reply)
	}
}

func TestBuiltinBtwCommand_PreservesQuestionWhitespace(t *testing.T) {
	called := false
	rt := &Runtime{
		AskSideQuestion: func(ctx context.Context, question string) (string, error) {
			called = true
			if question != "what   is   it?" {
				t.Fatalf("question=%q, want 'what   is   it?'", question)
			}
			return "ok", nil
		},
	}
	ex := NewExecutor(NewRegistry(BuiltinDefinitions()), rt)

	ex.Execute(context.Background(), Request{
		Text:  "/btw   what   is   it?  ",
		Reply: func(string) error { return nil },
	})
	if !called {
		t.Fatal("AskSideQuestion was not called")
	}
}
