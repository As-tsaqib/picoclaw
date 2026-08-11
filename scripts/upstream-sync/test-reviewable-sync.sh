#!/usr/bin/env bash

set -Eeuo pipefail
export LC_ALL=C

repo_root="${UPSTREAM_SYNC_REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)}"
sync_script="$repo_root/scripts/upstream-sync/reviewable-sync.sh"
sync_workflow="$repo_root/.github/workflows/sync-upstream-release.yml"
validation_workflow="$repo_root/.github/workflows/upstream-sync-validation.yml"
pr_workflow="$repo_root/.github/workflows/pr.yml"

test_root="$(mktemp -d)"
trap 'rm -rf -- "$test_root"' EXIT

fail_test() {
  printf 'FAIL: %s\n' "$1" >&2
  exit 1
}

assert_equal() {
  local expected="$1"
  local actual="$2"
  local description="$3"
  [[ "$actual" == "$expected" ]] || fail_test "$description (expected '$expected', got '$actual')"
}

assert_contains() {
  local file="$1"
  local expected="$2"
  local description="$3"
  grep -F -- "$expected" "$file" >/dev/null || fail_test "$description"
}

assert_not_contains() {
  local file="$1"
  local rejected="$2"
  local description="$3"
  if grep -F -- "$rejected" "$file" >/dev/null; then
    fail_test "$description"
  fi
}

assert_ref_exists() {
  local bare_repo="$1"
  local ref="$2"
  local description="$3"
  git --git-dir="$bare_repo" show-ref --verify --quiet "$ref" || fail_test "$description"
}

assert_ref_missing() {
  local bare_repo="$1"
  local ref="$2"
  local description="$3"
  if git --git-dir="$bare_repo" show-ref --verify --quiet "$ref"; then
    fail_test "$description"
  fi
}

configure_identity() {
  local repo="$1"
  git -C "$repo" config user.name 'Upstream Sync Test'
  git -C "$repo" config user.email 'upstream-sync-test@example.invalid'
}

write_fake_gh() {
  mkdir -p "$fixture_dir/fake-bin"
  cat > "$fixture_dir/fake-bin/gh" <<'FAKE_GH'
#!/usr/bin/env bash
set -Eeuo pipefail

[[ "${1:-}" == "api" ]] || exit 90
shift

method="GET"
endpoint=""
declare -a fields=()
while (($#)); do
  case "$1" in
    --method)
      method="$2"
      shift 2
      ;;
    -f|-F)
      fields+=("$2")
      shift 2
      ;;
    --paginate|--silent)
      shift
      ;;
    *)
      if [[ -z "$endpoint" ]]; then
        endpoint="$1"
      fi
      shift
      ;;
  esac
done

field_value() {
  local key="$1"
  local item
  for item in "${fields[@]}"; do
    if [[ "$item" == "$key="* ]]; then
      printf '%s' "${item#*=}"
      return 0
    fi
  done
  return 1
}

increment_file() {
  local file="$1"
  local count
  count="$(cat "$file")"
  printf '%s\n' "$((count + 1))" > "$file"
}

printf '%s %s\n' "$method" "$endpoint" >> "$FAKE_GH_CALLS"

case "$method $endpoint" in
  "GET repos/sipeed/picoclaw/releases/latest")
    cat "$FAKE_RELEASE_FILE"
    ;;
  "GET repos/As-tsaqib/picoclaw/pulls")
    cat "$FAKE_PULLS_FILE"
    ;;
  "POST repos/As-tsaqib/picoclaw/pulls")
    increment_file "$FAKE_PR_CREATE_COUNT"
    head_ref="$(field_value head)"
    base_ref="$(field_value base)"
    title="$(field_value title)"
    body="$(field_value body)"
    printf '%s' "$head_ref" > "$FAKE_LAST_PR_HEAD"
    printf '%s' "$base_ref" > "$FAKE_LAST_PR_BASE"
    printf '%s' "$title" > "$FAKE_LAST_PR_TITLE"
    printf '%s' "$body" > "$FAKE_LAST_PR_BODY"
    cat > "$FAKE_PULLS_FILE" <<JSON
[{"number":42,"state":"open","merged_at":null,"html_url":"https://github.com/As-tsaqib/picoclaw/pull/42","head":{"ref":"$head_ref"},"base":{"ref":"$base_ref"}}]
JSON
    printf '%s\n' '{"html_url":"https://github.com/As-tsaqib/picoclaw/pull/42"}'
    ;;
  "GET search/issues")
    printf '{"items":'
    cat "$FAKE_ISSUES_FILE"
    printf '}\n'
    ;;
  "POST repos/As-tsaqib/picoclaw/issues")
    increment_file "$FAKE_ISSUE_CREATE_COUNT"
    title="$(field_value title)"
    body="$(field_value body)"
    printf '%s' "$title" > "$FAKE_LAST_ISSUE_TITLE"
    printf '%s' "$body" > "$FAKE_LAST_ISSUE_BODY"
    cat > "$FAKE_ISSUES_FILE" <<JSON
[{"number":17,"state":"open","title":"$title","html_url":"https://github.com/As-tsaqib/picoclaw/issues/17"}]
JSON
    printf '%s\n' '{"html_url":"https://github.com/As-tsaqib/picoclaw/issues/17"}'
    ;;
  "PATCH repos/As-tsaqib/picoclaw/issues/17")
    increment_file "$FAKE_ISSUE_UPDATE_COUNT"
    title="$(field_value title)"
    body="$(field_value body)"
    printf '%s' "$title" > "$FAKE_LAST_ISSUE_TITLE"
    printf '%s' "$body" > "$FAKE_LAST_ISSUE_BODY"
    cat > "$FAKE_ISSUES_FILE" <<JSON
[{"number":17,"state":"open","title":"$title","html_url":"https://github.com/As-tsaqib/picoclaw/issues/17"}]
JSON
    printf '%s\n' '{"html_url":"https://github.com/As-tsaqib/picoclaw/issues/17"}'
    ;;
  *)
    printf 'Unexpected fake gh call: %s %s\n' "$method" "$endpoint" >&2
    exit 91
    ;;
esac
FAKE_GH
  chmod 0700 "$fixture_dir/fake-bin/gh"
}

new_fixture() {
  local name="$1"
  fixture_dir="$test_root/$name"
  fork_bare="$fixture_dir/fork.git"
  upstream_bare="$fixture_dir/upstream.git"
  release_file="$fixture_dir/release.json"
  pulls_file="$fixture_dir/pulls.json"
  issues_file="$fixture_dir/issues.json"
  calls_file="$fixture_dir/gh-calls.log"
  pr_create_count="$fixture_dir/pr-create-count"
  issue_create_count="$fixture_dir/issue-create-count"
  issue_update_count="$fixture_dir/issue-update-count"
  last_pr_head="$fixture_dir/last-pr-head"
  last_pr_base="$fixture_dir/last-pr-base"
  last_pr_title="$fixture_dir/last-pr-title"
  last_pr_body="$fixture_dir/last-pr-body"
  last_issue_title="$fixture_dir/last-issue-title"
  last_issue_body="$fixture_dir/last-issue-body"

  mkdir -p "$fixture_dir"
  git init --bare "$fork_bare" >/dev/null
  git init --bare "$upstream_bare" >/dev/null
  git init -b main "$fixture_dir/seed" >/dev/null
  configure_identity "$fixture_dir/seed"
  printf 'base\n' > "$fixture_dir/seed/shared.txt"
  git -C "$fixture_dir/seed" add shared.txt
  git -C "$fixture_dir/seed" commit -m 'base' >/dev/null
  git -C "$fixture_dir/seed" remote add fork "$fork_bare"
  git -C "$fixture_dir/seed" remote add canonical "$upstream_bare"
  git -C "$fixture_dir/seed" push fork main >/dev/null
  git -C "$fixture_dir/seed" push canonical main >/dev/null
  git --git-dir="$fork_bare" symbolic-ref HEAD refs/heads/main
  git --git-dir="$upstream_bare" symbolic-ref HEAD refs/heads/main

  printf '[]\n' > "$pulls_file"
  printf '[]\n' > "$issues_file"
  : > "$calls_file"
  printf '0\n' > "$pr_create_count"
  printf '0\n' > "$issue_create_count"
  printf '0\n' > "$issue_update_count"
  : > "$last_pr_head"
  : > "$last_pr_base"
  : > "$last_pr_title"
  : > "$last_pr_body"
  : > "$last_issue_title"
  : > "$last_issue_body"
  write_fake_gh
}

tag_upstream_base() {
  git clone "$upstream_bare" "$fixture_dir/upstream-work" >/dev/null 2>&1
  configure_identity "$fixture_dir/upstream-work"
  git -C "$fixture_dir/upstream-work" tag v1.2.3
  git -C "$fixture_dir/upstream-work" push origin refs/tags/v1.2.3 >/dev/null
  upstream_sha="$(git -C "$fixture_dir/upstream-work" rev-parse HEAD)"
  cat > "$release_file" <<JSON
{"tag_name":"v1.2.3","draft":false,"prerelease":false,"html_url":"https://github.com/sipeed/picoclaw/releases/tag/v1.2.3"}
JSON
}

add_upstream_release() {
  local mode="$1"
  git clone "$upstream_bare" "$fixture_dir/upstream-work" >/dev/null 2>&1
  configure_identity "$fixture_dir/upstream-work"
  if [[ "$mode" == "conflict" ]]; then
    printf 'upstream version\n' > "$fixture_dir/upstream-work/shared.txt"
    git -C "$fixture_dir/upstream-work" add shared.txt
  else
    printf 'upstream release\n' > "$fixture_dir/upstream-work/upstream.txt"
    git -C "$fixture_dir/upstream-work" add upstream.txt
  fi
  git -C "$fixture_dir/upstream-work" commit -m 'upstream release' >/dev/null
  git -C "$fixture_dir/upstream-work" tag v1.2.3
  git -C "$fixture_dir/upstream-work" push origin main refs/tags/v1.2.3 >/dev/null
  upstream_sha="$(git -C "$fixture_dir/upstream-work" rev-parse HEAD)"
  cat > "$release_file" <<JSON
{"tag_name":"v1.2.3","draft":false,"prerelease":false,"html_url":"https://github.com/sipeed/picoclaw/releases/tag/v1.2.3"}
JSON
}

add_fork_change() {
  local mode="$1"
  git clone "$fork_bare" "$fixture_dir/fork-work" >/dev/null 2>&1
  configure_identity "$fixture_dir/fork-work"
  if [[ "$mode" == "conflict" ]]; then
    printf 'fork version\n' > "$fixture_dir/fork-work/shared.txt"
    git -C "$fixture_dir/fork-work" add shared.txt
  else
    printf 'fork customization\n' > "$fixture_dir/fork-work/fork.txt"
    git -C "$fixture_dir/fork-work" add fork.txt
  fi
  git -C "$fixture_dir/fork-work" commit -m 'fork customization' >/dev/null
  git -C "$fixture_dir/fork-work" push origin main >/dev/null
}

prepare_runner() {
  local name="$1"
  runner_dir="$fixture_dir/$name"
  git clone "$fork_bare" "$runner_dir" >/dev/null 2>&1
  configure_identity "$runner_dir"
  summary_file="$fixture_dir/$name-summary.md"
  output_file="$fixture_dir/$name-output.txt"
  : > "$summary_file"
  : > "$output_file"
}

invoke_sync() {
  (
    cd "$runner_dir"
    env -u GITHUB_ACTIONS \
      GITHUB_REPOSITORY='As-tsaqib/picoclaw' \
      GITHUB_RUN_ID='123456' \
      GITHUB_STEP_SUMMARY="$summary_file" \
      GITHUB_OUTPUT="$output_file" \
      UPSTREAM_SYNC_TEST_MODE='1' \
      UPSTREAM_SYNC_TEST_UPSTREAM_URL="$upstream_bare" \
      UPSTREAM_SYNC_TEST_GH_BIN="$fixture_dir/fake-bin/gh" \
      FAKE_RELEASE_FILE="$release_file" \
      FAKE_PULLS_FILE="$pulls_file" \
      FAKE_ISSUES_FILE="$issues_file" \
      FAKE_GH_CALLS="$calls_file" \
      FAKE_PR_CREATE_COUNT="$pr_create_count" \
      FAKE_ISSUE_CREATE_COUNT="$issue_create_count" \
      FAKE_ISSUE_UPDATE_COUNT="$issue_update_count" \
      FAKE_LAST_PR_HEAD="$last_pr_head" \
      FAKE_LAST_PR_BASE="$last_pr_base" \
      FAKE_LAST_PR_TITLE="$last_pr_title" \
      FAKE_LAST_PR_BODY="$last_pr_body" \
      FAKE_LAST_ISSUE_TITLE="$last_issue_title" \
      FAKE_LAST_ISSUE_BODY="$last_issue_body" \
      bash "$sync_script"
  )
}

test_static_workflow_policy() {
  assert_contains "$sync_workflow" 'cron: "47 1 * * *"' 'daily schedule is missing'
  assert_contains "$sync_workflow" 'workflow_dispatch: {}' 'manual dispatch is missing'
  assert_contains "$sync_workflow" "if: github.repository == 'As-tsaqib/picoclaw'" 'repository mutation guard is missing'
  assert_contains "$sync_workflow" 'group: sync-upstream-release' 'sync concurrency group is missing'
  assert_contains "$sync_workflow" 'contents: write' 'sync branch write permission is missing'
  assert_contains "$sync_workflow" 'pull-requests: write' 'PR permission is missing'
  assert_contains "$sync_workflow" 'issues: write' 'conflict issue permission is missing'
  assert_contains "$sync_script" 'repos/$UPSTREAM_REPOSITORY/releases/latest' 'latest release API is missing'
  assert_contains "$sync_script" 'HEAD:refs/heads/$sync_branch' 'sync push target is missing'
  assert_not_contains "$sync_script" 'HEAD:main' 'direct HEAD-to-main push is forbidden'
  assert_not_contains "$sync_script" 'refs/heads/main"' 'a literal main push ref is forbidden'
  assert_not_contains "$sync_script" '--force' 'force operations are forbidden in the sync helper'
  push_command_count="$(grep -F -c '"$git_bin" push origin' "$sync_script" || true)"
  assert_equal '1' "$push_command_count" 'the helper must have exactly one explicit push command'
  assert_not_contains "$sync_script" 'auto_merge=' 'the helper must not enable auto-merge through the API'
  assert_not_contains "$sync_script" ' pr merge' 'the helper must not invoke an auto-merge command'

  assert_contains "$validation_workflow" 'workflow_call:' 'same-run validation call is missing'
  assert_contains "$validation_workflow" 'pull_request:' 'sync PR validation trigger is missing'
  assert_contains "$validation_workflow" 'contents: read' 'validation must be read-only'
  assert_contains "$validation_workflow" 'persist-credentials: false' 'validation checkout credentials must not persist'
  assert_not_contains "$validation_workflow" 'secrets.' 'validation must not receive repository secrets'
  assert_contains "$pr_workflow" 'contents: read' 'generic PR validation must be read-only'
  assert_not_contains "$pr_workflow" 'secrets.' 'generic PR validation must not receive secrets'

  if grep -R -E '^[[:space:]]*pull_request_target:' "$repo_root/.github/workflows" >/dev/null; then
    fail_test 'pull_request_target must not execute sync code'
  fi
  if grep -E 'uses:[[:space:]]+[^[:space:]]+@v[0-9]+' \
    "$sync_workflow" "$validation_workflow" "$pr_workflow" >/dev/null; then
    fail_test 'third-party actions in changed workflows must be pinned to commit SHAs'
  fi

  local release_workflow
  for release_workflow in \
    create-tag.yml create_dmg.yml docker-build.yml nightly.yml release.yml upload-tos.yml; do
    assert_not_contains \
      "$repo_root/.github/workflows/$release_workflow" \
      'sync/upstream-' \
      "$release_workflow must not target sync branches"
    if grep -E '^  (pull_request(_target)?|push):' \
      "$repo_root/.github/workflows/$release_workflow" >/dev/null; then
      fail_test "$release_workflow must not run for sync pushes or pull requests"
    fi
  done
}

test_already_imported_noop() {
  new_fixture noop
  tag_upstream_base
  prepare_runner runner
  main_before="$(git --git-dir="$fork_bare" rev-parse refs/heads/main)"

  invoke_sync

  main_after="$(git --git-dir="$fork_bare" rev-parse refs/heads/main)"
  assert_equal "$main_before" "$main_after" 'no-op changed fork main'
  assert_ref_missing "$fork_bare" 'refs/heads/sync/upstream-v1.2.3' 'no-op created a sync branch'
  assert_equal '0' "$(cat "$pr_create_count")" 'no-op created a pull request'
  assert_contains "$summary_file" 'no changes required' 'no-op summary is unclear'
}

test_new_release_and_duplicate_run() {
  new_fixture new-release
  add_upstream_release normal
  add_fork_change normal
  prepare_runner runner-one
  main_before="$(git --git-dir="$fork_bare" rev-parse refs/heads/main)"

  invoke_sync

  main_after="$(git --git-dir="$fork_bare" rev-parse refs/heads/main)"
  assert_equal "$main_before" "$main_after" 'new release changed fork main'
  sync_ref='refs/heads/sync/upstream-v1.2.3'
  assert_ref_exists "$fork_bare" "$sync_ref" 'new release did not create the sync branch'
  sync_tip="$(git --git-dir="$fork_bare" rev-parse "$sync_ref")"
  parent_line="$(git --git-dir="$fork_bare" rev-list --parents -n 1 "$sync_ref")"
  read -r tip first_parent second_parent extra_parent <<< "$parent_line"
  assert_equal "$sync_tip" "$tip" 'sync tip did not resolve consistently'
  assert_equal "$upstream_sha" "$second_parent" 'upstream release is not the merge second parent'
  assert_equal '' "${extra_parent:-}" 'sync commit has an unexpected extra parent'
  assert_equal 'chore: merge upstream release v1.2.3' \
    "$(git --git-dir="$fork_bare" log -1 --format=%s "$sync_ref")" \
    'sync merge message is unclear'
  assert_equal '1' "$(cat "$pr_create_count")" 'new release did not create exactly one PR'
  assert_equal 'sync/upstream-v1.2.3' "$(cat "$last_pr_head")" 'PR head is incorrect'
  assert_equal 'main' "$(cat "$last_pr_base")" 'PR base is incorrect'
  assert_contains "$calls_file" 'POST repos/As-tsaqib/picoclaw/pulls' 'PR targeted the wrong repository'
  assert_contains "$last_pr_body" 'https://github.com/sipeed/picoclaw/releases/tag/v1.2.3' 'PR lacks the release URL'
  assert_contains "$last_pr_body" "$upstream_sha" 'PR lacks the upstream SHA'
  assert_contains "$last_pr_body" 'GitHub Actions run 123456' 'PR lacks the workflow link'
  assert_contains "$last_pr_body" 'Configuration defaults' 'PR lacks configuration review'
  assert_contains "$last_pr_body" 'Session allocation' 'PR lacks session review'
  assert_contains "$last_pr_body" 'Agent prompts' 'PR lacks prompt review'
  assert_contains "$last_pr_body" 'Curated memory' 'PR lacks memory review'
  assert_contains "$last_pr_body" 'Dashboard API' 'PR lacks dashboard review'
  assert_contains "$last_pr_body" 'Magisk/KSU module' 'PR lacks module compatibility review'
  assert_contains "$last_pr_body" 'migration impact' 'PR lacks migration review'
  assert_contains "$last_pr_body" 'does **not** guarantee behavioral compatibility' 'PR lacks semantic warning'
  assert_contains "$output_file" "sync_sha=$sync_tip" 'sync SHA was not handed to read-only validation'

  prepare_runner runner-two
  invoke_sync
  assert_equal '1' "$(cat "$pr_create_count")" 'repeat run created a duplicate PR'
  assert_equal "$sync_tip" \
    "$(git --git-dir="$fork_bare" rev-parse "$sync_ref")" \
    'repeat run changed the reviewed sync branch'
  assert_contains "$summary_file" 'existing pull request' 'repeat run did not report the existing PR'
}

test_closed_pr_requires_manual_action() {
  new_fixture closed-pr
  add_upstream_release normal
  add_fork_change normal
  prepare_runner runner
  cat > "$pulls_file" <<'JSON'
[{"number":9,"state":"closed","merged_at":null,"html_url":"https://github.com/As-tsaqib/picoclaw/pull/9","head":{"ref":"sync/upstream-v1.2.3"},"base":{"ref":"main"}}]
JSON

  invoke_sync

  assert_ref_missing "$fork_bare" 'refs/heads/sync/upstream-v1.2.3' 'closed PR was recreated as a branch'
  assert_equal '0' "$(cat "$pr_create_count")" 'closed PR was recreated'
  assert_contains "$summary_file" 'closed without merge' 'closed PR did not request manual action'
}

test_conflict_is_aborted_and_deduplicated() {
  new_fixture conflict
  add_upstream_release conflict
  add_fork_change conflict
  prepare_runner runner-one
  main_before="$(git --git-dir="$fork_bare" rev-parse refs/heads/main)"

  if invoke_sync; then
    fail_test 'conflicting release unexpectedly succeeded'
  fi

  main_after="$(git --git-dir="$fork_bare" rev-parse refs/heads/main)"
  assert_equal "$main_before" "$main_after" 'conflict changed fork main'
  assert_ref_missing "$fork_bare" 'refs/heads/sync/upstream-v1.2.3' 'conflict pushed a sync branch'
  assert_equal '1' "$(cat "$issue_create_count")" 'conflict did not create one issue'
  assert_equal '0' "$(cat "$issue_update_count")" 'first conflict unexpectedly updated an issue'
  assert_contains "$last_issue_title" '[Upstream Sync] Conflict while importing v1.2.3' 'conflict issue title is incorrect'
  assert_contains "$last_issue_body" "$upstream_sha" 'conflict issue lacks upstream SHA'
  assert_contains "$last_issue_body" 'shared.txt' 'conflict issue lacks bounded path provenance'
  assert_not_contains "$last_issue_body" 'upstream version' 'conflict issue leaked file content'
  assert_contains "$summary_file" 'The merge was aborted' 'conflict summary does not confirm abort'

  prepare_runner runner-two
  if invoke_sync; then
    fail_test 'repeated conflicting release unexpectedly succeeded'
  fi
  assert_equal '1' "$(cat "$issue_create_count")" 'repeated conflict created a duplicate issue'
  assert_equal '1' "$(cat "$issue_update_count")" 'repeated conflict did not update the existing issue'
  assert_equal "$main_before" \
    "$(git --git-dir="$fork_bare" rev-parse refs/heads/main)" \
    'repeated conflict changed fork main'
}

test_invalid_tag_is_rejected() {
  new_fixture invalid-tag
  prepare_runner runner
  cat > "$release_file" <<'JSON'
{"tag_name":"v1.2.3$(touch-pwned)","draft":false,"prerelease":false,"html_url":"https://example.invalid"}
JSON

  invalid_log="$fixture_dir/invalid-tag.log"
  if invoke_sync > "$invalid_log" 2>&1; then
    fail_test 'invalid tag unexpectedly succeeded'
  fi

  assert_contains "$invalid_log" 'release tag is invalid' 'invalid tag failure was not reported'
  assert_ref_missing "$fork_bare" 'refs/heads/sync/upstream-v1.2.3$(touch-pwned)' 'invalid tag created a branch'
  assert_equal '0' "$(cat "$pr_create_count")" 'invalid tag created a PR'
  assert_equal '0' "$(cat "$issue_create_count")" 'invalid tag created an issue'
  if git -C "$runner_dir" remote get-url canonical >/dev/null 2>&1; then
    fail_test 'invalid tag was used after validation'
  fi
}

test_existing_unexpected_branch_is_not_force_pushed() {
  new_fixture existing-branch
  add_upstream_release normal
  add_fork_change normal
  git clone "$fork_bare" "$fixture_dir/manual-branch" >/dev/null 2>&1
  configure_identity "$fixture_dir/manual-branch"
  git -C "$fixture_dir/manual-branch" switch -c sync/upstream-v1.2.3 >/dev/null
  printf 'manual review state\n' > "$fixture_dir/manual-branch/manual.txt"
  git -C "$fixture_dir/manual-branch" add manual.txt
  git -C "$fixture_dir/manual-branch" commit -m 'manual reviewed branch' >/dev/null
  git -C "$fixture_dir/manual-branch" push origin sync/upstream-v1.2.3 >/dev/null
  branch_before="$(git --git-dir="$fork_bare" rev-parse refs/heads/sync/upstream-v1.2.3)"
  prepare_runner runner

  invoke_sync

  assert_equal "$branch_before" \
    "$(git --git-dir="$fork_bare" rev-parse refs/heads/sync/upstream-v1.2.3)" \
    'existing reviewed branch was changed'
  assert_equal '0' "$(cat "$pr_create_count")" 'unexpected existing branch received an automatic PR'
  assert_contains "$summary_file" 'did not force-push' 'existing branch safety was not reported'
}

test_static_workflow_policy
test_already_imported_noop
test_new_release_and_duplicate_run
test_closed_pr_requires_manual_action
test_conflict_is_aborted_and_deduplicated
test_invalid_tag_is_rejected
test_existing_unexpected_branch_is_not_force_pushed

printf 'All reviewable upstream sync tests passed.\n'
