package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/commands"
	"github.com/As-tsaqib/picoclaw/pkg/config"
	"github.com/As-tsaqib/picoclaw/pkg/modelcatalog"
	"github.com/As-tsaqib/picoclaw/pkg/providers"
	"github.com/As-tsaqib/picoclaw/pkg/session"
)

const (
	modelsPerPage         = 5
	modelDiscoveryTTL     = 5 * time.Minute
	modelDiscoveryTimeout = 12 * time.Second
)

type discoveredModel struct {
	Provider  string
	Model     string
	ConfigRef string
	OwnedBy   string
}

type discoveryCacheEntry struct {
	Models    []discoveredModel
	ExpiresAt time.Time
	Err       string
}

var modelDiscoveryCache = struct {
	sync.RWMutex
	entries map[string]discoveryCacheEntry
}{entries: make(map[string]discoveryCacheEntry)}

type modelMenuState struct {
	View      string `json:"view,omitempty"`
	Page      int    `json:"page,omitempty"`
	Provider  string `json:"provider,omitempty"`
	ConfigRef string `json:"config_ref,omitempty"`
	Query     string `json:"query,omitempty"`
}

type modelSelection struct {
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Alias     string `json:"alias,omitempty"`
	ConfigRef string `json:"config_ref"`
	Source    string `json:"source"`
}

type modelCommandContext struct {
	Agent      *AgentInstance
	SessionKey string
	Scope      *session.SessionScope
	Inbound    *bus.InboundContext
}

func configureModelCommandRuntime(rt *commands.Runtime, agent *AgentInstance, opts *processOptions, al *AgentLoop) {
	if rt == nil || agent == nil || opts == nil || al == nil {
		return
	}
	rt.ModelCommand = func(ctx context.Context, req commands.ModelCommandRequest) (*bus.StructuredContent, error) {
		return al.executeModelCommand(ctx, modelCommandContext{
			Agent:      agent,
			SessionKey: strings.TrimSpace(opts.Dispatch.SessionKey),
			Scope:      session.CloneScope(opts.Dispatch.SessionScope),
			Inbound:    cloneInboundContext(opts.Dispatch.InboundContext),
		}, req)
	}
}

func (al *AgentLoop) executeModelCommand(
	ctx context.Context,
	mcx modelCommandContext,
	req commands.ModelCommandRequest,
) (*bus.StructuredContent, error) {
	if mcx.Agent == nil || strings.TrimSpace(mcx.SessionKey) == "" {
		return nil, fmt.Errorf("session model context is unavailable")
	}
	store, ok := mcx.Agent.Sessions.(session.ModelOverrideStore)
	if !ok {
		return nil, fmt.Errorf("session model persistence is unavailable")
	}
	cfg := al.GetConfig()
	if cfg == nil {
		return nil, fmt.Errorf("config is unavailable")
	}

	switch strings.ToLower(strings.TrimSpace(req.Operation)) {
	case "", "dashboard":
		return al.buildModelDashboard(mcx, store, cfg), nil
	case "current":
		return al.buildModelCurrent(mcx, store, cfg), nil
	case "list":
		return al.buildConfiguredModels(mcx, store, cfg, 0), nil
	case "use":
		selection, err := al.resolveModelSelection(ctx, cfg, req.Argument)
		if err != nil {
			return nil, err
		}
		if err := validateAndPersistModelSelection(mcx, store, cfg, selection); err != nil {
			return nil, err
		}
		return buildModelChangedContent(selection), nil
	case "default":
		if err := store.ClearModelOverride(mcx.SessionKey); err != nil {
			return nil, err
		}
		return al.buildModelDefaultResult(mcx, store, cfg), nil
	case "search":
		return al.buildModelSearch(mcx, store, cfg, strings.TrimSpace(req.Argument), 0), nil
	default:
		return nil, fmt.Errorf("unknown model subcommand")
	}
}

func (al *AgentLoop) handleInternalModelCallback(
	ctx context.Context,
	req bus.InternalCallbackRequest,
) (*bus.InternalCallbackResponse, error) {
	inbound := bus.NormalizeInboundMessage(bus.InboundMessage{Context: req.Inbound}).Context
	if strings.TrimSpace(req.OwnerID) == "" || inbound.SenderID != req.OwnerID ||
		inbound.Channel != req.Channel || inbound.Account != req.Account ||
		inbound.ChatID != req.ChatID || inbound.TopicID != req.TopicID {
		return nil, fmt.Errorf("callback scope validation failed")
	}
	route, agent, routeErr := al.resolveMessageRoute(bus.InboundMessage{Context: inbound})
	if routeErr != nil || agent == nil || !strings.EqualFold(agent.ID, req.AgentID) {
		return nil, fmt.Errorf("callback agent validation failed")
	}
	allocation := session.AllocateRouteSession(session.AllocationInput{
		AgentID: route.AgentID, Context: inbound, SessionPolicy: route.SessionPolicy,
	})
	if session.CanonicalScopeSignature(allocation.Scope) != req.Scope {
		return nil, fmt.Errorf("callback session scope validation failed")
	}
	if strings.TrimSpace(req.SessionKey) == "" || req.SessionKey != allocation.SessionKey {
		catalog, ok := agent.Sessions.(session.ScopedSessionStore)
		if !ok || !catalogSessionInScope(catalog, &allocation.Scope, allocation.SessionAliases, req.SessionKey) {
			return nil, fmt.Errorf("callback session validation failed")
		}
	}
	mcx := modelCommandContext{
		Agent: agent, SessionKey: req.SessionKey, Scope: &allocation.Scope, Inbound: &inbound,
	}
	store, ok := agent.Sessions.(session.ModelOverrideStore)
	if !ok {
		return nil, fmt.Errorf("session model persistence is unavailable")
	}
	cfg := al.GetConfig()
	if cfg == nil {
		return nil, fmt.Errorf("config is unavailable")
	}

	state := modelMenuState{Page: req.Page}
	if strings.TrimSpace(req.Value) != "" {
		_ = json.Unmarshal([]byte(req.Value), &state)
	}

	switch strings.ToLower(strings.TrimSpace(req.Action)) {
	case "select":
		var selection modelSelection
		if err := json.Unmarshal([]byte(req.Value), &selection); err != nil {
			return nil, fmt.Errorf("invalid model selection")
		}
		if err := validateAndPersistModelSelection(mcx, store, cfg, selection); err != nil {
			content := al.buildModelDashboard(mcx, store, cfg)
			content.Title = "⚠️ Gagal Mengaktifkan Model"
			message := sanitizeModelCell(err.Error())
			content.Paragraphs = []string{message}
			content.Fallback = message + "\n\n" + content.Fallback
			return &bus.InternalCallbackResponse{Content: content}, nil
		}
		content := al.buildModelDashboard(mcx, store, cfg)
		content.Title = "✅ Model Diganti"
		content.Fallback = "✅ Model Diganti\n\n" + content.Fallback
		return &bus.InternalCallbackResponse{Content: content}, nil
	case "dashboard", "back":
		return &bus.InternalCallbackResponse{Content: al.buildModelDashboard(mcx, store, cfg)}, nil
	case "configured":
		return &bus.InternalCallbackResponse{Content: al.buildConfiguredModels(mcx, store, cfg, state.Page)}, nil
	case "available", "provider", "page", "refresh":
		if req.Action == "page" || req.Action == "provider" || req.Action == "refresh" {
			if err := json.Unmarshal([]byte(req.Value), &state); err != nil {
				return nil, fmt.Errorf("invalid model menu state")
			}
		}
		if state.View == "configured" {
			return &bus.InternalCallbackResponse{Content: al.buildConfiguredModels(mcx, store, cfg, state.Page)}, nil
		}
		if state.View == "search" {
			return &bus.InternalCallbackResponse{
				Content: al.buildModelSearch(mcx, store, cfg, state.Query, state.Page),
			}, nil
		}
		force := strings.EqualFold(req.Action, "refresh")
		content := al.buildAvailableModels(
			ctx,
			mcx,
			store,
			cfg,
			state.Provider,
			state.ConfigRef,
			state.Page,
			force,
		)
		return &bus.InternalCallbackResponse{Content: content}, nil
	case "detail":
		return &bus.InternalCallbackResponse{Content: al.buildModelDetail(mcx, store, cfg)}, nil
	case "default":
		if err := store.ClearModelOverride(mcx.SessionKey); err != nil {
			content := al.buildModelDashboard(mcx, store, cfg)
			content.Title = "⚠️ Gagal Mengembalikan Model Default"
			message := sanitizeModelCell(err.Error())
			content.Paragraphs = []string{message}
			content.Fallback = message + "\n\n" + content.Fallback
			return &bus.InternalCallbackResponse{Content: content}, nil
		}
		content := al.buildModelDashboard(mcx, store, cfg)
		content.Title = "♻️ Model Dikembalikan ke Default"
		content.Fallback = "♻️ Model Dikembalikan ke Default\n\n" + content.Fallback
		return &bus.InternalCallbackResponse{Content: content}, nil
	case "noop":
		return &bus.InternalCallbackResponse{Text: fmt.Sprintf("Halaman %d", req.Page+1)}, nil
	case "close":
		return &bus.InternalCallbackResponse{Close: true}, nil
	default:
		return nil, fmt.Errorf("invalid model callback action")
	}
}

func (al *AgentLoop) buildModelDashboard(
	mcx modelCommandContext,
	store session.ModelOverrideStore,
	cfg *config.Config,
) *bus.StructuredContent {
	info := effectiveSessionModel(mcx.Agent, store, cfg, mcx.SessionKey)
	rows := [][]string{
		{"Provider", displayProvider(info.Provider)},
		{"Alias", fallbackDash(info.Alias)},
		{"Model", fallbackDash(info.Model)},
		{"Scope", info.Scope},
		{"Status", "● Active"},
	}
	if info.Fallback != "" {
		rows = append(rows, []string{"Fallback", info.Fallback})
	}
	entries := []bus.InteractionEntry{
		{Label: "🔄 Ganti Model", Action: "configured", Value: mustModelState(modelMenuState{View: "configured"})},
		{
			Label:  "🌐 Available Models",
			Action: "available",
			Value: mustModelState(modelMenuState{
				View: "available", Provider: info.Provider, ConfigRef: info.ConfigRef,
			}),
		},
		{Label: "📋 Configured", Action: "configured", Value: mustModelState(modelMenuState{View: "configured"})},
		{Label: "🧠 Detail", Action: "detail"},
		{Label: "♻️ Default", Action: "default"},
		{Label: "✖️ Tutup", Action: "close"},
	}
	return modelStructuredContent(
		"model_dashboard",
		"Model",
		[]string{"Properti", "Nilai"},
		rows,
		modelDashboardFallback(rows),
		modelMenu(mcx, 0, 1, entries),
	)
}

func (al *AgentLoop) buildModelCurrent(
	mcx modelCommandContext,
	store session.ModelOverrideStore,
	cfg *config.Config,
) *bus.StructuredContent {
	info := effectiveSessionModel(mcx.Agent, store, cfg, mcx.SessionKey)
	rows := [][]string{
		{"Provider", displayProvider(info.Provider)},
		{"Alias", fallbackDash(info.Alias)},
		{"Model", fallbackDash(info.Model)},
		{"Scope", info.Scope},
		{"Status", "● Active"},
	}
	return modelStructuredContent(
		"model_current", "Model Aktif", []string{"Properti", "Nilai"}, rows,
		modelDashboardFallback(rows), nil,
	)
}

func (al *AgentLoop) buildConfiguredModels(
	mcx modelCommandContext,
	store session.ModelOverrideStore,
	cfg *config.Config,
	page int,
) *bus.StructuredContent {
	models := configuredSelections(cfg)
	active := effectiveSessionModel(mcx.Agent, store, cfg, mcx.SessionKey)
	return buildModelListContent(mcx, "Configured Models", "configured", models, active, page, cfg, nil)
}

func (al *AgentLoop) buildAvailableModels(
	ctx context.Context,
	mcx modelCommandContext,
	store session.ModelOverrideStore,
	cfg *config.Config,
	requestedProvider string,
	requestedConfigRef string,
	page int,
	force bool,
) *bus.StructuredContent {
	sources := discoverySources(cfg)
	if len(sources) == 0 {
		return modelParagraph("Tidak ada provider terkonfigurasi yang mendukung live model discovery.")
	}
	provider := providers.NormalizeProvider(strings.TrimSpace(requestedProvider))
	source := chooseDiscoverySource(sources, provider, requestedConfigRef)
	models, err := fetchDiscoveredModels(ctx, cfg, source, force)
	active := effectiveSessionModel(mcx.Agent, store, cfg, mcx.SessionKey)
	if err != nil {
		return buildDiscoveryErrorContent(mcx, cfg, sources, source, err)
	}
	selections := make([]modelSelection, 0, len(models))
	for _, model := range models {
		selections = append(selections, modelSelection{
			Provider: model.Provider, Model: model.Model, ConfigRef: model.ConfigRef, Source: "discovered",
		})
	}
	return buildModelListContent(
		mcx,
		"Available Models — "+discoverySourceLabel(source, sources),
		"available",
		selections,
		active,
		page,
		cfg,
		sources,
	)
}

func (al *AgentLoop) buildModelSearch(
	mcx modelCommandContext,
	store session.ModelOverrideStore,
	cfg *config.Config,
	query string,
	page int,
) *bus.StructuredContent {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return modelParagraph("Gunakan /model search <kata> untuk mencari model.")
	}
	all := configuredSelections(cfg)
	for _, cached := range cachedDiscoveredSelectionsForConfig(cfg) {
		all = appendUniqueSelection(all, cached)
	}
	filtered := make([]modelSelection, 0, len(all))
	for _, item := range all {
		haystack := strings.ToLower(item.Provider + " " + item.Alias + " " + item.Model)
		if strings.Contains(haystack, query) {
			filtered = append(filtered, item)
		}
	}
	active := effectiveSessionModel(mcx.Agent, store, cfg, mcx.SessionKey)
	content := buildModelListContent(mcx, "Hasil Pencarian — "+query, "search", filtered, active, page, cfg, nil)
	if content.Interaction != nil {
		for i := range content.Interaction.Entries {
			entry := &content.Interaction.Entries[i]
			if entry.Action == "page" {
				var state modelMenuState
				_ = json.Unmarshal([]byte(entry.Value), &state)
				state.Query = query
				entry.Value = mustModelState(state)
			}
		}
	}
	return content
}

func (al *AgentLoop) buildModelDetail(
	mcx modelCommandContext,
	store session.ModelOverrideStore,
	cfg *config.Config,
) *bus.StructuredContent {
	info := effectiveSessionModel(mcx.Agent, store, cfg, mcx.SessionKey)
	rows := [][]string{
		{"Provider", displayProvider(info.Provider)},
		{"Alias", fallbackDash(info.Alias)},
		{"Model ID", fallbackDash(info.Model)},
		{"Scope", info.Scope},
		{"Source", info.Source},
		{"Status", "● Active"},
	}
	if info.Thinking != "" {
		rows = append(rows, []string{"Thinking", info.Thinking})
	}
	if info.Fallback != "" {
		rows = append(rows, []string{"Fallback", info.Fallback})
	}
	entries := []bus.InteractionEntry{{Label: "◀️ Kembali", Action: "back"}, {Label: "✖️ Tutup", Action: "close"}}
	return modelStructuredContent(
		"model_detail",
		"Model Detail",
		[]string{"Properti", "Nilai"},
		rows,
		modelDashboardFallback(rows),
		modelMenu(mcx, 0, 1, entries),
	)
}

func (al *AgentLoop) buildModelDefaultResult(
	mcx modelCommandContext,
	store session.ModelOverrideStore,
	cfg *config.Config,
) *bus.StructuredContent {
	info := effectiveSessionModel(mcx.Agent, store, cfg, mcx.SessionKey)
	rows := [][]string{
		{"Provider", displayProvider(info.Provider)},
		{"Alias", fallbackDash(info.Alias)},
		{"Model", fallbackDash(info.Model)},
		{"Scope", "Default Agent"},
	}
	return modelStructuredContent(
		"model_default",
		"♻️ Model Dikembalikan ke Default",
		[]string{"Properti", "Nilai"},
		rows,
		modelDashboardFallback(rows),
		nil,
	)
}

type effectiveModelInfo struct {
	Provider  string
	Alias     string
	Model     string
	ConfigRef string
	Scope     string
	Source    string
	Thinking  string
	Fallback  string
}

func effectiveSessionModel(
	agent *AgentInstance,
	store session.ModelOverrideStore,
	cfg *config.Config,
	sessionKey string,
) effectiveModelInfo {
	if store != nil {
		if override, ok, err := store.GetModelOverride(sessionKey); err == nil && ok {
			if source := validSessionModelOverrideSource(cfg, override); source != nil {
				return effectiveModelInfo{
					Provider:  override.Provider,
					Alias:     override.Alias,
					Model:     override.Model,
					ConfigRef: override.ConfigRef,
					Scope:     "Session ini",
					Source:    modelSourceLabel(override.Source),
					Thinking:  strings.TrimSpace(source.ThinkingLevel),
					Fallback:  modelFallbackLabel(cfg, source.Fallbacks),
				}
			}
		}
	}
	provider := cfg.Agents.Defaults.Provider
	model := ""
	alias := ""
	configRef := ""
	fallbacks := []string(nil)
	thinking := ""
	if agent != nil {
		provider = resolvedCandidateProvider(agent.Candidates, provider)
		model = resolvedCandidateModel(agent.Candidates, agent.Model)
		alias = resolvedCandidateModelName(agent.Candidates, agent.Model)
		fallbacks = agent.Fallbacks
		if source := resolveActiveModelConfig(
			cfg,
			agent.Workspace,
			agent.Candidates,
			agent.Model,
			provider,
		); source != nil {
			thinking = strings.TrimSpace(source.ThinkingLevel)
			configRef = stableModelConfigRef(source)
		}
	}
	return effectiveModelInfo{
		Provider: provider, Alias: alias, Model: model, ConfigRef: configRef,
		Scope: "Default Agent", Source: "Configured",
		Thinking: thinking, Fallback: modelFallbackLabel(cfg, fallbacks),
	}
}

func configuredSelections(cfg *config.Config) []modelSelection {
	if cfg == nil {
		return nil
	}
	out := make([]modelSelection, 0, len(cfg.ModelList))
	for _, mc := range cfg.ModelList {
		if !modelConfigSelectable(mc) || strings.TrimSpace(mc.Model) == "" {
			continue
		}
		provider, model := providers.ExtractProtocol(mc)
		if strings.TrimSpace(model) == "" {
			continue
		}
		out = append(out, modelSelection{
			Provider: providers.NormalizeProvider(provider), Model: model,
			Alias: strings.TrimSpace(mc.ModelName), ConfigRef: stableModelConfigRef(mc), Source: "configured",
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		li := strings.ToLower(out[i].Alias + " " + out[i].Provider + " " + out[i].Model + " " + out[i].ConfigRef)
		lj := strings.ToLower(out[j].Alias + " " + out[j].Provider + " " + out[j].Model + " " + out[j].ConfigRef)
		return li < lj
	})
	return out
}

func discoverySources(cfg *config.Config) []modelSelection {
	configured := configuredSelections(cfg)
	seen := make(map[string]bool)
	out := make([]modelSelection, 0, len(configured))
	for _, item := range configured {
		if !providers.IsModelProviderFetchable(item.Provider) {
			continue
		}
		key := item.Provider + "\x00" + item.ConfigRef
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}

func chooseDiscoverySource(sources []modelSelection, provider, configRef string) modelSelection {
	configRef = strings.TrimSpace(configRef)
	if configRef != "" {
		for _, source := range sources {
			if strings.EqualFold(strings.TrimSpace(source.ConfigRef), configRef) &&
				(provider == "" || providers.NormalizeProvider(source.Provider) == provider) {
				return source
			}
		}
	}
	for _, source := range sources {
		if providers.NormalizeProvider(source.Provider) == provider {
			return source
		}
	}
	return sources[0]
}

func fetchDiscoveredModels(
	ctx context.Context,
	cfg *config.Config,
	source modelSelection,
	force bool,
) ([]discoveredModel, error) {
	sourceConfig := lookupSessionModelConfigByRef(cfg, source.ConfigRef, cfg.Agents.Defaults.Provider)
	if sourceConfig == nil || !modelConfigSelectable(sourceConfig) {
		return nil, fmt.Errorf("configured provider source %q is unavailable", source.ConfigRef)
	}
	cacheKey, err := modelDiscoveryCacheKey(source, sourceConfig)
	if err != nil {
		return nil, err
	}
	if !force {
		modelDiscoveryCache.RLock()
		cached, ok := modelDiscoveryCache.entries[cacheKey]
		modelDiscoveryCache.RUnlock()
		if ok && time.Now().Before(cached.ExpiresAt) {
			if cached.Err != "" {
				return nil, errors.New(cached.Err)
			}
			return append([]discoveredModel(nil), cached.Models...), nil
		}
	}
	fetchCtx, cancel := context.WithTimeout(ctx, modelDiscoveryTimeout)
	defer cancel()
	models, err := modelcatalog.Fetch(fetchCtx, sourceConfig)
	entry := discoveryCacheEntry{ExpiresAt: time.Now().Add(modelDiscoveryTTL)}
	if err != nil {
		entry.Err = err.Error()
	} else {
		entry.Models = make([]discoveredModel, 0, len(models))
		for _, item := range models {
			entry.Models = append(entry.Models, discoveredModel{
				Provider: source.Provider, Model: item.ID, ConfigRef: source.ConfigRef, OwnedBy: item.OwnedBy,
			})
		}
	}
	modelDiscoveryCache.Lock()
	modelDiscoveryCache.entries[cacheKey] = entry
	modelDiscoveryCache.Unlock()
	if err != nil {
		return nil, err
	}
	return append([]discoveredModel(nil), entry.Models...), nil
}

func (al *AgentLoop) resolveModelSelection(
	ctx context.Context,
	cfg *config.Config,
	raw string,
) (modelSelection, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return modelSelection{}, fmt.Errorf("model is required")
	}
	if selection, ok, err := resolveConfiguredModelSelection(raw, cfg); err != nil {
		return modelSelection{}, err
	} else if ok {
		return selection, nil
	}
	if selection, ok, err := resolveDiscoveredModelSelection(ctx, cfg, raw); err != nil {
		return modelSelection{}, err
	} else if ok {
		return selection, nil
	}
	return modelSelection{}, fmt.Errorf("model %q tidak ditemukan; gunakan /model list atau Available Models", raw)
}

func validateAndPersistModelSelection(
	mcx modelCommandContext,
	store session.ModelOverrideStore,
	cfg *config.Config,
	selection modelSelection,
) error {
	selection.Provider = providers.NormalizeProvider(strings.TrimSpace(selection.Provider))
	selection.Model = strings.TrimSpace(selection.Model)
	selection.ConfigRef = strings.TrimSpace(selection.ConfigRef)
	if selection.Provider == "" || selection.Model == "" || selection.ConfigRef == "" {
		return fmt.Errorf("invalid model selection")
	}
	if err := validateModelSelectionMembership(cfg, selection); err != nil {
		return err
	}
	source := lookupSessionModelConfigByRef(cfg, selection.ConfigRef, cfg.Agents.Defaults.Provider)
	if source == nil || !modelConfigSelectable(source) {
		return fmt.Errorf("provider configuration %q is unavailable", selection.ConfigRef)
	}
	sourceProvider, _ := providers.ExtractProtocol(source)
	if providers.NormalizeProvider(sourceProvider) != selection.Provider {
		return fmt.Errorf("model provider does not match configured source")
	}
	clone := *source
	clone.Model = selection.Model
	clone.Provider = selection.Provider
	provider, _, err := providers.CreateProviderFromConfig(&clone)
	if err != nil {
		return fmt.Errorf("failed to initialize model %q: %w", selection.Model, err)
	}
	if stateful, ok := provider.(providers.StatefulProvider); ok {
		stateful.Close()
	}
	return store.SetModelOverride(mcx.SessionKey, session.ModelOverride{
		Provider: selection.Provider, Model: selection.Model, Alias: selection.Alias,
		ConfigRef: selection.ConfigRef, Source: selection.Source,
	})
}

func buildModelChangedContent(selection modelSelection) *bus.StructuredContent {
	rows := [][]string{
		{"Provider", displayProvider(selection.Provider)},
		{"Alias", fallbackDash(selection.Alias)},
		{"Model", selection.Model},
		{"Scope", "Session ini"},
		{"Source", modelSourceLabel(selection.Source)},
	}
	return modelStructuredContent(
		"model_changed",
		"✅ Model Diganti",
		[]string{"Properti", "Nilai"},
		rows,
		modelDashboardFallback(rows),
		nil,
	)
}

func buildModelListContent(
	mcx modelCommandContext,
	title string,
	view string,
	models []modelSelection,
	active effectiveModelInfo,
	page int,
	cfg *config.Config,
	providersForFilter []modelSelection,
) *bus.StructuredContent {
	if len(models) == 0 {
		return modelParagraph("Tidak ada model yang tersedia untuk tampilan ini.")
	}
	pages := (len(models) + modelsPerPage - 1) / modelsPerPage
	if page < 0 {
		page = 0
	}
	if page >= pages {
		page = pages - 1
	}
	start := page * modelsPerPage
	end := start + modelsPerPage
	if end > len(models) {
		end = len(models)
	}
	rows := make([][]string, 0, end-start)
	entries := make([]bus.InteractionEntry, 0, end-start+12)
	for i := start; i < end; i++ {
		item := models[i]
		pageNumber := i - start + 1
		no := strconv.Itoa(pageNumber)
		status := ""
		if sameEffectiveModel(active, item) {
			no = "✅" + no
			status = "Active"
		} else if isConfiguredModel(cfg, item.Provider, item.Model, item.ConfigRef) {
			status = "★ Configured"
		}
		label := item.Model
		if view == "configured" && item.Alias != "" {
			label = item.Alias
		}
		rows = append(rows, []string{no, displayProvider(item.Provider), label, status})
		encoded, _ := json.Marshal(item)
		entries = append(
			entries,
			bus.InteractionEntry{Label: strconv.Itoa(pageNumber), Action: "select", Value: string(encoded)},
		)
	}
	if page > 0 {
		entries = append(
			entries,
			bus.InteractionEntry{
				Label: "◀️", Action: "page",
				Value: mustModelState(modelMenuState{
					View:      view,
					Page:      page - 1,
					Provider:  activeListProvider(models),
					ConfigRef: activeListConfigRef(models),
				}),
			},
		)
	}
	entries = append(entries, bus.InteractionEntry{Label: fmt.Sprintf("Halaman %d/%d", page+1, pages), Action: "noop"})
	if page+1 < pages {
		entries = append(
			entries,
			bus.InteractionEntry{
				Label: "▶️", Action: "page",
				Value: mustModelState(modelMenuState{
					View:      view,
					Page:      page + 1,
					Provider:  activeListProvider(models),
					ConfigRef: activeListConfigRef(models),
				}),
			},
		)
	}
	if view == "available" {
		seenSources := make(map[string]bool)
		for _, source := range providersForFilter {
			provider := providers.NormalizeProvider(source.Provider)
			key := provider + "\x00" + strings.TrimSpace(source.ConfigRef)
			if provider == "" || seenSources[key] {
				continue
			}
			seenSources[key] = true
			entries = append(entries, bus.InteractionEntry{
				Label: discoverySourceLabel(source, providersForFilter), Action: "provider",
				Value: mustModelState(modelMenuState{
					View:      "available",
					Provider:  provider,
					ConfigRef: source.ConfigRef,
				}),
			})
		}
		entries = append(entries, bus.InteractionEntry{
			Label: "🔃 Refresh", Action: "refresh",
			Value: mustModelState(modelMenuState{
				View:      "available",
				Provider:  activeListProvider(models),
				ConfigRef: activeListConfigRef(models),
			}),
		})
	}
	entries = append(entries,
		bus.InteractionEntry{Label: "◀️ Kembali", Action: "back"},
		bus.InteractionEntry{Label: "✖️ Tutup", Action: "close"},
	)
	fallback := modelListFallback(title, rows)
	return modelStructuredContent(
		"model_list", title, []string{"No", "Provider", "Model", "Status"}, rows, fallback,
		modelMenu(mcx, page, pages, entries),
	)
}

func buildDiscoveryErrorContent(
	mcx modelCommandContext,
	cfg *config.Config,
	sources []modelSelection,
	selected modelSelection,
	err error,
) *bus.StructuredContent {
	rows := [][]string{{discoverySourceLabel(selected, sources), "⚠ " + sanitizeModelCell(err.Error())}}
	entries := make([]bus.InteractionEntry, 0, len(sources)+3)
	seenSources := make(map[string]bool)
	for _, source := range sources {
		provider := providers.NormalizeProvider(source.Provider)
		key := provider + "\x00" + strings.TrimSpace(source.ConfigRef)
		if seenSources[key] {
			continue
		}
		seenSources[key] = true
		entries = append(entries, bus.InteractionEntry{
			Label: discoverySourceLabel(source, sources), Action: "provider",
			Value: mustModelState(modelMenuState{
				View: "available", Provider: provider, ConfigRef: source.ConfigRef,
			}),
		})
	}
	entries = append(
		entries,
		bus.InteractionEntry{
			Label: "🔃 Refresh", Action: "refresh",
			Value: mustModelState(modelMenuState{
				View: "available", Provider: selected.Provider, ConfigRef: selected.ConfigRef,
			}),
		},
		bus.InteractionEntry{Label: "◀️ Kembali", Action: "back"},
		bus.InteractionEntry{Label: "✖️ Tutup", Action: "close"},
	)
	_ = cfg
	return modelStructuredContent(
		"model_discovery_error",
		"Available Models",
		[]string{"Provider", "Status"},
		rows,
		modelListFallback("Available Models", rows),
		modelMenu(mcx, 0, 1, entries),
	)
}

func modelMenu(mcx modelCommandContext, page, pages int, entries []bus.InteractionEntry) *bus.InteractionMenu {
	inbound := bus.InboundContext{}
	if clone := cloneInboundContext(mcx.Inbound); clone != nil {
		inbound = *clone
	}
	scope := ""
	if mcx.Scope != nil {
		scope = session.CanonicalScopeSignature(*mcx.Scope)
	}
	return &bus.InteractionMenu{
		Kind:    "model",
		OwnerID: sessionMenuOwner(mcx.Inbound),
		Channel: inboundChannel(mcx.Inbound),
		Account: inboundAccount(mcx.Inbound),
		ChatID:  inboundChatID(mcx.Inbound),
		TopicID: inboundTopicID(mcx.Inbound),
		AgentID: mcx.Agent.ID,
		Scope:   scope,
		Inbound: inbound,
		Page:    page,
		Pages:   pages,
		Entries: entries,
		Current: mcx.SessionKey,
	}
}

func modelStructuredContent(
	kind, title string,
	columns []string,
	rows [][]string,
	fallback string,
	menu *bus.InteractionMenu,
) *bus.StructuredContent {
	return &bus.StructuredContent{
		Kind:        kind,
		Title:       title,
		Tables:      []bus.StructuredTable{{Columns: columns, Rows: rows, Border: true, Striped: true, Header: true}},
		Fallback:    fallback,
		Interaction: menu,
	}
}

func modelParagraph(text string) *bus.StructuredContent {
	return &bus.StructuredContent{Kind: "model_message", Paragraphs: []string{text}, Fallback: text}
}

func mustModelState(state modelMenuState) string {
	data, _ := json.Marshal(state)
	return string(data)
}

func modelDashboardFallback(rows [][]string) string {
	lines := make([]string, 0, len(rows)+1)
	for _, row := range rows {
		if len(row) >= 2 {
			lines = append(lines, row[0]+": "+row[1])
		}
	}
	return strings.Join(lines, "\n")
}

func modelListFallback(title string, rows [][]string) string {
	lines := []string{title, "| No | Provider | Model | Status |", "|---|---|---|---|"}
	for _, row := range rows {
		if len(row) < 4 {
			continue
		}
		lines = append(
			lines,
			fmt.Sprintf(
				"| %s | %s | %s | %s |",
				escapeTableCell(row[0]),
				escapeTableCell(row[1]),
				escapeTableCell(row[2]),
				escapeTableCell(row[3]),
			),
		)
	}
	return strings.Join(lines, "\n")
}

func modelFallbackLabel(cfg *config.Config, fallbacks []string) string {
	if len(fallbacks) == 0 {
		return ""
	}
	labels := make([]string, 0, len(fallbacks))
	for _, fallback := range fallbacks {
		fallback = strings.TrimSpace(fallback)
		if fallback == "" {
			continue
		}
		if mc := lookupModelConfigByRef(
			cfg,
			fallback,
			cfg.Agents.Defaults.Provider,
		); mc != nil &&
			strings.TrimSpace(mc.ModelName) != "" {
			labels = append(labels, mc.ModelName)
		} else {
			labels = append(labels, fallback)
		}
	}
	return strings.Join(labels, " → ")
}

func modelSourceLabel(source string) string {
	if strings.EqualFold(strings.TrimSpace(source), "discovered") {
		return "Live discovery"
	}
	return "Configured"
}

func displayProvider(provider string) string {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return "-"
	}
	parts := strings.FieldsFunc(provider, func(r rune) bool { return r == '-' || r == '_' })
	for i := range parts {
		if parts[i] == "" {
			continue
		}
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	return strings.Join(parts, " ")
}

func discoverySourceLabel(source modelSelection, sources []modelSelection) string {
	provider := providers.NormalizeProvider(source.Provider)
	base := displayProvider(provider)
	count := 0
	aliasCount := 0
	for _, candidate := range sources {
		if providers.NormalizeProvider(candidate.Provider) != provider {
			continue
		}
		count++
		if strings.EqualFold(strings.TrimSpace(candidate.Alias), strings.TrimSpace(source.Alias)) {
			aliasCount++
		}
	}
	if count <= 1 {
		return base
	}
	alias := strings.TrimSpace(source.Alias)
	if alias != "" && aliasCount == 1 {
		return base + " · " + alias
	}
	ref := strings.TrimPrefix(strings.TrimSpace(source.ConfigRef), modelConfigRefPrefix)
	if len(ref) > 8 {
		ref = ref[:8]
	}
	if alias != "" {
		return base + " · " + alias + " · " + ref
	}
	return base + " · " + ref
}

func fallbackDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return strings.TrimSpace(value)
}

func sanitizeModelCell(value string) string {
	value = strings.ReplaceAll(strings.ReplaceAll(value, "\n", " "), "\r", " ")
	if len(value) > 160 {
		return value[:157] + "..."
	}
	return value
}

func sameModel(providerA, modelA, providerB, modelB string) bool {
	return providers.NormalizeProvider(providerA) == providers.NormalizeProvider(providerB) &&
		strings.EqualFold(strings.TrimSpace(modelA), strings.TrimSpace(modelB))
}

func sameEffectiveModel(active effectiveModelInfo, item modelSelection) bool {
	if !sameModel(active.Provider, active.Model, item.Provider, item.Model) {
		return false
	}
	activeRef := strings.TrimSpace(active.ConfigRef)
	itemRef := strings.TrimSpace(item.ConfigRef)
	if activeRef != "" && itemRef != "" {
		return strings.EqualFold(activeRef, itemRef)
	}
	return true
}

func isConfiguredModel(cfg *config.Config, provider, model, configRef string) bool {
	for _, item := range configuredSelections(cfg) {
		if !sameModel(provider, model, item.Provider, item.Model) {
			continue
		}
		if strings.TrimSpace(configRef) == "" ||
			strings.EqualFold(strings.TrimSpace(configRef), strings.TrimSpace(item.ConfigRef)) {
			return true
		}
	}
	return false
}

func activeListProvider(models []modelSelection) string {
	if len(models) == 0 {
		return ""
	}
	return providers.NormalizeProvider(models[0].Provider)
}

func activeListConfigRef(models []modelSelection) string {
	if len(models) == 0 {
		return ""
	}
	return strings.TrimSpace(models[0].ConfigRef)
}

func appendUniqueSelection(items []modelSelection, candidate modelSelection) []modelSelection {
	for _, item := range items {
		if sameModel(item.Provider, item.Model, candidate.Provider, candidate.Model) &&
			strings.EqualFold(strings.TrimSpace(item.ConfigRef), strings.TrimSpace(candidate.ConfigRef)) {
			return items
		}
	}
	return append(items, candidate)
}
