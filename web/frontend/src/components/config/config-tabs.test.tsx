import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { useState } from "react"
import { I18nextProvider } from "react-i18next"
import { beforeAll, describe, expect, it } from "vitest"

import { EMPTY_FORM, type CoreConfigForm } from "@/components/config/form-model"
import i18n from "@/i18n"

import { AdvancedConfigLayout } from "./advanced-config-layout"
import { RuntimeConcurrencySection } from "./config-sections"
import { ConfigTabs, type ConfigPageTab } from "./config-tabs"

function AdvancedTabsHarness() {
  const [activeTab, setActiveTab] = useState<ConfigPageTab>("settings")
  const [form, setForm] = useState<CoreConfigForm>(EMPTY_FORM)

  const updateField = <K extends keyof CoreConfigForm>(
    key: K,
    value: CoreConfigForm[K],
  ) => setForm((current) => ({ ...current, [key]: value }))

  return (
    <I18nextProvider i18n={i18n}>
      <ConfigTabs activeTab={activeTab} onChange={setActiveTab} />
      <div
        id={`config-panel-${activeTab}`}
        role="tabpanel"
        aria-labelledby={`config-tab-${activeTab}`}
      >
        {activeTab === "settings" ? (
          <p>Upstream settings content</p>
        ) : (
          <RuntimeConcurrencySection
            form={form}
            onFieldChange={updateField}
          />
        )}
      </div>
    </I18nextProvider>
  )
}

describe("Advanced configuration tabs", () => {
  beforeAll(async () => {
    await i18n.changeLanguage("en")
  })

  it("keeps the fork concurrency control exclusively in Advanced", async () => {
    const user = userEvent.setup()
    render(<AdvancedTabsHarness />)

    expect(screen.getByRole("tab", { name: "Settings" })).toHaveAttribute(
      "aria-selected",
      "true",
    )
    expect(
      screen.queryByRole("spinbutton", { name: "Maximum Parallel Turns" }),
    ).not.toBeInTheDocument()

    await user.click(screen.getByRole("tab", { name: "Advanced" }))

    expect(
      screen.getByRole("spinbutton", { name: "Maximum Parallel Turns" }),
    ).toHaveValue(1)
    expect(
      screen.getByRole("spinbutton", { name: "Maximum Parallel Turns" }),
    ).toHaveAttribute("min", "1")
    expect(
      screen.getByRole("spinbutton", { name: "Maximum Parallel Turns" }),
    ).toHaveAttribute("step", "1")
    expect(screen.getByText(/Restart the gateway after saving/)).toBeVisible()
    expect(
      screen.getByText(/messages within the same session or topic remain serialized/),
    ).toBeVisible()
    expect(
      screen.getByText(/separate from subagent or subturn concurrency/),
    ).toBeVisible()
  })

  it("preserves unsaved concurrency state while switching tabs", async () => {
    const user = userEvent.setup()
    render(<AdvancedTabsHarness />)

    await user.click(screen.getByRole("tab", { name: "Advanced" }))
    const input = screen.getByRole("spinbutton", {
      name: "Maximum Parallel Turns",
    })
    await user.clear(input)
    await user.type(input, "3")

    const warning = screen.getByText(
      /Parallel turns can increase RAM and CPU use/,
    )
    expect(warning).toBeVisible()
    expect(warning).toHaveTextContent("simultaneous API/model requests")
    expect(warning).toHaveTextContent("token consumption")
    expect(warning).toHaveTextContent("provider rate limits")

    await user.click(screen.getByRole("tab", { name: "Settings" }))
    expect(
      screen.queryByRole("spinbutton", { name: "Maximum Parallel Turns" }),
    ).not.toBeInTheDocument()
    await user.click(screen.getByRole("tab", { name: "Advanced" }))

    expect(
      screen.getByRole("spinbutton", { name: "Maximum Parallel Turns" }),
    ).toHaveValue(3)
  })

  it("shows the resource warning only above the sequential value", async () => {
    const user = userEvent.setup()
    render(<AdvancedTabsHarness />)

    await user.click(screen.getByRole("tab", { name: "Advanced" }))
    expect(
      screen.queryByText(/Parallel turns can increase RAM and CPU use/),
    ).not.toBeInTheDocument()

    const input = screen.getByRole("spinbutton", {
      name: "Maximum Parallel Turns",
    })
    await user.clear(input)
    await user.type(input, "2")
    expect(
      screen.getByText(/Parallel turns can increase RAM and CPU use/),
    ).toBeVisible()
  })

  it("supports keyboard navigation between Settings and Advanced", async () => {
    const user = userEvent.setup()
    render(<AdvancedTabsHarness />)

    const settings = screen.getByRole("tab", { name: "Settings" })
    settings.focus()
    await user.keyboard("{ArrowRight}")

    expect(screen.getByRole("tab", { name: "Advanced" })).toHaveFocus()
    expect(screen.getByRole("tab", { name: "Advanced" })).toHaveAttribute(
      "aria-selected",
      "true",
    )
  })

  it("groups every fork-added dashboard feature under Advanced", () => {
    const noChange = () => undefined

    render(
      <I18nextProvider i18n={i18n}>
        <AdvancedConfigLayout
          runtimeConcurrency={{ form: EMPTY_FORM, onFieldChange: noChange }}
          memoryRecall={{
            form: EMPTY_FORM,
            onFieldChange: noChange,
            reviewProviders: [],
            reviewModels: [],
          }}
          evolutionSafety={{ form: EMPTY_FORM, onFieldChange: noChange }}
          management={{
            workspaceMemory: <p>Curated workspace memory</p>,
            currentUserProfile: <p>Compiled current-user profile</p>,
            evolution: <p>Evolution review & rollback</p>,
          }}
        />
      </I18nextProvider>,
    )

    expect(screen.getByText("Runtime & Concurrency")).toBeVisible()
    expect(screen.getByText("Memory & Recall")).toBeVisible()
    expect(screen.getByText("Curated workspace memory")).toBeVisible()
    expect(screen.getByText("Compiled current-user profile")).toBeVisible()
    expect(screen.getByText("Evolution Safety")).toBeVisible()
    expect(screen.getByText("Evolution review & rollback")).toBeVisible()
  })
})
