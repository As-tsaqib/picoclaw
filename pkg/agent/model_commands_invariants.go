package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/As-tsaqib/picoclaw/pkg/auth"
	"github.com/As-tsaqib/picoclaw/pkg/config"
	"github.com/As-tsaqib/picoclaw/pkg/providers"
	"github.com/As-tsaqib/picoclaw/pkg/session"
)

const modelConfigRefPrefix = "cfg:v1:"

// stableModelConfigRef identifies one concrete configured provider source.
// The persisted value is a one-way hash: raw keys, tokens, and header values
// are never serialized into session state or callback data. Including transport
// and credential material in the hash prevents two same-alias accounts from
// resolving to each other's credentials after a restart.
func stableModelConfigRef(mc *config.ModelConfig) string {
	if mc == nil {
		return ""
	}
	material := struct {
		ModelName           string            `json:"model_name"`
		Provider            string            `json:"provider"`
		Model               string            `json:"model"`
		APIBase             string            `json:"api_base"`
		Proxy               string            `json:"proxy"`
		Fallbacks           []string          `json:"fallbacks"`
		AuthMethod          string            `json:"auth_method"`
		ConnectMode         string            `json:"connect_mode"`
		Workspace           string            `json:"workspace"`
		RPM                 int               `json:"rpm"`
		MaxTokensField      string            `json:"max_tokens_field"`
		RequestTimeout      int               `json:"request_timeout"`
		ThinkingLevel       string            `json:"thinking_level"`
		ToolSchemaTransform string            `json:"tool_schema_transform"`
		ExtraBody           map[string]any    `json:"extra_body"`
		CustomHeaders       map[string]string `json:"custom_headers"`
		APIKeys             []string          `json:"api_keys"`
		Enabled             bool              `json:"enabled"`
		UserAgent           string            `json:"user_agent"`
	}{
		ModelName:           strings.TrimSpace(mc.ModelName),
		Provider:            providers.NormalizeProvider(mc.Provider),
		Model:               strings.TrimSpace(mc.Model),
		APIBase:             strings.TrimSpace(mc.APIBase),
		Proxy:               strings.TrimSpace(mc.Proxy),
		Fallbacks:           append([]string(nil), mc.Fallbacks...),
		AuthMethod:          strings.ToLower(strings.TrimSpace(mc.AuthMethod)),
		ConnectMode:         strings.ToLower(strings.TrimSpace(mc.ConnectMode)),
		Workspace:           strings.TrimSpace(mc.Workspace),
		RPM:                 mc.RPM,
		MaxTokensField:      strings.TrimSpace(mc.MaxTokensField),
		RequestTimeout:      mc.RequestTimeout,
		ThinkingLevel:       strings.TrimSpace(mc.ThinkingLevel),
		ToolSchemaTransform: strings.TrimSpace(mc.ToolSchemaTransform),
		ExtraBody:           mc.ExtraBody,
		CustomHeaders:       mc.CustomHeaders,
		Enabled:             mc.Enabled,
		UserAgent:           strings.TrimSpace(mc.UserAgent),
	}
	for _, key := range mc.APIKeys.Values() {
		digest := sha256.Sum256([]byte(key))
		material.APIKeys = append(material.APIKeys, fmt.Sprintf("%x", digest[:]))
	}
	encoded, err := json.Marshal(material)
	if err != nil {
		// ModelConfig's persisted fields are JSON-compatible by contract. Keep a
		// deterministic fail-closed fallback rather than exposing material.
		encoded = []byte(strings.Join([]string{
			material.ModelName,
			material.Provider,
			material.Model,
			material.APIBase,
			material.Proxy,
			material.AuthMethod,
			material.ConnectMode,
			material.Workspace,
		}, "\x00"))
	}
	sum := sha256.Sum256(encoded)
	return modelConfigRefPrefix + fmt.Sprintf("%x", sum[:])
}

// lookupSessionModelConfigByRef resolves the stable source identifier used by
// /model. Legacy alias-based refs are accepted only when the alias is unique;
// an ambiguous old override fails soft instead of selecting arbitrary account
// credentials through Config.GetModelConfig's round-robin behavior.
func lookupSessionModelConfigByRef(
	cfg *config.Config,
	ref string,
	defaultProvider ...string,
) *config.ModelConfig {
	ref = strings.TrimSpace(ref)
	if cfg == nil || ref == "" {
		return nil
	}
	if strings.HasPrefix(ref, modelConfigRefPrefix) {
		for _, mc := range cfg.ModelList {
			if mc != nil && stableModelConfigRef(mc) == ref {
				return mc
			}
		}
		return nil
	}

	var aliasMatch *config.ModelConfig
	aliasCount := 0
	for _, mc := range cfg.ModelList {
		if mc != nil && strings.EqualFold(strings.TrimSpace(mc.ModelName), ref) {
			aliasMatch = mc
			aliasCount++
		}
	}
	if aliasCount > 1 {
		return nil
	}
	if aliasCount == 1 {
		return aliasMatch
	}
	return lookupModelConfigByRef(cfg, ref, defaultProvider...)
}

func modelConfigSelectable(mc *config.ModelConfig) bool {
	if mc == nil || strings.TrimSpace(mc.Model) == "" {
		return false
	}
	if mc.Enabled || strings.TrimSpace(mc.APIKey()) != "" {
		return true
	}

	protocol, _ := providers.ExtractProtocol(mc)
	protocol = providers.NormalizeProvider(protocol)
	authMethod := strings.ToLower(strings.TrimSpace(mc.AuthMethod))

	// These providers intentionally obtain credentials outside api_keys, or do
	// not require an API key at all. Their provider factory accepts the source
	// without an inline secret.
	switch protocol {
	case "antigravity", "bedrock", "claude-cli", "codex-cli", "github-copilot":
		return true
	case "openai":
		if authMethod == "oauth" || authMethod == "token" {
			return true
		}
	case "azure":
		// Azure can use DefaultAzureCredential when api_key is absent.
		return strings.TrimSpace(mc.APIBase) != ""
	}

	if providers.IsEmptyAPIKeyAllowedForProtocol(protocol) {
		return true
	}
	if providers.IsHTTPAPIProtocol(protocol) && strings.TrimSpace(mc.APIBase) != "" {
		// Matches CreateProviderFromConfig: an explicitly configured compatible
		// endpoint may supply authentication through custom headers or externally.
		return true
	}
	name := strings.TrimSpace(mc.ModelName)
	model := strings.TrimSpace(mc.Model)
	return strings.EqualFold(name, "local-model") ||
		strings.EqualFold(model, "local-model") ||
		strings.HasSuffix(strings.ToLower(model), "/local-model")
}

func modelDiscoveryCacheKey(source modelSelection, mc *config.ModelConfig) (string, error) {
	if mc == nil {
		return "", fmt.Errorf("model discovery source is unavailable")
	}
	headers, err := json.Marshal(mc.CustomHeaders)
	if err != nil {
		return "", fmt.Errorf("encode model discovery identity: %w", err)
	}
	extraBody, err := json.Marshal(mc.ExtraBody)
	if err != nil {
		return "", fmt.Errorf("encode model discovery identity: %w", err)
	}
	provider := providers.NormalizeProvider(source.Provider)
	credentialIdentity := strings.Join(mc.APIKeys.Values(), "\x1f")
	if provider == "antigravity" {
		credentialIdentity = "antigravity:none"
		if cred, credErr := auth.GetCredential("google-antigravity"); credErr == nil && cred != nil {
			credentialIdentity = strings.Join([]string{
				cred.Email,
				cred.ProjectID,
				cred.AccessToken,
				cred.RefreshToken,
			}, "\x1f")
		}
	}
	material := strings.Join([]string{
		provider,
		strings.TrimSpace(source.ConfigRef),
		strings.TrimSpace(mc.APIBase),
		strings.TrimSpace(mc.Proxy),
		strings.TrimSpace(mc.AuthMethod),
		strings.TrimSpace(mc.ConnectMode),
		strings.TrimSpace(mc.Workspace),
		credentialIdentity,
		strings.TrimSpace(mc.UserAgent),
		string(headers),
		string(extraBody),
	}, "\x00")
	sum := sha256.Sum256([]byte(material))
	return provider + "\x00" + strings.TrimSpace(source.ConfigRef) + "\x00" + fmt.Sprintf("%x", sum[:]), nil
}

func uniqueModelSelections(items []modelSelection) []modelSelection {
	out := make([]modelSelection, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		key := providers.NormalizeProvider(item.Provider) + "\x00" +
			strings.ToLower(strings.TrimSpace(item.Model)) + "\x00" +
			strings.ToLower(strings.TrimSpace(item.ConfigRef))
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}

func uniqueModelMatch(raw string, items []modelSelection, allowAlias bool) (modelSelection, bool, error) {
	raw = strings.TrimSpace(raw)
	if allowAlias {
		aliases := make([]modelSelection, 0)
		for _, item := range items {
			if strings.TrimSpace(item.Alias) != "" && strings.EqualFold(item.Alias, raw) {
				aliases = append(aliases, item)
			}
		}
		aliases = uniqueModelSelections(aliases)
		if len(aliases) > 1 {
			return modelSelection{}, false, fmt.Errorf(
				"model %q ambiguous; gunakan provider/model atau alias unik",
				raw,
			)
		}
		if len(aliases) == 1 {
			return aliases[0], true, nil
		}
	}

	matches := make([]modelSelection, 0)
	for _, item := range items {
		qualified := providers.NormalizeProvider(item.Provider) + "/" + strings.TrimSpace(item.Model)
		if strings.EqualFold(item.Model, raw) || strings.EqualFold(qualified, raw) {
			matches = append(matches, item)
		}
	}
	matches = uniqueModelSelections(matches)
	if len(matches) > 1 {
		return modelSelection{}, false, fmt.Errorf(
			"model %q ambiguous; gunakan provider/model atau alias unik",
			raw,
		)
	}
	if len(matches) == 1 {
		return matches[0], true, nil
	}
	return modelSelection{}, false, nil
}

func resolveConfiguredModelSelection(raw string, cfg *config.Config) (modelSelection, bool, error) {
	return uniqueModelMatch(raw, configuredSelections(cfg), true)
}

func resolveDiscoveredModelSelection(
	ctx context.Context,
	cfg *config.Config,
	raw string,
) (modelSelection, bool, error) {
	items := make([]modelSelection, 0)
	for _, source := range discoverySources(cfg) {
		models, err := fetchDiscoveredModels(ctx, cfg, source, false)
		if err != nil {
			continue
		}
		for _, item := range models {
			items = append(items, modelSelection{
				Provider:  item.Provider,
				Model:     item.Model,
				ConfigRef: item.ConfigRef,
				Source:    "discovered",
			})
		}
	}
	return uniqueModelMatch(raw, items, false)
}

func validateModelSelectionMembership(cfg *config.Config, selection modelSelection) error {
	if strings.EqualFold(strings.TrimSpace(selection.Source), "discovered") {
		source := lookupSessionModelConfigByRef(cfg, selection.ConfigRef, cfg.Agents.Defaults.Provider)
		if source == nil || !modelConfigSelectable(source) {
			return fmt.Errorf("stale model selection: provider configuration is no longer available")
		}
		key, err := modelDiscoveryCacheKey(selection, source)
		if err != nil {
			return err
		}
		modelDiscoveryCache.RLock()
		entry, ok := modelDiscoveryCache.entries[key]
		modelDiscoveryCache.RUnlock()
		if !ok || entry.Err != "" || time.Now().After(entry.ExpiresAt) {
			return fmt.Errorf("stale model selection: refresh Available Models and select again")
		}
		for _, item := range entry.Models {
			if sameModel(item.Provider, item.Model, selection.Provider, selection.Model) &&
				strings.EqualFold(strings.TrimSpace(item.ConfigRef), strings.TrimSpace(selection.ConfigRef)) {
				return nil
			}
		}
		return fmt.Errorf("stale model selection: model is no longer in the current provider catalog")
	}

	for _, item := range configuredSelections(cfg) {
		if sameModel(item.Provider, item.Model, selection.Provider, selection.Model) &&
			strings.EqualFold(strings.TrimSpace(item.ConfigRef), strings.TrimSpace(selection.ConfigRef)) {
			return nil
		}
	}
	return fmt.Errorf("stale model selection: configured model is no longer available")
}

func validSessionModelOverrideSource(
	cfg *config.Config,
	override session.ModelOverride,
) *config.ModelConfig {
	if cfg == nil {
		return nil
	}
	source := lookupSessionModelConfigByRef(cfg, override.ConfigRef, cfg.Agents.Defaults.Provider)
	if source == nil || !modelConfigSelectable(source) {
		return nil
	}
	provider, _ := providers.ExtractProtocol(source)
	if providers.NormalizeProvider(provider) != providers.NormalizeProvider(override.Provider) {
		return nil
	}
	return source
}

func cachedDiscoveredSelectionsForConfig(cfg *config.Config) []modelSelection {
	out := make([]modelSelection, 0)
	for _, source := range discoverySources(cfg) {
		sourceConfig := lookupSessionModelConfigByRef(cfg, source.ConfigRef, cfg.Agents.Defaults.Provider)
		if sourceConfig == nil || !modelConfigSelectable(sourceConfig) {
			continue
		}
		key, err := modelDiscoveryCacheKey(source, sourceConfig)
		if err != nil {
			continue
		}
		modelDiscoveryCache.RLock()
		entry, ok := modelDiscoveryCache.entries[key]
		modelDiscoveryCache.RUnlock()
		if !ok || entry.Err != "" || time.Now().After(entry.ExpiresAt) {
			continue
		}
		for _, item := range entry.Models {
			out = appendUniqueSelection(out, modelSelection{
				Provider:  item.Provider,
				Model:     item.Model,
				ConfigRef: item.ConfigRef,
				Source:    "discovered",
			})
		}
	}
	return out
}
