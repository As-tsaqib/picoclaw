package commands

import (
	"context"
	"fmt"
	"testing"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
)

func TestSwitchModel_Success(t *testing.T) {
	rt := &Runtime{
		ModelCommand: func(_ context.Context, req ModelCommandRequest) (*bus.StructuredContent, error) {
			if req.Operation != "use" || req.Argument != "gpt-4" || !req.LegacySwitch {
				t.Fatalf("unexpected request: %+v", req)
			}
			return &bus.StructuredContent{Fallback: "Switched model from old-model to gpt-4"}, nil
		},
	}
	ex := NewExecutor(NewRegistry(BuiltinDefinitions()), rt)

	var reply string
	res := ex.Execute(context.Background(), Request{
		Text: "/switch model to gpt-4",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	})
	if res.Outcome != OutcomeHandled {
		t.Fatalf("outcome=%v, want=%v", res.Outcome, OutcomeHandled)
	}
	want := "Switched model from old-model to gpt-4"
	if reply != want {
		t.Fatalf("reply=%q, want=%q", reply, want)
	}
}

func TestSwitchModel_PreservesSpacedAlias(t *testing.T) {
	var selected string
	rt := &Runtime{ModelCommand: func(_ context.Context, req ModelCommandRequest) (*bus.StructuredContent, error) {
		selected = req.Argument
		return &bus.StructuredContent{Fallback: "ok"}, nil
	}}
	ex := NewExecutor(NewRegistry(BuiltinDefinitions()), rt)
	result := ex.Execute(context.Background(), Request{
		Text:  "/switch model to Team Account Model",
		Reply: func(string) error { return nil },
	})
	if result.Outcome != OutcomeHandled || result.Err != nil {
		t.Fatalf("result = %+v", result)
	}
	if selected != "Team Account Model" {
		t.Fatalf("selected = %q", selected)
	}
}

func TestSwitchModel_MissingToKeyword(t *testing.T) {
	rt := &Runtime{
		ModelCommand: func(context.Context, ModelCommandRequest) (*bus.StructuredContent, error) {
			t.Fatal("ModelCommand should not be called for missing 'to' keyword")
			return nil, nil
		},
	}
	ex := NewExecutor(NewRegistry(BuiltinDefinitions()), rt)

	var reply string
	res := ex.Execute(context.Background(), Request{
		Text: "/switch model gpt-4",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	})
	if res.Outcome != OutcomeHandled {
		t.Fatalf("outcome=%v, want=%v", res.Outcome, OutcomeHandled)
	}
	if reply != "Usage: /switch model to <model>\nDeprecated: use /model use <model> instead." {
		t.Fatalf("reply=%q, want usage message", reply)
	}
}

func TestSwitchModel_MissingValue(t *testing.T) {
	rt := &Runtime{
		ModelCommand: func(context.Context, ModelCommandRequest) (*bus.StructuredContent, error) {
			t.Fatal("ModelCommand should not be called for missing value")
			return nil, nil
		},
	}
	ex := NewExecutor(NewRegistry(BuiltinDefinitions()), rt)

	var reply string
	res := ex.Execute(context.Background(), Request{
		Text: "/switch model to",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	})
	if res.Outcome != OutcomeHandled {
		t.Fatalf("outcome=%v, want=%v", res.Outcome, OutcomeHandled)
	}
	if reply != "Usage: /switch model to <model>\nDeprecated: use /model use <model> instead." {
		t.Fatalf("reply=%q, want usage message", reply)
	}
}

func TestSwitchModel_Error(t *testing.T) {
	rt := &Runtime{
		ModelCommand: func(_ context.Context, req ModelCommandRequest) (*bus.StructuredContent, error) {
			return nil, fmt.Errorf("model not found")
		},
	}
	ex := NewExecutor(NewRegistry(BuiltinDefinitions()), rt)

	var reply string
	res := ex.Execute(context.Background(), Request{
		Text: "/switch model to bad-model",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	})
	if res.Outcome != OutcomeHandled {
		t.Fatalf("outcome=%v, want=%v", res.Outcome, OutcomeHandled)
	}
	if reply != "Model service is temporarily unavailable. Please try again." {
		t.Fatalf("reply=%q, want error message", reply)
	}
}

func TestSwitchModel_NilDep(t *testing.T) {
	ex := NewExecutor(NewRegistry(BuiltinDefinitions()), &Runtime{})

	var reply string
	res := ex.Execute(context.Background(), Request{
		Text: "/switch model to gpt-4",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	})
	if res.Outcome != OutcomeHandled {
		t.Fatalf("outcome=%v, want=%v", res.Outcome, OutcomeHandled)
	}
	if reply != "Command unavailable in current context." {
		t.Fatalf("reply=%q, want unavailable message", reply)
	}
}

func TestSwitchChannel_Redirect(t *testing.T) {
	ex := NewExecutor(NewRegistry(BuiltinDefinitions()), &Runtime{})

	var reply string
	res := ex.Execute(context.Background(), Request{
		Text: "/switch channel to telegram",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	})
	if res.Outcome != OutcomeHandled {
		t.Fatalf("outcome=%v, want=%v", res.Outcome, OutcomeHandled)
	}
	want := "/switch channel is deprecated and does not change channel state. Use /check channel <name> for status."
	if reply != want {
		t.Fatalf("reply=%q, want=%q", reply, want)
	}
}

func TestCheckChannel_Success(t *testing.T) {
	rt := &Runtime{
		CheckChannel: func(value string) (ChannelStatus, error) {
			return ChannelStatus{Name: value, Enabled: true, Available: true}, nil
		},
		SwitchChannel: func(string) error {
			t.Fatal("/check channel must not mutate channel state")
			return nil
		},
	}
	ex := NewExecutor(NewRegistry(BuiltinDefinitions()), rt)

	var reply string
	res := ex.Execute(context.Background(), Request{
		Text: "/check channel telegram",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	})
	if res.Outcome != OutcomeHandled {
		t.Fatalf("outcome=%v, want=%v", res.Outcome, OutcomeHandled)
	}
	want := "Channel Status\nChannel: telegram\nEnabled: yes\nAvailable: yes"
	if reply != want {
		t.Fatalf("reply=%q, want=%q", reply, want)
	}
}

func TestCheckChannel_Error(t *testing.T) {
	rt := &Runtime{
		CheckChannel: func(string) (ChannelStatus, error) {
			return ChannelStatus{}, fmt.Errorf("channel status backend unavailable")
		},
		SwitchChannel: func(string) error {
			t.Fatal("/check channel must not mutate channel state")
			return nil
		},
	}
	ex := NewExecutor(NewRegistry(BuiltinDefinitions()), rt)

	var reply string
	res := ex.Execute(context.Background(), Request{
		Text: "/check channel telegram",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	})
	if res.Outcome != OutcomeHandled {
		t.Fatalf("outcome=%v, want=%v", res.Outcome, OutcomeHandled)
	}
	if reply != "Command unavailable in current context." {
		t.Fatalf("reply=%q, want unavailable message", reply)
	}
}

func TestCheckChannel_NilDep(t *testing.T) {
	ex := NewExecutor(NewRegistry(BuiltinDefinitions()), &Runtime{})

	var reply string
	res := ex.Execute(context.Background(), Request{
		Text: "/check channel telegram",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	})
	if res.Outcome != OutcomeHandled {
		t.Fatalf("outcome=%v, want=%v", res.Outcome, OutcomeHandled)
	}
	if reply != "Command unavailable in current context." {
		t.Fatalf("reply=%q, want unavailable message", reply)
	}
}

func TestCheckChannel_MissingValue(t *testing.T) {
	rt := &Runtime{
		CheckChannel: func(string) (ChannelStatus, error) {
			t.Fatal("should not call CheckChannel without a value")
			return ChannelStatus{}, nil
		},
	}
	ex := NewExecutor(NewRegistry(BuiltinDefinitions()), rt)

	var reply string
	res := ex.Execute(context.Background(), Request{
		Text: "/check channel",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	})
	if res.Outcome != OutcomeHandled {
		t.Fatalf("outcome=%v, want=%v", res.Outcome, OutcomeHandled)
	}
	if reply != "Usage: /check channel <name>" {
		t.Fatalf("reply=%q, want usage message", reply)
	}
}

func TestSwitch_BangPrefix(t *testing.T) {
	rt := &Runtime{
		ModelCommand: func(_ context.Context, req ModelCommandRequest) (*bus.StructuredContent, error) {
			return &bus.StructuredContent{Fallback: "Switched model from old to gpt-4"}, nil
		},
	}
	ex := NewExecutor(NewRegistry(BuiltinDefinitions()), rt)

	var reply string
	res := ex.Execute(context.Background(), Request{
		Text: "!switch model to gpt-4",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	})
	if res.Outcome != OutcomeHandled {
		t.Fatalf("! prefix: outcome=%v, want=%v", res.Outcome, OutcomeHandled)
	}
	if reply != "Switched model from old to gpt-4" {
		t.Fatalf("! prefix: reply=%q, want success message", reply)
	}
}

func TestSwitch_NoSubCommand(t *testing.T) {
	ex := NewExecutor(NewRegistry(BuiltinDefinitions()), &Runtime{})

	var reply string
	res := ex.Execute(context.Background(), Request{
		Text: "/switch",
		Reply: func(text string) error {
			reply = text
			return nil
		},
	})
	if res.Outcome != OutcomeHandled {
		t.Fatalf("outcome=%v, want=%v", res.Outcome, OutcomeHandled)
	}
	if reply == "" {
		t.Fatal("expected a reply for /switch without subcommand")
	}
}
