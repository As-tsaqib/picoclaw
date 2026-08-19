package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/config"
	"github.com/As-tsaqib/picoclaw/pkg/memory"
	"github.com/As-tsaqib/picoclaw/pkg/routing"
	"github.com/As-tsaqib/picoclaw/pkg/session"
)

func callerScopeForTurn(agentID string, cfg *config.Config, opts processOptions) memory.CallerScope {
	normalizeProcessOptionsInPlace(&opts)
	caller := callerScopeFromInbound(
		agentID,
		opts.Dispatch.SessionKey,
		opts.Dispatch.InboundContext,
		opts.Dispatch.SessionScope,
		cfg,
	)
	if cfg != nil {
		caller.CaptureMode = cfg.Memory.EffectiveCaptureMode()
		caller.ExplicitMemoryIntent = explicitMemoryCaptureIntent(opts.UserMessage)
	}
	return caller
}

func callerScopeFromInbound(
	agentID string,
	sessionKey string,
	inbound *bus.InboundContext,
	sessionScope *session.SessionScope,
	cfg *config.Config,
) memory.CallerScope {
	caller := memory.CallerScope{
		AgentID:    routing.NormalizeAgentID(agentID),
		SessionKey: strings.TrimSpace(sessionKey),
		SessionRef: memorySessionRef(sessionKey),
	}
	if inbound != nil {
		caller.Channel = strings.ToLower(strings.TrimSpace(inbound.Channel))
		caller.Account = routing.NormalizeAccountID(inbound.Account)
		caller.ChatID = strings.TrimSpace(inbound.ChatID)
		caller.TopicID = strings.TrimSpace(inbound.TopicID)
		caller.MessageRef = strings.TrimSpace(inbound.MessageID)
		caller.GroupID = memoryGroupID(*inbound)
		if inbound.Raw != nil {
			caller.TopicName = strings.TrimSpace(inbound.Raw["topic_name"])
		}
	}
	if sessionScope != nil {
		if caller.Channel == "" {
			caller.Channel = strings.ToLower(strings.TrimSpace(sessionScope.Channel))
		}
		if caller.Account == "" || caller.Account == routing.DefaultAccountID {
			caller.Account = routing.NormalizeAccountID(sessionScope.Account)
		}
	}
	if caller.Account == "" {
		caller.Account = routing.DefaultAccountID
	}
	if inbound != nil {
		identityLinks := map[string][]string(nil)
		if cfg != nil {
			identityLinks = cfg.Session.IdentityLinks
			caller.CaptureMode = cfg.Memory.EffectiveCaptureMode()
		}
		caller.UserKey = session.CanonicalUserScopeKey(
			caller.Channel,
			caller.Account,
			inbound.SenderID,
			identityLinks,
		)
	}
	return caller
}

// explicitMemoryCaptureIntent is deliberately narrow. Explicit-only mode is a
// user-control boundary, so ordinary preference statements such as "I prefer
// concise answers" must not be treated as a save request merely because the
// information itself is explicit evidence.
func explicitMemoryCaptureIntent(value string) bool {
	normalized := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
	if normalized == "" {
		return false
	}
	markers := []string{
		"remember that ",
		"please remember that ",
		"save this to memory",
		"save that to memory",
		"store this in memory",
		"store that in memory",
		"forget that ",
		"please forget ",
		"remove this from memory",
		"remove that from memory",
		"delete this from memory",
		"delete that from memory",
		"update what you remember",
		"change what you remember",
		"ingat bahwa ",
		"tolong ingat bahwa ",
		"simpan ini ke memori",
		"simpan itu ke memori",
		"simpan ini dalam memori",
		"simpan itu dalam memori",
		"catat ini sebagai memori",
		"catat itu sebagai memori",
		"lupakan bahwa ",
		"tolong lupakan ",
		"hapus ini dari memori",
		"hapus itu dari memori",
		"hapus ini dari ingatan",
		"hapus itu dari ingatan",
		"perbarui yang kamu ingat",
		"ubah yang kamu ingat",
	}
	for _, marker := range markers {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func memorySessionRef(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(sessionKey))
	return "session_" + hex.EncodeToString(digest[:8])
}

func memoryGroupID(inbound bus.InboundContext) string {
	chatID := strings.TrimSpace(inbound.ChatID)
	if chatID == "" {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(inbound.ChatType), "direct") {
		return ""
	}
	if topicID := strings.TrimSpace(inbound.TopicID); topicID != "" {
		suffix := "/" + topicID
		chatID = strings.TrimSuffix(chatID, suffix)
	}
	return strings.ToLower(strings.TrimSpace(chatID))
}
