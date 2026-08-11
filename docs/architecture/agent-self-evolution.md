# Agent Self-Evolution

Agent self-evolution is PicoClaw's procedural learning layer. It observes
successfully delivered work, looks for repeated successful procedures, and can
turn those procedures into reviewable workspace-skill changes. It is separate
from session history, curated personal memory, checkpoints, and the agent's
personality files.

Evolution is disabled by default. Enabling it without specifying a mode selects
`observe`; automatic skill application is never enabled implicitly.

## Data boundaries

The layers have deliberately different purposes:

| Layer | Purpose | Model-writable through evolution |
| --- | --- | --- |
| `AGENT.md` / `SOUL.md` | Agent identity, personality, and high-priority behavior | No |
| `USER.md` | Workspace-wide static context | No |
| Curated workspace/current-user memory | Durable facts and personalization | No |
| Checkpoints | Resumable progress for a lesson or multi-step task | No |
| Evolution records and skills | Reusable, non-personal procedures | Yes, subject to mode and safety policy |

Evolution may learn reliable tool ordering, repository workflows, debugging
procedures, repeatable setup steps, and other stable operational lessons. It
must not learn names, account identifiers, locations, time zones, roles,
relationships, communication preferences, credentials, raw private
conversations, or data copied from current-user memory.

The evolution runtime does not enumerate or query private per-user memory
stores. Before procedural evidence is persisted, it removes secret-like
values, personal identifiers, prompt-injection-shaped text, forbidden
personality/private-memory paths, invalid UTF-8, bidirectional controls, and
other control characters. Findings are stored as categories rather than the
rejected value. This is defense in depth, not a guarantee that every possible
secret or identifying phrase can be recognized.

## Delivery-gated observation

The agent stages an evolution observation while a normal main-agent turn is in
progress. It commits that observation only after the authoritative delivery
path confirms that the final response was delivered successfully. Failed or
interrupted responses cannot become positive procedural evidence, and planned
but undelivered content is not recorded.

Heartbeat, cron, reviewer, subagent, internal/system, no-history, and other
private runtime events are excluded. Curator and evolution work also carry a
suppression marker, so they cannot recursively create observations or trigger
another learner run. Pending delivery-gated observations are transferred when
the agent runtime reloads rather than being falsely finalized.

Persisted session provenance is a bounded hash reference, not a raw session
key. User goals, final-output excerpts, and tool-error summaries are scrubbed
and length-limited before they enter the evolution store.

## Observe, draft, and apply

The pipeline has three explicit stages:

1. **Observe** records scrubbed task evidence after successful delivery.
2. **Draft** clusters repeated evidence, applies the configured evidence
   threshold, recalls relevant workspace skills, and asks the configured model
   for a bounded candidate change.
3. **Apply** re-verifies evidence and safety, writes the approved skill change,
   records a version and audit event, and retains rollback data.

The configured mode controls how far the automatic pipeline may proceed:

| Mode | Behavior |
| --- | --- |
| `observe` | Store eligible procedural observations only. |
| `draft` | Create reviewable candidates when the cold path runs; never write a skill. |
| `apply` | Permit skill writes, subject to `apply_policy` and all safety checks. |

`apply_policy` defaults to `approval_required`. In that policy, a candidate
must be explicitly approved and then explicitly applied from the authenticated
dashboard. `automatic` is available only as an explicit high-risk
configuration: eligible safe candidates in `apply` mode may be written without
human approval. It should be used only in a controlled workspace with trusted
inputs and reliable backups.

## Evidence and draft safety

A single task is never enough to produce an applicable skill change.
`min_task_count` must be at least 2, and `min_success_ratio` defines the minimum
verified success ratio for a pattern. The cold path bounds the evidence set
with `max_evidence_records`; approval and apply recalculate the metrics from
the stored workspace-scoped task and pattern records rather than trusting
model-provided counts.

Before a draft can be approved or applied, PicoClaw checks:

- target skill name and workspace path safety;
- supported change kind and valid `SKILL.md` schema/frontmatter;
- maximum draft size;
- secret, personal-data, injection, and control-character patterns;
- forbidden personality and private-memory paths;
- declared tool and skill-policy constraints;
- source evidence count and success ratio.

Unsafe candidates are quarantined and cannot be applied. The dashboard exposes
bounded findings, evidence metadata, and a bounded before/after diff; it does
not expose raw private transcripts. Applying a draft re-runs the checks, even
if the draft was previously approved.

## Cold-path triggers and model cost

Draft generation is the cold path and applies only in `draft` or `apply` mode.

| `cold_path_trigger` | Behavior |
| --- | --- |
| `after_turn` | Run after each eligible delivered turn. |
| `scheduled` | Run at the configured `cold_path_times` (`HH:MM`). |
| `manual` | Run only when requested from the authenticated dashboard. |

Observation itself is local and lightweight. Pattern judging, clustering, and
draft generation may call a model, so `draft` and `apply` can consume
additional API tokens and incur provider cost. `after_turn` is the most eager
option; `scheduled` or `manual` is easier to budget. Each draft generation and
manual review is timeout-bounded, and evidence and draft sizes are capped.

Background curated-memory review is a separate model consumer. Enabling both
features incurs both kinds of API usage.

## Dashboard control plane

The configuration dashboard provides evolution settings and an authenticated
management surface for:

- status, last observation, and bounded record/draft counts;
- a bounded manual cold-path review;
- draft list, details, evidence summary, findings, and diff preview;
- candidate approval or rejection;
- applying an approved draft while in `apply` mode;
- skill version history and rollback.

All management routes use the existing launcher-dashboard authentication and
derive the workspace from the server's active configuration. Requests cannot
select an arbitrary workspace, session, user store, or filesystem path. IDs,
skill names, query parameters, request sizes, and JSON fields are validated by
the backend.

There are intentionally no mutating `/evolution` chat commands. PicoClaw does
not currently have a trusted, channel-independent owner/admin authorization
primitive suitable for those commands; exposing approval, apply, or rollback
to arbitrary chat participants would be unsafe. Use the authenticated
dashboard instead.

## Versions, audit, and rollback

Before applying a change, PicoClaw snapshots the prior skill state, including
the "skill did not exist" baseline for a newly created skill. Successful
approve, reject, quarantine, apply, and rollback operations create bounded
audit records. Skill profiles retain version history according to
`rollback_retention`.

Rollback validates the requested skill and stored snapshot before restoring
it. Rolling back a newly created skill to its absent baseline removes the
generated skill while keeping version and audit metadata. Rollback is an
administrator action and does not itself generate new learning evidence.

Evolution state lives under `workspace/state/evolution` by default. `state_dir`
may override that location. The state includes task and pattern records,
drafts, profiles, version snapshots, backups, and an audit log. Files containing
evidence or control state use private permissions; generated workspace skills
retain the repository's normal skill-file permissions.

## Recommended starting point

Start with reviewable drafts and stronger-than-minimum evidence:

```json
{
  "evolution": {
    "enabled": true,
    "mode": "draft",
    "apply_policy": "approval_required",
    "private_data_scrubbing": true,
    "min_task_count": 3,
    "min_success_ratio": 0.8,
    "cold_path_trigger": "after_turn",
    "draft_timeout_seconds": 45,
    "max_evidence_records": 50,
    "max_draft_chars": 12000,
    "rollback_retention": 10
  }
}
```

Review multiple drafts and their evidence before moving to `apply`. Keep
`approval_required` unless the operational environment explicitly accepts the
risk of autonomous skill mutation.

## Troubleshooting and limitations

- **No observations appear:** confirm evolution is enabled and the turn was a
  normal main-agent turn whose final response was delivered successfully.
- **No draft appears:** `observe` never drafts; in other modes, confirm the
  trigger ran and enough successful, related evidence meets both thresholds.
- **A draft is quarantined:** inspect the category-only findings and bounded
  preview. Correct the source procedure or create a clean candidate; a
  quarantined draft cannot be applied.
- **Apply is unavailable:** the effective mode must be `apply`, the draft must
  be approved under the normal policy, and its evidence and safety checks must
  still pass.
- **A generated skill regresses:** use the dashboard version history to restore
  the previous snapshot.
- **Costs are higher than expected:** prefer `manual` or a restrained
  `scheduled` cold path and review provider usage alongside the independent
  memory-curator setting.

Heuristic clustering and model-generated drafts cannot guarantee improvement.
The local scrubber cannot identify every private fact or adversarial
instruction, and rollback retention is finite. Keep workspace backups, inspect
previews, and treat automatic apply as an expert-only option.

For configuration fields and the combined memory/evolution preset, see the
[Configuration Guide](../guides/configuration.md#agent-self-evolution) and the
[Curated Memory Guide](../guides/curated-memory.md).
