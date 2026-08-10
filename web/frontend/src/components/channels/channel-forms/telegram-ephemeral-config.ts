export const TELEGRAM_COMMAND_PATTERN = /^[a-z0-9_]{1,32}$/

export type TelegramEphemeralMode = "off" | "commands" | "all"

const TELEGRAM_COMMAND_SEPARATORS = /[,\uFF0C\u3001;\uFF1B\n\r\t]+/

function asRecord(value: unknown): Record<string, unknown> {
  if (value && typeof value === "object" && !Array.isArray(value)) {
    return value as Record<string, unknown>
  }
  return {}
}

function isTelegramEphemeralMode(
  value: unknown,
): value is TelegramEphemeralMode {
  return value === "off" || value === "commands" || value === "all"
}

function splitTelegramCommandInput(raw: string): string[] {
  return raw
    .split(TELEGRAM_COMMAND_SEPARATORS)
    .map((item) => item.trim())
    .filter((item) => item !== "")
}

export function normalizeTelegramCommand(command: string): string {
  const normalized = command.trim().toLowerCase()
  return normalized.startsWith("/") ? normalized.slice(1) : normalized
}

export function normalizeTelegramCommands(value: unknown): string[] {
  if (!Array.isArray(value)) {
    return []
  }

  const commands: string[] = []
  const seen = new Set<string>()
  for (const item of value) {
    if (typeof item !== "string") {
      continue
    }
    const command = normalizeTelegramCommand(item)
    if (command === "" || seen.has(command)) {
      continue
    }
    seen.add(command)
    commands.push(command)
  }
  return commands
}

export function parseTelegramCommandInput(raw: string): string[] {
  return normalizeTelegramCommands(splitTelegramCommandInput(raw))
}

export function isTelegramCommandInputValid(raw: string): boolean {
  const commands = splitTelegramCommandInput(raw).map(normalizeTelegramCommand)
  return (
    commands.length > 0 &&
    commands.every((command) => TELEGRAM_COMMAND_PATTERN.test(command))
  )
}

export function getTelegramEphemeralMode(
  value: unknown,
): TelegramEphemeralMode {
  const mode = asRecord(value).mode
  return isTelegramEphemeralMode(mode) ? mode : "off"
}

export function getTelegramEphemeralCommands(value: unknown): string[] {
  return normalizeTelegramCommands(asRecord(value).commands)
}

export function isTelegramPersonalSessionIsolationEnabled(
  value: unknown,
): boolean {
  return asRecord(value).personal_session_isolation !== false
}

export function setTelegramEphemeralMode(
  value: unknown,
  mode: TelegramEphemeralMode,
): Record<string, unknown> {
  const current = asRecord(value)
  const next: Record<string, unknown> = { ...current, mode }

  if (mode === "commands") {
    next.commands = normalizeTelegramCommands(current.commands)
    next.personal_session_isolation = true
  } else {
    delete next.commands
    if (mode === "all") {
      next.personal_session_isolation = true
    }
  }

  return next
}

export function setTelegramEphemeralCommands(
  value: unknown,
  commands: string[],
): Record<string, unknown> {
  return {
    ...asRecord(value),
    mode: "commands",
    commands: normalizeTelegramCommands(commands),
    personal_session_isolation: true,
  }
}

export function setTelegramPersonalSessionIsolation(
  value: unknown,
  enabled: boolean,
): Record<string, unknown> {
  const current = asRecord(value)
  const mode = getTelegramEphemeralMode(current)
  return {
    ...current,
    mode,
    personal_session_isolation: mode === "off" ? enabled : true,
  }
}

export function serializeTelegramEphemeralForSubmit(value: unknown): unknown {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return value
  }

  const current = value as Record<string, unknown>
  if (current.mode !== undefined && !isTelegramEphemeralMode(current.mode)) {
    return value
  }

  const mode = getTelegramEphemeralMode(current)
  const next: Record<string, unknown> = { ...current, mode }
  if (mode === "commands") {
    next.commands = normalizeTelegramCommands(current.commands)
    next.personal_session_isolation = true
  } else {
    delete next.commands
    if (mode === "all") {
      next.personal_session_isolation = true
    }
  }
  return next
}
