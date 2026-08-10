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
  evolutionMode: string
  evolutionStateDir: string
  evolutionMinTaskCount: string
  evolutionMinSuccessRatio: string
  evolutionColdPathTrigger: string
  evolutionColdPathTimesText: string
  memoryEnabled: boolean
  memoryBackgroundReviewEnabled: boolean
  memoryReviewInterval: string
  memoryReviewProvider: string
  memoryReviewModel: string
  memoryReviewTimeoutSeconds: string
  memoryReviewMaxIterations: string
  memoryWriteApproval: boolean
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
}

export type MCPServerType = "http" | "sse" | "stdio"

export type TurnProfileMode = "default" | "off" | "custom"

export type MemoryNotificationMode = "off" | "on" | "verbose"

export type MemoryRecallMode = "isolated" | "user_recall" | "group_recall"

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
  memoryEnabled: true,
  memoryBackgroundReviewEnabled: true,
  memoryReviewInterval: "10",
  memoryReviewProvider: "",
  memoryReviewModel: "",
  memoryReviewTimeoutSeconds: "30",
  memoryReviewMaxIterations: "2",
  memoryWriteApproval: false,
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
    evolutionMode: asString(evolution.mode) || EMPTY_FORM.evolutionMode,
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
    evolutionColdPathTrigger:
      asString(evolution.cold_path_trigger) ||
      EMPTY_FORM.evolutionColdPathTrigger,
    evolutionColdPathTimesText: Array.isArray(evolution.cold_path_times)
      ? evolution.cold_path_times
          .filter((value): value is string => typeof value === "string")
          .join("\n")
      : EMPTY_FORM.evolutionColdPathTimesText,
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
      memory.write_approval === undefined
        ? EMPTY_FORM.memoryWriteApproval
        : asBool(memory.write_approval),
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
    write_approval: form.memoryWriteApproval,
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
