#!/usr/bin/env bash

set -Eeuo pipefail
export LC_ALL=C

readonly TARGET_REPOSITORY="As-tsaqib/picoclaw"
readonly TARGET_OWNER="As-tsaqib"
readonly UPSTREAM_REPOSITORY="sipeed/picoclaw"
readonly UPSTREAM_GIT_URL="https://github.com/sipeed/picoclaw.git"
readonly MAIN_BRANCH="main"
readonly SYNC_BRANCH_PREFIX="sync/upstream-"

summary_file="${GITHUB_STEP_SUMMARY:-}"
output_file="${GITHUB_OUTPUT:-}"
test_mode="${UPSTREAM_SYNC_TEST_MODE:-0}"

append_summary() {
  if [[ -n "$summary_file" ]]; then
    printf '%s\n' "$@" >> "$summary_file"
  fi
}

safe_for_log() {
  local value="$1"
  if [[ "$value" =~ ([Ss][Ee][Cc][Rr][Ee][Tt]|[Pp][Aa][Ss][Ss][Ww][Oo][Rr][Dd]|[Tt][Oo][Kk][Ee][Nn]|[Cc][Rr][Ee][Dd][Ee][Nn][Tt][Ii][Aa][Ll]|gh[pousr]_[0-9A-Za-z]{8}) ]]; then
    printf '%s' '<redacted-sensitive-looking-value>'
    return 0
  fi
  printf '%q' "${value:0:160}"
}

fail() {
  local message="$1"
  printf '::error::%s\n' "$message" >&2
  append_summary \
    "### Reviewable upstream release sync" \
    "" \
    "- Result: **failed before mutation**" \
    "- Reason: $message"
  exit 1
}

require_command() {
  local command_name="$1"
  command -v "$command_name" >/dev/null 2>&1 || fail "Required command is unavailable: $command_name"
}

if [[ "${GITHUB_REPOSITORY:-}" != "$TARGET_REPOSITORY" ]]; then
  fail "This workflow may mutate only $TARGET_REPOSITORY."
fi

if [[ ! "${GITHUB_RUN_ID:-}" =~ ^[0-9]+$ ]]; then
  fail "GITHUB_RUN_ID is missing or invalid."
fi

require_command git
require_command jq

git_bin="$(command -v git)"
jq_bin="$(command -v jq)"

if [[ "$test_mode" == "1" ]]; then
  if [[ "${GITHUB_ACTIONS:-false}" == "true" ]]; then
    fail "Test overrides are disabled in a production Actions environment."
  fi
  upstream_git_url="${UPSTREAM_SYNC_TEST_UPSTREAM_URL:-}"
  gh_bin="${UPSTREAM_SYNC_TEST_GH_BIN:-}"
  [[ -n "$upstream_git_url" ]] || fail "The test upstream URL is missing."
  [[ -x "$gh_bin" ]] || fail "The fake GitHub client is missing or not executable."
else
  require_command gh
  upstream_git_url="$UPSTREAM_GIT_URL"
  gh_bin="$(command -v gh)"
  [[ -n "${GH_TOKEN:-}" ]] || fail "GH_TOKEN is required for the mutating workflow."

  origin_url="$("$git_bin" remote get-url origin 2>/dev/null || true)"
  case "$origin_url" in
    "https://github.com/$TARGET_REPOSITORY"|"https://github.com/$TARGET_REPOSITORY.git"|"git@github.com:$TARGET_REPOSITORY.git") ;;
    *) fail "The origin remote does not point to the authorized fork." ;;
  esac
fi

tmp_dir="$(mktemp -d)"
trap 'rm -rf -- "$tmp_dir"' EXIT

if [[ -n "$("$git_bin" status --porcelain)" ]]; then
  fail "The sync checkout is not clean."
fi

"$git_bin" fetch --no-tags origin \
  "refs/heads/$MAIN_BRANCH:refs/remotes/origin/$MAIN_BRANCH"

if ! "$git_bin" show-ref --verify --quiet "refs/remotes/origin/$MAIN_BRANCH"; then
  fail "The fork main branch is unavailable."
fi

release_json="$("$gh_bin" api "repos/$UPSTREAM_REPOSITORY/releases/latest")" || \
  fail "GitHub did not return the latest stable upstream release."

if ! "$jq_bin" -e '
  type == "object" and
  .draft == false and
  .prerelease == false and
  (.tag_name | type == "string")
' >/dev/null 2>&1 <<< "$release_json"; then
  fail "The upstream latest-release response is not a stable release object."
fi

upstream_tag="$("$jq_bin" -r '.tag_name' <<< "$release_json")"
if (( ${#upstream_tag} > 64 )) ||
  [[ ! "$upstream_tag" =~ ^v(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})([-+][0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$ ]]; then
  fail "The upstream release tag is invalid and was not used."
fi

sync_branch="${SYNC_BRANCH_PREFIX}${upstream_tag}"
release_url="https://github.com/$UPSTREAM_REPOSITORY/releases/tag/$upstream_tag"
run_url="https://github.com/$TARGET_REPOSITORY/actions/runs/$GITHUB_RUN_ID"
release_ref="refs/remotes/canonical/releases/$upstream_tag"

if "$git_bin" remote get-url canonical >/dev/null 2>&1; then
  existing_upstream_url="$("$git_bin" remote get-url canonical)"
  if [[ "$existing_upstream_url" != "$upstream_git_url" ]]; then
    fail "The canonical remote has an unexpected URL."
  fi
else
  "$git_bin" remote add canonical "$upstream_git_url"
fi

# No depth is specified: the tag's reachable commit history is fetched in full.
"$git_bin" fetch --no-tags canonical \
  "refs/tags/$upstream_tag:$release_ref"

upstream_commit="$("$git_bin" rev-parse "${release_ref}^{commit}")"
if [[ ! "$upstream_commit" =~ ^[0-9a-f]{40}$ ]]; then
  fail "The fetched upstream tag did not resolve to a valid commit."
fi

write_result_summary() {
  local result="$1"
  shift
  append_summary \
    "### Reviewable upstream release sync" \
    "" \
    "- Result: **$result**" \
    "- Upstream release: [$upstream_tag]($release_url)" \
    "- Upstream commit: \`$upstream_commit\`" \
    "- Sync branch: \`$sync_branch\`" \
    "$@"
}

mark_validation_ref() {
  local ref="$1"
  local validation_sha
  validation_sha="$("$git_bin" rev-parse "${ref}^{commit}")"
  if [[ ! "$validation_sha" =~ ^[0-9a-f]{40}$ ]]; then
    fail "The sync branch did not resolve to a valid validation commit."
  fi
  if [[ -n "$output_file" ]]; then
    {
      printf 'sync_branch=%s\n' "$sync_branch"
      printf 'sync_sha=%s\n' "$validation_sha"
    } >> "$output_file"
  fi
}

if "$git_bin" merge-base --is-ancestor \
  "$upstream_commit" "refs/remotes/origin/$MAIN_BRANCH"; then
  write_result_summary \
    "no changes required" \
    "- The release commit is already an ancestor of \`$MAIN_BRANCH\`."
  exit 0
fi

list_release_pulls() {
  "$gh_bin" api --method GET "repos/$TARGET_REPOSITORY/pulls" \
    -f "state=all" \
    -f "head=$TARGET_OWNER:$sync_branch" \
    -f "base=$MAIN_BRANCH" \
    -f "per_page=100"
}

pulls_json="$(list_release_pulls)" || fail "Unable to inspect existing sync pull requests."
if ! "$jq_bin" -e 'type == "array"' >/dev/null 2>&1 <<< "$pulls_json"; then
  fail "The pull-request query returned an invalid response."
fi

open_pr_url="$("$jq_bin" -r --arg branch "$sync_branch" '
  [
    .[] |
    select(.state == "open") |
    select(.head.ref == $branch) |
    select(.base.ref == "main")
  ][0].html_url // ""
' <<< "$pulls_json")"

if [[ -n "$open_pr_url" ]]; then
  if [[ ! "$open_pr_url" =~ ^https://github\.com/As-tsaqib/picoclaw/pull/[0-9]+$ ]]; then
    fail "The existing pull request returned an invalid URL."
  fi
  write_result_summary \
    "existing pull request" \
    "- Pull request: $open_pr_url" \
    "- No branch or pull request was changed."
  exit 0
fi

closed_pr="$("$jq_bin" -c --arg branch "$sync_branch" '
  [
    .[] |
    select(.state == "closed") |
    select(.head.ref == $branch) |
    select(.base.ref == "main")
  ] |
  sort_by(.number // 0) |
  reverse |
  .[0] // empty
' <<< "$pulls_json")"

if [[ -n "$closed_pr" ]]; then
  closed_pr_url="$("$jq_bin" -r '.html_url // ""' <<< "$closed_pr")"
  merged_at="$("$jq_bin" -r '.merged_at // ""' <<< "$closed_pr")"
  if [[ ! "$closed_pr_url" =~ ^https://github\.com/As-tsaqib/picoclaw/pull/[0-9]+$ ]]; then
    fail "The closed pull request returned an invalid URL."
  fi
  if [[ -n "$merged_at" ]]; then
    closed_reason="The prior pull request was merged, but the release commit is not an ancestor of main. Inspect the merge strategy manually."
  else
    closed_reason="The prior pull request was closed without merge. Manual maintainer action is required."
  fi
  write_result_summary \
    "manual action required" \
    "- Prior pull request: $closed_pr_url" \
    "- $closed_reason" \
    "- The workflow did not recreate, reopen, or modify the pull request."
  exit 0
fi

build_pr_body() {
  cat <<EOF
## Upstream release import

This pull request imports the latest stable release from the canonical upstream repository. The upstream changes are untrusted until this fork's review and validation are complete.

- Upstream release: [$upstream_tag]($release_url)
- Upstream commit: \`$upstream_commit\`
- Created by: [GitHub Actions run $GITHUB_RUN_ID]($run_url)
- Source repository: \`$UPSTREAM_REPOSITORY\`
- Target repository: \`$TARGET_REPOSITORY\`

No auto-merge is enabled. A clean Git merge does **not** guarantee behavioral compatibility with this fork.

### Review checklist

- [ ] Configuration defaults and backward compatibility
- [ ] Session allocation, isolation, persistence, and reset behavior
- [ ] Agent prompts, context assembly, and tool exposure
- [ ] Curated memory, recall scoping, checkpoints, and evolution boundaries
- [ ] Dashboard API, configuration round trips, frontend behavior, and mobile layout
- [ ] Magisk/KSU module and Android runtime compatibility (review only; do not rebuild here)
- [ ] Upgrade, storage, schema, and configuration migration impact
- [ ] Supply-chain-sensitive workflow, dependency, script, and generated-file changes
EOF
}

create_pull_request() {
  local pr_body pr_response pr_url retry_pulls retry_url
  pr_body="$(build_pr_body)"

  if ! pr_response="$("$gh_bin" api --method POST "repos/$TARGET_REPOSITORY/pulls" \
    -f "title=chore: import upstream release $upstream_tag" \
    -f "head=$sync_branch" \
    -f "base=$MAIN_BRANCH" \
    -f "body=$pr_body")"; then
    retry_pulls="$(list_release_pulls)" || fail "Pull-request creation failed and its state could not be checked."
    retry_url="$("$jq_bin" -r --arg branch "$sync_branch" '
      [.[] | select(.state == "open" and .head.ref == $branch and .base.ref == "main")][0].html_url // ""
    ' <<< "$retry_pulls")"
    if [[ "$retry_url" =~ ^https://github\.com/As-tsaqib/picoclaw/pull/[0-9]+$ ]]; then
      write_result_summary \
        "existing pull request" \
        "- Pull request: $retry_url" \
        "- Another run created the pull request first."
      return 0
    fi
    fail "The sync branch was pushed, but pull-request creation failed. Manual action is required."
  fi

  pr_url="$("$jq_bin" -r '.html_url // ""' <<< "$pr_response")"
  if [[ ! "$pr_url" =~ ^https://github\.com/As-tsaqib/picoclaw/pull/[0-9]+$ ]]; then
    fail "Pull-request creation returned an invalid URL."
  fi

  write_result_summary \
    "pull request created" \
    "- Pull request: $pr_url" \
    "- Review and required validation must complete before any merge."
}

set +e
"$git_bin" ls-remote --exit-code --heads origin \
  "refs/heads/$sync_branch" > "$tmp_dir/existing-branch"
branch_lookup_status=$?
set -e

if (( branch_lookup_status == 0 )); then
  "$git_bin" fetch --no-tags origin \
    "refs/heads/$sync_branch:refs/remotes/origin/$sync_branch"
  remote_sync_ref="refs/remotes/origin/$sync_branch"
  parents="$("$git_bin" rev-list --parents -n 1 "$remote_sync_ref")"
  read -r branch_tip first_parent second_parent extra_parent <<< "$parents"
  branch_subject="$("$git_bin" log -1 --format=%s "$remote_sync_ref")"

  if [[ -z "${branch_tip:-}" || -z "${first_parent:-}" || -z "${second_parent:-}" ||
        -n "${extra_parent:-}" || "$second_parent" != "$upstream_commit" ||
        "$branch_subject" != "chore: merge upstream release $upstream_tag" ]] ||
    ! "$git_bin" merge-base --is-ancestor \
      "$first_parent" "refs/remotes/origin/$MAIN_BRANCH"; then
    write_result_summary \
      "manual action required" \
      "- The sync branch already exists without a pull request and does not match the expected merge structure." \
      "- The workflow did not force-push or alter the existing branch."
    exit 0
  fi

  mark_validation_ref "$remote_sync_ref"
  create_pull_request
  exit 0
elif (( branch_lookup_status != 2 )); then
  fail "Unable to inspect the remote sync branch."
fi

"$git_bin" -c core.hooksPath=/dev/null switch --detach \
  "refs/remotes/origin/$MAIN_BRANCH" >/dev/null
"$git_bin" -c core.hooksPath=/dev/null switch -c "$sync_branch" >/dev/null
"$git_bin" config --local user.name 'github-actions[bot]'
"$git_bin" config --local user.email '41898282+github-actions[bot]@users.noreply.github.com'

merge_log="$tmp_dir/merge.log"
set +e
"$git_bin" -c core.hooksPath=/dev/null merge \
  --no-ff \
  --no-edit \
  --strategy=ort \
  -m "chore: merge upstream release $upstream_tag" \
  "$upstream_commit" > "$merge_log" 2>&1
merge_status=$?
set -e

if (( merge_status != 0 )); then
  mapfile -d '' -t conflict_paths < <(
    "$git_bin" diff --name-only --diff-filter=U -z --
  )
  if (( ${#conflict_paths[@]} == 0 )); then
    if "$git_bin" rev-parse -q --verify MERGE_HEAD >/dev/null 2>&1; then
      "$git_bin" merge --abort >/dev/null 2>&1 || true
    fi
    fail "The upstream merge failed without a reportable file conflict. No branch was pushed."
  fi

  conflict_lines=""
  conflict_limit=20
  for ((i = 0; i < ${#conflict_paths[@]} && i < conflict_limit; i++)); do
    safe_path="$(safe_for_log "${conflict_paths[$i]}")"
    conflict_lines+="- \`$safe_path\`"$'\n'
  done
  if (( ${#conflict_paths[@]} > conflict_limit )); then
    omitted_count=$((${#conflict_paths[@]} - conflict_limit))
    conflict_lines+="- ... and $omitted_count more conflicted file(s)"$'\n'
  fi

  if ! "$git_bin" merge --abort >/dev/null 2>&1; then
    fail "The merge conflicted and git merge --abort failed. Nothing was pushed."
  fi

  issue_title="[Upstream Sync] Conflict while importing $upstream_tag"
  issue_body="$(cat <<EOF
## Upstream release merge conflict

The reviewable upstream sync could not merge this release automatically. No sync branch was pushed and \`main\` was not changed.

- Upstream release: [$upstream_tag]($release_url)
- Upstream commit: \`$upstream_commit\`
- Workflow run: [GitHub Actions run $GITHUB_RUN_ID]($run_url)

### Conflicted files (bounded)

$conflict_lines
### Manual resolution

1. A maintainer should create a fresh branch from the current fork \`main\`.
2. Fetch and verify the upstream release tag and commit shown above.
3. Merge and resolve each conflict manually; do not copy private configuration or credentials into the branch.
4. Review behavioral compatibility and open a normal pull request in \`$TARGET_REPOSITORY\`.
5. Run all required untrusted-PR validation before considering merge.

Do not resolve this by pushing directly to \`main\` or by force-pushing an already reviewed sync branch.
EOF
)"

  issue_query="repo:$TARGET_REPOSITORY is:issue in:title \"$issue_title\""
  issues_json="$("$gh_bin" api --method GET "search/issues" \
    -f "q=$issue_query" \
    -f "per_page=100")" || fail "The merge conflicted, and existing conflict issues could not be inspected."
  if ! "$jq_bin" -e 'type == "object" and (.items | type == "array")' \
    >/dev/null 2>&1 <<< "$issues_json"; then
    fail "The merge conflicted, and the issue query returned an invalid response."
  fi

  existing_issue_number="$("$jq_bin" -r --arg title "$issue_title" '
    [
      .items[] |
      select(has("pull_request") | not) |
      select(.title == $title)
    ] |
    sort_by(.number // 0) |
    .[0].number // ""
  ' <<< "$issues_json")"

  if [[ -n "$existing_issue_number" ]]; then
    if [[ ! "$existing_issue_number" =~ ^[0-9]+$ ]]; then
      fail "The merge conflicted, and the matching issue number was invalid."
    fi
    issue_response="$("$gh_bin" api --method PATCH \
      "repos/$TARGET_REPOSITORY/issues/$existing_issue_number" \
      -f "title=$issue_title" \
      -f "body=$issue_body" \
      -f "state=open")" || fail "The merge conflicted, and its existing issue could not be updated."
  else
    issue_response="$("$gh_bin" api --method POST "repos/$TARGET_REPOSITORY/issues" \
      -f "title=$issue_title" \
      -f "body=$issue_body")" || fail "The merge conflicted, and its issue could not be created."
  fi

  issue_url="$("$jq_bin" -r '.html_url // ""' <<< "$issue_response")"
  if [[ ! "$issue_url" =~ ^https://github\.com/As-tsaqib/picoclaw/issues/[0-9]+$ ]]; then
    fail "The merge conflicted, and issue reporting returned an invalid URL."
  fi

  write_result_summary \
    "failed: manual conflict resolution required" \
    "- Conflict issue: $issue_url" \
    "- Conflicted files (maximum $conflict_limit shown):" \
    "$conflict_lines" \
    "- The merge was aborted. No branch was pushed and \`main\` was not changed."
  exit 1
fi

# Re-check main immediately before the only push, in case a maintainer imported
# the release while this run was preparing the review branch.
"$git_bin" fetch --no-tags origin \
  "refs/heads/$MAIN_BRANCH:refs/remotes/origin/$MAIN_BRANCH"
if "$git_bin" merge-base --is-ancestor \
  "$upstream_commit" "refs/remotes/origin/$MAIN_BRANCH"; then
  write_result_summary \
    "no changes required" \
    "- The release reached \`$MAIN_BRANCH\` while this run was in progress." \
    "- No sync branch was pushed."
  exit 0
fi

# Intentionally push only the validated sync branch. Never push or force-push main.
if [[ "$test_mode" != "1" ]]; then
  "$gh_bin" auth setup-git >/dev/null
fi
"$git_bin" push origin "HEAD:refs/heads/$sync_branch"
mark_validation_ref HEAD
create_pull_request
