import { launcherFetch } from "@/api/http"

export interface SessionSuperadminConfig {
  telegram_user_id: string
  bot_account: string
  agent_id: string
  enabled: boolean
  include_legacy_unknown: boolean
}

interface SessionSuperadminResponse {
  status: string
  superadmin: Partial<SessionSuperadminConfig>
}

const EMPTY_SUPERADMIN: SessionSuperadminConfig = {
  telegram_user_id: "",
  bot_account: "",
  agent_id: "",
  enabled: false,
  include_legacy_unknown: false,
}

async function request(
  options?: RequestInit,
): Promise<SessionSuperadminResponse> {
  const response = await launcherFetch("/api/dashboard/superadmin", options)
  if (!response.ok) {
    let message = `API error: ${response.status} ${response.statusText}`
    try {
      const body = (await response.json()) as {
        error?: string
        errors?: string[]
      }
      if (Array.isArray(body.errors) && body.errors.length > 0) {
        message = body.errors.join("; ")
      } else if (typeof body.error === "string" && body.error.trim() !== "") {
        message = body.error
      }
    } catch {
      // Keep the HTTP status fallback when the body is not JSON.
    }
    throw new Error(message)
  }
  return response.json() as Promise<SessionSuperadminResponse>
}

function normalize(
  value?: Partial<SessionSuperadminConfig>,
): SessionSuperadminConfig {
  return {
    telegram_user_id: value?.telegram_user_id ?? "",
    bot_account: value?.bot_account ?? "",
    agent_id: value?.agent_id ?? "",
    enabled: value?.enabled === true,
    include_legacy_unknown: value?.include_legacy_unknown === true,
  }
}

export async function getSessionSuperadmin(): Promise<SessionSuperadminConfig> {
  const response = await request()
  return normalize(response.superadmin)
}

export async function putSessionSuperadmin(
  config: SessionSuperadminConfig,
): Promise<SessionSuperadminConfig> {
  const response = await request({
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(config),
  })
  return normalize(response.superadmin)
}

export async function deleteSessionSuperadmin(): Promise<SessionSuperadminConfig> {
  const response = await request({ method: "DELETE" })
  return normalize(response.superadmin)
}

export { EMPTY_SUPERADMIN }
