import { describe, expect, it } from "vitest"

import {
  getTelegramEphemeralCommands,
  getTelegramEphemeralMode,
  isTelegramCommandInputValid,
  isTelegramPersonalSessionIsolationEnabled,
  normalizeTelegramCommand,
  parseTelegramCommandInput,
  serializeTelegramEphemeralForSubmit,
  setTelegramEphemeralCommands,
  setTelegramEphemeralMode,
  setTelegramPersonalSessionIsolation,
} from "./telegram-ephemeral-config"

describe("Telegram ephemeral configuration", () => {
  it("defaults missing configuration to off with safe isolation", () => {
    expect(getTelegramEphemeralMode(undefined)).toBe("off")
    expect(isTelegramPersonalSessionIsolationEnabled(undefined)).toBe(true)
    expect(getTelegramEphemeralCommands(undefined)).toEqual([])
  })

  it.each(["off", "commands", "all"] as const)("loads the %s mode", (mode) => {
    expect(getTelegramEphemeralMode({ mode })).toBe(mode)
  })

  it("normalizes command names and removes duplicates", () => {
    expect(normalizeTelegramCommand(" /HELP ")).toBe("help")
    expect(parseTelegramCommandInput(" /HELP, help; CLEAR\n/show ")).toEqual([
      "help",
      "clear",
      "show",
    ])
  })

  it("validates Telegram command names after normalization", () => {
    expect(isTelegramCommandInputValid(" /HELP, clear_2 ")).toBe(true)
    expect(isTelegramCommandInputValid("bad-command")).toBe(false)
    expect(isTelegramCommandInputValid("two words")).toBe(false)
    expect(isTelegramCommandInputValid("//help")).toBe(false)
    expect(isTelegramCommandInputValid("x".repeat(33))).toBe(false)
  })

  it("forces isolation and clears stale commands when modes change", () => {
    const current = {
      mode: "commands",
      commands: ["help"],
      personal_session_isolation: false,
      future_setting: "preserved",
    }

    expect(setTelegramEphemeralMode(current, "all")).toEqual({
      mode: "all",
      personal_session_isolation: true,
      future_setting: "preserved",
    })
    expect(setTelegramEphemeralMode(current, "off")).toEqual({
      mode: "off",
      personal_session_isolation: false,
      future_setting: "preserved",
    })
  })

  it("never permits enabled mode isolation to be disabled", () => {
    expect(
      setTelegramPersonalSessionIsolation(
        { mode: "commands", personal_session_isolation: true },
        false,
      ),
    ).toEqual({ mode: "commands", personal_session_isolation: true })
    expect(
      setTelegramPersonalSessionIsolation(
        { mode: "off", personal_session_isolation: true },
        false,
      ),
    ).toEqual({ mode: "off", personal_session_isolation: false })
  })

  it("keeps an empty command list meaningful and preserves unknown fields", () => {
    expect(
      setTelegramEphemeralCommands(
        { mode: "off", future_setting: { enabled: true } },
        [],
      ),
    ).toEqual({
      mode: "commands",
      commands: [],
      personal_session_isolation: true,
      future_setting: { enabled: true },
    })
  })

  it("sanitizes enabled payloads without flattening unknown settings", () => {
    expect(
      serializeTelegramEphemeralForSubmit({
        mode: "commands",
        commands: [" /HELP ", "help", "clear"],
        personal_session_isolation: false,
        future_setting: 7,
      }),
    ).toEqual({
      mode: "commands",
      commands: ["help", "clear"],
      personal_session_isolation: true,
      future_setting: 7,
    })
  })
})
