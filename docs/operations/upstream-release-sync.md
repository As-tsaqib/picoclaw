# Reviewable Upstream Release Sync

The fork follows stable releases from `sipeed/picoclaw`, not every upstream
commit. The scheduled workflow runs daily at **01:47 UTC** and the same process
can be started manually from **Actions → Sync upstream release → Run workflow**.
It is intentionally limited to the `As-tsaqib/picoclaw` repository.

## What the workflow does

1. Calls `repos/sipeed/picoclaw/releases/latest` and accepts only a strictly
   validated stable SemVer-style `vX.Y.Z` tag (with a bounded safe suffix).
2. Fetches the tag with full reachable Git history and records the resolved
   upstream commit SHA.
3. Checks whether that commit is already an ancestor of the fork's `main`.
   If it is, the run is a no-op and the Step Summary explains why.
4. Otherwise, creates `sync/upstream-<tag>` from the current fork `main` and
   makes a clearly labelled merge commit:
   `chore: merge upstream release <tag>`.
5. Pushes **only** that sync branch and opens a pull request in this fork with
   `main` as its base. Auto-merge is never enabled.
6. Runs the read-only upstream-sync validation suite against the resulting
   commit. The suite covers the policy tests, all Go tests, a PicoClaw build,
   static analysis, focused race tests, and dashboard tests, lint, formatting,
   and build.

The helper is copied to the runner's temporary directory before the merge.
Consequently, a file added or changed by upstream cannot replace the helper
while it is running. The sync job does not execute upstream scripts, invoke
release/deployment workflows, upload artifacts, or build the Android module.

## Idempotency and review state

- An already-imported release is a successful no-op.
- An open PR for `sync/upstream-<tag>` is reported and never duplicated.
- A closed PR is reported for manual maintainer action. It is not reopened or
  recreated automatically, whether it was closed without merge or merged with
  a strategy that removed the upstream commit's ancestry.
- An existing branch is never force-pushed. If it has the exact expected merge
  shape, the workflow can create its missing PR; otherwise it reports that
  manual inspection is required.
- A repository-level concurrency group prevents two scheduled/manual sync runs
  from mutating at the same time.

## Conflicts

The workflow never resolves conflicts automatically. It aborts the merge,
leaves both the local and remote `main` unchanged, lists at most 20 sanitized
conflicting paths in the Step Summary, and creates or updates one issue titled
`[Upstream Sync] Conflict while importing <tag>`. The issue contains the tag,
upstream SHA, run link, and bounded manual-resolution instructions. It does not
contain file contents, credentials, private configuration, or tokens.

Resolve a conflict manually on a fresh branch from the current `main`, verify
the upstream tag and commit, review the resulting diff, and open a normal fork
PR. Do not force-push a branch that has already been reviewed or push directly
to `main`.

## Security and validation boundaries

The mutating job uses only the fork's `GITHUB_TOKEN`, with `contents: write`
for the sync branch, `pull-requests: write` for the fork PR, and `issues: write`
only for conflict reporting. Validation jobs use `contents: read`, disable
checkout credential persistence, and receive no repository secrets. They use
`pull_request` (never `pull_request_target`) for later PR updates. Because a
`GITHUB_TOKEN`-created PR may not emit a second workflow event, the sync run
also invokes the reusable read-only validation workflow directly after the
branch/PR step; later pushes and manually opened sync PRs use the same workflow
through their normal triggers.

Third-party actions in the sync and validation workflows are pinned to commit
SHAs. Release, deployment, upload, and module workflows are not triggered by
`sync/upstream-*`; they remain manual, scheduled, or reusable as defined in
their own files. No new PAT is required.

## Review checklist

Before merging an upstream-sync PR, review at least:

- configuration defaults, migrations, and compatibility with existing files;
- session allocation, Telegram topic isolation, persistence, and reset behavior;
- prompt/context assembly and newly exposed tools;
- curated memory, recall boundaries, checkpoints, and evolution separation;
- dashboard APIs, round-trip behavior, responsive UI, and generated assets;
- Magisk/KSU module and Android runtime compatibility (review only in this
  workflow; the module is neither rebuilt nor changed here);
- dependency, workflow, supply-chain, and migration changes.

A clean Git merge only proves that Git could combine the histories. It does not
prove semantic compatibility, safe resource usage, privacy behavior, or runtime
compatibility with this fork. Feature branches are not automatically rebased
or updated by this workflow.
