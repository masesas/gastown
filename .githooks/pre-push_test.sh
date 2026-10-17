#!/bin/bash
# Test suite for the pre-push hook integration branch guardrails.
# Creates temporary git repos to simulate push scenarios.
#
# Usage: bash .githooks/pre-push_test.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
HOOK="$SCRIPT_DIR/pre-push"
PASS=0
FAIL=0
TMPDIR=""
DEFAULT_BRANCH=""

cleanup() {
  cd /tmp  # Ensure CWD exists before removing TMPDIR
  if [[ -n "$TMPDIR" && -d "$TMPDIR" ]]; then
    rm -rf "$TMPDIR"
  fi
  TMPDIR=""
}
trap cleanup EXIT

setup_repos() {
  TMPDIR=$(mktemp -d)
  # Create a bare "remote" repo
  git init --bare "$TMPDIR/remote.git" >/dev/null 2>&1
  # Clone it as the "local" repo
  git clone "$TMPDIR/remote.git" "$TMPDIR/local" >/dev/null 2>&1
  cd "$TMPDIR/local"
  git config user.email "test@test.com"
  git config user.name "Test"
  # Initial commit
  echo "init" > file.txt
  git add file.txt
  git commit -m "initial" >/dev/null 2>&1
  # Detect the default branch name (main or master)
  DEFAULT_BRANCH=$(git branch --show-current)
  git push origin "$DEFAULT_BRANCH" >/dev/null 2>&1
  # Set up origin/HEAD so hook can detect default branch
  git remote set-head origin "$DEFAULT_BRANCH" >/dev/null 2>&1
  # Copy the hook
  cp "$HOOK" "$TMPDIR/local/.git/hooks/pre-push"
  chmod +x "$TMPDIR/local/.git/hooks/pre-push"
}

run_hook() {
  # Simulate pre-push stdin: local_ref local_sha remote_ref remote_sha
  local local_ref=$1 local_sha=$2 remote_ref=$3 remote_sha=$4
  echo "$local_ref $local_sha $remote_ref $remote_sha" | bash "$HOOK" "origin" 2>&1
}

get_sha() {
  git rev-parse "$1"
}

assert_pass() {
  local test_name=$1
  shift
  if "$@" >/dev/null 2>&1; then
    echo "  PASS: $test_name"
    PASS=$((PASS + 1))
  else
    echo "  FAIL: $test_name (expected pass, got block)"
    FAIL=$((FAIL + 1))
  fi
}

assert_block() {
  local test_name=$1
  shift
  if "$@" >/dev/null 2>&1; then
    echo "  FAIL: $test_name (expected block, got pass)"
    FAIL=$((FAIL + 1))
  else
    echo "  PASS: $test_name"
    PASS=$((PASS + 1))
  fi
}

# Run as a human: agent identity variables change the landing rules.
unset GT_ROLE BD_ACTOR GT_POLECAT GT_CREW GT_INTEGRATION_LAND 2>/dev/null || true

echo "=== Pre-push hook test suite ==="
echo ""

# Test 1: Normal push to default branch (no integration content)
echo "Test 1: Normal push to default branch (no integration content)"
setup_repos
cd "$TMPDIR/local"
remote_sha=$(get_sha HEAD)
echo "change1" >> file.txt
git add file.txt && git commit -m "normal change" >/dev/null 2>&1
local_sha=$(get_sha HEAD)
assert_pass "Normal push allowed" run_hook "refs/heads/$DEFAULT_BRANCH" "$local_sha" "refs/heads/$DEFAULT_BRANCH" "$remote_sha"
cleanup

# Test 2: Push to polecat/* branch
echo "Test 2: Push to polecat/* branch"
setup_repos
cd "$TMPDIR/local"
git checkout -b polecat/worker1 >/dev/null 2>&1
echo "polecat work" >> file.txt
git add file.txt && git commit -m "polecat work" >/dev/null 2>&1
local_sha=$(get_sha HEAD)
assert_pass "Polecat push allowed" run_hook "refs/heads/polecat/worker1" "$local_sha" "refs/heads/polecat/worker1" "0000000000000000000000000000000000000000"
cleanup

# Test 3: Push to integration/* branch
echo "Test 3: Push to integration/* branch"
setup_repos
cd "$TMPDIR/local"
git checkout -b integration/epic-1 >/dev/null 2>&1
echo "integration work" >> file.txt
git add file.txt && git commit -m "integration work" >/dev/null 2>&1
local_sha=$(get_sha HEAD)
assert_pass "Integration branch push allowed" run_hook "refs/heads/integration/epic-1" "$local_sha" "refs/heads/integration/epic-1" "0000000000000000000000000000000000000000"
cleanup

# Test 4: Push to feature/* without upstream remote (blocked)
echo "Test 4: Push to feature/* without upstream remote"
setup_repos
cd "$TMPDIR/local"
git checkout -b feature/thing >/dev/null 2>&1
echo "feature" >> file.txt
git add file.txt && git commit -m "feature" >/dev/null 2>&1
local_sha=$(get_sha HEAD)
assert_block "Feature branch blocked (no upstream)" run_hook "refs/heads/feature/thing" "$local_sha" "refs/heads/feature/thing" "0000000000000000000000000000000000000000"
cleanup

# Test 5: Push to feature/* with upstream remote (allowed)
echo "Test 5: Push to feature/* with upstream remote"
setup_repos
cd "$TMPDIR/local"
git remote add upstream "$TMPDIR/remote.git" >/dev/null 2>&1
git checkout -b feature/thing >/dev/null 2>&1
echo "feature" >> file.txt
git add file.txt && git commit -m "feature" >/dev/null 2>&1
local_sha=$(get_sha HEAD)
assert_pass "Feature branch allowed (upstream exists)" run_hook "refs/heads/feature/thing" "$local_sha" "refs/heads/feature/thing" "0000000000000000000000000000000000000000"
cleanup

# Test 6: Push to default branch with integration merge (no env var) — BLOCKED
echo "Test 6: Push to default branch with integration merge (no env var)"
setup_repos
cd "$TMPDIR/local"
# Create and push an integration branch
git checkout -b integration/epic-2 >/dev/null 2>&1
echo "epic work" >> file.txt
git add file.txt && git commit -m "epic work" >/dev/null 2>&1
git push origin integration/epic-2 >/dev/null 2>&1
# Fetch so refs/remotes/origin/integration/epic-2 exists
git fetch origin >/dev/null 2>&1
# Back to default branch, merge the integration branch
git checkout "$DEFAULT_BRANCH" >/dev/null 2>&1
remote_sha=$(get_sha HEAD)
git merge --no-ff integration/epic-2 -m "land integration" >/dev/null 2>&1
local_sha=$(get_sha HEAD)
unset GT_INTEGRATION_LAND 2>/dev/null || true
assert_block "Integration merge blocked (no env var)" run_hook "refs/heads/$DEFAULT_BRANCH" "$local_sha" "refs/heads/$DEFAULT_BRANCH" "$remote_sha"
cleanup

# Test 7: Push to default branch with integration merge + GT_INTEGRATION_LAND=1 — ALLOWED
echo "Test 7: Push to default branch with integration merge + GT_INTEGRATION_LAND=1"
setup_repos
cd "$TMPDIR/local"
git checkout -b integration/epic-3 >/dev/null 2>&1
echo "epic work" >> file.txt
git add file.txt && git commit -m "epic work" >/dev/null 2>&1
git push origin integration/epic-3 >/dev/null 2>&1
git fetch origin >/dev/null 2>&1
git checkout "$DEFAULT_BRANCH" >/dev/null 2>&1
remote_sha=$(get_sha HEAD)
git merge --no-ff integration/epic-3 -m "land integration" >/dev/null 2>&1
local_sha=$(get_sha HEAD)
GT_INTEGRATION_LAND=1 assert_pass "Integration merge allowed (env var set)" run_hook "refs/heads/$DEFAULT_BRANCH" "$local_sha" "refs/heads/$DEFAULT_BRANCH" "$remote_sha"
cleanup

# Test 8: Push to default branch with non-integration merge — allowed
echo "Test 8: Push to default branch with non-integration merge"
setup_repos
cd "$TMPDIR/local"
# Create a local feature branch and merge it (no need to push to origin)
git checkout -b feature/normal >/dev/null 2>&1
echo "feature work" >> file.txt
git add file.txt && git commit -m "feature work" >/dev/null 2>&1
git checkout "$DEFAULT_BRANCH" >/dev/null 2>&1
remote_sha=$(get_sha HEAD)
git merge --no-ff feature/normal -m "merge feature" >/dev/null 2>&1
local_sha=$(get_sha HEAD)
assert_pass "Non-integration merge allowed" run_hook "refs/heads/$DEFAULT_BRANCH" "$local_sha" "refs/heads/$DEFAULT_BRANCH" "$remote_sha"
cleanup

# Test 9: Tag push — allowed
echo "Test 9: Tag push"
setup_repos
cd "$TMPDIR/local"
local_sha=$(get_sha HEAD)
assert_pass "Tag push allowed" run_hook "refs/tags/v1.0.0" "$local_sha" "refs/tags/v1.0.0" "0000000000000000000000000000000000000000"
cleanup

# Test 10: Push to default branch with fast-forward integration merge (no merge commit) — BLOCKED
echo "Test 10: Push to default branch with ff integration merge (no merge commit)"
setup_repos
cd "$TMPDIR/local"
git checkout -b integration/epic-4 >/dev/null 2>&1
echo "epic ff work" >> file.txt
git add file.txt && git commit -m "epic ff work" >/dev/null 2>&1
git push origin integration/epic-4 >/dev/null 2>&1
git fetch origin >/dev/null 2>&1
git checkout "$DEFAULT_BRANCH" >/dev/null 2>&1
remote_sha=$(get_sha HEAD)
git merge --ff-only integration/epic-4 >/dev/null 2>&1
local_sha=$(get_sha HEAD)
unset GT_INTEGRATION_LAND 2>/dev/null || true
assert_block "FF integration merge blocked" run_hook "refs/heads/$DEFAULT_BRANCH" "$local_sha" "refs/heads/$DEFAULT_BRANCH" "$remote_sha"
cleanup

# Test 11: Off-branch push — HEAD on a session branch, pushing the default branch
# (the classic `git push origin main` from a feature branch). HEAD mismatch — BLOCKED.
echo "Test 11: Off-branch push (HEAD on session branch, pushing default)"
setup_repos
cd "$TMPDIR/local"
git checkout -b session/x >/dev/null 2>&1
echo "session work" >> file.txt
git add file.txt && git commit -m "session work" >/dev/null 2>&1
default_sha=$(get_sha "$DEFAULT_BRANCH")
unset GT_ALLOW_OFFBRANCH_PUSH 2>/dev/null || true
assert_block "Off-branch default push blocked (HEAD mismatch)" run_hook "refs/heads/$DEFAULT_BRANCH" "$default_sha" "refs/heads/$DEFAULT_BRANCH" "$default_sha"
cleanup

# Test 12: Off-branch push with GT_ALLOW_OFFBRANCH_PUSH=1 — ALLOWED (override).
echo "Test 12: Off-branch push with GT_ALLOW_OFFBRANCH_PUSH=1"
setup_repos
cd "$TMPDIR/local"
git checkout -b session/y >/dev/null 2>&1
echo "session work" >> file.txt
git add file.txt && git commit -m "session work" >/dev/null 2>&1
default_sha=$(get_sha "$DEFAULT_BRANCH")
GT_ALLOW_OFFBRANCH_PUSH=1 assert_pass "Off-branch push allowed with override" run_hook "refs/heads/$DEFAULT_BRANCH" "$default_sha" "refs/heads/$DEFAULT_BRANCH" "$default_sha"
cleanup

# Test 13: Off-branch deletion push (zero local sha) — not a HEAD mismatch, ALLOWED.
echo "Test 13: Off-branch deletion push (zero local sha)"
setup_repos
cd "$TMPDIR/local"
git checkout -b session/z >/dev/null 2>&1
default_sha=$(get_sha "$DEFAULT_BRANCH")
unset GT_ALLOW_OFFBRANCH_PUSH 2>/dev/null || true
assert_pass "Off-branch deletion not blocked by HEAD guard" run_hook "refs/heads/$DEFAULT_BRANCH" "0000000000000000000000000000000000000000" "refs/heads/$DEFAULT_BRANCH" "$default_sha"
cleanup

# Test 14: Agent landing with GT_INTEGRATION_LAND=1 — BLOCKED (gt-pd1c.72).
# GT_INTEGRATION_LAND is not a bypass for agents; outside a rig auto-land is
# off, so neither the Go guard nor the bash fallback lets an agent land.
echo "Test 14: Agent integration merge with GT_INTEGRATION_LAND=1"
setup_repos
cd "$TMPDIR/local"
git checkout -b integration/epic-5 >/dev/null 2>&1
echo "epic work" >> file.txt
git add file.txt && git commit -m "epic work" >/dev/null 2>&1
git push origin integration/epic-5 >/dev/null 2>&1
git checkout "$DEFAULT_BRANCH" >/dev/null 2>&1
remote_sha=$(get_sha HEAD)
git merge --no-ff integration/epic-5 -m "land integration" >/dev/null 2>&1
local_sha=$(get_sha HEAD)
GT_ROLE=refinery GT_INTEGRATION_LAND=1 assert_block "Agent land blocked despite GT_INTEGRATION_LAND=1" run_hook "refs/heads/$DEFAULT_BRANCH" "$local_sha" "refs/heads/$DEFAULT_BRANCH" "$remote_sha"
agent_out=$(GT_ROLE=refinery run_hook "refs/heads/$DEFAULT_BRANCH" "$local_sha" "refs/heads/$DEFAULT_BRANCH" "$remote_sha" || true)
if [[ "$agent_out" == *GT_INTEGRATION_LAND* ]]; then
  echo "  FAIL: agent refusal must not suggest GT_INTEGRATION_LAND"
  FAIL=$((FAIL + 1))
else
  echo "  PASS: agent refusal does not suggest GT_INTEGRATION_LAND"
  PASS=$((PASS + 1))
fi
# Agent deleting an integration branch that has not landed — BLOCKED.
GT_ROLE=refinery assert_block "Agent delete of unlanded integration branch blocked" run_hook "(delete)" "0000000000000000000000000000000000000000" "refs/heads/integration/epic-5" "$(get_sha integration/epic-5)"
# Agent rewinding the integration branch to the default branch tip — BLOCKED (B1).
GT_ROLE=refinery assert_block "Agent rewind of integration branch blocked" run_hook "refs/heads/$DEFAULT_BRANCH" "$remote_sha" "refs/heads/integration/epic-5" "$(get_sha integration/epic-5)"
# Agent normal push still allowed.
git checkout -b polecat/nux >/dev/null 2>&1
echo "polecat" >> file.txt
git add file.txt && git commit -m "polecat" >/dev/null 2>&1
GT_ROLE=polecat assert_pass "Agent polecat push allowed" run_hook "refs/heads/polecat/nux" "$(get_sha HEAD)" "refs/heads/polecat/nux" "0000000000000000000000000000000000000000"
cleanup

# Test 15: coordinator rules of the bash fallback (gt-pd1c.78), run with no gt
# on PATH so the fallback is what decides.
echo "Test 15: Fallback coordinator rules (no gt on PATH)"
setup_repos
cd "$TMPDIR/local"
nogt="$TMPDIR/nogt-bin"
mkdir -p "$nogt"
for tool in git sed awk cat; do
  ln -s "$(command -v "$tool")" "$nogt/$tool"
done
run_hook_nogt() {
  local local_ref=$1 local_sha=$2 remote_ref=$3 remote_sha=$4
  echo "$local_ref $local_sha $remote_ref $remote_sha" | PATH="$nogt" "$BASH" "$HOOK" "origin" 2>&1
}
base_sha=$(get_sha HEAD)
git checkout -b polecat/nux >/dev/null 2>&1
echo "polecat" >> file.txt
git add file.txt && git commit -m "polecat" >/dev/null 2>&1
pc_sha=$(get_sha HEAD)
git push origin polecat/nux >/dev/null 2>&1
git checkout "$DEFAULT_BRANCH" >/dev/null 2>&1
echo "main change" >> file.txt
git add file.txt && git commit -m "main change" >/dev/null 2>&1
main_sha=$(get_sha HEAD)
zero15=0000000000000000000000000000000000000000
GT_ROLE=gastown/witness assert_block "Witness delete of polecat/* blocked without gt" run_hook_nogt "(delete)" "$zero15" "refs/heads/polecat/nux" "$pc_sha"
GT_ROLE=gastown/refinery assert_block "Refinery delete of polecat/* blocked without gt" run_hook_nogt "(delete)" "$zero15" "refs/heads/polecat/nux" "$pc_sha"
BD_ACTOR=gastown/polecats/nux assert_block "Polecat (BD_ACTOR) delete of polecat/* blocked without gt" run_hook_nogt "(delete)" "$zero15" "refs/heads/polecat/nux" "$pc_sha"
GT_ROLE=gastown/crew/max GT_POLECAT=nux assert_block "Polecat claiming crew blocked" run_hook_nogt "(delete)" "$zero15" "refs/heads/polecat/nux" "$pc_sha"
GT_ROLE=gastown/crew/max assert_pass "Crew delete of polecat/* allowed" run_hook_nogt "(delete)" "$zero15" "refs/heads/polecat/nux" "$pc_sha"
GT_CREW=max assert_pass "Crew (GT_CREW only) delete of polecat/* allowed" run_hook_nogt "(delete)" "$zero15" "refs/heads/polecat/nux" "$pc_sha"
BD_ACTOR=overseer assert_pass "Overseer delete of polecat/* allowed" run_hook_nogt "(delete)" "$zero15" "refs/heads/polecat/nux" "$pc_sha"
assert_pass "Human delete of polecat/* allowed" run_hook_nogt "(delete)" "$zero15" "refs/heads/polecat/nux" "$pc_sha"
GT_ROLE=mayor assert_block "Mayor push to default branch blocked" run_hook_nogt "refs/heads/$DEFAULT_BRANCH" "$main_sha" "refs/heads/$DEFAULT_BRANCH" "$base_sha"
GT_ROLE=gastown/witness assert_block "Witness push to default branch blocked" run_hook_nogt "refs/heads/$DEFAULT_BRANCH" "$main_sha" "refs/heads/$DEFAULT_BRANCH" "$base_sha"
GT_ROLE=gastown/polecats/nux assert_block "Polecat push to default branch blocked without gt" run_hook_nogt "refs/heads/$DEFAULT_BRANCH" "$main_sha" "refs/heads/$DEFAULT_BRANCH" "$base_sha"
GT_ROLE=deacon assert_block "Deacon push to integration/* blocked" run_hook_nogt "refs/heads/$DEFAULT_BRANCH" "$main_sha" "refs/heads/integration/x" "$zero15"
GT_ROLE=gastown/refinery assert_pass "Refinery push to default branch unchanged" run_hook_nogt "refs/heads/$DEFAULT_BRANCH" "$main_sha" "refs/heads/$DEFAULT_BRANCH" "$base_sha"
GT_ROLE=gastown/crew/max assert_pass "Crew push to default branch unchanged" run_hook_nogt "refs/heads/$DEFAULT_BRANCH" "$main_sha" "refs/heads/$DEFAULT_BRANCH" "$base_sha"
assert_pass "Human push to default branch unchanged" run_hook_nogt "refs/heads/$DEFAULT_BRANCH" "$main_sha" "refs/heads/$DEFAULT_BRANCH" "$base_sha"
GT_ROLE=gastown/witness assert_pass "Witness push of polecat/* allowed" run_hook_nogt "HEAD" "$pc_sha" "refs/heads/polecat/nux" "$zero15"
# Round 1 (H1): rewriting polecat/* (not a fast-forward) discards its old tip.
GT_ROLE=gastown/witness assert_block "Witness force-push over polecat/* blocked" run_hook_nogt "HEAD" "$main_sha" "refs/heads/polecat/nux" "$pc_sha"
GT_ROLE=gastown/polecats/max assert_block "Other polecat force-push over polecat/nux blocked" run_hook_nogt "HEAD" "$main_sha" "refs/heads/polecat/nux" "$pc_sha"
# tahap 2 S-M2: whether the owner's old tip was submitted needs the merge
# queue; without gt the owner's rewrite is refused too (unless archived).
GT_ROLE=gastown/polecats/nux GT_POLECAT=nux assert_block "Owning polecat force-push blocked without gt" run_hook_nogt "HEAD" "$main_sha" "refs/heads/polecat/nux" "$pc_sha"
GT_ROLE=gastown/polecats/nux assert_block "Owning polecat force-push of polecat/nux/gt-1 blocked without gt" run_hook_nogt "HEAD" "$main_sha" "refs/heads/polecat/nux/gt-1" "$pc_sha"
GT_ROLE=gastown/refinery assert_pass "Refinery force-push over polecat/* allowed" run_hook_nogt "HEAD" "$main_sha" "refs/heads/polecat/nux" "$pc_sha"
GT_ROLE=gastown/crew/max assert_pass "Crew force-push over polecat/* allowed" run_hook_nogt "HEAD" "$main_sha" "refs/heads/polecat/nux" "$pc_sha"
GT_ROLE=gastown/witness assert_pass "Witness fast-forward of polecat/* allowed" run_hook_nogt "HEAD" "$main_sha" "refs/heads/polecat/nux" "$base_sha"
# Round 1 (M2): conflicting identities are read the strictest way.
GT_ROLE=gastown/crew/max BD_ACTOR=gastown/witness assert_block "Crew GT_ROLE with witness BD_ACTOR is an agent" run_hook_nogt "(delete)" "$zero15" "refs/heads/polecat/nux" "$pc_sha"
GT_ROLE=gastown/crew/max BD_ACTOR=overseer assert_block "Overseer BD_ACTOR with a GT_ROLE is not exempt" run_hook_nogt "(delete)" "$zero15" "refs/heads/polecat/nux" "$pc_sha"
GT_CREW=max BD_ACTOR=overseer assert_pass "Overseer BD_ACTOR with GT_CREW only is exempt" run_hook_nogt "(delete)" "$zero15" "refs/heads/polecat/nux" "$pc_sha"
GT_ROLE=gastown/refinery BD_ACTOR=mayor assert_block "Conflicting agent roles cannot push to the default branch" run_hook_nogt "refs/heads/$DEFAULT_BRANCH" "$main_sha" "refs/heads/$DEFAULT_BRANCH" "$base_sha"
GT_ROLE=gastown/refinery GT_CREW=max assert_pass "Refinery GT_ROLE with GT_CREW is the refinery" run_hook_nogt "refs/heads/$DEFAULT_BRANCH" "$main_sha" "refs/heads/$DEFAULT_BRANCH" "$base_sha"
# gt-pd1c.78 tahap 2: archives (refs/gt/archive/*, gt polecat abandon).
archive_ref="refs/gt/archive/polecat/nux/1760000000-${pc_sha:0:12}"
# S-H1: a branch whose name merely ends in an archive ref is not an archive.
# (created in the bare remote directly: this repo's own hook refuses feature branches)
git -C "$TMPDIR/remote.git" update-ref "refs/heads/x/$archive_ref" "$pc_sha"
GT_ROLE=gastown/witness assert_block "Branch named like an archive is not an archive" run_hook_nogt "(delete)" "$zero15" "refs/heads/polecat/nux" "$pc_sha"
GT_ROLE=mayor assert_block "Agent archive creation blocked without gt" run_hook_nogt "HEAD" "$pc_sha" "$archive_ref" "$zero15"
GT_ROLE=gastown/witness assert_block "Agent delete of polecat/* with unarchived tip blocked" run_hook_nogt "(delete)" "$zero15" "refs/heads/polecat/nux" "$pc_sha"
git push origin "$pc_sha:$archive_ref" >/dev/null 2>&1
GT_ROLE=gastown/witness assert_pass "Agent delete of polecat/* with archived tip allowed" run_hook_nogt "(delete)" "$zero15" "refs/heads/polecat/nux" "$pc_sha"
GT_ROLE=gastown/witness assert_pass "Agent rewrite of polecat/* with archived tip allowed" run_hook_nogt "HEAD" "$main_sha" "refs/heads/polecat/nux" "$pc_sha"
GT_ROLE=mayor assert_block "Agent delete of an archive ref blocked" run_hook_nogt "(delete)" "$zero15" "$archive_ref" "$pc_sha"
GT_ROLE=gastown/refinery assert_block "Refinery delete of an archive ref blocked" run_hook_nogt "(delete)" "$zero15" "$archive_ref" "$pc_sha"
GT_ROLE=gastown/witness assert_block "Agent rewrite of an archive ref blocked" run_hook_nogt "HEAD" "$main_sha" "$archive_ref" "$pc_sha"
GT_ROLE=gastown/crew/max assert_pass "Crew prune of an archive ref allowed" run_hook_nogt "(delete)" "$zero15" "$archive_ref" "$pc_sha"
assert_pass "Human prune of an archive ref allowed" run_hook_nogt "(delete)" "$zero15" "$archive_ref" "$pc_sha"
assert_pass "Human archive creation allowed" run_hook_nogt "HEAD" "$pc_sha" "$archive_ref" "$zero15"
cleanup

# Test 16 (gt-pd1c.78 S-H1): a branch named "refs/heads/integration/x" (full
# name refs/heads/refs/heads/integration/x) is not an integration branch: a
# human merging it into the default branch is not landing integration work.
echo "Test 16: Fallback ignores branches named like integration branches"
setup_repos
cd "$TMPDIR/local"
nogt="$TMPDIR/nogt-bin"
mkdir -p "$nogt"
for tool in git sed awk cat; do
  ln -s "$(command -v "$tool")" "$nogt/$tool"
done
git checkout -b fake >/dev/null 2>&1
echo "fake" >> file.txt
git add file.txt && git commit -m "fake" >/dev/null 2>&1
git push origin "HEAD:refs/heads/polecat/fake" >/dev/null 2>&1
git -C "$TMPDIR/remote.git" update-ref "refs/heads/refs/heads/integration/fake" "$(get_sha HEAD)"
git checkout "$DEFAULT_BRANCH" >/dev/null 2>&1
remote_sha=$(get_sha HEAD)
git merge --no-ff fake -m "merge fake" >/dev/null 2>&1
local_sha=$(get_sha HEAD)
assert_pass "Branch named refs/heads/integration/* is not a land" run_hook_nogt "refs/heads/$DEFAULT_BRANCH" "$local_sha" "refs/heads/$DEFAULT_BRANCH" "$remote_sha"
git -C "$TMPDIR/remote.git" update-ref "refs/heads/integration/real" "$(get_sha fake)"
assert_block "A real integration branch is still a land" run_hook_nogt "refs/heads/$DEFAULT_BRANCH" "$local_sha" "refs/heads/$DEFAULT_BRANCH" "$remote_sha"
cleanup

# Test 17 (gt-pd1c.78 N1): the fallback never trusts the local origin/HEAD.
echo "Test 17: Fallback ignores a repointed local origin/HEAD"
setup_repos
cd "$TMPDIR/local"
nogt="$TMPDIR/nogt-bin"
mkdir -p "$nogt"
for tool in git sed awk cat; do
  ln -s "$(command -v "$tool")" "$nogt/$tool"
done
git checkout -b integration/epic-17 >/dev/null 2>&1
echo "epic 17" >> file.txt
git add file.txt && git commit -m "epic 17" >/dev/null 2>&1
git -C "$TMPDIR/remote.git" update-ref refs/heads/integration/epic-17 "$(get_sha HEAD)" 2>/dev/null || git push origin integration/epic-17 >/dev/null 2>&1
git checkout "$DEFAULT_BRANCH" >/dev/null 2>&1
remote_sha=$(get_sha HEAD)
git merge --no-ff integration/epic-17 -m "land 17" >/dev/null 2>&1
local_sha=$(get_sha HEAD)
git symbolic-ref refs/remotes/origin/HEAD refs/remotes/origin/zzz
# With an upstream remote any branch name is allowed, so only the default
# branch detection keeps main in the land check.
git remote add upstream "$TMPDIR/remote.git" >/dev/null 2>&1
assert_block "Land to the default branch still checked with origin/HEAD -> zzz" run_hook_nogt "refs/heads/$DEFAULT_BRANCH" "$local_sha" "refs/heads/$DEFAULT_BRANCH" "$remote_sha"
GT_ROLE=gastown/witness assert_block "Witness push to the default branch still refused with origin/HEAD -> zzz" run_hook_nogt "refs/heads/$DEFAULT_BRANCH" "$local_sha" "refs/heads/$DEFAULT_BRANCH" "$remote_sha"
# Remote HEAD unknown: main and master are both protected.
git -C "$TMPDIR/remote.git" symbolic-ref HEAD refs/heads/nonexistent
GT_ROLE=gastown/witness assert_block "Witness push to main refused when the remote names no HEAD" run_hook_nogt "refs/heads/$DEFAULT_BRANCH" "$local_sha" "refs/heads/$DEFAULT_BRANCH" "$remote_sha"
cleanup

echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="
if [[ $FAIL -gt 0 ]]; then
  exit 1
fi
