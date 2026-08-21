package commands

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
)

func executeForSemanticTest(t *testing.T, rt *Runtime, text string) string {
	t.Helper()
	var reply string
	res := NewExecutor(NewRegistry(BuiltinDefinitions()), rt).Execute(context.Background(), Request{
		Text: text,
		Reply: func(value string) error {
			reply = value
			return nil
		},
	})
	if res.Outcome != OutcomeHandled {
		t.Fatalf("%s outcome=%v, want handled", text, res.Outcome)
	}
	if res.Err != nil {
		t.Fatalf("%s returned error: %v", text, res.Err)
	}
	return reply
}

func TestNewAndClearRoutingAreDestructivelyDisjoint(t *testing.T) {
	cases := []struct {
		name             string
		text             string
		wantSessionCalls int
		wantClearCalls   int
		wantArgument     string
	}{
		{name: "new generated", text: "/new", wantSessionCalls: 1},
		{name: "new named", text: "/new alpha", wantSessionCalls: 1, wantArgument: "alpha"},
		{name: "clear", text: "/clear", wantClearCalls: 1},
		{name: "reset alias", text: "/reset", wantClearCalls: 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sessionCalls := 0
			clearCalls := 0
			var got SessionCommandRequest
			rt := &Runtime{
				SessionCommand: func(_ context.Context, req SessionCommandRequest) (*bus.StructuredContent, error) {
					sessionCalls++
					got = req
					return &bus.StructuredContent{Fallback: "session ok"}, nil
				},
				ClearHistory: func() error {
					clearCalls++
					return nil
				},
			}

			executeForSemanticTest(t, rt, tc.text)
			if sessionCalls != tc.wantSessionCalls {
				t.Fatalf("%s SessionCommand calls=%d, want=%d", tc.text, sessionCalls, tc.wantSessionCalls)
			}
			if clearCalls != tc.wantClearCalls {
				t.Fatalf("%s ClearHistory calls=%d, want=%d", tc.text, clearCalls, tc.wantClearCalls)
			}
			if tc.wantSessionCalls > 0 {
				if got.Operation != "new" {
					t.Fatalf("%s operation=%q, want new", tc.text, got.Operation)
				}
				if got.Argument != tc.wantArgument {
					t.Fatalf("%s argument=%q, want=%q", tc.text, got.Argument, tc.wantArgument)
				}
			}
		})
	}
}

func TestCheckpointArchiveAndForgetShareArchiveSemantic(t *testing.T) {
	for _, text := range []string{"/checkpoint archive cp-1", "/checkpoint forget cp-1"} {
		t.Run(text, func(t *testing.T) {
			calls := 0
			var got CheckpointCommandRequest
			rt := &Runtime{
				CheckpointCommand: func(_ context.Context, req CheckpointCommandRequest) (*bus.StructuredContent, error) {
					calls++
					got = req
					return &bus.StructuredContent{Fallback: "ok"}, nil
				},
			}
			executeForSemanticTest(t, rt, text)
			if calls != 1 || got.Operation != "archive" || got.ID != "cp-1" {
				t.Fatalf("%s got calls=%d request=%+v, want one archive cp-1", text, calls, got)
			}
		})
	}
}

func TestSwitchModelDelegatesAndChannelNeverMutates(t *testing.T) {
	modelCalls := 0
	channelMutations := 0
	var got ModelCommandRequest
	rt := &Runtime{
		ModelCommand: func(_ context.Context, req ModelCommandRequest) (*bus.StructuredContent, error) {
			modelCalls++
			got = req
			return &bus.StructuredContent{Fallback: "ok"}, nil
		},
		SwitchChannel: func(string) error {
			channelMutations++
			return nil
		},
	}

	executeForSemanticTest(t, rt, "/switch model to gpt-test")
	if modelCalls != 1 || got.Operation != "use" || got.Argument != "gpt-test" || !got.LegacySwitch {
		t.Fatalf("legacy switch request=%+v calls=%d", got, modelCalls)
	}

	reply := executeForSemanticTest(t, rt, "/switch channel telegram")
	if channelMutations != 0 {
		t.Fatalf("/switch channel mutated state %d time(s)", channelMutations)
	}
	if !strings.Contains(reply, "/check channel") {
		t.Fatalf("/switch channel reply=%q, want replacement guidance", reply)
	}
}

func TestParseUseIntentOwnsAllSupportedForms(t *testing.T) {
	cases := []struct {
		text    string
		kind    UseIntentKind
		skill   string
		message string
	}{
		{text: "/use", kind: UseIntentPicker},
		{text: "/use shell", kind: UseIntentArm, skill: "shell"},
		{
			text:    "/use shell inspect this repository",
			kind:    UseIntentForcedTurn,
			skill:   "shell",
			message: "inspect this repository",
		},
		{text: "/use clear", kind: UseIntentClear},
		{text: "/use off", kind: UseIntentClear},
	}
	for _, tc := range cases {
		intent, ok := ParseUseIntent(tc.text)
		if !ok {
			t.Fatalf("ParseUseIntent(%q) did not match", tc.text)
		}
		if intent.Kind != tc.kind || intent.Skill != tc.skill || intent.Message != tc.message {
			t.Fatalf("ParseUseIntent(%q)=%+v", tc.text, intent)
		}
	}
	if _, ok := ParseUseIntent("hello"); ok {
		t.Fatal("normal text must not parse as /use")
	}
}

func TestUnknownSlashFailsClosedWhileNormalTextPassesThrough(t *testing.T) {
	ex := NewExecutor(NewRegistry(BuiltinDefinitions()), &Runtime{})
	var reply string
	unknown := ex.Execute(context.Background(), Request{
		Text: "/unknowncmd",
		Reply: func(value string) error {
			reply = value
			return nil
		},
	})
	if unknown.Outcome != OutcomeHandled {
		t.Fatalf("unknown slash outcome=%v, want handled", unknown.Outcome)
	}
	if !strings.Contains(reply, "Unknown command: /unknowncmd") {
		t.Fatalf("unknown slash reply=%q", reply)
	}

	normal := ex.Execute(context.Background(), Request{Text: "normal conversation"})
	if normal.Outcome != OutcomePassthrough {
		t.Fatalf("normal text outcome=%v, want passthrough", normal.Outcome)
	}
}

func TestUserFacingErrorKeepsOnlyExplicitSafeErrors(t *testing.T) {
	safe := UserFacingError(NewUserError("Model \"foo\" was not found."), "fallback")
	if safe != "Model \"foo\" was not found." {
		t.Fatalf("safe domain error=%q", safe)
	}
	secret := "https://user:secret@example.invalid/private?token=abc"
	mapped := UserFacingError(errors.New(secret), "Service temporarily unavailable.")
	if mapped != "Service temporarily unavailable." {
		t.Fatalf("internal error leaked or changed fallback: %q", mapped)
	}
	if strings.Contains(mapped, "secret") || strings.Contains(mapped, "token") {
		t.Fatalf("internal secret leaked: %q", mapped)
	}
}

func findDefinitionByName(t *testing.T, defs []Definition, name string) Definition {
	for _, d := range defs {
		if d.Name == name {
			return d
		}
	}
	t.Fatalf("definition %s not found", name)
	return Definition{}
}

func TestCompatibilityMetadataIsTruthful(t *testing.T) {
	defs := BuiltinDefinitions()
	switchDef := findDefinitionByName(t, defs, "switch")
	if !switchDef.Deprecated || switchDef.Replacement != "/model use <model>" {
		t.Fatalf("switch metadata=%+v", switchDef)
	}
	checkpoint := findDefinitionByName(t, defs, "checkpoint")
	foundForget := false
	for _, sub := range checkpoint.SubCommands {
		if sub.Name == "forget" {
			foundForget = true
			if !sub.Deprecated || sub.Replacement != "/checkpoint archive <id>" {
				t.Fatalf("checkpoint forget metadata=%+v", sub)
			}
		}
	}
	if !foundForget {
		t.Fatal("checkpoint forget compatibility subcommand missing")
	}
}
