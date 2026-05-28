#!/usr/bin/env bash
# push.sh — one-keystroke git workflow: status → stage → commit → push.
#
# USAGE
#   ./scripts/push.sh                       Auto-generate message from changed files.
#   ./scripts/push.sh "your message"        Use that message verbatim.
#   ./scripts/push.sh -n                    Dry run; show what WOULD happen.
#   ./scripts/push.sh -n "your message"
#   ./scripts/push.sh --amend [msg]         Amend last commit, force-with-lease push.
#                                           Asks for confirmation first.
#
# DESIGN
#   - Always prints `git status --short` before doing anything so a
#     misplaced file is obvious in the half-second before you hit ↵.
#   - Auto-generated messages are "Update <basename>, <basename> ..."
#     based on the first few staged files; capped at 3 names + "(+N more)".
#   - Won't push to a detached HEAD.
#   - Won't amend a commit that wasn't authored by you, so a collaborator's
#     last commit cannot be silently rewritten.
#   - Won't run while you have an in-progress merge/rebase/cherry-pick.
#   - Uses `--force-with-lease` (never `--force`) for amends, so a
#     remote update since you last fetched will safely abort the push.

set -euo pipefail

# ── helpers ─────────────────────────────────────────────────────────
die() { printf 'push.sh: %s\n' "$*" >&2; exit 1; }
log() { printf '%s\n' "$*"; }

# ── arg parsing ─────────────────────────────────────────────────────
dry_run=false
amend=false
while [[ $# -gt 0 ]]; do
    case "$1" in
        -h|--help)
            sed -n '2,21p' "$0" | sed -E 's/^# ?//'
            exit 0
            ;;
        -n|--dry-run)
            dry_run=true; shift ;;
        --amend)
            amend=true; shift ;;
        --) shift; break ;;
        -*)
            die "unknown flag: $1 (use --help)" ;;
        *)
            break ;;
    esac
done
msg="${*:-}"

# ── repo sanity ─────────────────────────────────────────────────────
git rev-parse --git-dir >/dev/null 2>&1 \
    || die "not a git repo"
cd "$(git rev-parse --show-toplevel)"

# In-flight merge/rebase/cherry-pick? Bail rather than make a mess.
git_dir="$(git rev-parse --git-dir)"
for marker in MERGE_HEAD REBASE_HEAD CHERRY_PICK_HEAD REVERT_HEAD; do
    [[ -e "$git_dir/$marker" ]] && die "in-progress operation: $marker exists — finish or abort it first"
done

# Detached HEAD → no upstream to push to.
branch="$(git symbolic-ref --short -q HEAD || true)"
[[ -n "$branch" ]] || die "detached HEAD — checkout a branch first"

# ── show the user what's about to happen ────────────────────────────
log "── status ─────────────────────────────────────"
git status --short | sed 's/^/  /'
log "── branch ─────────────────────────────────────"
log "  $branch  →  $(git config --get "branch.$branch.remote" || echo '(no upstream)')"

# ── amend path ──────────────────────────────────────────────────────
if $amend; then
    head_author="$(git log -1 --format='%an <%ae>')"
    me="$(git config user.name) <$(git config user.email)>"
    if [[ "$head_author" != "$me" ]]; then
        die "HEAD commit is by $head_author, not you ($me) — refusing to amend someone else's commit"
    fi
    log "── amending  ──────────────────────────────────"
    log "  HEAD     : $(git log -1 --oneline)"
    log "  new msg  : ${msg:-<keep existing>}"
    if $dry_run; then
        log "(dry-run; would amend and force-with-lease push)"
        exit 0
    fi
    printf "Amend HEAD and force-with-lease push? [y/N] "
    read -r ans
    [[ "$ans" =~ ^[Yy]$ ]] || die "aborted"
    git add -A
    if [[ -n "$msg" ]]; then
        git commit --amend -m "$msg"
    else
        git commit --amend --no-edit
    fi
    git push --force-with-lease
    log "✓ amended:  $(git log -1 --oneline)"
    exit 0
fi

# ── normal path ─────────────────────────────────────────────────────
git add -A
if git diff --cached --quiet; then
    log "nothing staged — working tree matches HEAD"
    exit 0
fi

# Build a default message from the staged file basenames if none given.
if [[ -z "$msg" ]]; then
    files=$(git diff --cached --name-only)
    n=$(printf '%s\n' "$files" | wc -l | tr -d ' ')
    # Note: macOS `paste -d ', '` cycles those two chars as separators
    # (A,B C,D), so we use awk to join with a single proper ", ".
    head_names=$(printf '%s\n' "$files" \
        | head -3 \
        | awk -F/ 'NR>1{printf ", "} {printf "%s", $NF} END{print ""}')
    if (( n <= 3 )); then
        msg="Update $head_names"
    else
        msg="Update $head_names (+$((n - 3)) more)"
    fi
    log "── auto-message ───────────────────────────────"
    log "  $msg"
fi

if $dry_run; then
    log "(dry-run; would commit + push)"
    exit 0
fi

git commit -m "$msg"
git push
log "✓ pushed:   $(git log -1 --oneline)"
