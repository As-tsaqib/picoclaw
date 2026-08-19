package bus

import "testing"

func TestPollStopRouteTokenRoundTripAndIsolation(t *testing.T) {
	const (
		handle     = "poll-opaque-1"
		account    = "personal"
		chatID     = "-100123/42"
		topicID    = "42"
		agentID    = "main"
		sessionKey = "sk_v1_secret_session"
	)
	token := NewPollStopRouteToken(handle, account, chatID, topicID, agentID, "", sessionKey)
	gotHandle, digest, ok := ParsePollStopRouteToken(token)
	if !ok || gotHandle != handle || digest == "" {
		t.Fatalf("route token parse = handle=%q digest=%q ok=%t", gotHandle, digest, ok)
	}
	want := PollStopRouteDigest(account, "-100123", topicID, agentID, "", sessionKey)
	if digest != want {
		t.Fatalf("route digest = %q, want %q", digest, want)
	}
	if token == "" || containsSensitiveRouteValue(token, sessionKey, chatID, account) {
		t.Fatalf("route token leaked trusted route material: %q", token)
	}

	tests := []struct {
		name       string
		account    string
		chatID     string
		topicID    string
		agentID    string
		sessionKey string
	}{
		{"wrong account", "other", chatID, topicID, agentID, sessionKey},
		{"wrong chat", account, "-100999/42", topicID, agentID, sessionKey},
		{"wrong topic", account, "-100123/99", "99", agentID, sessionKey},
		{"wrong agent", account, chatID, topicID, "other-agent", sessionKey},
		{"wrong session", account, chatID, topicID, agentID, "other-session"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			other := PollStopRouteDigest(tc.account, tc.chatID, tc.topicID, tc.agentID, "", tc.sessionKey)
			if other == digest {
				t.Fatalf("route dimension %q did not isolate digest", tc.name)
			}
		})
	}
}

func TestParsePollStopRouteTokenRejectsMalformedValues(t *testing.T) {
	for _, value := range []string{"", "poll", "pcpollstop:v1:", "pcpollstop:v1:handle:not-hex"} {
		if _, _, ok := ParsePollStopRouteToken(value); ok {
			t.Fatalf("malformed route token accepted: %q", value)
		}
	}
}

func containsSensitiveRouteValue(value string, needles ...string) bool {
	for _, needle := range needles {
		if needle != "" && len(needle) > 4 && containsString(value, needle) {
			return true
		}
	}
	return false
}

func containsString(value, needle string) bool {
	for i := 0; i+len(needle) <= len(value); i++ {
		if value[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
