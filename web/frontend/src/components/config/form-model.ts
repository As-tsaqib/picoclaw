export type JsonRecord = Record<string, unknown>

export interface CoreConfigForm {
  workspace: string
  restrictToWorkspace: boolean
  splitOnMarker: boolean
  toolFeedbackEnabled: boolean
  toolFeedbackMaxArgsLength: string
  toolFeedbackSeparateMessages: boolean
  execEnabled: boolean
  allowRemote: boolean
  enableDenyPatterns: boolean
  customDenyPatternsText: string
  customAllowPatternsText: string
  execTimeoutSeconds: string
  allowCommand: boolean
  cronExecTimeoutMinutes: string
  maxTokens: string
  contextWindow: string
  maxToolIterations: string
  summarizeMessageThreshold: string
  summarizeTokenPercent: string
  turnProfile: TurnProfileForm
  dmScope: string
  heartbeatEnabled: boolean
  heartbeatInterval: string
  devicesEnabled: boolean
  monitorUSB: boolean
  mcpEnabled: boolean
  mcpDiscoveryEnabled: boolean
  mcpDiscoveryTTL: string
  mcpDiscoveryMaxSearchResults: string
  mcpDiscoveryUseBM25: boolean
  mcpDiscoveryUseRegex: boolean
  mcpServers: MCPServerForm[]
  evolutionEnabled: boolean
  evolutionMode: EvolutionMode
  evolutionStateDir: string
  evolutionMinTaskCount: string
  evolutionMinSuccessRatio: string
  evolutionColdPathTrigger: EvolutionColdPathTrigger
  evolutionColdPathTimesText: string
  evolutionApplyPolicy: EvolutionApplyPolicy
  evolutionPrivateDataScrubbing: boolean
  evolutionDraftTimeoutSeconds: string
  evolutionMaxEvidenceRecords: string
  evolutionMaxDraftChars: string
  evolutionRollbackRetention: string
  memoryEnabled: boolean
  memoryBackgroundReviewEnabled: boolean
  memoryReviewInterval: string
  memoryReviewProvider: string
  memoryReviewModel: string
  memoryReviewTimeoutSeconds: string
  memoryReviewMaxIterations: string
  memoryWriteApproval: boolean
  memoryApprovalMode: MemoryApprovalMode
  memoryNotifications: MemoryNotificationMode
  memoryWorkspaceCharLimit: string
  memoryPerUserCharLimit: string
  memoryRecallMode: MemoryRecallMode
  memoryRecallMaxResults: string
  memoryRecallMaxChars: string
  memoryCheckpointsEnabled: boolean
  memoryCheckpointMaxCount: string
  memoryCheckpointMaxContextChars: string
  memoryCheckpointCompletedRetentionDays: string
  memoryRetrievalEnabled: boolean
  memoryRetrievalEngine: MemoryRetrievalEngine
  memoryRetrievalMaxWorkspaceResults: string
  memoryRetrievalMaxUserResults: string
  memoryRetrievalMaxTotalChars: string
  memoryRetrievalPinnedCharBudget: string
  memoryRetrievalMinimumScore: string
  memoryRetrievalRecencyWeight: string
  memoryRetrievalRecencyHalfLifeDays: string
  memoryRetrievalFuzzyWeight: string
  memoryRetrievalRecentFallbackCount: string
  memoryArchivedRetentionDays: string
  memoryStaleThresholdDays: string
  memoryAutoArchiveExpired: boolean
}

export type MCPServerType = "http" | "sse" | "stdio"

export type TurnProfileMode = "default" | "off" | "custom"

export type MemoryNotificationMode = "off" | "on" | "verbose"

export type MemoryRecallMode = "isolated" | "user_recall" | "group_recall"
export type MemoryApprovalMode = "off" | "background_only" | "all_writes"
export type MemoryRetrievalEngine = "hybrid_lexical"
export type EvolutionMode = "observe" | "draft" | "apply"
export type EvolutionColdPathTrigger = "after_turn" | "scheduled" | "manual"
export type EvolutionApplyPolicy = "approval_required" | "automatic"

export interface MemoryReviewModelOption {
  provider: string
  value: string
  label: string
}

export const MEMORY_NOTIFICATION_OPTIONS: readonly MemoryNotificationMode[] = [
  "off",
  "on",
  "verbose",
]

export const MEMORY_RECALL_OPTIONS: readonly MemoryRecallMode[] = [
  "isolated",
  "user_recall",
  "group_recall",
]

export const MEMORY_APPROVAL_OPTIONS: readonly MemoryApprovalMode[] = [
  "off",
  "background_only",
  "all_writes",
]

export const EVOLUTION_APPLY_POLICY_OPTIONS: readonly EvolutionApplyPolicy[] = [
  "approval_required",
  "automatic",
]

export const EVOLUTION_MODE_OPTIONS: readonly EvolutionMode[] = [
  "observe",
  "draft",
  "apply",
]

export const EVOLUTION_COLD_PATH_TRIGGER_OPTIONS: readonly EvolutionColdPathTrigger[] = [
  "after_turn",
  "scheduled",
  "manual",
]

export interface TurnProfileForm {
  enabled: boolean
  historyMode: Exclude<TurnProfileMode, "custom">
  systemPromptMode: Exclude<TurnProfileMode, "custom">
  skillsMode: TurnProfileMode
  skillsAllowText: string
  toolsMode: TurnProfileMode
  toolsAllowText: string
}

export interface MCPServerForm {
  id: string
  name: string
  enabled: boolean
  deferredOverride: boolean | null
  type: MCPServerType
  url: string
  command: string
  argsText: string
  envText: string
  envFile: string
  headersText: string
}

export interface LauncherForm {
  port: string
  publicAccess: boolean
  allowedCIDRsText: string
  allowLocalhostBypass: boolean
  trustedProxyCIDRsText: string
  dashboardPassword: string
  dashboardPasswordConfirm: string
}

export const DM_SCOPE_OPTIONS = [
  {
    value: "per-channel-peer",
    labelKey: "pages.config.session_scope_per_channel_peer",
    labelDefault: "Per Channel + Peer",
    descKey: "pages.config.session_scope_per_channel_peer_desc",
    descDefault: "Separate context for each user in each channel.",
  },
  {
    value: "per-channel",
    labelKey: "pages.config.session_scope_per_channel",
    labelDefault: "Per Channel",
    descKey: "pages.config.session_scope_per_channel_desc",
    descDefault: "One shared context per channel.",
  },
  {
    value: "per-peer",
    labelKey: "pages.config.session_scope_per_peer",
    labelDefault: "Per Peer",
    descKey: "pages.config.session_scope_per_peer_desc",
    descDefault: "One context per user across channels.",
  },
  {
    value: "global",
    labelKey: "pages.config.session_scope_global",
    labelDefault: "Global",
    descKey: "pages.config.session_scope_global_desc",
    descDefault: "All messages share one global context.",
  },
] as const

export const EMPTY_FORM: CoreConfigForm = {
  workspace: "",
  restrictToWorkspace: true,
  splitOnMarker: false,
  toolFeedbackEnabled: false,
  toolFeedbackMaxArgsLength: "300",
  toolFeedbackSeparateMessages: false,
  execEnabled: true,
  allowRemote: true,
  enableDenyPatterns: true,
  customDenyPatternsText: "",
  customAllowPatternsText: "",
  execTimeoutSeconds: "0",
  allowCommand: true,
  cronExecTimeoutMinutes: "5",
  maxTokens: "32768",
  contextWindow: "",
  maxToolIterations: "50",
  summarizeMessageThreshold: "20",
  summarizeTokenPercent: "75",
  turnProfile: {
    enabled: false,
    historyMode: "default",
    systemPromptMode: "default",
    skillsMode: "default",
    skillsAllowText: "",
    toolsMode: "default",
    toolsAllowText: "",
  },
  dmScope: "per-channel-peer",
  heartbeatEnabled: true,
  heartbeatInterval: "30",
  devicesEnabled: false,
  monitorUSB: true,
  mcpEnabled: false,
  mcpDiscoveryEnabled: false,
  mcpDiscoveryTTL: "5",
  mcpDiscoveryMaxSearchResults: "5",
  mcpDiscoveryUseBM25: true,
  mcpDiscoveryUseRegex: false,
  mcpServers: [],
  evolutionEnabled: false,
  evolutionMode: "observe",
  evolutionStateDir: "",
  evolutionMinTaskCount: "2",
  evolutionMinSuccessRatio: "0.7",
  evolutionColdPathTrigger: "after_turn",
  evolutionColdPathTimesText: "",
  evolutionApplyPolicy: "approval_required",
  evolutionPrivateDataScrubbing: true,
  evolutionDraftTimeoutSeconds: "45",
  evolutionMaxEvidenceRecords: "50",
  evolutionMaxDraftChars: "12000",
  evolutionRollbackRetention: "10",
  memoryEnabled: true,
  memoryBackgroundReviewEnabled: true,
  memoryReviewInterval: "10",
  memoryReviewProvider: "",
  memoryReviewModel: "",
  memoryReviewTimeoutSeconds: "30",
  memoryReviewMaxIterations: "2",
  memoryWriteApproval: false,
  memoryApprovalMode: "off",
  memoryNotifications: "off",
  memoryWorkspaceCharLimit: "12000",
  memoryPerUserCharLimit: "8000",
  memoryRecallMode: "isolated",
  memoryRecallMaxResults: "5",
  memoryRecallMaxChars: "4000",
  memoryCheckpointsEnabled: false,
  memoryCheckpointMaxCount: "100",
  memoryCheckpointMaxContextChars: "2000",
  memoryCheckpointCompletedRetentionDays: "90",
  memoryRetrievalEnabled: true,
  memoryRetrievalEngine: "hybrid_lexical",
  memoryRetrievalMaxWorkspaceResults: "6",
  memoryRetrievalMaxUserResults: "6",
  memoryRetrievalMaxTotalChars: "4000",
  memoryRetrievalPinnedCharBudget: "1200",
  memoryRetrievalMinimumScore: "0.35",
  memoryRetrievalRecencyWeight: "0.25",
  memoryRetrievalRecencyHalfLifeDays: "90",
  memoryRetrievalFuzzyWeight: "0.75",
  memoryRetrievalRecentFallbackCount: "2",
  memoryArchivedRetentionDays: "365",
  memoryStaleThresholdDays: "180",
  memoryAutoArchiveExpired: false,
}

export const EMPTY_LAUNCHER_FORM: LauncherForm = {
  port: "18800",
  publicAccess: false,
  allowedCIDRsText: "",
  allowLocalhostBypass: true,
  trustedProxyCIDRsText: "",
  dashboardPassword: "",
  dashboardPasswordConfirm: "",
}

function asRecord(value: unknown): JsonRecord {
  if (value && typeof value === "object" && !Array.isArray(value)) {
    return value as JsonRecord
  }
  return {}
}

function asString(value: unknown): string {
  return typeof value === "string" ? value : ""
}

function asBool(value: unknown): boolean {
  return value === true
}

function asOptionalBool(value: unknown): boolean | null {
  return typeof value === "boolean" ? value : null
}

function asNumberString(value: unknown, fallback: string): string {
  if (typeof value === "number" && Number.isFinite(value)) {
    return String(value)
  }
  if (typeof value === "string" && value.trim() !== "") {
    return value
  }
  return fallback
}

function toMCPServerType(value: unknown): MCPServerType {
  if (value === "http" || value === "sse") {
    return value
  }
  return "stdio"
}

function makeMCPServerID(name: string): string {
  const encoded = encodeURIComponent(name)
  if (encoded.length > 0) {
    return `mcp-${encoded}`
  }
  return `mcp-${Math.random().toString(36).slice(2, 10)}`
}

function mapMCPServers(value: unknown): MCPServerForm[] {
  const servers = asRecord(value)
  return Object.entries(servers).map(([name, rawConfig]) => {
    const cfg = asRecord(rawConfig)
    const argsList = Array.isArray(cfg.args)
      ? cfg.args.filter((item): item is string => typeof item === "string")
      : []
    const url = asString(cfg.url)
    const type =
      cfg.type === undefined
        ? url
          ? "sse"
          : "stdio"
        : toMCPServerType(cfg.type)
    const env = asRecord(cfg.env)
    const headers = asRecord(cfg.headers)

    return {
      id: makeMCPServerID(name),
      name,
      enabled: cfg.enabled !== false,
      deferredOverride: asOptionalBool(cfg.deferred),
      type,
      url,
      command: asString(cfg.command),
      argsText: argsList.join("\n"),
      envText: JSON.stringify(env, null, 2),
      envFile: asString(cfg.env_file),
      headersText: JSON.stringify(headers, null, 2),
    }
  })
}

function toTurnProfileMode(value: unknown): TurnProfileMode {
  if (value === "off" || value === "custom") {
    return value
  }
  return "default"
}

function toBasicTurnProfileMode(
  value: unknown,
): Exclude<TurnProfileMode, "custom"> {
  return value === "off" ? "off" : "default"
}

function toMemoryNotificationMode(value: unknown): MemoryNotificationMode {
  if (value === "on" || value === "verbose") {
    return value
  }
  return "off"
}

function toMemoryRecallMode(value: unknown): MemoryRecallMode {
  if (value === "user_recall" || value === "group_recall") {
    return value
  }
  return "isolated"
}

function toMemoryApprovalMode(
  value: unknown,
  legacyApproval: unknown,
): MemoryApprovalMode {
  if (value === "background_only" || value === "all_writes") {
    return value
  }
  if (value === "off") {
    return "off"
  }
  return legacyApproval === true ? "background_only" : "off"
}

function toEvolutionApplyPolicy(value: unknown): EvolutionApplyPolicy {
  return value === "automatic" ? "automatic" : "approval_required"
}

function toEvolutionMode(value: unknown): EvolutionMode {
  return value === "draft" || value === "apply" ? value : "observe"
}

function toEvolutionColdPathTrigger(value: unknown): EvolutionColdPathTrigger {
  if (value === "scheduled") {
    return "scheduled"
  }
  if (value === "manual" || value === "none" || value === "off") {
    return "manual"
  }
  return "after_turn"
}

function allowListText(value: unknown): string {
  if (!Array.isArray(value)) {
    return ""
  }
  return value
    .filter((item): item is string => typeof item === "string")
    .join("\n")
}

function mapTurnProfile(value: unknown): TurnProfileForm {
  const profile = asRecord(value)
  const history = asRecord(profile.history)
  const systemPrompt = asRecord(profile.system_prompt)
  const skills = asRecord(profile.skills)
  const tools = asRecord(profile.tools)

  return {
    enabled: asBool(profile.enabled),
    historyMode: toBasicTurnProfileMode(history.mode),
    systemPromptMode: toBasicTurnProfileMode(systemPrompt.mode),
    skillsMode: toTurnProfileMode(skills.mode),
    skillsAllowText: allowListText(skills.allow),
    toolsMode: toTurnProfileMode(tools.mode),
    toolsAllowText: allowListText(tools.allow),
  }
}

export function buildFormFromConfig(config: unknown): CoreConfigForm {
  const root = asRecord(config)
  const agents = asRecord(root.agents)
  const defaults = asRecord(agents.defaults)
  const session = asRecord(root.session)
  const heartbeat = asRecord(root.heartbeat)
  const devices = asRecord(root.devices)
  const evolution = asRecord(root.evolution)
  const memory = asRecord(root.memory)
  const memoryBackgroundReview = asRecord(memory.background_review)
  const memoryRecall = asRecord(memory.recall)
  const memoryCheckpoints = asRecord(memory.checkpoints)
  const memoryRetrieval = asRecord(memory.retrieval)
  const memoryLifecycle = asRecord(memory.lifecycle)
  const tools = asRecord(root.tools)
  const mcp = asRecord(tools.mcp)
  const mcpDiscovery = asRecord(mcp.discovery)
  const cron = asRecord(tools.cron)
  const exec = asRecord(tools.exec)
  const toolFeedback = asRecord(defaults.tool_feedback)

  return {
    workspace: asString(defaults.workspace) || EMPTY_FORM.workspace,
    restrictToWorkspace:
      defaults.restrict_to_workspace === undefined
        ? EMPTY_FORM.restrictToWorkspace
        : asBool(defaults.restrict_to_workspace),
    splitOnMarker:
      defaults.split_on_marker === undefined
        ? EMPTY_FORM.splitOnMarker
        : asBool(defaults.split_on_marker),
    toolFeedbackEnabled:
      toolFeedback.enabled === undefined
        ? EMPTY_FORM.toolFeedbackEnabled
        : asBool(toolFeedback.enabled),
    toolFeedbackMaxArgsLength: asNumberString(
      toolFeedback.max_args_length,
      EMPTY_FORM.toolFeedbackMaxArgsLength,
    ),
    toolFeedbackSeparateMessages:
      toolFeedback.separate_messages === undefined
        ? EMPTY_FORM.toolFeedbackSeparateMessages
        : asBool(toolFeedback.separate_messages),
    execEnabled:
      exec.enabled === undefined
        ? EMPTY_FORM.execEnabled
        : asBool(exec.enabled),
    allowRemote:
      exec.allow_remote === undefined
        ? EMPTY_FORM.allowRemote
        : asBool(exec.allow_remote),
    enableDenyPatterns:
      exec.enable_deny_patterns === undefined
        ? EMPTY_FORM.enableDenyPatterns
        : asBool(exec.enable_deny_patterns),
    customDenyPatternsText: Array.isArray(exec.custom_deny_patterns)
      ? exec.custom_deny_patterns
          .filter((value): value is string => typeof value === "string")
          .join("\n")
      : EMPTY_FORM.customDenyPatternsText,
    customAllowPatternsText: Array.isArray(exec.custom_allow_patterns)
      ? exec.custom_allow_patterns
          .filter((value): value is string => typeof value === "string")
          .join("\n")
      : EMPTY_FORM.customAllowPatternsText,
    execTimeoutSeconds: asNumberString(
      exec.timeout_seconds,
      EMPTY_FORM.execTimeoutSeconds,
    ),
    allowCommand:
      cron.allow_command === undefined
        ? EMPTY_FORM.allowCommand
        : asBool(cron.allow_command),
    cronExecTimeoutMinutes: asNumberString(
      cron.exec_timeout_minutes,
      EMPTY_FORM.cronExecTimeoutMinutes,
    ),
    maxTokens: asNumberString(defaults.max_tokens, EMPTY_FORM.maxTokens),
    contextWindow: asNumberString(
      defaults.context_window,
      EMPTY_FORM.contextWindow,
    ),
    maxToolIterations: asNumberString(
      defaults.max_tool_iterations,
      EMPTY_FORM.maxToolIterations,
    ),
    summarizeMessageThreshold: asNumberString(
      defaults.summarize_message_threshold,
      EMPTY_FORM.summarizeMessageThreshold,
    ),
    summarizeTokenPercent: asNumberString(
      defaults.summarize_token_percent,
      EMPTY_FORM.summarizeTokenPercent,
    ),
    turnProfile: mapTurnProfile(defaults.turn_profile),
    dmScope: asString(session.dm_scope) || EMPTY_FORM.dmScope,
    heartbeatEnabled:
      heartbeat.enabled === undefined
        ? EMPTY_FORM.heartbeatEnabled
        : asBool(heartbeat.enabled),
    heartbeatInterval: asNumberString(
      heartbeat.interval,
      EMPTY_FORM.heartbeatInterval,
    ),
    devicesEnabled:
      devices.enabled === undefined
        ? EMPTY_FORM.devicesEnabled
        : asBool(devices.enabled),
    monitorUSB:
      devices.monitor_usb === undefined
        ? EMPTY_FORM.monitorUSB
        : asBool(devices.monitor_usb),
    mcpEnabled:
      mcp.enabled === undefined ? EMPTY_FORM.mcpEnabled : asBool(mcp.enabled),
    mcpDiscoveryEnabled:
      mcpDiscovery.enabled === undefined
        ? EMPTY_FORM.mcpDiscoveryEnabled
        : asBool(mcpDiscovery.enabled),
    mcpDiscoveryTTL: asNumberString(
      mcpDiscovery.ttl,
      EMPTY_FORM.mcpDiscoveryTTL,
    ),
    mcpDiscoveryMaxSearchResults: asNumberString(
      mcpDiscovery.max_search_results,
      EMPTY_FORM.mcpDiscoveryMaxSearchResults,
    ),
    mcpDiscoveryUseBM25:
      mcpDiscovery.use_bm25 === undefined
        ? EMPTY_FORM.mcpDiscoveryUseBM25
        : asBool(mcpDiscovery.use_bm25),
    mcpDiscoveryUseRegex:
      mcpDiscovery.use_regex === undefined
        ? EMPTY_FORM.mcpDiscoveryUseRegex
        : asBool(mcpDiscovery.use_regex),
    mcpServers: mapMCPServers(mcp.servers),
    evolutionEnabled:
      evolution.enabled === undefined
        ? EMPTY_FORM.evolutionEnabled
        : asBool(evolution.enabled),
    evolutionMode: toEvolutionMode(evolution.mode),
    evolutionStateDir:
      asString(evolution.state_dir) || EMPTY_FORM.evolutionStateDir,
    evolutionMinTaskCount: asNumberString(
      evolution.min_task_count,
      EMPTY_FORM.evolutionMinTaskCount,
    ),
    evolutionMinSuccessRatio: asNumberString(
      evolution.min_success_ratio,
      EMPTY_FORM.evolutionMinSuccessRatio,
    ),
    evolutionColdPathTrigger: toEvolutionColdPathTrigger(
      evolution.cold_path_trigger,
    ),
    evolutionColdPathTimesText: Array.isArray(evolution.cold_path_times)
      ? evolution.cold_path_times
          .filter((value): value is string => typeof value === "string")
          .join("\n")
      : EMPTY_FORM.evolutionColdPathTimesText,
    evolutionApplyPolicy: toEvolutionApplyPolicy(evolution.apply_policy),
    evolutionPrivateDataScrubbing:
      evolution.private_data_scrubbing === undefined
        ? EMPTY_FORM.evolutionPrivateDataScrubbing
        : asBool(evolution.private_data_scrubbing),
    evolutionDraftTimeoutSeconds: asNumberString(
      evolution.draft_timeout_seconds,
      EMPTY_FORM.evolutionDraftTimeoutSeconds,
    ),
    evolutionMaxEvidenceRecords: asNumberString(
      evolution.max_evidence_records,
      EMPTY_FORM.evolutionMaxEvidenceRecords,
    ),
    evolutionMaxDraftChars: asNumberString(
      evolution.max_draft_chars,
      EMPTY_FORM.evolutionMaxDraftChars,
    ),
    evolutionRollbackRetention: asNumberString(
      evolution.rollback_retention,
      EMPTY_FORM.evolutionRollbackRetention,
    ),
    memoryEnabled:
      memory.enabled === undefined
        ? EMPTY_FORM.memoryEnabled
        : asBool(memory.enabled),
    memoryBackgroundReviewEnabled:
      memoryBackgroundReview.enabled === undefined
        ? EMPTY_FORM.memoryBackgroundReviewEnabled
        : asBool(memoryBackgroundReview.enabled),
    memoryReviewInterval: asNumberString(
      memoryBackgroundReview.interval,
      EMPTY_FORM.memoryReviewInterval,
    ),
    memoryReviewProvider: asString(memoryBackgroundReview.provider),
    memoryReviewModel: asString(memoryBackgroundReview.model),
    memoryReviewTimeoutSeconds: asNumberString(
      memoryBackgroundReview.timeout_seconds,
      EMPTY_FORM.memoryReviewTimeoutSeconds,
    ),
    memoryReviewMaxIterations: asNumberString(
      memoryBackgroundReview.max_iterations,
      EMPTY_FORM.memoryReviewMaxIterations,
    ),
    memoryWriteApproval:
      toMemoryApprovalMode(memory.approval_mode, memory.write_approval) !==
      "off",
    memoryApprovalMode: toMemoryApprovalMode(
      memory.approval_mode,
      memory.write_approval,
    ),
    memoryNotifications: toMemoryNotificationMode(memory.notifications),
    memoryWorkspaceCharLimit: asNumberString(
      memory.workspace_char_limit,
      EMPTY_FORM.memoryWorkspaceCharLimit,
    ),
    memoryPerUserCharLimit: asNumberString(
      memory.per_user_char_limit,
      EMPTY_FORM.memoryPerUserCharLimit,
    ),
    memoryRecallMode: toMemoryRecallMode(memoryRecall.mode),
    memoryRecallMaxResults: asNumberString(
      memoryRecall.max_results,
      EMPTY_FORM.memoryRecallMaxResults,
    ),
    memoryRecallMaxChars: asNumberString(
      memoryRecall.max_chars,
      EMPTY_FORM.memoryRecallMaxChars,
    ),
    memoryCheckpointsEnabled:
      memoryCheckpoints.enabled === undefined
        ? EMPTY_FORM.memoryCheckpointsEnabled
        : asBool(memoryCheckpoints.enabled),
    memoryCheckpointMaxCount: asNumberString(
      memoryCheckpoints.max_count,
      EMPTY_FORM.memoryCheckpointMaxCount,
    ),
    memoryCheckpointMaxContextChars: asNumberString(
      memoryCheckpoints.max_context_chars,
      EMPTY_FORM.memoryCheckpointMaxContextChars,
    ),
    memoryCheckpointCompletedRetentionDays: asNumberString(
      memoryCheckpoints.completed_retention_days,
      EMPTY_FORM.memoryCheckpointCompletedRetentionDays,
    ),
    memoryRetrievalEnabled:
      memoryRetrieval.enabled === undefined
        ? EMPTY_FORM.memoryRetrievalEnabled
        : asBool(memoryRetrieval.enabled),
    memoryRetrievalEngine: "hybrid_lexical",
    memoryRetrievalMaxWorkspaceResults: asNumberString(
      memoryRetrieval.max_workspace_results,
      EMPTY_FORM.memoryRetrievalMaxWorkspaceResults,
    ),
    memoryRetrievalMaxUserResults: asNumberString(
      memoryRetrieval.max_user_results,
      EMPTY_FORM.memoryRetrievalMaxUserResults,
    ),
    memoryRetrievalMaxTotalChars: asNumberString(
      memoryRetrieval.max_total_chars,
      EMPTY_FORM.memoryRetrievalMaxTotalChars,
    ),
    memoryRetrievalPinnedCharBudget: asNumberString(
      memoryRetrieval.pinned_char_budget,
      EMPTY_FORM.memoryRetrievalPinnedCharBudget,
    ),
    memoryRetrievalMinimumScore: asNumberString(
      memoryRetrieval.minimum_relevance_score,
      EMPTY_FORM.memoryRetrievalMinimumScore,
    ),
    memoryRetrievalRecencyWeight: asNumberString(
      memoryRetrieval.recency_weight,
      EMPTY_FORM.memoryRetrievalRecencyWeight,
    ),
    memoryRetrievalRecencyHalfLifeDays: asNumberString(
      memoryRetrieval.recency_half_life_days,
      EMPTY_FORM.memoryRetrievalRecencyHalfLifeDays,
    ),
    memoryRetrievalFuzzyWeight: asNumberString(
      memoryRetrieval.fuzzy_weight,
      EMPTY_FORM.memoryRetrievalFuzzyWeight,
    ),
    memoryRetrievalRecentFallbackCount: asNumberString(
      memoryRetrieval.recent_fallback_count,
      EMPTY_FORM.memoryRetrievalRecentFallbackCount,
    ),
    memoryArchivedRetentionDays: asNumberString(
      memoryLifecycle.archived_retention_days,
      EMPTY_FORM.memoryArchivedRetentionDays,
    ),
    memoryStaleThresholdDays: asNumberString(
      memoryLifecycle.stale_threshold_days,
      EMPTY_FORM.memoryStaleThresholdDays,
    ),
    memoryAutoArchiveExpired:
      memoryLifecycle.auto_archive_expired === undefined
        ? EMPTY_FORM.memoryAutoArchiveExpired
        : asBool(memoryLifecycle.auto_archive_expired),
  }
}

export function buildMemoryReviewOptions(config: unknown): {
  providers: string[]
  models: MemoryReviewModelOption[]
} {
  const root = asRecord(config)
  const rawModels = Array.isArray(root.model_list) ? root.model_list : []
  const providers = new Set<string>()
  const models = new Map<string, MemoryReviewModelOption>()

  for (const rawModel of rawModels) {
    const model = asRecord(rawModel)
    const modelName = asString(model.model_name).trim()
    const modelID = asString(model.model).trim()
    let provider = asString(model.provider).trim().toLowerCase()
    if (!provider && modelID.includes("/")) {
      provider = modelID.slice(0, modelID.indexOf("/")).toLowerCase()
    }
    if (provider) {
      providers.add(provider)
    }
    const value = modelName || modelID
    if (!value || models.has(`${provider}\u0000${value}`)) {
      continue
    }
    const detail = modelID && modelID !== value ? ` (${modelID})` : ""
    models.set(`${provider}\u0000${value}`, {
      provider,
      value,
      label: `${value}${detail}`,
    })
  }

  return {
    providers: Array.from(providers).sort((a, b) => a.localeCompare(b)),
    models: Array.from(models.values()).sort((a, b) =>
      a.label.localeCompare(b.label),
    ),
  }
}

export function buildMemoryConfigPatch(
  form: CoreConfigForm,
): Record<string, unknown> {
  if (!MEMORY_NOTIFICATION_OPTIONS.includes(form.memoryNotifications)) {
    throw new Error("Notification mode is invalid.")
  }
  if (!MEMORY_RECALL_OPTIONS.includes(form.memoryRecallMode)) {
    throw new Error("Cross-topic recall mode is invalid.")
  }
  if (!MEMORY_APPROVAL_OPTIONS.includes(form.memoryApprovalMode)) {
    throw new Error("Memory approval mode is invalid.")
  }

  const provider = form.memoryReviewProvider.trim()
  const model = form.memoryReviewModel.trim()
  return {
    enabled: form.memoryEnabled,
    workspace_char_limit: parseIntField(
      form.memoryWorkspaceCharLimit,
      "Workspace memory character limit",
      { min: 1 },
    ),
    per_user_char_limit: parseIntField(
      form.memoryPerUserCharLimit,
      "Per-user memory character limit",
      { min: 1 },
    ),
    write_approval: form.memoryApprovalMode !== "off",
    approval_mode: form.memoryApprovalMode,
    notifications: form.memoryNotifications,
    background_review: {
      enabled: form.memoryBackgroundReviewEnabled,
      interval: parseIntField(form.memoryReviewInterval, "Review interval", {
        min: 1,
      }),
      provider: provider || null,
      model: model || null,
      timeout_seconds: parseIntField(
        form.memoryReviewTimeoutSeconds,
        "Review timeout",
        { min: 1 },
      ),
      max_iterations: parseIntField(
        form.memoryReviewMaxIterations,
        "Maximum review iterations",
        { min: 1, max: 4 },
      ),
    },
    retrieval: {
      enabled: form.memoryRetrievalEnabled,
      engine: form.memoryRetrievalEngine,
      max_workspace_results: parseIntField(
        form.memoryRetrievalMaxWorkspaceResults,
        "Maximum workspace memory results",
        { min: 1, max: 50 },
      ),
      max_user_results: parseIntField(
        form.memoryRetrievalMaxUserResults,
        "Maximum user memory results",
        { min: 1, max: 50 },
      ),
      max_total_chars: parseIntField(
        form.memoryRetrievalMaxTotalChars,
        "Maximum retrieved memory characters",
        { min: 1, max: 20000 },
      ),
      pinned_char_budget: parseIntField(
        form.memoryRetrievalPinnedCharBudget,
        "Pinned memory character budget",
        { min: 1, max: 10000 },
      ),
      minimum_relevance_score: parseFloatField(
        form.memoryRetrievalMinimumScore,
        "Minimum memory relevance score",
        { min: 0, max: 10 },
      ),
      recency_weight: parseFloatField(
        form.memoryRetrievalRecencyWeight,
        "Memory recency weight",
        { min: 0, max: 5 },
      ),
      recency_half_life_days: parseIntField(
        form.memoryRetrievalRecencyHalfLifeDays,
        "Memory recency half-life",
        { min: 1, max: 3650 },
      ),
      fuzzy_weight: parseFloatField(
        form.memoryRetrievalFuzzyWeight,
        "Memory fuzzy weight",
        { min: 0, max: 5 },
      ),
      recent_fallback_count: parseIntField(
        form.memoryRetrievalRecentFallbackCount,
        "Recent memory fallback count",
        { min: 0, max: 50 },
      ),
    },
    lifecycle: {
      archived_retention_days: parseIntField(
        form.memoryArchivedRetentionDays,
        "Archived memory retention",
        { min: 1, max: 3650 },
      ),
      stale_threshold_days: parseIntField(
        form.memoryStaleThresholdDays,
        "Stale memory threshold",
        { min: 1, max: 3650 },
      ),
      auto_archive_expired: form.memoryAutoArchiveExpired,
    },
    recall: {
      mode: form.memoryRecallMode,
      max_results: parseIntField(
        form.memoryRecallMaxResults,
        "Maximum recall results",
        { min: 1, max: 20 },
      ),
      max_chars: parseIntField(
        form.memoryRecallMaxChars,
        "Maximum recall characters",
        { min: 1, max: 20000 },
      ),
    },
    checkpoints: {
      enabled: form.memoryCheckpointsEnabled,
      max_count: parseIntField(
        form.memoryCheckpointMaxCount,
        "Maximum checkpoint count",
        { min: 1, max: 1000 },
      ),
      max_context_chars: parseIntField(
        form.memoryCheckpointMaxContextChars,
        "Checkpoint context limit",
        { min: 1, max: 20000 },
      ),
      completed_retention_days: parseIntField(
        form.memoryCheckpointCompletedRetentionDays,
        "Completed checkpoint retention days",
        { min: 1 },
      ),
    },
  }
}

export function buildEvolutionConfigPatch(
  form: CoreConfigForm,
): Record<string, unknown> {
  if (!EVOLUTION_MODE_OPTIONS.includes(form.evolutionMode)) {
    throw new Error("Evolution mode is invalid.")
  }
  if (
    !EVOLUTION_COLD_PATH_TRIGGER_OPTIONS.includes(
      form.evolutionColdPathTrigger,
    )
  ) {
    throw new Error("Evolution cold-path trigger is invalid.")
  }
  if (!EVOLUTION_APPLY_POLICY_OPTIONS.includes(form.evolutionApplyPolicy)) {
    throw new Error("Evolution apply policy is invalid.")
  }
  if (form.evolutionEnabled && !form.evolutionPrivateDataScrubbing) {
    throw new Error(
      "Private-data scrubbing must remain enabled while evolution is enabled.",
    )
  }

  const coldPathTimes = parseMultilineList(form.evolutionColdPathTimesText)
  if (form.evolutionColdPathTrigger === "scheduled") {
    if (coldPathTimes.length === 0) {
      throw new Error(
        "Scheduled evolution requires at least one cold-path time.",
      )
    }
    const invalid = coldPathTimes.find(
      (value) => !/^(?:[01]\d|2[0-3]):[0-5]\d$/.test(value),
    )
    if (invalid) {
      throw new Error(
        `Evolution cold-path time ${invalid} must use 24-hour HH:MM format.`,
      )
    }
  }

  return {
    enabled: form.evolutionEnabled,
    mode: form.evolutionMode,
    state_dir:
      form.evolutionStateDir.trim() === ""
        ? null
        : form.evolutionStateDir.trim(),
    min_task_count: parseIntField(
      form.evolutionMinTaskCount,
      "Evolution minimum task count",
      { min: 2, max: 500 },
    ),
    min_success_ratio: parseFloatField(
      form.evolutionMinSuccessRatio,
      "Evolution minimum success ratio",
      { min: 0.01, max: 1 },
    ),
    cold_path_trigger: form.evolutionColdPathTrigger,
    cold_path_times: coldPathTimes,
    apply_policy: form.evolutionApplyPolicy,
    private_data_scrubbing: form.evolutionPrivateDataScrubbing,
    draft_timeout_seconds: parseIntField(
      form.evolutionDraftTimeoutSeconds,
      "Evolution draft timeout",
      { min: 1, max: 300 },
    ),
    max_evidence_records: parseIntField(
      form.evolutionMaxEvidenceRecords,
      "Evolution maximum evidence records",
      { min: 2, max: 500 },
    ),
    max_draft_chars: parseIntField(
      form.evolutionMaxDraftChars,
      "Evolution maximum draft characters",
      { min: 1, max: 50000 },
    ),
    rollback_retention: parseIntField(
      form.evolutionRollbackRetention,
      "Evolution rollback retention",
      { min: 1, max: 100 },
    ),
  }
}

export function parseIntField(
  rawValue: string,
  label: string,
  options: { min?: number; max?: number } = {},
): number {
  const value = Number(rawValue)
  if (!Number.isInteger(value)) {
    throw new Error(`${label} must be an integer.`)
  }
  if (options.min !== undefined && value < options.min) {
    throw new Error(`${label} must be >= ${options.min}.`)
  }
  if (options.max !== undefined && value > options.max) {
    throw new Error(`${label} must be <= ${options.max}.`)
  }
  return value
}

export function parseFloatField(
  rawValue: string,
  label: string,
  options: { min?: number; max?: number } = {},
): number {
  const value = Number(rawValue)
  if (!Number.isFinite(value)) {
    throw new Error(`${label} must be a number.`)
  }
  if (options.min !== undefined && value < options.min) {
    throw new Error(`${label} must be >= ${options.min}.`)
  }
  if (options.max !== undefined && value > options.max) {
    throw new Error(`${label} must be <= ${options.max}.`)
  }
  return value
}

export function parseCIDRText(raw: string): string[] {
  if (!raw.trim()) {
    return []
  }
  return raw
    .split(/[\n,]/)
    .map((v) => v.trim())
    .filter((v) => v.length > 0)
}

export function parseMultilineList(raw: string): string[] {
  if (!raw.trim()) {
    return []
  }
  return raw
    .split("\n")
    .map((value) => value.trim())
    .filter((value) => value.length > 0)
}

export function parseJSONObjectField(
  rawValue: string,
  label: string,
): Record<string, string> {
  const trimmed = rawValue.trim()
  if (trimmed === "") {
    return {}
  }

  let parsed: unknown
  try {
    parsed = JSON.parse(trimmed)
  } catch {
    throw new Error(`${label} must be valid JSON.`)
  }

  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error(`${label} must be a JSON object.`)
  }

  const entries = Object.entries(parsed as Record<string, unknown>)
  const result: Record<string, string> = {}
  for (const [key, value] of entries) {
    if (typeof value !== "string") {
      throw new Error(`${label}.${key} must be a string.`)
    }
    result[key] = value
  }
  return result
}
