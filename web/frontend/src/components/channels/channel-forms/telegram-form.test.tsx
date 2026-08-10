import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { useState } from "react"
import { I18nextProvider } from "react-i18next"
import { beforeAll, describe, expect, it } from "vitest"

import type { ChannelConfig } from "@/api/channels"
import i18n from "@/i18n"

import { TelegramForm } from "./telegram-form"

interface FormHarnessResult {
  getConfig: () => ChannelConfig
  user: ReturnType<typeof userEvent.setup>
}

const CONFIG_STATE_TEST_ID = "telegram-config-state"

function TelegramFormHarness({
  initialConfig,
}: {
  initialConfig: ChannelConfig
}) {
  const [config, setConfig] = useState(initialConfig)
  return (
    <>
      <output data-testid={CONFIG_STATE_TEST_ID} hidden>
        {JSON.stringify(config)}
      </output>
      <I18nextProvider i18n={i18n}>
        <TelegramForm
          config={config}
          onChange={(key, value) =>
            setConfig((current) => ({ ...current, [key]: value }))
          }
          configuredSecrets={[]}
        />
      </I18nextProvider>
    </>
  )
}

function renderForm(initialConfig: ChannelConfig = {}): FormHarnessResult {
  render(<TelegramFormHarness initialConfig={initialConfig} />)
  return {
    getConfig: () =>
      JSON.parse(
        screen.getByTestId(CONFIG_STATE_TEST_ID).textContent ?? "{}",
      ) as ChannelConfig,
    user: userEvent.setup(),
  }
}

async function selectMode(
  user: ReturnType<typeof userEvent.setup>,
  option: "Off" | "Selected Commands" | "All Group Responses",
) {
  await user.click(screen.getByRole("combobox", { name: "Mode" }))
  await user.click(await screen.findByRole("option", { name: option }))
}

describe("TelegramForm ephemeral settings", () => {
  beforeAll(async () => {
    await i18n.changeLanguage("en")
  })

  it("renders missing and explicit mode values correctly", () => {
    const { unmount } = render(
      <I18nextProvider i18n={i18n}>
        <TelegramForm config={{}} onChange={() => {}} configuredSecrets={[]} />
      </I18nextProvider>,
    )
    expect(screen.getByRole("combobox", { name: "Mode" })).toHaveTextContent(
      "Off",
    )
    unmount()

    for (const [mode, label] of [
      ["off", "Off"],
      ["commands", "Selected Commands"],
      ["all", "All Group Responses"],
    ] as const) {
      const view = render(
        <I18nextProvider i18n={i18n}>
          <TelegramForm
            config={{ ephemeral: { mode } }}
            onChange={() => {}}
            configuredSecrets={[]}
          />
        </I18nextProvider>,
      )
      expect(screen.getByRole("combobox", { name: "Mode" })).toHaveTextContent(
        label,
      )
      view.unmount()
    }
  })

  it("shows command input only in commands mode and clears stale data", async () => {
    const { getConfig, user } = renderForm({
      ephemeral: {
        mode: "all",
        personal_session_isolation: true,
        future_setting: "keep",
      },
    })

    expect(
      screen.queryByRole("textbox", { name: "Ephemeral Commands" }),
    ).not.toBeInTheDocument()
    await selectMode(user, "Selected Commands")
    expect(
      screen.getByRole("textbox", { name: "Ephemeral Commands" }),
    ).toBeInTheDocument()

    await user.type(
      screen.getByRole("textbox", { name: "Ephemeral Commands" }),
      "help",
    )
    await user.keyboard("{Enter}")
    await selectMode(user, "All Group Responses")

    expect(
      screen.queryByRole("textbox", { name: "Ephemeral Commands" }),
    ).not.toBeInTheDocument()
    expect(getConfig().ephemeral).toEqual({
      mode: "all",
      personal_session_isolation: true,
      future_setting: "keep",
    })
  })

  it("normalizes commands, trims whitespace, and removes duplicates", async () => {
    const { getConfig, user } = renderForm({
      ephemeral: {
        mode: "commands",
        commands: [],
        personal_session_isolation: true,
      },
    })
    const input = screen.getByRole("textbox", { name: "Ephemeral Commands" })

    await user.type(input, " /HELP, help,  CLEAR ")
    await user.keyboard("{Enter}")

    expect(getConfig().ephemeral).toEqual({
      mode: "commands",
      commands: ["help", "clear"],
      personal_session_isolation: true,
    })
    expect(screen.getByRole("button", { name: "Remove help" })).toBeEnabled()
    await user.click(screen.getByRole("button", { name: "Remove clear" }))
    expect(getConfig().ephemeral).toEqual({
      mode: "commands",
      commands: ["help"],
      personal_session_isolation: true,
    })
  })

  it("rejects invalid command names with an inline error", async () => {
    const { getConfig, user } = renderForm({
      ephemeral: { mode: "commands", commands: [] },
    })
    const input = screen.getByRole("textbox", { name: "Ephemeral Commands" })

    await user.type(input, "bad-command")
    await user.keyboard("{Enter}")

    expect(input).toHaveAttribute("aria-invalid", "true")
    expect(screen.getByText(/Use 1–32 lowercase letters/)).toBeInTheDocument()
    expect(getConfig().ephemeral).toEqual({
      mode: "commands",
      commands: [],
    })
  })

  it("explains empty command behavior and locks safe isolation", async () => {
    const { getConfig, user } = renderForm({
      ephemeral: {
        mode: "off",
        personal_session_isolation: false,
      },
    })

    await selectMode(user, "Selected Commands")
    expect(
      screen.getByText(
        "Empty means every registered PicoClaw command will be ephemeral.",
      ),
    ).toBeInTheDocument()

    const isolation = screen.getByRole("switch", {
      name: "Personal Session Isolation",
    })
    expect(isolation).toBeChecked()
    expect(isolation).toBeDisabled()
    expect(getConfig().ephemeral).toEqual({
      mode: "commands",
      commands: [],
      personal_session_isolation: true,
    })
  })

  it("shows contextual warnings without disabling public streaming", async () => {
    const { user } = renderForm({
      streaming: { enabled: true },
      ephemeral: { mode: "commands", commands: ["help"] },
    })

    expect(
      screen.getByText("Only the selected slash commands become private."),
    ).toBeInTheDocument()
    expect(
      screen.getByText(
        "Ephemeral replies require Telegram Bot API 10.2 compatibility.",
      ),
    ).toBeInTheDocument()
    expect(
      screen.getByRole("switch", { name: "Streaming Output" }),
    ).toBeEnabled()

    await selectMode(user, "All Group Responses")
    expect(
      screen.getByText(
        "Ordinary group responses may require the bot to be an administrator.",
      ),
    ).toBeInTheDocument()
    expect(
      screen.queryByText("Only the selected slash commands become private."),
    ).not.toBeInTheDocument()
  })
})
