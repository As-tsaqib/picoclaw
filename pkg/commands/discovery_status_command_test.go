package commands

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
)

func TestDiscoveryRootCommandsDelegateSemanticDomains(t *testing.T) {
	var requests []DiscoveryCommandRequest
	rt := &Runtime{
		DiscoveryCommand: func(
			_ context.Context,
			req DiscoveryCommandRequest,
		) (*bus.StructuredContent, error) {
			requests = append(requests, req)
			return &bus.StructuredContent{Kind: req.Domain + "_dashboard", Fallback: req.Domain + " dashboard"}, nil
		},
	}
	ex := NewExecutor(NewRegistry(BuiltinDefinitions()), rt)
	for _, tc := range []struct {
		text   string
		domain string
	}{
		{text: "/show", domain: "show"},
		{text: "/list", domain: "list"},
	} {
		var reply string
		res := ex.Execute(context.Background(), Request{Text: tc.text, Reply: func(text string) error {
			reply = text
			return nil
		}})
		if res.Outcome != OutcomeHandled {
			t.Fatalf("%s outcome=%v, want handled", tc.text, res.Outcome)
		}
		if reply != tc.domain+" dashboard" {
			t.Fatalf("%s reply=%q", tc.text, reply)
		}
	}
	if len(requests) != 2 || requests[0].Domain != "show" || requests[0].Operation != "dashboard" ||
		requests[1].Domain != "list" || requests[1].Operation != "dashboard" {
		t.Fatalf("unexpected semantic requests: %#v", requests)
	}
}

func TestListModelsDelegatesMatureCatalogAndNeverUsesCurrentModelFallback(t *testing.T) {
	called := false
	legacyCalled := false
	rt := &Runtime{
		ModelCommand: func(_ context.Context, req ModelCommandRequest) (*bus.StructuredContent, error) {
			called = true
			if req.Operation != "list" {
				t.Fatalf("operation=%q, want list", req.Operation)
			}
			return &bus.StructuredContent{
				Kind:     "model_list",
				Fallback: "Configured Models\n- alpha\n- beta",
				Interaction: &bus.InteractionMenu{
					Kind: "model",
				},
			}, nil
		},
		GetModelInfo: func() (string, string) {
			legacyCalled = true
			return "only-active", "legacy"
		},
	}
	var reply string
	res := NewExecutor(NewRegistry(BuiltinDefinitions()), rt).Execute(context.Background(), Request{
		Text: "/list models",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	})
	if res.Outcome != OutcomeHandled || !called {
		t.Fatalf("catalog delegation failed: outcome=%v called=%v", res.Outcome, called)
	}
	if legacyCalled {
		t.Fatal("/list models must not collapse to GetModelInfo current-model semantics")
	}
	if !strings.Contains(reply, "alpha") || !strings.Contains(reply, "beta") {
		t.Fatalf("catalog reply=%q", reply)
	}
}

func TestListModelsWithoutCatalogFailsClosedInsteadOfLyingWithCurrentModel(t *testing.T) {
	legacyCalled := false
	rt := &Runtime{GetModelInfo: func() (string, string) {
		legacyCalled = true
		return "only-active", "legacy"
	}}
	var reply string
	NewExecutor(NewRegistry(BuiltinDefinitions()), rt).Execute(context.Background(), Request{
		Text: "/list models",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	})
	if legacyCalled {
		t.Fatal("current-model getter must not be used as an inventory source")
	}
	if reply != unavailableMsg {
		t.Fatalf("reply=%q, want unavailable", reply)
	}
}

func TestListSkillsDelegatesSkillPickerDomain(t *testing.T) {
	called := false
	legacyCalled := false
	rt := &Runtime{
		SkillCommand: func(_ context.Context, req SkillCommandRequest) (*bus.StructuredContent, error) {
			called = true
			if req.Operation != "dashboard" {
				t.Fatalf("operation=%q, want dashboard", req.Operation)
			}
			return &bus.StructuredContent{
				Kind:     "skill_picker",
				Fallback: "Skill Picker\n- calendar\n- shell",
				Interaction: &bus.InteractionMenu{
					Kind: "skill",
				},
			}, nil
		},
		ListSkillNames: func() []string {
			legacyCalled = true
			return []string{"legacy"}
		},
	}
	var reply string
	NewExecutor(NewRegistry(BuiltinDefinitions()), rt).Execute(context.Background(), Request{
		Text: "/list skills",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	})
	if !called || legacyCalled {
		t.Fatalf("skill delegation called=%v legacyCalled=%v", called, legacyCalled)
	}
	if !strings.Contains(reply, "calendar") {
		t.Fatalf("reply=%q", reply)
	}
}

func TestShowModelUsesSessionAwareModelCurrentSemantic(t *testing.T) {
	legacyCalled := false
	rt := &Runtime{
		ModelCommand: func(_ context.Context, req ModelCommandRequest) (*bus.StructuredContent, error) {
			if req.Operation != "current" {
				t.Fatalf("operation=%q, want current", req.Operation)
			}
			return &bus.StructuredContent{Fallback: "Model Aktif\nModel: session-override\nProvider: provider-a"}, nil
		},
		GetModelInfo: func() (string, string) {
			legacyCalled = true
			return "agent-default", "provider-b"
		},
	}
	var reply string
	NewExecutor(NewRegistry(BuiltinDefinitions()), rt).Execute(context.Background(), Request{
		Text: "/show model",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	})
	if legacyCalled {
		t.Fatal("session-aware model semantic must take precedence over legacy getter")
	}
	if !strings.Contains(reply, "session-override") {
		t.Fatalf("reply=%q", reply)
	}
}

func TestCheckChannelUsesReadOnlyStatusPrimitiveAndNeverSwitches(t *testing.T) {
	switchCalls := 0
	checkCalls := 0
	rt := &Runtime{
		CheckChannel: func(name string) (ChannelStatus, error) {
			checkCalls++
			if name != "telegram" {
				t.Fatalf("name=%q", name)
			}
			return ChannelStatus{Name: "telegram", Enabled: true, Available: false, Reason: "not running"}, nil
		},
		SwitchChannel: func(string) error {
			switchCalls++
			return nil
		},
	}
	var reply string
	NewExecutor(NewRegistry(BuiltinDefinitions()), rt).Execute(context.Background(), Request{
		Text: "/check channel telegram",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	})
	if checkCalls != 1 || switchCalls != 0 {
		t.Fatalf("checkCalls=%d switchCalls=%d", checkCalls, switchCalls)
	}
	for _, want := range []string{"Channel: telegram", "Enabled: yes", "Available: no", "Reason: not running"} {
		if !strings.Contains(reply, want) {
			t.Fatalf("reply=%q missing %q", reply, want)
		}
	}
}

func TestCheckChannelExpectedUnknownStatusIsStructuredData(t *testing.T) {
	rt := &Runtime{CheckChannel: func(name string) (ChannelStatus, error) {
		return ChannelStatus{
			Name:      name,
			Enabled:   false,
			Available: false,
			Reason:    "channel is not enabled or registered",
		}, nil
	}}
	var reply string
	NewExecutor(NewRegistry(BuiltinDefinitions()), rt).Execute(context.Background(), Request{
		Text: "/check channel missing",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	})
	if !strings.Contains(reply, "Channel: missing") || !strings.Contains(reply, "Available: no") {
		t.Fatalf("reply=%q", reply)
	}
}

func TestCheckChannelInternalFailureIsSanitized(t *testing.T) {
	secret := "backend-token-super-secret"
	rt := &Runtime{CheckChannel: func(string) (ChannelStatus, error) {
		return ChannelStatus{}, errors.New(secret)
	}}
	var reply string
	NewExecutor(NewRegistry(BuiltinDefinitions()), rt).Execute(context.Background(), Request{
		Text: "/check channel telegram",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	})
	if strings.Contains(reply, secret) {
		t.Fatalf("internal failure leaked into user reply: %q", reply)
	}
	if reply != unavailableMsg {
		t.Fatalf("reply=%q, want unavailable", reply)
	}
}

func TestListChannelsTextFallbackIsDeterministic(t *testing.T) {
	rt := &Runtime{GetEnabledChannels: func() []string {
		return []string{"Telegram", "discord", "alpha", "Discord"}
	}}
	var reply string
	NewExecutor(NewRegistry(BuiltinDefinitions()), rt).Execute(context.Background(), Request{
		Text: "/list channels",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	})
	alpha := strings.Index(reply, "alpha")
	upperDiscord := strings.Index(reply, "Discord")
	lowerDiscord := strings.Index(reply, "discord")
	telegram := strings.Index(reply, "Telegram")
	if !(alpha >= 0 && upperDiscord > alpha && lowerDiscord > upperDiscord && telegram > lowerDiscord) {
		t.Fatalf("non-deterministic ordering: %q", reply)
	}
}
