# Memory & Recall

PicoClaw keeps several kinds of context separate. This separation is a privacy
and correctness boundary, not only an implementation detail.

| Layer | Answers | Scope and ownership |
| --- | --- | --- |
| `AGENT.md` and `SOUL.md` | Who the agent is and how it behaves | Stable, administrator-controlled personality; memory and evolution never edit these files |
| Session history | What was said recently | One allocated session or Telegram topic |
| Session summary | What older context in this session means | The same session or topic only |
| Curated memory | Which selective facts remain useful | Non-personal workspace facts or one trusted canonical user |
| Task checkpoints | Where temporary work stopped | One session or topic; never a user profile |
| Skills and evolution | How a repeatable procedure should be performed | Agent/workspace procedural knowledge; never personal semantic memory |

`USER.md` remains workspace-wide static context and is not a private profile.
The existing `workspace/memory/MEMORY.md` and recent daily notes continue to
enter prompts as manual workspace context. Structured memory lives separately
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
- `supersedes` for an explicit correction;
- optional verification, usage, archive, and expiry timestamps.

Old structured entries without these fields still load. They behave as active
`other` entries with full effective confidence, and a read does not rewrite the
store merely to add defaults.

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
Telegram user can use their profile across topics, while another Telegram user
cannot read or mutate it. Private current-user listing and mutation commands
are available only in a safe direct chat. Shared chats expose at most safe
workspace information and redacted status.

### What can be remembered

The agent should save compact durable information after an explicit “remember
this” request, a stable preference, a correction, a durable environment fact,
a project convention, or a reliable workflow lesson.

It must not save:

- API keys, passwords, tokens, cookies, credentials, or private keys;
- raw logs, large tool output, entire conversations, or temporary paths/errors;
- unverified assumptions or instructions copied from untrusted external data;
- task progress, which belongs in a checkpoint;
- private user facts in workspace memory.

Secret patterns, prompt-injection shapes, forged delimiters, invalid UTF-8,
hidden bidirectional text, and unsafe controls are rejected before persistence.
Exact normalized duplicates are rejected. Likely conflicts are returned as
non-destructive hints so the caller can clarify, archive, replace, or explicitly
supersede the old entry. Batch consolidation is atomic, capacity-limited, and
written with owner-only permissions.

## Query-aware retrieval

Normal prompt assembly does not inject the whole structured store. For each
turn, PicoClaw retrieves:

1. bounded active pinned entries;
2. top query-relevant active workspace entries;
3. top query-relevant active current-user entries;
4. only the configured recent fallback when the message has no meaningful
   lexical tokens.

Workspace and current-user result/character budgets remain separate. Archived,
superseded, and expired entries are excluded. Scoring is deterministic for a
fixed timestamp and combines normalized token overlap, BM25-like rarity,
prefix/trigram similarity, type and correction priority, confidence, recency,
previous delivered use, and a stale penalty. It works locally without a vector
database or embedding provider and supports ordinary Indonesian and English
tokens.

`last_used_at` is staged during prompt assembly and written only after the
authoritative final response is delivered. Failed or partial delivery leaves it
unchanged. Setting `memory.retrieval.enabled` to `false` retains the older
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

`/clear` and its `/reset` alias clear the current session history/summary,
current-session recall records, and its reviewer cursor. They discard
undelivered checkpoint mutations but preserve committed checkpoints, curated
memory, `MEMORY.md`, daily notes, and every unrelated session/topic. Starting
or switching Telegram topics selects a separate session and deletes nothing.

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

Notification modes are `off`, `on` (`💾 Memory updated`), and `verbose`. Verbose
notifications contain at most a compact defense-in-depth redacted preview;
private shared-chat operations never reveal their content.

## Commands and dashboard

Memory commands are:

- `/memory status`
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
checkpoint controls. Its management card lists only non-personal workspace
entries, including type/status/pin/provenance, search and lifecycle actions,
character use, reviewer status, and bounded redacted pending diffs. It does not
enumerate private user stores; manage those through trusted direct-chat
commands.

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
      "max_iterations": 2
    },
    "retrieval": {
      "enabled": true,
      "engine": "hybrid_lexical",
      "max_workspace_results": 6,
      "max_user_results": 6,
      "max_total_chars": 4000,
      "pinned_char_budget": 1200
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

Memory is selective, and lexical retrieval can miss paraphrases. A curator can
correctly decide that nothing is durable enough to save. Capacity, retention,
model judgment, failed reviews, and explicit deletion also affect recall;
PicoClaw cannot guarantee perfect memory.

Use `/memory status` and the dashboard reviewer cursor when reviews appear
stalled. Check configured provider/model availability and logs for category-only
errors; PicoClaw deliberately does not log memory content or provider errors
that could include transcripts. Use `/memory pending` when approval mode is
active, and prefer `user_recall` over `group_recall` unless group-wide sharing
is intentional.
