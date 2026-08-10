import { describe, expect, it } from "vitest"

import type { ChannelConfig, SupportedChannel } from "@/api/channels"

import { buildSavePayload } from "./channel-save-payload"

const telegramChannel: SupportedChannel = {
  name: "telegram",
  config_key: "telegram",
}

function settingsFrom(payload: ChannelConfig): ChannelConfig {
  return payload.settings as ChannelConfig
}

describe("Telegram channel save payload", () => {
  it("keeps older configurations without an ephemeral block unchanged", () => {
    const payload = buildSavePayload(
      telegramChannel,
      { base_url: "https://api.telegram.org", proxy: "" },
      true,
    )

    expect(payload).toEqual({
      enabled: true,
      type: "telegram",
      settings: {
        base_url: "https://api.telegram.org",
        proxy: "",
      },
    })
    expect(settingsFrom(payload)).not.toHaveProperty("ephemeral")
  })

  it("writes a safe nested commands configuration", () => {
    const payload = buildSavePayload(
      telegramChannel,
      {
        allow_from: [" 123 ", "123", "456"],
        ephemeral: {
          mode: "commands",
          commands: ["/HELP", "help", " clear "],
          personal_session_isolation: false,
        },
      },
      true,
    )

    expect(payload.allow_from).toBe("123\n456")
    expect(settingsFrom(payload).ephemeral).toEqual({
      mode: "commands",
      commands: ["help", "clear"],
      personal_session_isolation: true,
    })
  })

  it("removes commands outside commands mode", () => {
    const payload = buildSavePayload(
      telegramChannel,
      {
        ephemeral: {
          mode: "all",
          commands: ["help"],
          personal_session_isolation: false,
        },
      },
      true,
    )

    expect(settingsFrom(payload).ephemeral).toEqual({
      mode: "all",
      personal_session_isolation: true,
    })
  })

  it("preserves ephemeral and unknown Telegram settings when another field changes", () => {
    const payload = buildSavePayload(
      telegramChannel,
      {
        base_url: "https://telegram.example.test",
        future_telegram_setting: { nested: true },
        ephemeral: {
          mode: "commands",
          commands: [],
          personal_session_isolation: true,
          future_ephemeral_setting: "keep-me",
        },
      },
      true,
    )

    expect(settingsFrom(payload)).toEqual({
      base_url: "https://telegram.example.test",
      future_telegram_setting: { nested: true },
      ephemeral: {
        mode: "commands",
        commands: [],
        personal_session_isolation: true,
        future_ephemeral_setting: "keep-me",
      },
    })
  })
})
