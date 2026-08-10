import type { ChannelConfig, SupportedChannel } from "@/api/channels"
import {
  normalizeAllowFromValues,
  serializeStringArrayForSubmit,
} from "@/components/channels/channel-array-utils"
import {
  SECRET_FIELD_MAP,
  isSecretField,
} from "@/components/channels/channel-config-fields"
import { serializeTelegramEphemeralForSubmit } from "@/components/channels/channel-forms/telegram-ephemeral-config"

function asRecord(value: unknown): Record<string, unknown> {
  if (value && typeof value === "object" && !Array.isArray(value)) {
    return value as Record<string, unknown>
  }
  return {}
}

function asString(value: unknown): string {
  return typeof value === "string" ? value : ""
}

function serializeGroupTriggerForSubmit(value: unknown): unknown {
  const groupTrigger = asRecord(value)
  if (Object.keys(groupTrigger).length === 0) {
    return value
  }
  return {
    ...groupTrigger,
    prefixes: serializeStringArrayForSubmit(groupTrigger.prefixes),
  }
}

const CHANNEL_COMMON_CONFIG_KEYS = new Set([
  "allow_from",
  "group_trigger",
  "placeholder",
  "reasoning_channel_id",
  "typing",
])

function serializeChannelSettingForSubmit(
  channel: SupportedChannel,
  key: string,
  value: unknown,
): unknown {
  if (channel.name === "telegram" && key === "ephemeral") {
    return serializeTelegramEphemeralForSubmit(value)
  }
  return serializeStringArrayForSubmit(value)
}

export function buildSavePayload(
  channel: SupportedChannel,
  editConfig: ChannelConfig,
  enabled: boolean,
): ChannelConfig {
  const payload: ChannelConfig = { enabled, type: channel.config_key }
  const settings: ChannelConfig = {}

  for (const [key, value] of Object.entries(editConfig)) {
    if (key.startsWith("_")) continue
    if (key === "enabled") continue
    if (CHANNEL_COMMON_CONFIG_KEYS.has(key)) {
      if (key === "allow_from") {
        payload[key] = serializeStringArrayForSubmit(
          normalizeAllowFromValues(value),
        )
      } else if (key === "group_trigger") {
        payload[key] = serializeGroupTriggerForSubmit(value)
      } else {
        payload[key] = value
      }
      continue
    }
    if (isSecretField(key)) continue

    settings[key] = serializeChannelSettingForSubmit(channel, key, value)
  }

  for (const [secretKey, editKey] of Object.entries(SECRET_FIELD_MAP)) {
    const incoming = asString(editConfig[editKey])
    if (incoming !== "") {
      settings[secretKey] = incoming
      continue
    }
    const existing = asString(editConfig[secretKey]).trim()
    if (existing !== "") {
      settings[secretKey] = existing
    }
  }

  if (channel.name === "whatsapp_native") {
    settings.use_native = true
  }
  if (channel.name === "whatsapp") {
    settings.use_native = false
  }

  if (Object.keys(settings).length > 0) {
    payload.settings = settings
  }

  return payload
}
