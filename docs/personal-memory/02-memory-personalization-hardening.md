# 02 — Personal Memory and Personalization Hardening

## 1. Preserve the merged architecture

This phase is a hardening pass over the existing personal-memory subsystem.

Do **not** replace:

- `CuratedStore` with a new profile database;
- compiled profile with a second writable source of truth;
- checkpoints with permanent memory;
- recall with always-on transcript injection;
- existing evidence/supersession logic with ad-hoc string memory.

The canonical durable source remains curated memory. Compiled profile and retrieval are derived views.

## 2. Canonical person scope across trusted channels

### 2.1 Problem

Existing `CanonicalUserScopeKey(channel, account, rawID, identityLinks)` intentionally scopes by channel/account even after resolving a canonical identity alias.

This prevents accidental cross-channel leakage but also means:

```text
Telegram identity linked to "owner"
→ channel:telegram|account:default|user:owner

Pico dashboard identity linked to "owner"
→ channel:pico|account:default|user:owner
```

remain different stores.

### 2.2 Required design

Introduce a **person-scope resolver** that is used only when trusted configuration provides positive proof that multiple runtime identities refer to one canonical person.

Behavior:

```text
trusted identity + explicit identity link exists
→ person:<canonical-identity>

no identity link / ambiguous / invalid link
→ existing channel/account-scoped user key
```

Exact key format may differ, but requirements are normative:

- deterministic;
- stable across restart;
- independent from ChatID/TopicID/SessionKey;
- independent from user/model-provided values;
- per-agent isolation remains intact;
- canonical identity name must come from trusted `session.identity_links` or equivalent trusted config;
- missing/ambiguous identity fails closed;
- no heuristic joining by display name, username, email, or similar weak data.

### 2.3 No arbitrary dashboard impersonation

The management API must **not** accept arbitrary `user_id`, Telegram ID, canonical alias, or `UserKey` from request query/body merely to make dashboard switching easy.

Trusted owner binding must come from configuration/runtime authentication, not request-selected identity.

## 3. Dashboard owner binding

### 3.1 Current behavior to preserve if unconfigured

An authenticated Pico dashboard identity may continue to have its own dashboard-local profile if no cross-channel owner identity is configured.

In that case UI must not misleadingly imply that the profile is the Telegram user's profile.

Preferred labels:

- `Dashboard-local profile`
- or `Personal profile: <canonical alias>` only when binding is proven.

### 3.2 Recommended configuration model

Prefer a setting that references a **canonical identity-link alias**, not a raw platform ID. For example conceptually:

```yaml
memory:
  owner_identity: owner
```

Normative semantics:

- value must correspond to a configured canonical identity link;
- the authenticated dashboard identity must itself be a member of that canonical identity link or otherwise be explicitly authorized by the same trusted config;
- raw Telegram user IDs are not supplied through dashboard request fields;
- configuration updates use minimal/delta patch semantics and preserve unrelated settings.

If current configuration architecture suggests a better trusted binding mechanism, use it, but preserve the security properties above.

## 4. Migration to person scope

When a newly configured identity link causes previously separated channel-scoped stores to represent the same person, provide deterministic migration/consolidation.

### 4.1 Migration inputs

Potential sources include:

- Telegram current-user store
- Pico/dashboard current-user store
- other explicitly linked channels

### 4.2 Merge policy

For each structured preference key:

1. compare active candidates;
2. preserve evidence authority;
3. preserve recency/confirmation metadata;
4. use existing preference reconciliation rules where possible;
5. result must contain exactly one effective active state per canonical preference key.

For non-preference curated facts:

- preserve unique entries;
- exact duplicates must not multiply;
- near-duplicates should be consolidated when confidence is high enough;
- ambiguous conflicting facts must not be silently destroyed; retain provenance and mark/archive/supersede according to existing schema.

### 4.3 Migration safety

Migration must be:

- idempotent;
- crash-safe/atomic enough for existing storage architecture;
- race-safe with live memory writes;
- repeatable on restart without duplicating data;
- auditable through sanitized logs/status;
- reversible only if current migration framework supports a safe rollback; otherwise preserve source backups/legacy compatibility as appropriate.

Never delete the only copy of an entry before the consolidated target is safely committed.

## 5. Canonical preference-key vocabulary

### 5.1 Existing syntax normalization stays

Keep lowercase/trim/syntax validation.

### 5.2 Add semantic canonicalization

Introduce a bounded registry/alias map for recognized built-in preferences.

Example recommended canonical keys:

```text
communication.language
communication.verbosity
communication.response_format
workflow.command_style
presentation.quiz.mode
presentation.poll.mode
presentation.rich_content
interaction.buttons
```

Recommended quiz values:

```text
auto
native
text
```

A legacy key/value such as:

```text
workflow.quiz_format = telegram_native_quiz
```

should normalize/migrate to an equivalent canonical form rather than coexist indefinitely as a second logical preference.

### 5.3 Custom keys

Do not forbid useful custom preferences.

Prefer a namespace such as:

```text
custom.<name>
```

for keys that are not recognized built-ins.

The exact compatibility strategy may retain unknown historical keys unchanged, but background/autonomous inference should prefer known canonical keys or explicit `custom.*` keys rather than inventing synonyms.

### 5.4 Alias migration requirements

- deterministic;
- no two active states after normalization;
- explicit evidence wins over observed/inferred conflicts;
- preserve provenance;
- migration tested across restart.

## 6. Tier-0 / Tier-1 prompt deduplication

### 6.1 Problem

The compiled profile is derived from curated entries. The same entries can then independently be selected by current-user Tier-1 retrieval.

### 6.2 Required behavior

When building the prompt:

1. compile current-user profile;
2. obtain the profile `SourceIDs` or equivalent source identity;
3. retrieve Tier-1 current-user memory;
4. exclude/deprioritize entries already fully represented by Tier-0 profile;
5. allow a source to reappear only when retrieval requires materially more detail not represented in the profile and the renderer explicitly avoids redundant content.

The prompt should not contain the same preference twice merely because it appears in two views.

### 6.3 Usage accounting

Avoid creating retrieval feedback loops.

If usage/presentation accounting exists:

- distinguish “represented in profile” from “rendered as Tier-1 detail”;
- do not make always-on profile presentation artificially increase future retrieval ranking every turn;
- preserve existing usage metrics semantics where possible.

### 6.4 Budget

Tier-0 + Tier-1 must remain bounded. Deduplication should reduce tokens, not expand the prompt.

## 7. Memory notification coalescing

### 7.1 Product invariant

One logical user turn should produce **at most one** user-visible memory-change notification.

This includes changes from:

- explicit foreground `memory_manage` calls;
- proactive foreground capture;
- background reviewer triggered by the same turn;
- batch mutation results.

### 7.2 Required design

Replace “spawn one notification per memory event” with turn/session-aware aggregation.

Possible mechanisms:

- turn-local accumulator flushed at finalization;
- bounded short debounce keyed by turn ID + route;
- explicit foreground/reviewer transaction correlation.

Exact mechanism may follow existing event architecture.

Requirements:

- deterministic single flush;
- no unbounded goroutine per event;
- no global cross-user coalescing;
- group/topic route preserved;
- private-memory preview remains redacted in shared contexts;
- failed/no-op writes do not produce false success notifications;
- `off`, compact, verbose modes remain meaningful;
- pending approval vs applied updates remain distinguishable;
- internal notification messages must not be re-captured/reviewed as user memory.

## 8. Foreground/background idempotency

The current store already rejects exact duplicates and reconciles preference keys. Preserve this and harden reviewer/live-turn coordination.

Requirements:

- if foreground already applied canonical preference `K=V`, reviewer must not add an equivalent second entry;
- if reviewer discovers a real correction, use normal authority/recency reconciliation;
- concurrent foreground/reviewer updates must not stale-overwrite newer authoritative state;
- near-duplicate fact consolidation must preserve provenance;
- no silent deletion of distinct facts because similarity score is merely high.

## 9. Shared-context privacy remains strict

Cross-channel person scope does **not** grant permission to inject all personal memory everywhere.

In shared/group contexts:

- behavioral preferences may influence style without disclosure;
- private facts stay out of prompt/output;
- one user's memory never affects another sender's turn;
- dashboard or another linked channel cannot be used to bypass shared-context privacy.

Person identity answers **who owns the memory**, not **where every entry may be exposed**.

## 10. Quiz preference integration

The memory subsystem must support the capability architecture without embedding Telegram-specific execution logic.

Recommended profile state:

```text
presentation.quiz.mode = native
```

Interpretation belongs to agent policy:

```text
preference=native + capability poll.quiz=supported + allowed
→ use send_quiz

preference=native + capability unavailable
→ deterministic fallback
```

Do not save raw Bot API method names, bot tokens, chat IDs, callback IDs, or server capability probe errors as personal preferences.

## 11. Regression requirements

Preserve:

- explicit remember/forget/correction behavior;
- evidence semantics;
- one-effective-preference behavior;
- stable compiled profile;
- bounded retrieval;
- checkpoints;
- recall on demand;
- legacy migration compatibility;
- secret rejection;
- user A/B isolation;
- trusted group/topic capture where currently permitted by policy;
- `/session` and `/clear`/`reset` semantics.
