import {
  IconArchive,
  IconCheck,
  IconEdit,
  IconHistory,
  IconPin,
  IconPlayerPlay,
  IconRefresh,
  IconRestore,
  IconSearch,
  IconTrash,
  IconX,
} from "@tabler/icons-react"
import { useCallback, useEffect, useState } from "react"
import { toast } from "sonner"

import { launcherFetch } from "@/api/http"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"

type MemoryType =
  | "identity"
  | "communication_preference"
  | "workflow_preference"
  | "correction"
  | "environment"
  | "project_fact"
  | "relationship"
  | "episodic_fact"
  | "other"

interface MemoryEntry {
  id: string
  content: string
  type: MemoryType
  status: "active" | "superseded" | "archived"
  pinned?: boolean
  confidence?: number
  evidence_kind?: "explicit" | "observed" | "inferred" | "legacy"
  evidence_count?: number
  observation_count?: number
  preference_key?: string
  preference_value?: string
  supersedes?: string
  provenance?: {
    source?: string
    session_ref?: string
    channel?: string
    account?: string
    topic_id?: string
    topic_name?: string
    message_ref?: string
    recorded_at?: string
  }
  created_at: string
  updated_at: string
}

interface UserProfileField {
  key?: string
  value?: string
  content?: string
  evidence_kind: string
  confidence: number
  source_id: string
}

interface UserProfileSnapshot {
  version: number
  identity?: UserProfileField[]
  communication?: UserProfileField[]
  workflow?: UserProfileField[]
  interaction?: UserProfileField[]
  boundaries?: UserProfileField[]
  source_ids?: string[]
  characters: number
}

interface CurrentUserProfileResponse {
  scope_label: string
  scope_description: string
  profile: UserProfileSnapshot
  entries: MemoryEntry[]
  stats: {
    entries: number
    entry_capacity: number
    characters: number
    capacity: number
    serialized_characters: number
    serialized_capacity: number
  }
}

interface PendingDiff {
  pending_id: string
  action: string
  target: string
  id?: string
  type?: string
  old_value?: string
  proposed_value?: string
  provenance?: string
  created_at: string
  mutation_index: number
}

interface MemoryStatus {
  enabled: boolean
  approval_mode: string
  background_review_enabled: boolean
  review_interval: number
  workspace: {
    entries: number
    characters: number
    capacity: number
    pending_count: number
  }
  review: {
    sessions: number
    successful_turns_pending: number
    last_successful_review_at?: string
    last_attempt_at?: string
  }
}

interface EvolutionStatus {
  enabled: boolean
  mode: string
  apply_policy: string
  task_records: number
  pattern_records: number
  drafts: number
  pending_drafts: number
  approved_drafts: number
  quarantined_drafts: number
  profiles: number
  last_observation?: string
  last_audit?: string
}

interface EvolutionDraft {
  id: string
  target_skill_name: string
  human_summary: string
  change_kind: string
  status: string
  evidence_count?: number
  success_ratio?: number
  scan_findings?: string[]
  review_notes?: string[]
  created_at: string
  updated_at?: string
}

interface DraftPreview {
  current_body: string
  rendered_body: string
  diff_preview: string
}

interface SkillProfile {
  skill_name: string
  current_version: string
  version_history: Array<{
    version: string
    action: string
    timestamp: string
    rollback?: boolean
  }>
}

const workspaceMemoryTypes: MemoryType[] = [
  "correction",
  "environment",
  "project_fact",
  "other",
]

async function managementJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await launcherFetch(path, init)
  if (!response.ok) {
    let code = `HTTP ${response.status}`
    try {
      const payload = (await response.json()) as { error?: { code?: string } }
      code = payload.error?.code || code
    } catch {
      // The management API normally returns JSON; retain the bounded status.
    }
    throw new Error(code)
  }
  return (await response.json()) as T
}

function managementPOST(path: string, body: unknown = {}): Promise<unknown> {
  return managementJSON(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  })
}

function shortTime(value?: string): string {
  if (!value) return "never"
  const parsed = new Date(value)
  return Number.isNaN(parsed.valueOf()) ? "unknown" : parsed.toLocaleString()
}

function formatPreferenceLabel(key?: string): string {
  if (!key) return ""
  switch (key) {
    case "communication.language":
      return "Language"
    case "communication.response_format":
      return "Response format"
    case "communication.verbosity":
      return "Verbosity"
    case "presentation.quiz.mode":
      return "Quiz mode"
    case "presentation.poll.mode":
      return "Poll mode"
    case "interaction.button_style":
      return "Command style"
    case "coding.formatting":
      return "Coding style"
    default:
      return key
        .split(".")
        .pop()!
        .replace(/_/g, " ")
        .replace(/\b\w/g, (c) => c.toUpperCase())
  }
}

function formatPreferenceValue(key?: string, val?: string): string {
  if (!val) return ""
  if (key === "presentation.quiz.mode" || key === "presentation.poll.mode") {
    if (val === "native") return "Telegram Native"
    if (val === "text") return "Plain text"
    if (val === "auto") return "Automatic"
  }
  if (key === "communication.language") {
    if (val === "id") return "Indonesian"
    if (val === "en") return "English"
  }
  if (key === "communication.response_format") {
    if (val === "structured") return "Structured"
    if (val === "markdown") return "Markdown"
    if (val === "concise") return "Concise"
  }
  if (key === "communication.verbosity") {
    if (val === "detailed") return "Detailed"
    if (val === "concise") return "Concise"
  }
  if (key === "interaction.button_style" || key === "coding.formatting") {
    if (val === "copy_paste") return "Copy-paste ready"
  }
  return val.replace(/_/g, " ").replace(/\b\w/g, (c) => c.toUpperCase())
}

export function MemoryManagementSection() {
  const [entries, setEntries] = useState<MemoryEntry[]>([])
  const [pending, setPending] = useState<PendingDiff[]>([])
  const [status, setStatus] = useState<MemoryStatus | null>(null)
  const [query, setQuery] = useState("")
  const [content, setContent] = useState("")
  const [entryType, setEntryType] = useState<MemoryType>("project_fact")
  const [confidence, setConfidence] = useState("1")
  const [editingID, setEditingID] = useState("")
  const [editingContent, setEditingContent] = useState("")
  const [busy, setBusy] = useState(false)

  const reload = useCallback(async (search = "") => {
    const suffix = search.trim()
      ? `?query=${encodeURIComponent(search.trim())}`
      : ""
    const [entryData, pendingData, statusData] = await Promise.all([
      managementJSON<{ entries: MemoryEntry[] }>(
        `/api/memory/workspace${suffix}`,
      ),
      managementJSON<{ pending: PendingDiff[] }>("/api/memory/pending"),
      managementJSON<MemoryStatus>("/api/memory/status"),
    ])
    setEntries(entryData.entries || [])
    setPending(pendingData.pending || [])
    setStatus(statusData)
  }, [])

  useEffect(() => {
    void reload("").catch(() =>
      toast.error("Failed to load memory management data"),
    )
  }, [reload])

  const mutate = async (
    body: Record<string, unknown>,
    message: string,
  ): Promise<boolean> => {
    setBusy(true)
    try {
      await managementPOST("/api/memory/workspace", body)
      await reload(query)
      toast.success(message)
      return true
    } catch (error) {
      toast.error(`Memory operation failed: ${(error as Error).message}`)
      return false
    } finally {
      setBusy(false)
    }
  }

  const addEntry = async () => {
    const value = Number(confidence)
    if (!content.trim() || !Number.isFinite(value) || value <= 0 || value > 1) {
      toast.error("Enter content and a confidence greater than 0 and at most 1")
      return
    }
    const applied = await mutate(
      {
        action: "add",
        content: content.trim(),
        type: entryType,
        confidence: value,
      },
      "Workspace memory added",
    )
    if (applied) setContent("")
  }

  const resolvePending = async (id: string, decision: "approve" | "reject") => {
    setBusy(true)
    try {
      await managementPOST(
        `/api/memory/pending/${encodeURIComponent(id)}/${decision}`,
      )
      await reload(query)
      toast.success(`Pending memory ${decision}d`)
    } catch (error) {
      toast.error(`Pending decision failed: ${(error as Error).message}`)
    } finally {
      setBusy(false)
    }
  }

  const duplicateFactCount = entries.reduce((acc, entry, idx) => {
    const isDup = entries.slice(idx + 1).some((other) => {
      const n1 = entry.content
        .toLowerCase()
        .trim()
        .replace(/[.,!?;:'"()]/g, "")
      const n2 = other.content
        .toLowerCase()
        .trim()
        .replace(/[.,!?;:'"()]/g, "")
      return n1 === n2 && entry.status === "active" && other.status === "active"
    })
    return isDup ? acc + 1 : acc
  }, 0)

  return (
    <Card size="sm">
      <CardHeader className="border-border border-b">
        <CardTitle>Workspace Knowledge</CardTitle>
        <CardDescription>
          Typed, query-aware non-personal facts for the default agent workspace.
          Personal memory follows trusted canonical identities.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-5 pt-5">
        {duplicateFactCount > 0 && (
          <div className="rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-xs text-amber-950 dark:text-amber-100">
            {duplicateFactCount} likely duplicate fact(s) detected. Review and
            archive or remove redundant entries.
          </div>
        )}
        {status && (
          <div className="grid grid-cols-2 gap-2 text-xs sm:grid-cols-4">
            <StatusTile
              label="Usage"
              value={`${status.workspace.characters}/${status.workspace.capacity}`}
            />
            <StatusTile
              label="Entries"
              value={String(status.workspace.entries)}
            />
            <StatusTile
              label="Pending"
              value={String(status.workspace.pending_count)}
            />
            <StatusTile
              label="Next review"
              value={`${Math.max(0, status.review_interval - status.review.successful_turns_pending)} turns`}
            />
          </div>
        )}

        <div className="grid gap-3 rounded-lg border p-3 sm:grid-cols-[minmax(0,1fr)_220px_120px_auto]">
          <Textarea
            value={content}
            onChange={(event) => setContent(event.target.value)}
            placeholder="Compact durable workspace fact"
            className="min-h-20 sm:col-span-4"
          />
          <Select
            value={entryType}
            onValueChange={(value) => setEntryType(value as MemoryType)}
          >
            <SelectTrigger aria-label="Memory type">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {workspaceMemoryTypes.map((value) => (
                <SelectItem key={value} value={value}>
                  {value}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Input
            type="number"
            min={0.01}
            max={1}
            step="0.05"
            value={confidence}
            onChange={(event) => setConfidence(event.target.value)}
            aria-label="Memory confidence"
          />
          <Button
            disabled={busy}
            onClick={() => void addEntry()}
            className="sm:col-start-4"
          >
            Add entry
          </Button>
        </div>

        <div className="flex flex-col gap-2 sm:flex-row">
          <div className="relative flex-1">
            <IconSearch className="text-muted-foreground absolute top-2.5 left-3 size-4" />
            <Input
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") void reload(query)
              }}
              placeholder="Search workspace memory"
              className="pl-9"
            />
          </div>
          <Button
            variant="outline"
            disabled={busy}
            onClick={() => void reload(query)}
          >
            Search
          </Button>
        </div>

        <div className="space-y-3">
          {entries.length === 0 && (
            <p className="text-muted-foreground text-sm">
              No matching workspace entries.
            </p>
          )}
          {entries.map((entry) => (
            <div key={entry.id} className="rounded-lg border p-3">
              <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                <div className="min-w-0 space-y-2">
                  <div className="flex flex-wrap gap-1.5">
                    <Badge variant="outline">{entry.type || "other"}</Badge>
                    <Badge
                      variant={
                        entry.status === "active" ? "secondary" : "outline"
                      }
                    >
                      {entry.status || "active"}
                    </Badge>
                    {entry.pinned && (
                      <Badge>
                        <IconPin className="mr-1 size-3" />
                        pinned
                      </Badge>
                    )}
                    <span className="text-muted-foreground font-mono text-xs">
                      {entry.id}
                    </span>
                  </div>
                  {editingID === entry.id ? (
                    <Textarea
                      value={editingContent}
                      onChange={(event) =>
                        setEditingContent(event.target.value)
                      }
                    />
                  ) : (
                    <p className="text-sm break-words whitespace-pre-wrap">
                      {entry.content}
                    </p>
                  )}
                  <p className="text-muted-foreground text-xs">
                    {entry.provenance?.source || "legacy"} · updated{" "}
                    {shortTime(entry.updated_at)}
                  </p>
                </div>
                <div className="flex shrink-0 flex-wrap gap-1.5">
                  {editingID === entry.id ? (
                    <>
                      <Button
                        size="sm"
                        disabled={busy}
                        onClick={() => {
                          void (async () => {
                            const applied = await mutate(
                              {
                                action: "replace",
                                id: entry.id,
                                content: editingContent.trim(),
                                type: entry.type,
                              },
                              "Memory entry updated",
                            )
                            if (applied) setEditingID("")
                          })()
                        }}
                      >
                        <IconCheck className="size-4" />
                      </Button>
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={() => setEditingID("")}
                      >
                        <IconX className="size-4" />
                      </Button>
                    </>
                  ) : (
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => {
                        setEditingID(entry.id)
                        setEditingContent(entry.content)
                      }}
                    >
                      Edit
                    </Button>
                  )}
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={busy}
                    onClick={() =>
                      void mutate(
                        {
                          action: entry.pinned ? "unpin" : "pin",
                          id: entry.id,
                        },
                        entry.pinned ? "Memory unpinned" : "Memory pinned",
                      )
                    }
                  >
                    <IconPin className="size-4" />
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={busy}
                    onClick={() =>
                      void mutate(
                        {
                          action:
                            entry.status === "archived" ? "restore" : "archive",
                          id: entry.id,
                        },
                        entry.status === "archived"
                          ? "Memory restored"
                          : "Memory archived",
                      )
                    }
                  >
                    {entry.status === "archived" ? (
                      <IconRestore className="size-4" />
                    ) : (
                      <IconArchive className="size-4" />
                    )}
                  </Button>
                  <Button
                    size="sm"
                    variant="destructive"
                    disabled={busy}
                    onClick={() =>
                      void mutate(
                        { action: "remove", id: entry.id },
                        "Memory removed",
                      )
                    }
                  >
                    <IconTrash className="size-4" />
                  </Button>
                </div>
              </div>
            </div>
          ))}
        </div>

        {pending.length > 0 && (
          <div className="space-y-3 border-t pt-5">
            <div>
              <h3 className="text-sm font-medium">Pending redacted diffs</h3>
              <p className="text-muted-foreground text-xs">
                Only bounded workspace previews are displayed.
              </p>
            </div>
            {pending.map((diff) => (
              <div
                key={`${diff.pending_id}-${diff.mutation_index}`}
                className="rounded-lg border p-3 text-sm"
              >
                <div className="flex flex-col gap-3 sm:flex-row sm:justify-between">
                  <div className="min-w-0 space-y-1">
                    <p>
                      <span className="font-mono text-xs">
                        {diff.pending_id}
                      </span>{" "}
                      · {diff.action} · {diff.type || "other"}
                    </p>
                    {diff.old_value && (
                      <p className="text-muted-foreground break-words">
                        Old: {diff.old_value}
                      </p>
                    )}
                    {diff.proposed_value && (
                      <p className="break-words">
                        Proposed: {diff.proposed_value}
                      </p>
                    )}
                  </div>
                  <div className="flex shrink-0 gap-2">
                    <Button
                      size="sm"
                      disabled={busy}
                      onClick={() =>
                        void resolvePending(diff.pending_id, "approve")
                      }
                    >
                      <IconCheck className="size-4" />
                      Approve
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={busy}
                      onClick={() =>
                        void resolvePending(diff.pending_id, "reject")
                      }
                    >
                      <IconX className="size-4" />
                      Reject
                    </Button>
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

const currentUserMemoryTypes: MemoryType[] = [
  "identity",
  "communication_preference",
  "workflow_preference",
  "correction",
  "environment",
  "project_fact",
  "relationship",
  "episodic_fact",
  "other",
]

export function CurrentUserProfileManagementSection() {
  const [data, setData] = useState<CurrentUserProfileResponse | null>(null)
  const [statusFilter, setStatusFilter] = useState("all")
  const [content, setContent] = useState("")
  const [entryType, setEntryType] = useState<MemoryType>(
    "communication_preference",
  )
  const [preferenceKey, setPreferenceKey] = useState("")
  const [preferenceValue, setPreferenceValue] = useState("")
  const [editingID, setEditingID] = useState("")
  const [editingContent, setEditingContent] = useState("")
  const [editingPreferenceKey, setEditingPreferenceKey] = useState("")
  const [editingPreferenceValue, setEditingPreferenceValue] = useState("")
  const [busy, setBusy] = useState(false)

  const reload = useCallback(async () => {
    setData(
      await managementJSON<CurrentUserProfileResponse>(
        "/api/memory/current-user",
      ),
    )
  }, [])

  useEffect(() => {
    void reload().catch(() =>
      toast.error("Failed to load the current Pico dashboard user profile"),
    )
  }, [reload])

  const mutate = async (body: Record<string, unknown>, message: string) => {
    setBusy(true)
    try {
      await managementPOST("/api/memory/current-user", body)
      await reload()
      toast.success(message)
      return true
    } catch (error) {
      toast.error(`Profile operation failed: ${(error as Error).message}`)
      return false
    } finally {
      setBusy(false)
    }
  }

  const addEntry = async () => {
    if (!content.trim()) {
      toast.error("Enter a compact current-user fact or preference")
      return
    }
    if ((preferenceKey.trim() === "") !== (preferenceValue.trim() === "")) {
      toast.error("Preference key and value must be supplied together")
      return
    }
    const applied = await mutate(
      {
        action: "add",
        content: content.trim(),
        type: entryType,
        preference_key: preferenceKey.trim(),
        preference_value: preferenceValue.trim(),
      },
      "Current-user memory added",
    )
    if (applied) {
      setContent("")
      setPreferenceKey("")
      setPreferenceValue("")
    }
  }

  const profileGroups: Array<readonly [string, UserProfileField[]]> = data
    ? [
        ["Identity", data.profile.identity || []],
        ["Communication", data.profile.communication || []],
        ["Workflow", data.profile.workflow || []],
        ["Interaction", data.profile.interaction || []],
        ["Boundaries", data.profile.boundaries || []],
      ]
    : []
  const entries = (data?.entries || []).filter(
    (entry) => statusFilter === "all" || entry.status === statusFilter,
  )

  return (
    <Card size="sm">
      <CardHeader className="border-border border-b">
        <CardTitle>Compiled current-user profile</CardTitle>
        <CardDescription>
          {data?.scope_description ||
            "Loading the fixed authenticated Pico dashboard identity."}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-5 pt-5">
        {data && (
          <>
            <div className="grid grid-cols-2 gap-2 text-xs sm:grid-cols-4">
              <StatusTile label="Scope" value={data.scope_label} />
              <StatusTile
                label="Profile budget"
                value={`${data.profile.characters} chars`}
              />
              <StatusTile
                label="Sources"
                value={String(data.profile.source_ids?.length || 0)}
              />
              <StatusTile
                label="Memory usage"
                value={`${data.stats.characters}/${data.stats.capacity}`}
              />
            </div>

            <div className="grid gap-3 md:grid-cols-2">
              {profileGroups.map(([label, fields]) => {
                if (fields.length === 0) return null
                return (
                  <div key={label} className="rounded-lg border p-3">
                    <h3 className="mb-2 text-sm font-medium">{label}</h3>
                    <div className="space-y-2">
                      {fields.map((field) => (
                        <div key={field.source_id} className="text-sm">
                          <p className="font-medium">
                            {field.key ? (
                              <span>
                                {formatPreferenceLabel(field.key)}:{" "}
                                <span className="text-muted-foreground font-normal">
                                  {formatPreferenceValue(
                                    field.key,
                                    field.value,
                                  )}
                                </span>
                              </span>
                            ) : (
                              field.content
                            )}
                          </p>
                          <p className="text-muted-foreground text-xs">
                            {field.key && (
                              <span className="mr-1 font-mono">
                                {field.key} ·
                              </span>
                            )}
                            {field.evidence_kind} · confidence{" "}
                            {field.confidence.toFixed(2)} · source{" "}
                            <span className="font-mono">{field.source_id}</span>
                          </p>
                        </div>
                      ))}
                    </div>
                  </div>
                )
              })}
            </div>
          </>
        )}

        <div className="grid gap-3 rounded-lg border p-3 sm:grid-cols-2 lg:grid-cols-[minmax(0,1fr)_220px]">
          <Textarea
            value={content}
            onChange={(event) => setContent(event.target.value)}
            placeholder="Explicit fact or corrected preference for this user"
            className="min-h-20 sm:col-span-2"
            aria-label="Current-user memory content"
          />
          <Select
            value={entryType}
            onValueChange={(value) => setEntryType(value as MemoryType)}
          >
            <SelectTrigger aria-label="Current-user memory type">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {currentUserMemoryTypes.map((value) => (
                <SelectItem key={value} value={value}>
                  {value}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
            <Input
              value={preferenceKey}
              onChange={(event) => setPreferenceKey(event.target.value)}
              placeholder="communication.verbosity"
              aria-label="Preference key"
            />
            <Input
              value={preferenceValue}
              onChange={(event) => setPreferenceValue(event.target.value)}
              placeholder="detailed"
              aria-label="Preference value"
            />
          </div>
          <Button disabled={busy} onClick={() => void addEntry()}>
            Add explicit entry
          </Button>
          <p className="text-muted-foreground self-center text-xs">
            Key/value are optional, but when used both are required. Same-key
            explicit corrections deterministically supersede older values.
          </p>
        </div>

        <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h3 className="text-sm font-medium">Auditable source memories</h3>
            <p className="text-muted-foreground text-xs">
              Inspect active, superseded, and archived evidence with provenance.
            </p>
          </div>
          <Select value={statusFilter} onValueChange={setStatusFilter}>
            <SelectTrigger
              className="w-full sm:w-44"
              aria-label="Memory status filter"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">all statuses</SelectItem>
              <SelectItem value="active">active</SelectItem>
              <SelectItem value="superseded">superseded</SelectItem>
              <SelectItem value="archived">archived</SelectItem>
            </SelectContent>
          </Select>
        </div>

        <div className="space-y-3">
          {entries.length === 0 && (
            <p className="text-muted-foreground text-sm">
              No current-user entries match this status.
            </p>
          )}
          {entries.map((entry) => (
            <div key={entry.id} className="rounded-lg border p-3">
              <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
                <div className="min-w-0 space-y-2">
                  <div className="flex flex-wrap gap-1.5">
                    <Badge variant="outline">{entry.type || "other"}</Badge>
                    <Badge
                      variant={
                        entry.status === "active" ? "secondary" : "outline"
                      }
                    >
                      {entry.status || "active"}
                    </Badge>
                    <Badge variant="outline">
                      {entry.evidence_kind || "legacy"} ·{" "}
                      {(entry.confidence || 0).toFixed(2)}
                    </Badge>
                    <span className="text-muted-foreground font-mono text-xs">
                      {entry.id}
                    </span>
                  </div>
                  {editingID === entry.id ? (
                    <div className="grid gap-2">
                      <Textarea
                        value={editingContent}
                        onChange={(event) =>
                          setEditingContent(event.target.value)
                        }
                        aria-label={`Edit ${entry.id} content`}
                      />
                      <div className="grid gap-2 sm:grid-cols-2">
                        <Input
                          value={editingPreferenceKey}
                          onChange={(event) =>
                            setEditingPreferenceKey(event.target.value)
                          }
                          placeholder="Preference key"
                          aria-label={`Edit ${entry.id} preference key`}
                        />
                        <Input
                          value={editingPreferenceValue}
                          onChange={(event) =>
                            setEditingPreferenceValue(event.target.value)
                          }
                          placeholder="Preference value"
                          aria-label={`Edit ${entry.id} preference value`}
                        />
                      </div>
                    </div>
                  ) : (
                    <>
                      <p className="text-sm break-words whitespace-pre-wrap">
                        {entry.content}
                      </p>
                      {entry.preference_key && (
                        <p className="text-sm font-medium break-all">
                          {entry.preference_key} = {entry.preference_value}
                        </p>
                      )}
                    </>
                  )}
                  <p className="text-muted-foreground text-xs break-words">
                    source {entry.provenance?.source || "legacy"} · channel{" "}
                    {entry.provenance?.channel || "unknown"} · account{" "}
                    {entry.provenance?.account || "unknown"} · session{" "}
                    {entry.provenance?.session_ref || "none"} · topic{" "}
                    {entry.provenance?.topic_name ||
                      entry.provenance?.topic_id ||
                      "none"}
                    {entry.supersedes
                      ? ` · supersedes ${entry.supersedes}`
                      : ""}
                  </p>
                </div>
                <div className="flex shrink-0 flex-wrap gap-1.5">
                  {editingID === entry.id ? (
                    <>
                      <Button
                        size="sm"
                        disabled={
                          busy ||
                          !editingContent.trim() ||
                          (editingPreferenceKey.trim() === "") !==
                            (editingPreferenceValue.trim() === "")
                        }
                        aria-label={`Save correction for ${entry.id}`}
                        onClick={() => {
                          void (async () => {
                            const applied = await mutate(
                              {
                                action: "add",
                                content: editingContent.trim(),
                                type: entry.type,
                                preference_key: editingPreferenceKey.trim(),
                                preference_value: editingPreferenceValue.trim(),
                                supersedes: entry.id,
                              },
                              "Current-user memory corrected",
                            )
                            if (applied) setEditingID("")
                          })()
                        }}
                      >
                        <IconCheck className="size-4" />
                        Save correction
                      </Button>
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={() => setEditingID("")}
                      >
                        <IconX className="size-4" />
                      </Button>
                    </>
                  ) : (
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={busy || entry.status !== "active"}
                      aria-label={`Correct ${entry.id}`}
                      onClick={() => {
                        setEditingID(entry.id)
                        setEditingContent(entry.content)
                        setEditingPreferenceKey(entry.preference_key || "")
                        setEditingPreferenceValue(entry.preference_value || "")
                      }}
                    >
                      <IconEdit className="size-4" />
                      Correct
                    </Button>
                  )}
                  {entry.status === "active" &&
                    entry.evidence_kind !== "explicit" && (
                      <Button
                        size="sm"
                        variant="outline"
                        disabled={busy}
                        aria-label={`Confirm ${entry.id}`}
                        onClick={() =>
                          void mutate(
                            { action: "confirm", id: entry.id },
                            "Memory confirmed as explicit",
                          )
                        }
                      >
                        <IconCheck className="size-4" />
                        Confirm
                      </Button>
                    )}
                  {entry.status !== "superseded" && (
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={busy}
                      aria-label={`${
                        entry.status === "archived" ? "Restore" : "Archive"
                      } ${entry.id}`}
                      onClick={() =>
                        void mutate(
                          {
                            action:
                              entry.status === "archived"
                                ? "restore"
                                : "archive",
                            id: entry.id,
                          },
                          entry.status === "archived"
                            ? "Memory restored"
                            : "Memory archived",
                        )
                      }
                    >
                      {entry.status === "archived" ? (
                        <IconRestore className="size-4" />
                      ) : (
                        <IconArchive className="size-4" />
                      )}
                    </Button>
                  )}
                  <Button
                    size="sm"
                    variant="destructive"
                    disabled={busy}
                    aria-label={`Delete ${entry.id}`}
                    onClick={() =>
                      void mutate(
                        { action: "remove", id: entry.id },
                        "Current-user memory deleted",
                      )
                    }
                  >
                    <IconTrash className="size-4" />
                  </Button>
                </div>
              </div>
            </div>
          ))}
        </div>
      </CardContent>
    </Card>
  )
}

export function EvolutionManagementSection() {
  const [status, setStatus] = useState<EvolutionStatus | null>(null)
  const [drafts, setDrafts] = useState<EvolutionDraft[]>([])
  const [preview, setPreview] = useState<DraftPreview | null>(null)
  const [selected, setSelected] = useState<EvolutionDraft | null>(null)
  const [profile, setProfile] = useState<SkillProfile | null>(null)
  const [busy, setBusy] = useState(false)

  const reload = useCallback(async () => {
    const [nextStatus, draftData] = await Promise.all([
      managementJSON<EvolutionStatus>("/api/evolution/status"),
      managementJSON<{ drafts: EvolutionDraft[] }>("/api/evolution/drafts"),
    ])
    setStatus(nextStatus)
    setDrafts(draftData.drafts || [])
  }, [])

  useEffect(() => {
    void reload().catch(() =>
      toast.error("Failed to load evolution management data"),
    )
  }, [reload])

  const decision = async (
    draft: EvolutionDraft,
    action: "approve" | "reject" | "apply",
  ) => {
    setBusy(true)
    try {
      await managementPOST(
        `/api/evolution/drafts/${encodeURIComponent(draft.id)}/${action}`,
      )
      await reload()
      toast.success(`Evolution draft ${action} completed`)
    } catch (error) {
      toast.error(`Evolution ${action} failed: ${(error as Error).message}`)
    } finally {
      setBusy(false)
    }
  }

  const showPreview = async (draft: EvolutionDraft) => {
    setBusy(true)
    try {
      const [nextPreview, nextProfile] = await Promise.all([
        managementJSON<DraftPreview>(
          `/api/evolution/drafts/${encodeURIComponent(draft.id)}/preview`,
        ),
        managementJSON<SkillProfile>(
          `/api/evolution/versions/${encodeURIComponent(draft.target_skill_name)}`,
        ).catch(() => null),
      ])
      setSelected(draft)
      setPreview(nextPreview)
      setProfile(nextProfile)
    } catch (error) {
      toast.error(`Preview failed: ${(error as Error).message}`)
    } finally {
      setBusy(false)
    }
  }

  const runReview = async () => {
    setBusy(true)
    try {
      await managementPOST("/api/evolution/review")
      await reload()
      toast.success("Bounded evolution review completed")
    } catch (error) {
      toast.error(`Evolution review failed: ${(error as Error).message}`)
    } finally {
      setBusy(false)
    }
  }

  const rollback = async (skillName: string, version: string) => {
    setBusy(true)
    try {
      await managementPOST("/api/evolution/rollback", {
        skill_name: skillName,
        version,
      })
      await reload()
      toast.success("Skill rollback completed")
    } catch (error) {
      toast.error(`Rollback failed: ${(error as Error).message}`)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card size="sm">
      <CardHeader className="border-border border-b">
        <CardTitle>Evolution review & rollback</CardTitle>
        <CardDescription>
          Review sanitized repeated procedural evidence, inspect a bounded diff,
          approve explicitly, apply only in apply mode, and roll back a version.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-5 pt-5">
        {status && (
          <div className="grid grid-cols-2 gap-2 text-xs sm:grid-cols-4">
            <StatusTile
              label="Mode"
              value={status.enabled ? status.mode : "disabled"}
            />
            <StatusTile
              label="Tasks / patterns"
              value={`${status.task_records}/${status.pattern_records}`}
            />
            <StatusTile
              label="Drafts pending"
              value={String(status.pending_drafts)}
            />
            <StatusTile label="Apply policy" value={status.apply_policy} />
          </div>
        )}
        <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
          <p className="text-muted-foreground text-xs">
            Last observation: {shortTime(status?.last_observation)} · Last
            audit: {shortTime(status?.last_audit)}
          </p>
          <Button
            variant="outline"
            disabled={busy || !status?.enabled}
            onClick={() => void runReview()}
          >
            <IconRefresh className="size-4" />
            Run bounded review
          </Button>
        </div>

        <div className="space-y-3">
          {drafts.length === 0 && (
            <p className="text-muted-foreground text-sm">
              No evolution drafts.
            </p>
          )}
          {drafts.map((draft) => (
            <div key={draft.id} className="rounded-lg border p-3">
              <div className="flex flex-col gap-3 sm:flex-row sm:justify-between">
                <div className="min-w-0 space-y-2">
                  <div className="flex flex-wrap gap-1.5">
                    <Badge variant="outline">{draft.change_kind}</Badge>
                    <Badge
                      variant={
                        draft.status === "quarantined"
                          ? "destructive"
                          : "secondary"
                      }
                    >
                      {draft.status}
                    </Badge>
                    <Badge variant="outline">
                      {draft.evidence_count || 0} evidence
                    </Badge>
                    <Badge variant="outline">
                      {Math.round((draft.success_ratio || 0) * 100)}% success
                    </Badge>
                  </div>
                  <p className="text-sm font-medium break-words">
                    {draft.target_skill_name}
                  </p>
                  <p className="text-muted-foreground text-sm break-words">
                    {draft.human_summary}
                  </p>
                  {(draft.scan_findings || []).map((finding) => (
                    <p key={finding} className="text-destructive text-xs">
                      {finding}
                    </p>
                  ))}
                  <p className="text-muted-foreground font-mono text-xs">
                    {draft.id}
                  </p>
                </div>
                <div className="flex shrink-0 flex-wrap gap-1.5">
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={busy}
                    onClick={() => void showPreview(draft)}
                  >
                    Preview
                  </Button>
                  {draft.status === "candidate" && (
                    <Button
                      size="sm"
                      disabled={busy}
                      onClick={() => void decision(draft, "approve")}
                    >
                      <IconCheck className="size-4" />
                      Approve
                    </Button>
                  )}
                  {(draft.status === "candidate" ||
                    draft.status === "approved") && (
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={busy}
                      onClick={() => void decision(draft, "reject")}
                    >
                      <IconX className="size-4" />
                      Reject
                    </Button>
                  )}
                  {draft.status === "approved" && (
                    <Button
                      size="sm"
                      disabled={busy || status?.mode !== "apply"}
                      onClick={() => void decision(draft, "apply")}
                    >
                      <IconPlayerPlay className="size-4" />
                      Apply
                    </Button>
                  )}
                </div>
              </div>
            </div>
          ))}
        </div>

        {selected && preview && (
          <div className="space-y-3 border-t pt-5">
            <h3 className="text-sm font-medium">
              Diff preview: {selected.target_skill_name}
            </h3>
            <pre className="bg-muted max-h-96 overflow-auto rounded-lg p-3 text-xs whitespace-pre-wrap">
              {preview.diff_preview}
            </pre>
            {profile && profile.version_history.length > 0 && (
              <div className="space-y-2">
                <h4 className="flex items-center gap-2 text-sm font-medium">
                  <IconHistory className="size-4" />
                  Version history
                </h4>
                {profile.version_history
                  .slice()
                  .reverse()
                  .map((version, index) => (
                    <div
                      key={`${version.version}-${index}`}
                      className="flex flex-col gap-2 rounded border p-2 text-xs sm:flex-row sm:items-center sm:justify-between"
                    >
                      <span className="break-all">
                        {version.version} · {version.action} ·{" "}
                        {shortTime(version.timestamp)}
                      </span>
                      {version.version !== profile.current_version && (
                        <Button
                          size="sm"
                          variant="outline"
                          disabled={busy}
                          onClick={() =>
                            void rollback(profile.skill_name, version.version)
                          }
                        >
                          <IconRestore className="size-4" />
                          Rollback
                        </Button>
                      )}
                    </div>
                  ))}
              </div>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function StatusTile({ label, value }: { label: string; value: string }) {
  return (
    <div className="bg-muted/50 min-w-0 rounded-lg border p-2.5">
      <p className="text-muted-foreground truncate">{label}</p>
      <p className="mt-1 font-medium break-words">{value}</p>
    </div>
  )
}
