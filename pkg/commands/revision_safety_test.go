package commands

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
)

func executeRevisionCommand(t *testing.T, rt *Runtime, text string) (string, *bus.StructuredContent) {
	t.Helper()
	var reply string
	var structured *bus.StructuredContent
	result := NewExecutor(NewRegistry(BuiltinDefinitions()), rt).Execute(context.Background(), Request{
		Text: text,
		Reply: func(value string) error {
			reply = value
			return nil
		},
		ReplyStructured: func(content bus.StructuredContent) error {
			structured = content.Clone()
			reply = content.FallbackText()
			return nil
		},
	})
	if result.Outcome != OutcomeHandled {
		t.Fatalf("%s outcome=%v, want handled", text, result.Outcome)
	}
	if result.Err != nil {
		t.Fatalf("%s returned error: %v", text, result.Err)
	}
	return reply, structured
}

func TestOperationalCommandsSanitizeSecretBearingErrors(t *testing.T) {
	secret := strings.Join([]string{
		"provider response sk-secret at https://user:secret@example.invalid/private?token=abc",
		"via /home/private/.config/picoclaw/token.sock",
	}, " ")
	internalErr := errors.New(secret)
	cases := []struct {
		name string
		text string
		rt   *Runtime
	}{
		{
			name: "stop",
			text: "/stop",
			rt: &Runtime{StopActiveTurn: func() (StopResult, error) {
				return StopResult{}, internalErr
			}},
		},
		{
			name: "btw",
			text: "/btw is this healthy?",
			rt: &Runtime{AskSideQuestion: func(context.Context, string) (string, error) {
				return "", internalErr
			}},
		},
		{
			name: "reload",
			text: "/reload",
			rt: &Runtime{ReloadConfig: func() error {
				return internalErr
			}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reply, _ := executeRevisionCommand(t, tc.rt, tc.text)
			if strings.TrimSpace(reply) == "" {
				t.Fatal("sanitized reply is empty")
			}
			for _, forbidden := range []string{
				"sk-secret",
				"user:secret",
				"token=abc",
				"/home/private",
				"example.invalid/private",
			} {
				if strings.Contains(reply, forbidden) {
					t.Fatalf("reply leaked %q: %q", forbidden, reply)
				}
			}
		})
	}
}

func TestOperationalCommandsPreserveExplicitSafeUserError(t *testing.T) {
	const safe = "Configuration is invalid: missing required model selection."
	reply, _ := executeRevisionCommand(t, &Runtime{ReloadConfig: func() error {
		return NewUserError(safe)
	}}, "/reload")
	if reply != safe {
		t.Fatalf("safe domain error=%q, want %q", reply, safe)
	}
}

func TestContextualTypographyForRepresentativeCommandCards(t *testing.T) {
	legacyDetail := func(kind string) *bus.StructuredContent {
		return &bus.StructuredContent{
			Kind: kind,
			Tables: []bus.StructuredTable{{
				Columns: []string{"Properti", "Nilai"},
				Rows:    [][]string{{"Model", "model-a"}},
			}},
			Fallback: "Model: model-a",
		}
	}

	_, model := executeRevisionCommand(t, &Runtime{ModelCommand: func(
		context.Context,
		ModelCommandRequest,
	) (*bus.StructuredContent, error) {
		return legacyDetail("model_current"), nil
	}}, "/model current")
	if model == nil || len(model.Tables) == 0 {
		t.Fatal("/model current did not return structured detail")
	}
	assertHeaderColumns(t, model.Tables[0].Columns, detailHeaderColumns())

	_, currentState := executeRevisionCommand(t, &Runtime{DiscoveryCommand: func(
		context.Context,
		DiscoveryCommandRequest,
	) (*bus.StructuredContent, error) {
		return legacyDetail("current_state"), nil
	}}, "/show")
	if currentState == nil || len(currentState.Tables) == 0 {
		t.Fatal("/show did not return structured current-state detail")
	}
	assertHeaderColumns(t, currentState.Tables[0].Columns, detailHeaderColumns())

	status := channelStatusContent(ChannelStatus{Name: "telegram", Enabled: true, Available: true})
	if len(status.Tables) == 0 {
		t.Fatal("channel status did not return a table")
	}
	assertHeaderColumns(t, status.Tables[0].Columns, statusHeaderColumns())

	inventory := numberedListContent("Channels", "Channel", []string{"telegram"}, "Enabled channels: telegram")
	if len(inventory.Tables) == 0 {
		t.Fatal("inventory did not return a table")
	}
	assertHeaderColumns(t, inventory.Tables[0].Columns, inventoryHeaderColumns())

	for _, content := range []*bus.StructuredContent{model, currentState, &status, &inventory} {
		for _, table := range content.Tables {
			for _, column := range table.Columns {
				normalized := strings.ToUpper(strings.TrimSpace(column))
				if normalized == "PROPERTI" || normalized == "NILAI" {
					t.Fatalf("legacy generic header remained in structured card: %q", table.Columns)
				}
			}
		}
	}
}

func TestTypographyPlainFallbackHeadersRemainCanonicalASCII(t *testing.T) {
	cases := []struct {
		kind bus.CardHeaderKind
		want []string
	}{
		{kind: bus.CardHeaderDetail, want: []string{"ATTRIBUTE", "DETAILS"}},
		{kind: bus.CardHeaderStatus, want: []string{"METRIC", "STATUS"}},
		{kind: bus.CardHeaderInventory, want: []string{"ITEM", "DETAILS"}},
	}
	for _, tc := range cases {
		assertHeaderColumns(t, bus.CardHeaderColumns(tc.kind, false), tc.want)
	}
}

func TestStructuredRootHelpKeepsRegistryCategoriesVisible(t *testing.T) {
	defs := []Definition{
		{Name: "new", Description: "Create a session", Category: "Conversation"},
		{Name: "clear", Description: "Clear history", Category: "Conversation"},
		{Name: "stop", Description: "Stop task", Category: "Utility"},
	}
	_, content := executeRevisionCommand(t, &Runtime{ListDefinitions: func() []Definition {
		return append([]Definition(nil), defs...)
	}}, "/help")
	if content == nil {
		t.Fatal("/help did not return structured content")
	}
	if len(content.Tables) != 2 {
		t.Fatalf("/help tables=%d, want 2 category sections", len(content.Tables))
	}
	captions := []string{content.Tables[0].Caption, content.Tables[1].Caption}
	if !containsString(captions, "Conversation") || !containsString(captions, "Utility") {
		t.Fatalf("structured help category captions=%v", captions)
	}
	fallback := content.FallbackText()
	if !strings.Contains(fallback, "Conversation:") || !strings.Contains(fallback, "Utility:") {
		t.Fatalf("plain help fallback lost category grouping: %q", content.FallbackText())
	}
	seen := map[string]bool{}
	for _, table := range content.Tables {
		assertHeaderColumns(t, table.Columns, inventoryHeaderColumns())
		for _, row := range table.Rows {
			if len(row) > 0 {
				seen[row[0]] = true
			}
		}
	}
	for _, want := range []string{"/new", "/clear", "/stop"} {
		if !seen[want] {
			t.Fatalf("structured help did not derive %s from supplied canonical definitions", want)
		}
	}
}

func assertHeaderColumns(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("columns=%v, want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("columns=%v, want=%v", got, want)
		}
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
