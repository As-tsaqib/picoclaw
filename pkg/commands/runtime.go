package commands

import (
	"context"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/config"
	"github.com/As-tsaqib/picoclaw/pkg/session"
)

type SessionCommandRequest struct {
	Operation      string
	Argument       string
	SessionKey     string
	SessionAliases []string
	Scope          *session.SessionScope
	Inbound        *bus.InboundContext
}

type SessionCommandHandler func(context.Context, SessionCommandRequest) (*bus.StructuredContent, error)

type ModelCommandRequest struct {
	Operation    string
	Argument     string
	LegacySwitch bool
}

type ModelCommandHandler func(context.Context, ModelCommandRequest) (*bus.StructuredContent, error)

type SkillCommandRequest struct {
	Operation string
	Argument  string
	Message   string
	Page      int
	Query     string
}

type SkillCommandHandler func(context.Context, SkillCommandRequest) (*bus.StructuredContent, error)

type CheckpointCommandRequest struct {
	Operation string
	ID        string
	Page      int
}

type CheckpointCommandHandler func(context.Context, CheckpointCommandRequest) (*bus.StructuredContent, error)

// DiscoveryCommandRequest is a narrow semantic request for read-only current-state,
// inventory, and status flows. Domain and Operation are intentionally explicit so
// callbacks never need to synthesize slash-command text.
type DiscoveryCommandRequest struct {
	Domain    string
	Operation string
	Argument  string
	Page      int
}

type DiscoveryCommandHandler func(context.Context, DiscoveryCommandRequest) (*bus.StructuredContent, error)

// MemoryCommandRequest is the single typed semantic request shared by textual
// /memory handlers and interactive memory callbacks. Presentation/navigation
// inputs are explicit and never encoded as slash-command text.
type MemoryCommandRequest struct {
	Operation   string
	Argument    string
	ID          string
	Content     string
	Page        int
	Query       string
	Interactive bool
}

type MemoryCommandHandler func(context.Context, MemoryCommandRequest) (*bus.StructuredContent, error)

// ChannelStatus is a read-only snapshot. Available describes whether the
// channel is currently usable/running; Enabled describes configuration/runtime
// registration. Expected negative states are data, not mutation errors.
type ChannelStatus struct {
	Name      string
	Enabled   bool
	Available bool
	Reason    string
}

type CheckChannelHandler func(name string) (ChannelStatus, error)

type MCPServerInfo struct {
	Name      string
	Enabled   bool
	Deferred  bool
	Connected bool
	ToolCount int
}

type MCPToolParameterInfo struct {
	Name        string
	Type        string
	Description string
	Required    bool
}

type MCPToolInfo struct {
	Name        string
	Description string
	Parameters  []MCPToolParameterInfo
}

// ContextStats describes current session context window usage.
type ContextStats struct {
	UsedTokens        int
	TotalTokens       int // model context window
	HistoryTokens     int // history-only tokens (what maybeSummarize checks)
	CompressAtTokens  int // hard budget compression threshold
	SummarizeAtTokens int // soft summarization trigger
	UsedPercent       int // 0-100
	MessageCount      int
}

// StopResult describes the outcome of a stop request for the current session.
type StopResult struct {
	Stopped  bool
	TaskName string
}

// Runtime provides runtime dependencies to command handlers. It is constructed
// per-request by the agent loop so that per-request state (like session scope)
// can coexist with long-lived callbacks (like GetModelInfo).
type Runtime struct {
	Config             *config.Config
	GetModelInfo       func() (name, provider string)
	AskSideQuestion    func(ctx context.Context, question string) (string, error)
	ListAgentIDs       func() []string
	ListDefinitions    func() []Definition
	ListSkillNames     func() []string
	ListMCPServers     func(ctx context.Context) []MCPServerInfo
	ListMCPTools       func(ctx context.Context, serverName string) ([]MCPToolInfo, error)
	GetEnabledChannels func() []string
	GetActiveTurn      func() any // Returning any to avoid circular dependency with agent package
	GetContextStats    func() *ContextStats
	SwitchModel        func(value string) (oldModel string, err error)
	SwitchChannel      func(value string) error
	ClearHistory       func() error
	ReloadConfig       func() error
	StopActiveTurn     func() (StopResult, error)

	// Legacy memory callbacks remain temporarily available to embedders, but
	// built-in textual and interactive memory commands must use MemoryCommand.
	MemoryStatus      func() string
	MemoryProfile     func() (string, error)
	MemoryList        func() (string, error)
	MemorySearch      func(query string) (string, error)
	MemoryEdit        func(id, content string) (string, error)
	MemoryEntryAction func(action, id string) (string, error)
	MemoryForget      func(id string) (string, error)
	MemoryPending     func() (string, error)
	MemoryApprove     func(id string) (string, error)
	MemoryReject      func(id string) (string, error)
	MemoryReview      func(ctx context.Context) (string, error)

	CheckpointList    func() (string, error)
	CheckpointResume  func(id string) (string, error)
	CheckpointForget  func(id string) (string, error)
	SessionCommand    SessionCommandHandler
	ModelCommand      ModelCommandHandler
	MemoryCommand     MemoryCommandHandler
	SkillCommand      SkillCommandHandler
	CheckpointCommand CheckpointCommandHandler
	DiscoveryCommand  DiscoveryCommandHandler
	CheckChannel      CheckChannelHandler
}
