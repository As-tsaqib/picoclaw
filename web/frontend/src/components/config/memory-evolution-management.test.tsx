import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { beforeEach, describe, expect, it, vi } from "vitest"

const { launcherFetchMock, toastErrorMock } = vi.hoisted(() => ({
  launcherFetchMock: vi.fn(),
  toastErrorMock: vi.fn(),
}))

vi.mock("@/api/http", () => ({ launcherFetch: launcherFetchMock }))
vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: toastErrorMock },
}))

import { CurrentUserProfileManagementSection } from "./memory-evolution-management"

const activeID = "mem_0000000000000001"
const inferredID = "mem_0000000000000002"
const archivedID = "mem_0000000000000003"
const deleteID = "mem_0000000000000004"

const profileResponse = {
  scope_label: "Pico dashboard user",
  scope_description:
    "Only the fixed authenticated Pico channel identity is shown. Telegram and other channel profiles remain isolated and must be managed from their trusted direct chat.",
  profile: {
    version: 1,
    communication: [
      {
        key: "communication.verbosity",
        value: "concise",
        evidence_kind: "explicit",
        confidence: 1,
        source_id: activeID,
      },
    ],
    source_ids: [activeID],
    characters: 180,
  },
  entries: [
    {
      id: activeID,
      content: "Prefers concise answers",
      type: "communication_preference",
      status: "active",
      confidence: 1,
      evidence_kind: "explicit",
      preference_key: "communication.verbosity",
      preference_value: "concise",
      provenance: {
        source: "authenticated_dashboard_correction",
        channel: "pico",
        account: "default",
      },
      created_at: "2026-08-12T00:00:00Z",
      updated_at: "2026-08-12T00:00:00Z",
    },
    {
      id: inferredID,
      content: "May prefer examples before theory",
      type: "communication_preference",
      status: "active",
      confidence: 0.55,
      evidence_kind: "inferred",
      provenance: { source: "background_review", channel: "pico" },
      created_at: "2026-08-12T00:00:00Z",
      updated_at: "2026-08-12T00:00:00Z",
    },
    {
      id: archivedID,
      content: "Archived interaction preference",
      type: "other",
      status: "archived",
      confidence: 0.7,
      evidence_kind: "observed",
      provenance: { source: "background_review", channel: "pico" },
      created_at: "2026-08-12T00:00:00Z",
      updated_at: "2026-08-12T00:00:00Z",
    },
    {
      id: deleteID,
      content: "Superseded preference history",
      type: "communication_preference",
      status: "superseded",
      confidence: 1,
      evidence_kind: "explicit",
      provenance: { source: "user_request", channel: "pico" },
      created_at: "2026-08-12T00:00:00Z",
      updated_at: "2026-08-12T00:00:00Z",
    },
  ],
  stats: {
    entries: 4,
    entry_capacity: 256,
    characters: 140,
    capacity: 8000,
    serialized_characters: 900,
    serialized_capacity: 1000000,
  },
}

function jsonResponse(value: unknown): Response {
  return new Response(JSON.stringify(value), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  })
}

function postedBodies(): Array<Record<string, unknown>> {
  return launcherFetchMock.mock.calls
    .filter(([, init]) => (init as RequestInit | undefined)?.method === "POST")
    .map(([, init]) =>
      JSON.parse(String((init as RequestInit).body)) as Record<string, unknown>,
    )
}

describe("CurrentUserProfileManagementSection", () => {
  beforeEach(() => {
    launcherFetchMock.mockReset()
    toastErrorMock.mockReset()
    launcherFetchMock.mockImplementation(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        expect(String(input)).toBe("/api/memory/current-user")
        if (init?.method === "POST") return jsonResponse({ applied: [] })
        return jsonResponse(profileResponse)
      },
    )
  })

  it("renders the bounded fixed-scope profile and filters audit history", async () => {
    const user = userEvent.setup()
    render(<CurrentUserProfileManagementSection />)

    expect(
      (await screen.findAllByText("communication.verbosity = concise"))[0],
    ).toBeVisible()
    expect(screen.getByText(/fixed authenticated Pico channel identity/)).toBeVisible()
    expect(screen.getByText(/Telegram and other channel profiles remain isolated/)).toBeVisible()
    expect(screen.getByText("Prefers concise answers")).toBeVisible()
    expect(screen.getByText("Archived interaction preference")).toBeVisible()

    await user.click(
      screen.getByRole("combobox", { name: "Memory status filter" }),
    )
    await user.click(await screen.findByRole("option", { name: "archived" }))

    expect(screen.getByText("Archived interaction preference")).toBeVisible()
    expect(screen.queryByText("Prefers concise answers")).not.toBeInTheDocument()
  })

  it("sends fixed-scope confirm, correction, archive, and delete mutations", async () => {
    const user = userEvent.setup()
    render(<CurrentUserProfileManagementSection />)
    await screen.findByText("Prefers concise answers")

    await user.click(screen.getByRole("button", { name: `Confirm ${inferredID}` }))
    await waitFor(() =>
      expect(postedBodies()).toContainEqual({ action: "confirm", id: inferredID }),
    )

    await user.click(screen.getByRole("button", { name: `Correct ${activeID}` }))
    const content = screen.getByRole("textbox", {
      name: `Edit ${activeID} content`,
    })
    await user.clear(content)
    await user.type(content, "Now prefers detailed answers")
    const value = screen.getByRole("textbox", {
      name: `Edit ${activeID} preference value`,
    })
    await user.clear(value)
    await user.type(value, "detailed")
    await user.click(
      screen.getByRole("button", { name: `Save correction for ${activeID}` }),
    )
    await waitFor(() =>
      expect(postedBodies()).toContainEqual({
        action: "add",
        content: "Now prefers detailed answers",
        type: "communication_preference",
        preference_key: "communication.verbosity",
        preference_value: "detailed",
        supersedes: activeID,
      }),
    )

    await user.click(screen.getByRole("button", { name: `Restore ${archivedID}` }))
    await waitFor(() =>
      expect(postedBodies()).toContainEqual({ action: "restore", id: archivedID }),
    )
    await user.click(screen.getByRole("button", { name: `Delete ${deleteID}` }))
    await waitFor(() =>
      expect(postedBodies()).toContainEqual({ action: "remove", id: deleteID }),
    )
    expect(toastErrorMock).not.toHaveBeenCalled()
  })
})
