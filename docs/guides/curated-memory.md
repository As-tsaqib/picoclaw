# Memory & Recall

PicoClaw keeps several kinds of context separate. This separation is a privacy
and correctness boundary, not only an implementation detail.

| Layer | Answers | Scope and ownership |
| --- | --- | --- |
| Immutable kernel | Which security/privacy/tool rules can never be weakened | Runtime-owned; higher authority than personality or memory |
| `SOUL.md` | Who the agent is and how it naturally communicates | Authoritative personality/identity when present; generic PicoClaw identity is fallback only |
| `AGENT.md` | What this workspace/agent should do | Stable operator/workspace instructions |
| Compiled user profile | Who the current trusted user is and how to interact with them | Small derived view of canonical `current_user` memory; shared contexts include behavioral fields only |
| Session history | What was said recently | One allocated session or Telegram topic |
| Session summary | What older context in this session means | The same session or topic only |
| Curated memory | Which selective facts remain useful | Non-personal workspace facts or one trusted canonical user |
| Task checkpoints | Where temporary work stopped | One session or topic; never a user profile |
| Skills and evolution | How a repeatable procedure should be performed | Agent/workspace procedural knowledge; never personal semantic memory |

The effective system-prompt order is deterministic: immutable kernel and
hierarchy policy, `SOUL.md`, `AGENT.md`/workspace instructions, capabilities
and applicable skills, compiled current-user profile, relevant curated and
legacy workspace memory, output/runtime context, legacy `USER.md` defaults,
and the current-session summary. Recent conversation remains ordinary message
history after that system prompt. Explicit structured corrections/preferences
carry their own evidence authority and therefore override stale `USER.md`
defaults even though both are reference context. Personality and memory cannot
weaken the immutable kernel's security, authorization, tool, workspace, or
privacy rules.

`USER.md` is now explicitly a legacy/operator seed or default, not the live authoritative profile. A newer explicit structured user preference always wins when the two conflict. The existing `workspace/memory/MEMORY.md` and recent daily notes continue to enter prompts as manual **non-private workspace** context. Structured memory lives separately
under `workspace/memory/structured/` and does not rewrite or migrate those
legacy files.

## Defaults and model cost

Curated memory and the bounded background curator are enabled by default. The
curator runs after every 10 successfully delivered eligible user turns unless
configured otherwise.

> Background review makes additional model requests and consumes additional
> provider tokens/API quota. It is asynchronous and cannot delay the main
> response, but it can increase cost. Set
> `memory.background_review.enabled` to `false` to disable scheduled reviews.

The counter and review cursor survive restarts. Failed, canceled, interrupted,
internal, reviewer, subagent, cron, heartbeat, and no-history turns do not
advance the review. Only messages newer than the last successful cursor are
reviewed. A new live turn cancels an in-flight review, and at most one curator
runs for an agent at a time.

The reviewer receives a compact transcript snapshot, a timeout, and a small
iteration limit. Its registry contains only `memory_manage`; shell, file, web,
MCP, recall, checkpoint, subagent, and evolution tools are unavailable. Its
prompt and output never enter ordinary session history, and it cannot trigger
itself recursively.

## Typed curated memory

Structured entries have a stable ID, compact content, provenance, creation and
update timestamps, and these lifecycle fields:

- `type`: `identity`, `communication_preference`, `workflow_preference`,
  `correction`, `environment`, `project_fact`, `relationship`,
  `episodic_fact`, or `other`;
- `status`: `active`, `superseded`, or `archived`;
- `pinned`: whether a compact fact is eligible regardless of the current query;
- `confidence` from greater than zero through one;
- `evidence_kind`: `explicit`, `observed`, `inferred`, or the migration-only `legacy`;
- `visibility`: `behavioral`, `private`, or `shared` (`shared` is workspace-only);
- optional `evidence_count` / `observation_count`;
- optional stable `preference_key` / `preference_value` for deterministic preference changes;
- `supersedes` for corrections/history;
- confirmation, presentation, archive, and expiry timestamps.

The store schema is versioned. Older structured documents are normalized and
rewritten as schema v3 under the same process and cross-process write lock, using
the store's bounded atomic/private-permission write path. Legacy current-user
communication/workflow preferences migrate conservatively to `behavioral`; other
personal entries default to `private`. A malformed or newer
unsupported document fails closed instead of being partially migrated. Legacy
non-reviewer records retain compatibility authority; old background-review
records are conservatively treated as inferred rather than silently becoming
user-confirmed facts. Manual `MEMORY.md` and daily notes are outside this
migration and are never rewritten.

Workspace memory is for non-personal facts shared by sessions in the same
agent workspace, such as project conventions, environment details, build
policy, stable tool quirks, and reliable workflow rules. Private identities,
preferences, relationships, and episodic user facts are rejected from this
scope.

Current-user memory contains a trusted sender's stable identity, preferences,
corrections, and personal workflow choices. The user key is derived in backend
code from channel, account, sender identity, and configured canonical identity
links. Neither model tools nor dashboard requests can select an arbitrary user
ID, agent ID, workspace, session key, or memory path.

Different users and agents use different structured stores. The same canonical
Telegram user uses the same durable owner store across direct chats, groups, topics,
and `/session` switches; ChatID, TopicID, SessionKey, and SessionRef never become
the owner identity. Another Telegram user cannot read or mutate that store.

Trusted group/topic turns may capture current-user memory. `behavioral` entries
(for example communication/workflow preferences safe to apply silently) may be
listed/retrieved and influence only that sender in shared context. `private`
entries may be captured there but are filtered from shared prompt, retrieval,
list/search/inspect output, tool-result payloads, and notification previews.
Management of an existing private entry requires a private context and shared
lookups fail closed without revealing whether the private ID exists.

## Compiled current-user profile

For any trusted canonical-user turn, PicoClaw compiles a bounded `UserProfileSnapshot` from active curated current-user memory. In direct chat it may include eligible private profile fields; in group/topic context it is rebuilt from `behavioral` entries only. This profile is always available to prompt assembly, so stable interaction preferences do not depend on retrieval luck. It contains only profile-relevant identity/communication/workflow/interaction fields and source memory IDs; project and episodic details remain query-retrieved.

The snapshot is a cache, **not another source of truth**. It is rebuilt automatically when the underlying structured memory revision changes and cache validity also stops at the earliest relevant memory expiry, so expired preferences cannot survive through a stale profile cache. The in-process cache is a fixed-size least-recently-used map, preventing unbounded growth as users are encountered. Low-confidence inference is excluded by default, while explicit user preferences remain eligible. The default profile budget is 1,200 serialized characters. Private profiles are never loaded into shared/group or unknown-identity prompts.

Structured preference keys make corrections deterministic. For example, a newer explicit `communication.verbosity=detailed` supersedes an older active `communication.verbosity=concise`; a weak inferred value cannot displace an explicit preference.

Evidence authority is intentionally different: direct user statements are `explicit`, repeated behavioral evidence may be `observed`, and cautious model conclusions are `inferred`. `observed` requires at least two observations; a single observation is conservatively downgraded to inference. Inferred confidence is capped below profile eligibility, and neither observed nor inferred entries retain direct-user confirmation timestamps. An inferred entry is not automatically confirmed simply because the curator created it.

### What can be remembered

The agent should save compact durable information after an explicit “remember this” request, a stable preference, a correction, a durable environment fact, a project convention, or a reliable workflow lesson. Strong explicit preference/correction language can trigger an early bounded curator pass instead of waiting for the normal interval; the normal model-facing `memory_manage` path remains semantic and is not limited to English keywords.

It must not save:

- API keys, passwords, tokens, cookies, credentials, or private keys;
- raw logs, large tool output, entire conversations, or temporary paths/errors;
- unsupported psychological/sensitive labels inferred from conversational style;
- unverified assumptions or instructions copied from untrusted external data;
- task progress, which belongs in a checkpoint;
- private user facts in workspace memory.

Secret patterns, prompt-injection shapes, forged delimiters, invalid UTF-8,
hidden bidirectional text, and unsafe controls are rejected before persistence.
Exact normalized content duplicates are rejected. Reaffirming the same active
`preference_key`/`preference_value` is an idempotent no-op (or an evidence upgrade
when authority strictly increases), so foreground/background capture cannot create
a second effective preference. Likely conflicts are returned as
non-destructive hints so the caller can clarify, archive, replace, or explicitly
supersede the old entry. Batch consolidation is atomic, capacity-limited, and
written with owner-only permissions.

## Query-aware retrieval

Normal prompt assembly does not inject the whole structured store. For each turn, PicoClaw builds a compact retrieval query from the current message plus bounded session summary/recent user turns, then retrieves:

1. bounded active pinned entries;
2. top query-relevant active workspace entries;
3. top query-relevant active current-user entries;
4. only the configured recent fallback when the message has no meaningful
   lexical tokens.

Workspace and current-user result/character budgets remain separate and configurable. In direct personal chats the default retrieval budget favors current-user memory (70%) while retaining 30% for workspace facts; the compact profile has its own fixed budget. Archived,
superseded, and expired entries are excluded. Scoring is deterministic for a fixed timestamp and combines normalized token overlap, BM25-like rarity, prefix/trigram similarity, structured preference terms, type/correction priority, evidence/confidence, type-aware recency, confirmation, a small presentation signal, and staleness. Stable identity/communication preferences decay much more slowly than episodic facts. It works locally without a vector
database or embedding provider and supports ordinary Indonesian and English
tokens. The default `hybrid_lexical` engine remains the smallest path. Optional
`semantic_rerank` adds a deterministic, provider-neutral multilingual concept
score for common durable interaction preferences, then falls back to the same
lexical retrieval. It does not call an embedding API or load a model; the
scorer interface leaves room for a richer optional implementation later.

`last_presented_at` is staged during prompt assembly and written only after the authoritative final response is delivered. It receives only a small ranking bonus because “shown to the model” is not proof of usefulness. Legacy `last_used_at` remains readable for compatibility. Failed or partial delivery leaves presentation state unchanged. Setting `memory.retrieval.enabled` to `false` retains the older
bounded active-entry behavior.

All rendered memory is bounded JSON-like reference data inside explicit
delimiters. It is not a system instruction, and instruction-shaped text inside
it must be ignored.

## Telegram topics and transcript recall

Telegram forum topics remain separate sessions. PicoClaw never merges their
complete histories. A normal prompt contains the current topic history and
summary, bounded structured memory, and current-topic checkpoints only.

The `session_recall` tool performs bounded lexical search after an explicit
cross-topic reference such as “yang kita bahas sebelumnya”, “di topic OAuth
kemarin”, or “remember the error from the other topic”. Results include a short
matching excerpt and available topic ID/name, opaque session reference,
timestamp, role, and message provenance.

Backend code enforces the configured mode:

- `isolated`: no transcript search outside the current session. This is the
  privacy-safe generic default.
- `user_recall`: other sessions owned by the same canonical user in the same
  channel/account. This is recommended for a personal Telegram bot.
- `group_recall`: other topics in the same channel/account/group, which can
  expose excerpts written by other participants. Enable it only when everyone
  expects topic content to be shared across that group.

Tool arguments cannot widen these boundaries or request an arbitrary session.
Results and scanned record counts are bounded, and irrelevant topic content is
not inserted automatically.

## Task checkpoints

Enable checkpoints for lessons, debugging, coding, research, or multi-step
setup that may span turns. A checkpoint records a stable ID, kind, title and
objective, status, completed items, current and next step, compact important
context, the last delivered assistant excerpt/reference, topic provenance, and
timestamps.

Mutations made during a turn are staged and committed only after successful
final delivery. Planned but unsent content does not advance progress. An
unrelated question does not destroy an active checkpoint. When asked to
continue, the agent considers only active/suspended checkpoints in the current
topic, resumes the most recent relevant one from `next_step`, and asks for
clarification when several are equally plausible. Completed or archived work
is not resumed accidentally.

Before `/clear`, `/reset`, or `/new` discards session recall, PicoClaw performs a bounded synchronous flush of still-unreviewed delivered turns whenever curated memory is enabled, even if scheduled background review is disabled. The operation is fail-closed: if that bounded flush fails or times out, history is left intact and the user can retry instead of silently losing unreviewed durable information. The same bounded best-effort path runs before context compression, provider/config-registry replacement, and shutdown; the persisted cursor makes repeated hooks idempotent. After a successful destructive-command flush, all three reset commands clear the current session history/summary,
current-session recall records, and its reviewer cursor. They discard
undelivered checkpoint mutations but preserve committed checkpoints, curated
memory, `MEMORY.md`, daily notes, and every unrelated session/topic. Starting
or switching Telegram topics selects a separate session and deletes nothing.

Context compression does not delete the durable recall store, so still-unreviewed delivered turns remain available to the curator after compaction. Recall and review-state stores are persisted as well, which means a process restart does not require a shutdown-time model call to preserve unreviewed turns; they can be curated after the process starts again.

## Approval and notifications

`memory.approval_mode` supports:

- `off`: allowed interactive and background writes apply immediately;
- `background_only`: curator writes are staged, while an explicit interactive
  remember request can apply immediately;
- `all_writes`: curator and model-initiated interactive writes are staged.

Authenticated dashboard administration applies its requested workspace
operation directly. For compatibility, legacy `write_approval: false` maps to
`off`, `true` maps to `background_only`, and an explicit `approval_mode` wins.
Existing config files are not rewritten merely because this enum exists.

Notification modes are `off`, `on` (`💾 Memory updated`), and `verbose`. Foreground
and reviewer writes for the same inbound message/turn share a bounded race-safe
claim, so at most one visible memory notification is emitted. No-op/failed writes
do not claim success. Model-facing writes expose deterministic outcomes
(`added`, `updated`, `superseded`, `reaffirmed`, `no_op`, `pending`, or `rejected`)
so the assistant can distinguish durable mutation from reaffirmation, staging,
no-op, or failure. Verbose notifications contain at most a compact defense-in-depth
redacted preview; private shared-chat operations never reveal their content.

## Commands and dashboard

Memory commands are:

- `/memory status`
- `/memory profile` (trusted direct chat only)
- `/memory list`
- `/memory search QUERY`
- `/memory edit ID CONTENT`
- `/memory pin ID`
- `/memory unpin ID`
- `/memory archive ID`
- `/memory restore ID`
- `/memory forget ID`
- `/memory pending`
- `/memory approve ID_OR_ALL`
- `/memory reject ID_OR_ALL`
- `/memory review`

Checkpoint commands are:

- `/checkpoint list`
- `/checkpoint resume ID`
- `/checkpoint forget ID`

The authenticated dashboard's **Memory & Recall** configuration section exposes
review, approval, notification, capacity, retrieval, lifecycle, recall, and
checkpoint controls. Its workspace management card includes
type/status/pin/provenance, search and lifecycle actions, character use,
reviewer status, and bounded redacted pending diffs.

The adjacent current-user profile card calls `GET /api/memory/current-user`
and `POST /api/memory/current-user`. It displays the compiled profile, source
IDs/evidence, and offers confirm, correction, archive/restore, and delete
actions. That API is deliberately fixed to the authenticated dashboard's
canonical Pico-channel user identity; request fields and query parameters
cannot select another user. It does **not** enumerate Telegram or arbitrary
private stores. Telegram profiles remain manageable only from that trusted
user's direct chat commands.

## Personal Telegram-bot preset

Blank reviewer provider/model values follow the main model. This preset keeps
background changes reviewable, enables same-user recall across topics, and
creates evolution drafts without applying them automatically:

```json
{
  "memory": {
    "enabled": true,
    "approval_mode": "background_only",
    "notifications": "on",
    "workspace_char_limit": 12000,
    "per_user_char_limit": 8000,
    "background_review": {
      "enabled": true,
      "interval": 10,
      "provider": "",
      "model": "",
      "timeout_seconds": 30,
      "max_iterations": 3
    },
    "profile": {
      "enabled": true,
      "max_chars": 1200,
      "min_confidence": 0.65
    },
    "retrieval": {
      "enabled": true,
      "engine": "hybrid_lexical",
      "max_workspace_results": 6,
      "max_user_results": 6,
      "max_total_chars": 4000,
      "pinned_char_budget": 1200,
      "user_share": 0.70
    },
    "recall": {
      "mode": "user_recall",
      "max_results": 5,
      "max_chars": 4000
    },
    "checkpoints": {
      "enabled": true,
      "max_count": 100,
      "max_context_chars": 2000,
      "completed_retention_days": 90
    }
  },
  "evolution": {
    "enabled": true,
    "mode": "draft",
    "min_task_count": 3,
    "min_success_ratio": 0.8,
    "cold_path_trigger": "after_turn",
    "apply_policy": "approval_required",
    "private_data_scrubbing": true
  }
}
```

Both background memory review and evolution draft generation make extra model
calls. Monitor provider usage before enabling them on high-volume agents.

## Limitations and troubleshooting

Memory is selective. Default retrieval deliberately remains lightweight and local; compact profile fields and contextual query construction improve personalization, and optional `semantic_rerank` recognizes only a bounded set of common preference concepts rather than providing general embedding-quality semantics. Deep or domain-specific paraphrases can therefore still be missed. A curator can
correctly decide that nothing is durable enough to save. Capacity, retention,
model judgment, failed reviews, and explicit deletion also affect recall;
PicoClaw cannot guarantee perfect memory.

For a deterministic implementation/resource smoke benchmark, run
`go run ./cmd/membench personalization`. It writes a JSON report covering
preference correction/adherence, inference and false-memory resistance,
semantic recall, long-horizon stability, cross-user/group isolation, memory
pollution, prompt character/token overhead, retrieval latency, and cold/cached
profile compilation latency. Timings are environment-dependent and are
diagnostic rather than a portable performance guarantee.

Use `/memory status` and the dashboard reviewer cursor when reviews appear
stalled. Check configured provider/model availability and logs for category-only
errors; PicoClaw deliberately does not log memory content or provider errors
that could include transcripts. Use `/memory pending` when approval mode is
active, and prefer `user_recall` over `group_recall` unless group-wide sharing
is intentional.
