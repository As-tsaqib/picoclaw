package session

// ScopeVersionV1 is the first structured session-scope schema version.
const ScopeVersionV1 = 1

// SessionScope describes the semantic session partition selected for a turn.
type SessionScope struct {
	Version    int               `json:"version"`
	AgentID    string            `json:"agent_id"`
	Channel    string            `json:"channel"`
	Account    string            `json:"account"`
	Dimensions []string          `json:"dimensions"`
	Values     map[string]string `json:"values"`

	// Ownership and origin metadata are deliberately excluded from the
	// canonical scope signature. They enrich existing durable session metadata
	// without changing session keys created by older PicoClaw versions.
	OwnerUserID    string `json:"owner_user_id,omitempty"`
	OriginChannel  string `json:"origin_channel,omitempty"`
	OriginAccount  string `json:"origin_account,omitempty"`
	OriginAgentID  string `json:"origin_agent_id,omitempty"`
	OriginChatID   string `json:"origin_chat_id,omitempty"`
	OriginTopicID  string `json:"origin_topic_id,omitempty"`
	OriginSenderID string `json:"origin_sender_id,omitempty"`
	OriginChatType string `json:"origin_chat_type,omitempty"`
	OriginRoute    string `json:"origin_route,omitempty"`
	Platform       string `json:"platform,omitempty"`
	BotAccount     string `json:"bot_account,omitempty"`

	// PrivateResponse is a non-secret, persisted fail-closed marker. It keeps a
	// resumed or queued turn private even after its session scope is reloaded.
	// The receiver capability itself remains process-local and is never stored.
	PrivateResponse   bool   `json:"private_response,omitempty"`
	PrivateRouteToken string `json:"-"`
}

// CloneScope returns a deep copy of scope.
func CloneScope(scope *SessionScope) *SessionScope {
	if scope == nil {
		return nil
	}
	cloned := *scope
	if len(scope.Dimensions) > 0 {
		cloned.Dimensions = append([]string(nil), scope.Dimensions...)
	}
	if len(scope.Values) > 0 {
		cloned.Values = make(map[string]string, len(scope.Values))
		for key, value := range scope.Values {
			cloned.Values[key] = value
		}
	}
	return &cloned
}
