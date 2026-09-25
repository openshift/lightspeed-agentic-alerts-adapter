#!/usr/bin/env bash
#
# Gathers open PRs from red-hat-konflux[bot] on the upstream repo and merges
# all their changes into a single squashed commit on a local branch.
#
# After merging, compares go.mod against the baseline, reverts any module
# version that was downgraded, and runs `go mod tidy`.
#
# Prerequisites:
#   - gh CLI authenticated (gh auth login / GITHUB_TOKEN)
#   - git remote "upstream" pointing to openshift/lightspeed-agentic-alerts-adapter
#     (the script creates it if missing)
#
# Usage:
#   ./scripts/merge-konflux-prs.sh
#
set -euo pipefail

UPSTREAM_REPO="openshift/lightspeed-agentic-alerts-adapter"
UPSTREAM_REMOTE="upstream"
COMBINED_BRANCH="konflux/combined-updates"
BASE_BRANCH="main"

# --- Temp files ---
GOMOD_DIFF_TOOL=$(mktemp)
GOMOD_DIFF_SRC=$(mktemp --suffix=.go)
GOMOD_DIFF_DIR=$(mktemp -d)
BASELINE_GOMOD=$(mktemp)
trap 'rm -f "$GOMOD_DIFF_TOOL" "$GOMOD_DIFF_SRC" "$BASELINE_GOMOD"; rm -rf "$GOMOD_DIFF_DIR"' EXIT

# --- Build Go helper that compares two go.mod files ---
# Mode "downgrades": prints "module baseline_ver current_ver" for each downgrade.
# Mode "max-versions": prints "module max_ver" for every module, taking the higher
#   of baseline and current. This is used to set all modules to their correct
#   versions before running go mod tidy.
cat > "$GOMOD_DIFF_SRC" <<'GOEOF'
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/mod/semver"
)

func parseGoMod(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	versions := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "module") ||
			strings.HasPrefix(line, "go ") || strings.HasPrefix(line, "toolchain") ||
			line == "require (" || line == ")" || line == "require(" {
			continue
		}
		line = strings.TrimPrefix(line, "require ")
		parts := strings.Fields(line)
		if len(parts) >= 2 && strings.HasPrefix(parts[1], "v") {
			versions[parts[0]] = parts[1]
		}
	}
	return versions, scanner.Err()
}

func main() {
	if len(os.Args) < 4 {
		fmt.Fprintf(os.Stderr, "usage: %s <mode> <baseline-go.mod> <current-go.mod>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "modes: downgrades, max-versions\n")
		os.Exit(1)
	}
	mode := os.Args[1]
	baseline, err := parseGoMod(os.Args[2])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading baseline: %v\n", err)
		os.Exit(1)
	}
	current, err := parseGoMod(os.Args[3])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading current: %v\n", err)
		os.Exit(1)
	}

	switch mode {
	case "downgrades":
		for mod, baseVer := range baseline {
			curVer, ok := current[mod]
			if !ok {
				continue
			}
			if semver.Compare(curVer, baseVer) < 0 {
				fmt.Printf("%s %s %s\n", mod, baseVer, curVer)
			}
		}
	case "max-versions":
		all := make(map[string]string)
		for mod, ver := range baseline {
			all[mod] = ver
		}
		for mod, ver := range current {
			if existing, ok := all[mod]; !ok || semver.Compare(ver, existing) > 0 {
				all[mod] = ver
			}
		}
		for mod, ver := range all {
			fmt.Printf("%s %s\n", mod, ver)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown mode: %s\n", mode)
		os.Exit(1)
	}
}
GOEOF

echo "==> Building version comparison helper..."
(cd "$GOMOD_DIFF_DIR" && go mod init gomod-diff >/dev/null 2>&1 && go get golang.org/x/mod/semver >/dev/null 2>&1 && go build -o "$GOMOD_DIFF_TOOL" "$GOMOD_DIFF_SRC" 2>&1) || {
    echo "FATAL: could not build version comparison helper"
    exit 1
}

# --- Ensure upstream remote exists ---
if ! git remote get-url "$UPSTREAM_REMOTE" &>/dev/null; then
    echo "==> Adding remote '$UPSTREAM_REMOTE' for $UPSTREAM_REPO"
    git remote add "$UPSTREAM_REMOTE" "https://github.com/${UPSTREAM_REPO}.git"
fi

# --- Fetch latest upstream ---
echo "==> Fetching $UPSTREAM_REMOTE..."
git fetch "$UPSTREAM_REMOTE"

# --- Save baseline go.mod ---
git show "$UPSTREAM_REMOTE/$BASE_BRANCH:go.mod" > "$BASELINE_GOMOD"

# --- Gather open PRs from konflux-bot ---
echo "==> Listing open PRs from 'red-hat-konflux[bot]'..."
PR_JSON=$(gh pr list \
    --repo "$UPSTREAM_REPO" \
    --author 'red-hat-konflux[bot]' \
    --state open \
    --json number,title,headRefName,body \
    --limit 100)

PR_COUNT=$(echo "$PR_JSON" | jq 'length')
if [[ "$PR_COUNT" -eq 0 ]]; then
    echo "No open PRs from red-hat-konflux[bot] found. Nothing to do."
    exit 0
fi

echo "==> Found $PR_COUNT open PR(s):"
echo "$PR_JSON" | jq -r '.[] | "  #\(.number) — \(.title)"'

# --- Create combined branch from upstream/main ---
echo ""
echo "==> Creating branch '$COMBINED_BRANCH' from $UPSTREAM_REMOTE/$BASE_BRANCH..."
git checkout -B "$COMBINED_BRANCH" "$UPSTREAM_REMOTE/$BASE_BRANCH"

# --- Fetch and merge each PR ---
MERGED_PRS=()
FAILED_PRS=()

for ROW in $(echo "$PR_JSON" | jq -r '.[] | @base64'); do
    _jq() { echo "$ROW" | base64 -d | jq -r "$1"; }
    PR_NUM=$(_jq '.number')
    PR_TITLE=$(_jq '.title')

    echo ""
    echo "==> Merging PR #${PR_NUM}: ${PR_TITLE}..."

    if ! git fetch "$UPSTREAM_REMOTE" "+refs/pull/${PR_NUM}/head:refs/remotes/${UPSTREAM_REMOTE}/pr-${PR_NUM}" 2>&1; then
        echo "    WARN: Could not fetch PR #${PR_NUM}, skipping."
        FAILED_PRS+=("#${PR_NUM}")
        continue
    fi

    if git merge --no-edit "${UPSTREAM_REMOTE}/pr-${PR_NUM}" -m "Merge PR #${PR_NUM}: ${PR_TITLE}"; then
        echo "    OK: merged successfully."
        MERGED_PRS+=("#${PR_NUM} — ${PR_TITLE}")
    else
        echo "    WARN: Merge conflict on PR #${PR_NUM}, attempting auto-resolution..."
        CONFLICTED=$(git diff --name-only --diff-filter=U 2>/dev/null || true)
        if [[ -n "$CONFLICTED" ]]; then
            # For go.mod/go.sum, accept "ours" to preserve previously-merged upgrades,
            # then apply the specific module bump via go get.
            # For other files, accept "theirs".
            HAS_GOMOD=false
            echo "$CONFLICTED" | while read -r f; do
                if [[ "$f" == "go.mod" || "$f" == "go.sum" ]]; then
                    git checkout --ours "$f" 2>/dev/null && git add "$f" 2>/dev/null || true
                else
                    git checkout --theirs "$f" 2>/dev/null && git add "$f" 2>/dev/null || true
                fi
            done
            if echo "$CONFLICTED" | grep -q '^go\.mod$'; then
                HAS_GOMOD=true
            fi
            if git commit --no-edit 2>/dev/null; then
                # If go.mod had a conflict, apply the intended module bump
                if $HAS_GOMOD; then
                    # Extract module and version from PR title
                    if [[ "$PR_TITLE" =~ update\ module\ (.+)\ to\ (v[0-9][^ ]*) ]]; then
                        MOD="${BASH_REMATCH[1]}"
                        VER="${BASH_REMATCH[2]}"
                        echo "    Applying intended bump: $MOD@$VER"
                        go get "${MOD}@${VER}" 2>/dev/null || true
                        go mod tidy 2>/dev/null || true
                        git add go.mod go.sum
                        git commit --amend --no-edit 2>/dev/null || true
                    fi
                fi
                echo "    OK: resolved conflicts."
                MERGED_PRS+=("#${PR_NUM} — ${PR_TITLE}")
            else
                echo "    FAIL: could not resolve conflicts for PR #${PR_NUM}, aborting merge."
                git merge --abort 2>/dev/null || true
                FAILED_PRS+=("#${PR_NUM}")
            fi
        else
            git merge --abort 2>/dev/null || true
            FAILED_PRS+=("#${PR_NUM}")
        fi
    fi
done

# --- Detect and fix downgrades in go.mod ---
DOWNGRADES=()
if [[ ${#MERGED_PRS[@]} -gt 0 ]]; then
    echo ""
    echo "==> Checking for dependency downgrades..."

    DOWNGRADE_OUTPUT=$("$GOMOD_DIFF_TOOL" downgrades "$BASELINE_GOMOD" "go.mod")

    if [[ -n "$DOWNGRADE_OUTPUT" ]]; then
        while IFS=' ' read -r mod baseline_ver current_ver; do
            DOWNGRADES+=("$mod: $baseline_ver → $current_ver")
            echo "    ! Downgrade: $mod $baseline_ver → $current_ver"
        done <<< "$DOWNGRADE_OUTPUT"

        echo ""
        echo "==> Fixing downgrades (restoring baseline versions for downgraded modules)..."
        while IFS=' ' read -r mod baseline_ver _current_ver; do
            go get "${mod}@${baseline_ver}" 2>/dev/null || echo "    WARN: could not restore $mod@$baseline_ver"
        done <<< "$DOWNGRADE_OUTPUT"
    else
        echo "    No downgrades detected."
    fi

    echo "==> Running go mod tidy..."
    go mod tidy

    # go mod tidy may re-introduce downgrades via MVS — check again and force-pin if needed
    DOWNGRADE_RECHECK=$("$GOMOD_DIFF_TOOL" downgrades "$BASELINE_GOMOD" "go.mod")
    if [[ -n "$DOWNGRADE_RECHECK" ]]; then
        echo "==> go mod tidy re-introduced downgrades, force-pinning..."
        while IFS=' ' read -r mod baseline_ver _current_ver; do
            echo "    ! Re-pinning: $mod to $baseline_ver"
            go get "${mod}@${baseline_ver}" 2>/dev/null || true
        done <<< "$DOWNGRADE_RECHECK"
    fi

    git add go.mod go.sum
fi

# --- Squash all merges into a single commit ---
if [[ ${#MERGED_PRS[@]} -gt 0 ]]; then
    echo ""
    echo "==> Squashing into a single commit..."
    COMMIT_MSG="fix(deps): combined Konflux dependency updates

This commit combines the following open PRs from red-hat-konflux[bot]:

$(for pr in "${MERGED_PRS[@]}"; do echo "- ${pr}"; done)"

    git reset --soft "$UPSTREAM_REMOTE/$BASE_BRANCH"
    git commit -m "$COMMIT_MSG"
fi

# --- Summary ---
echo ""
echo "========================================"
echo "  Summary"
echo "========================================"
echo "Merged (${#MERGED_PRS[@]}):"
for pr in "${MERGED_PRS[@]}"; do echo "  ✓ $pr"; done
if [[ ${#DOWNGRADES[@]} -gt 0 ]]; then
    echo "Downgrades fixed (${#DOWNGRADES[@]}):"
    for d in "${DOWNGRADES[@]}"; do echo "  ↑ $d (restored)"; done
fi
if [[ ${#FAILED_PRS[@]} -gt 0 ]]; then
    echo "Failed (${#FAILED_PRS[@]}):"
    for pr in "${FAILED_PRS[@]}"; do echo "  ✗ $pr"; done
fi
echo "========================================"

if [[ ${#MERGED_PRS[@]} -eq 0 ]]; then
    echo "No PRs were merged. Exiting."
    exit 1
fi

echo ""
echo "==> Branch '$COMBINED_BRANCH' is ready with a single squashed commit."
echo "    Inspect with: git log --oneline $UPSTREAM_REMOTE/$BASE_BRANCH..$COMBINED_BRANCH"
echo "    Push with:    git push -u origin $COMBINED_BRANCH --force-with-lease"
