import {
  IconArchive,
  IconCheck,
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
  supersedes?: string
  provenance?: { source?: string; recorded_at?: string }
  created_at: string
  updated_at: string
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
      managementJSON<{ entries: MemoryEntry[] }>(`/api/memory/workspace${suffix}`),
      managementJSON<{ pending: PendingDiff[] }>("/api/memory/pending"),
      managementJSON<MemoryStatus>("/api/memory/status"),
    ])
    setEntries(entryData.entries || [])
    setPending(pendingData.pending || [])
    setStatus(statusData)
  }, [])

  useEffect(() => {
    void reload("").catch(() => toast.error("Failed to load memory management data"))
  }, [reload])

  const mutate = async (body: Record<string, unknown>, message: string) => {
    setBusy(true)
    try {
      await managementPOST("/api/memory/workspace", body)
      await reload(query)
      toast.success(message)
    } catch (error) {
      toast.error(`Memory operation failed: ${(error as Error).message}`)
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
    await mutate(
      {
        action: "add",
        content: content.trim(),
        type: entryType,
        confidence: value,
      },
      "Workspace memory added",
    )
    setContent("")
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

  return (
    <Card size="sm">
      <CardHeader className="border-border border-b">
        <CardTitle>Curated workspace memory</CardTitle>
        <CardDescription>
          Typed, query-aware non-personal facts for the default agent workspace.
          Private current-user stores are intentionally managed only from a
          trusted direct chat.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-5 pt-5">
        {status && (
          <div className="grid grid-cols-2 gap-2 text-xs sm:grid-cols-4">
            <StatusTile label="Usage" value={`${status.workspace.characters}/${status.workspace.capacity}`} />
            <StatusTile label="Entries" value={String(status.workspace.entries)} />
            <StatusTile label="Pending" value={String(status.workspace.pending_count)} />
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
          <Select value={entryType} onValueChange={(value) => setEntryType(value as MemoryType)}>
            <SelectTrigger aria-label="Memory type"><SelectValue /></SelectTrigger>
            <SelectContent>
              {workspaceMemoryTypes.map((value) => <SelectItem key={value} value={value}>{value}</SelectItem>)}
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
          <Button disabled={busy} onClick={() => void addEntry()} className="sm:col-start-4">
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
          {entries.length === 0 && <p className="text-muted-foreground text-sm">No matching workspace entries.</p>}
          {entries.map((entry) => (
            <div key={entry.id} className="rounded-lg border p-3">
              <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                <div className="min-w-0 space-y-2">
                  <div className="flex flex-wrap gap-1.5">
                    <Badge variant="outline">{entry.type || "other"}</Badge>
                    <Badge variant={entry.status === "active" ? "secondary" : "outline"}>{entry.status || "active"}</Badge>
                    {entry.pinned && <Badge><IconPin className="mr-1 size-3" />pinned</Badge>}
                    <span className="text-muted-foreground font-mono text-xs">{entry.id}</span>
                  </div>
                  {editingID === entry.id ? (
                    <Textarea value={editingContent} onChange={(event) => setEditingContent(event.target.value)} />
                  ) : (
                    <p className="text-sm break-words whitespace-pre-wrap">{entry.content}</p>
                  )}
                  <p className="text-muted-foreground text-xs">
                    {entry.provenance?.source || "legacy"} · updated {shortTime(entry.updated_at)}
                  </p>
                </div>
                <div className="flex shrink-0 flex-wrap gap-1.5">
                  {editingID === entry.id ? (
                    <>
                      <Button size="sm" disabled={busy} onClick={() => {
                        void mutate({ action: "replace", id: entry.id, content: editingContent.trim(), type: entry.type }, "Memory entry updated")
                        setEditingID("")
                      }}><IconCheck className="size-4" /></Button>
                      <Button size="sm" variant="ghost" onClick={() => setEditingID("")}><IconX className="size-4" /></Button>
                    </>
                  ) : (
                    <Button size="sm" variant="outline" onClick={() => { setEditingID(entry.id); setEditingContent(entry.content) }}>Edit</Button>
                  )}
                  <Button size="sm" variant="outline" disabled={busy} onClick={() => void mutate({ action: entry.pinned ? "unpin" : "pin", id: entry.id }, entry.pinned ? "Memory unpinned" : "Memory pinned")}>
                    <IconPin className="size-4" />
                  </Button>
                  <Button size="sm" variant="outline" disabled={busy} onClick={() => void mutate({ action: entry.status === "archived" ? "restore" : "archive", id: entry.id }, entry.status === "archived" ? "Memory restored" : "Memory archived")}>
                    {entry.status === "archived" ? <IconRestore className="size-4" /> : <IconArchive className="size-4" />}
                  </Button>
                  <Button size="sm" variant="destructive" disabled={busy} onClick={() => void mutate({ action: "remove", id: entry.id }, "Memory removed")}>
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
              <p className="text-muted-foreground text-xs">Only bounded workspace previews are displayed.</p>
            </div>
            {pending.map((diff) => (
              <div key={`${diff.pending_id}-${diff.mutation_index}`} className="rounded-lg border p-3 text-sm">
                <div className="flex flex-col gap-3 sm:flex-row sm:justify-between">
                  <div className="min-w-0 space-y-1">
                    <p><span className="font-mono text-xs">{diff.pending_id}</span> · {diff.action} · {diff.type || "other"}</p>
                    {diff.old_value && <p className="text-muted-foreground break-words">Old: {diff.old_value}</p>}
                    {diff.proposed_value && <p className="break-words">Proposed: {diff.proposed_value}</p>}
                  </div>
                  <div className="flex shrink-0 gap-2">
                    <Button size="sm" disabled={busy} onClick={() => void resolvePending(diff.pending_id, "approve")}><IconCheck className="size-4" />Approve</Button>
                    <Button size="sm" variant="outline" disabled={busy} onClick={() => void resolvePending(diff.pending_id, "reject")}><IconX className="size-4" />Reject</Button>
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
    void reload().catch(() => toast.error("Failed to load evolution management data"))
  }, [reload])

  const decision = async (draft: EvolutionDraft, action: "approve" | "reject" | "apply") => {
    setBusy(true)
    try {
      await managementPOST(`/api/evolution/drafts/${encodeURIComponent(draft.id)}/${action}`)
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
        managementJSON<DraftPreview>(`/api/evolution/drafts/${encodeURIComponent(draft.id)}/preview`),
        managementJSON<SkillProfile>(`/api/evolution/versions/${encodeURIComponent(draft.target_skill_name)}`).catch(() => null),
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
      await managementPOST("/api/evolution/rollback", { skill_name: skillName, version })
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
            <StatusTile label="Mode" value={status.enabled ? status.mode : "disabled"} />
            <StatusTile label="Tasks / patterns" value={`${status.task_records}/${status.pattern_records}`} />
            <StatusTile label="Drafts pending" value={String(status.pending_drafts)} />
            <StatusTile label="Apply policy" value={status.apply_policy} />
          </div>
        )}
        <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
          <p className="text-muted-foreground text-xs">
            Last observation: {shortTime(status?.last_observation)} · Last audit: {shortTime(status?.last_audit)}
          </p>
          <Button variant="outline" disabled={busy || !status?.enabled} onClick={() => void runReview()}>
            <IconRefresh className="size-4" />Run bounded review
          </Button>
        </div>

        <div className="space-y-3">
          {drafts.length === 0 && <p className="text-muted-foreground text-sm">No evolution drafts.</p>}
          {drafts.map((draft) => (
            <div key={draft.id} className="rounded-lg border p-3">
              <div className="flex flex-col gap-3 sm:flex-row sm:justify-between">
                <div className="min-w-0 space-y-2">
                  <div className="flex flex-wrap gap-1.5">
                    <Badge variant="outline">{draft.change_kind}</Badge>
                    <Badge variant={draft.status === "quarantined" ? "destructive" : "secondary"}>{draft.status}</Badge>
                    <Badge variant="outline">{draft.evidence_count || 0} evidence</Badge>
                    <Badge variant="outline">{Math.round((draft.success_ratio || 0) * 100)}% success</Badge>
                  </div>
                  <p className="text-sm font-medium break-words">{draft.target_skill_name}</p>
                  <p className="text-muted-foreground text-sm break-words">{draft.human_summary}</p>
                  {(draft.scan_findings || []).map((finding) => (
                    <p key={finding} className="text-destructive text-xs">{finding}</p>
                  ))}
                  <p className="text-muted-foreground font-mono text-xs">{draft.id}</p>
                </div>
                <div className="flex shrink-0 flex-wrap gap-1.5">
                  <Button size="sm" variant="outline" disabled={busy} onClick={() => void showPreview(draft)}>Preview</Button>
                  {draft.status === "candidate" && <Button size="sm" disabled={busy} onClick={() => void decision(draft, "approve")}><IconCheck className="size-4" />Approve</Button>}
                  {(draft.status === "candidate" || draft.status === "approved") && <Button size="sm" variant="outline" disabled={busy} onClick={() => void decision(draft, "reject")}><IconX className="size-4" />Reject</Button>}
                  {draft.status === "approved" && <Button size="sm" disabled={busy || status?.mode !== "apply"} onClick={() => void decision(draft, "apply")}><IconPlayerPlay className="size-4" />Apply</Button>}
                </div>
              </div>
            </div>
          ))}
        </div>

        {selected && preview && (
          <div className="space-y-3 border-t pt-5">
            <h3 className="text-sm font-medium">Diff preview: {selected.target_skill_name}</h3>
            <pre className="bg-muted max-h-96 overflow-auto rounded-lg p-3 text-xs whitespace-pre-wrap">{preview.diff_preview}</pre>
            {profile && profile.version_history.length > 0 && (
              <div className="space-y-2">
                <h4 className="flex items-center gap-2 text-sm font-medium"><IconHistory className="size-4" />Version history</h4>
                {profile.version_history.slice().reverse().map((version, index) => (
                  <div key={`${version.version}-${index}`} className="flex flex-col gap-2 rounded border p-2 text-xs sm:flex-row sm:items-center sm:justify-between">
                    <span className="break-all">{version.version} · {version.action} · {shortTime(version.timestamp)}</span>
                    {version.version !== profile.current_version && (
                      <Button size="sm" variant="outline" disabled={busy} onClick={() => void rollback(profile.skill_name, version.version)}>
                        <IconRestore className="size-4" />Rollback
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
      <p className="mt-1 break-words font-medium">{value}</p>
    </div>
  )
}
