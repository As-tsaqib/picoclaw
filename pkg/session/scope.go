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
