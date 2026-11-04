#!/usr/bin/env bash
# Upload coverage to codecov.io using the shared lightspeed CI pattern
# (modeled on lightspeed-rag-content / lightspeed-service).
# The standalone Konflux pipeline supplies the Prow-compatible metadata below.
#
# Usage: scripts/codecov.sh <report-file> [flag]
#
# The upload SOFT-FAILS by design: codecov problems must never block CI, so
# this script always exits 0. The upload state is always clearly logged:
#   - "[codecov] upload succeeded"              report is on codecov.io
#   - "[codecov] CODECOV UPLOAD FAILED ..."     upload was attempted and failed
#   - "[codecov] upload skipped: <reason>"      upload was not attempted
#
# Environment (set by CI):
#   JOB_TYPE                    presubmit | postsubmit | batch | local
#   PULL_NUMBER                 PR number (presubmit only)
#   PULL_PULL_SHA               PR head commit SHA (presubmit only)
#   PULL_BASE_SHA / PULL_BASE_REF  base commit SHA and branch
#   REPO_OWNER / REPO_NAME      repository slug parts
#   CODECOV_TOKEN               repository upload token (Konflux secret)
#   ARTIFACT_DIR                writable directory for the upload log

set -o nounset
set -o pipefail

CODECOV_CLI_VERSION="11.3.1"
MAX_ATTEMPTS="${CODECOV_MAX_ATTEMPTS:-2}"
RETRY_SLEEP_SECONDS="${CODECOV_RETRY_SLEEP_SECONDS:-15}"
UPLOAD_TIMEOUT_SECONDS="${CODECOV_TIMEOUT_SECONDS:-300}"

log() {
    echo "[codecov] $*"
}

report_not_uploaded() {
    log "!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!"
    log "!! CODECOV UPLOAD FAILED - coverage was NOT uploaded to codecov.io"
    log "!! CI is not blocked (soft-fail by design), but coverage data is"
    log "!! missing and codecov.io / dashboards will not update."
    log "!! Reason: $*"
    log "!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!"
    exit 0
}

skip_upload() {
    log "upload skipped: $* (coverage was NOT uploaded to codecov.io)"
    exit 0
}

report_file="${1:-}"
flag="${2:-}"

if [ -z "$report_file" ]; then
    skip_upload "no report file given (usage: codecov.sh <report-file> [flag])"
fi
if [ ! -f "$report_file" ]; then
    report_not_uploaded "report file '$report_file' does not exist"
fi

case "${JOB_TYPE:-local}" in
    presubmit)
        sha="${PULL_PULL_SHA:-}"
        branch="${PULL_BASE_REF:-}"
        pr="${PULL_NUMBER:-}"
        ;;
    postsubmit | batch)
        sha="${PULL_BASE_SHA:-}"
        branch="${PULL_BASE_REF:-}"
        pr=""
        ;;
    *)
        skip_upload "JOB_TYPE '${JOB_TYPE:-local}' does not upload (local run)"
        ;;
esac

if [ -z "${CODECOV_TOKEN:-}" ]; then
    report_not_uploaded "CODECOV_TOKEN is not set (check the CI secret)"
fi
if [ -z "$sha" ]; then
    report_not_uploaded "commit SHA is not set"
fi
if [ -z "$branch" ]; then
    report_not_uploaded "branch is not set"
fi

cli_args=(
    upload-process
    --git-service github
    --slug "${REPO_OWNER:-openshift}/${REPO_NAME:-lightspeed-agentic-alerts-adapter}"
    --token "$CODECOV_TOKEN"
    --file "$report_file"
    --sha "$sha"
    --branch "$branch"
    --fail-on-error
    --disable-search
)
if [ -n "$flag" ]; then
    cli_args+=(--flag "$flag")
fi
if [ -n "$pr" ]; then
    cli_args+=(--pr "$pr")
fi

if command -v codecovcli > /dev/null 2>&1; then
    cli=(codecovcli)
elif command -v uv > /dev/null 2>&1; then
    cli=(uv tool run --from "codecov-cli==${CODECOV_CLI_VERSION}" codecovcli)
    log "codecovcli not found on PATH, using uv (codecov-cli==${CODECOV_CLI_VERSION})"
else
    # Neither the codecov CLI nor uv is installed (e.g. a bare CI build
    # root): install uv to the user prefix, like lightspeed-service's
    # install-uv Makefile target. The installer is fetched over HTTPS
    # from astral.sh, matching the approved lightspeed-service pattern.
    # Bound the install so a stalled download cannot block 'make test'.
    log "codecovcli and uv not found on PATH, installing uv first"
    # prow test containers run with an unusable HOME ('/' or empty), which
    # the uv installer would turn into a read-only '//.local' path. Install
    # to an explicit writable directory instead, bypassing HOME entirely.
    uv_root="${TMPDIR:-/tmp}/ols-codecov-uv"
    uv_bin="$uv_root/bin"
    mkdir -p "$uv_bin"
    # Point every HOME-derived uv path at a writable directory: prow test
    # containers run with an unusable HOME ('/' or empty).
    export UV_UNMANAGED_INSTALL="$uv_bin"
    export UV_CACHE_DIR="$uv_root/cache"
    export UV_TOOL_DIR="$uv_root/tools"
    export UV_PYTHON_INSTALL_DIR="$uv_root/python"
    if ! timeout "${UV_INSTALL_TIMEOUT_SECONDS:-120}" curl -LsSf https://astral.sh/uv/install.sh | sh; then
        report_not_uploaded "uv installer failed or timed out"
    fi
    export PATH="$uv_bin:$PATH"
    if ! command -v uv > /dev/null 2>&1; then
        report_not_uploaded "uv still not available after install attempt"
    fi
    cli=(uv tool run --from "codecov-cli==${CODECOV_CLI_VERSION}" codecovcli)
fi

upload_log="${ARTIFACT_DIR:-.}/codecov_upload.log"
pr_note=""
if [ -n "$pr" ]; then
    pr_note=", pr: ${pr}"
fi

attempt=1
while [ "$attempt" -le "$MAX_ATTEMPTS" ]; do
    log "upload attempt ${attempt}/${MAX_ATTEMPTS} (flag: ${flag:-none}, branch: ${branch}${pr_note})"
    if timeout "$UPLOAD_TIMEOUT_SECONDS" "${cli[@]}" "${cli_args[@]}" > "$upload_log" 2>&1; then
        cat "$upload_log"
        log "upload succeeded - report available on codecov.io"
        exit 0
    fi
    cat "$upload_log"
    log "upload attempt ${attempt}/${MAX_ATTEMPTS} failed"
    attempt=$((attempt + 1))
    if [ "$attempt" -le "$MAX_ATTEMPTS" ]; then
        sleep "$RETRY_SLEEP_SECONDS"
    fi
done

report_not_uploaded "codecov CLI exited non-zero after ${MAX_ATTEMPTS} attempt(s), see ${upload_log} above"
