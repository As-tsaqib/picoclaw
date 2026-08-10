# Memory & Recall

PicoClaw has several kinds of persistent context. They solve different problems and intentionally do not collapse every conversation into one global history.

| Layer | Purpose | Scope |
| --- | --- | --- |
| Session history | Recent messages in the current conversation | One allocated session/topic |
| Session summary | Compressed older context for the same session | One allocated session/topic |
| Curated memory | Selective durable facts and preferences | Workspace or trusted canonical user |
| Task checkpoints | Resumable lesson, debugging, research, coding, or setup state | One allocated session/topic |
| Skills/evolution | Reusable procedural instructions | Agent/workspace; not personal semantic memory |

The existing `workspace/memory/MEMORY.md` and recent daily notes still enter the prompt as manual workspace context. Structured curated memory is stored separately below `workspace/memory/structured/` and never overwrites or migrates `MEMORY.md`. `USER.md` remains workspace-wide static context; it is not a private per-user profile.

## Defaults and API cost

Curated memory and the bounded background curator are enabled by default. The curator reviews newly delivered turns every 10 successful user turns unless configured otherwise.

> Background reviews make additional model requests and therefore consume additional provider tokens/API quota. They run asynchronously after delivery, but enabling them can increase cost.

The reviewer has a timeout and a small iteration limit. It can call only `memory_manage`; shell, filesystem, web, MCP, session recall, checkpoints, subagents, and unrelated tools are unavailable. Reviewer prompts and outputs are not written to normal session history, and a reviewer cannot recursively start another review.

Set `memory.background_review.enabled` to `false` to avoid those extra requests. An explicit `/memory review` still starts one bounded review when curated memory is available.

## What curated memory stores

Workspace memory is for non-personal facts that apply to the agent or project, including project conventions, environment details, build policy, stable tool quirks, and reliable workflow rules. It may be used by other sessions/topics handled by the same agent workspace.

Current-user memory is for stable information about the trusted sender, including communication preferences, name, timezone, role, explicit corrections, and persistent workflow preferences. The user scope comes from channel/account/sender identity and configured canonical identity links. A model cannot supply an arbitrary user ID.

PicoClaw rejects exact normalized duplicates and content shaped like credentials, secrets, prompt injection, hidden controls, or forged memory delimiters. Do not save:

- API keys, passwords, cookies, tokens, or other credentials;
- raw logs, large tool output, whole conversations, or temporary paths/errors;
- unverified assumptions or instructions copied from untrusted external content;
- task progress that belongs in a checkpoint.

Entries have stable IDs, timestamps, and compact provenance. Writes are atomic, concurrency-safe, capacity-limited, and stored with owner-only permissions. Memory is rendered into prompts as bounded, explicitly delimited data rather than higher-priority instructions.

## Telegram topics and cross-topic recall

Telegram forum topics remain separate sessions by default. PicoClaw does not merge full histories from multiple topics. Each prompt contains only the current topic history and summary, bounded workspace/current-user memory, and active or suspended checkpoints for that topic.

The `session_recall` tool performs bounded lexical search only when the user explicitly refers to another topic or earlier discussion, for example “yang kita bahas sebelumnya”, “di topic OAuth kemarin”, or “remember the error from the other topic”. Results contain a short excerpt and available topic, session, timestamp, role, and message provenance.

Backend code enforces one of these modes:

- `isolated`: never search transcripts outside the current session. This is the privacy-safe default.
- `user_recall`: search other sessions belonging to the same canonical user, channel, and account. This is recommended for a personal Telegram bot.
- `group_recall`: search topics in the same channel/account/group, including excerpts from other participants. Use this only when everyone expects topic content to be shared across that group.

No tool argument can override the trusted user, group, agent, or session boundary. Different users cannot read each other's private curated memory, and structured stores are separated by agent even when agents share a workspace.

## Task checkpoints

Enable checkpoints for work that may span multiple turns: lessons, debugging, coding, research, or multi-step setup. A checkpoint records a stable ID, kind, objective, status, completed items, current and next step, compact important context, the last delivered assistant excerpt, topic/session provenance, and timestamps.

Checkpoint updates made during an agent turn are staged in memory and committed only after the final response is successfully delivered. A canceled, interrupted, or failed response does not falsely advance progress. An unrelated question does not replace an active checkpoint.

When asked to continue, the agent resolves active/suspended checkpoints in the current topic and continues from `next_step`. It asks for clarification when multiple checkpoints are equally plausible. Completed or archived checkpoints are not resumed accidentally.

`/clear` and its `/reset` alias clear current session history/summary, the current session's recall excerpts, and its reviewer cursor. They discard undelivered checkpoint mutations but preserve committed checkpoints, workspace memory, current-user memory, `MEMORY.md`, daily notes, and other sessions. Starting or switching to another Telegram topic creates/selects another isolated session and does not delete any of these layers.

## Commands

Curated-memory controls:

- `/memory status`
- `/memory list`
- `/memory forget <id>`
- `/memory pending`
- `/memory approve <id|all>`
- `/memory reject <id|all>`
- `/memory review`

Checkpoint controls:

- `/checkpoint list`
- `/checkpoint resume <id>`
- `/checkpoint forget <id>`

When `write_approval` is enabled, background mutations are staged. Immediate user/agent writes still apply directly. Notification modes are `off`, `on` (`💾 Memory updated`), and `verbose` (a compact redacted preview). Private current-user previews, status counts, and pending-change details are suppressed in shared chats; use a direct chat for private list/approval controls.

## Personal Telegram-bot preset

This preset enables curated memory, checkpoints, same-user recall across Telegram topics, a review every 10 delivered turns, and simple notifications. Blank reviewer provider/model values follow the main model.

```json
{
  "memory": {
    "enabled": true,
    "workspace_char_limit": 12000,
    "per_user_char_limit": 8000,
    "write_approval": false,
    "notifications": "on",
    "background_review": {
      "enabled": true,
      "interval": 10,
      "provider": "",
      "model": "",
      "timeout_seconds": 30,
      "max_iterations": 2
    },
    "recall": {
      "mode": "user_recall",
      "max_results": 5,
      "max_chars": 4000,
      "max_records": 2000
    },
    "checkpoints": {
      "enabled": true,
      "max_count": 100,
      "max_context_chars": 2000,
      "completed_retention_days": 90
    }
  }
}
```

The same settings are available in the dashboard under **Memory & Recall**. The dashboard warns when background review is active and when `group_recall` is selected.

## Limitations

Memory is selective. Lexical search may miss paraphrases, and a curator may decide that nothing is durable enough to save. Capacity limits, transcript retention, failures, model judgment, and explicit deletion all affect recall. PicoClaw cannot guarantee perfect memory, and curated memory should not be used as a credential store, audit log, or authoritative database.
