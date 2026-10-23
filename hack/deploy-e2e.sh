#!/usr/bin/env bash
set -euo pipefail

NAMESPACE="${NAMESPACE:-openshift-lightspeed}"
DEPLOYMENT_NAME="${DEPLOYMENT_NAME:-lightspeed-agentic-alerts-adapter}"
DEPLOYMENT_TIMEOUT="${DEPLOYMENT_TIMEOUT:-420s}"
OPERATOR_NAMESPACE="${OPERATOR_NAMESPACE:-openshift-lightspeed}"
RUN_E2E="${RUN_E2E:-0}"

if [[ "${RUN_E2E}" != "0" && "${RUN_E2E}" != "1" ]]; then
    echo "ERROR: RUN_E2E must be 0 or 1" >&2
    exit 1
fi

if [[ -z "${IMAGE:-}" ]]; then
    echo "ERROR: IMAGE is required. Set it to a container image accessible from the cluster." >&2
    echo "" >&2
    echo "  CI:    ci-operator sets this automatically via dependencies[].env" >&2
    echo "  Local: build and push to a registry accessible from the cluster, e.g.:" >&2
    echo "    make container-build IMAGE_NAME=quay.io/\$USER/alerts-adapter IMAGE_TAG=dev" >&2
    echo "    make container-push  IMAGE_NAME=quay.io/\$USER/alerts-adapter IMAGE_TAG=dev" >&2
    echo "    IMAGE=quay.io/\$USER/alerts-adapter:dev make deploy-e2e" >&2
    exit 1
fi
OPERATOR_DEPLOYMENT="${OPERATOR_DEPLOYMENT:-lightspeed-agentic-operator}"
OPERATOR_REPO="${OPERATOR_REPO:-https://github.com/openshift/lightspeed-agentic-operator.git}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
MANIFESTS_DIR="${REPO_ROOT}/manifests"

TMPDIR=$(mktemp -d)
PORT_FORWARD_PID=""

cleanup() {
    local status=$?
    if [[ -n "${PORT_FORWARD_PID}" ]]; then
        kill "${PORT_FORWARD_PID}" 2>/dev/null || true
        wait "${PORT_FORWARD_PID}" 2>/dev/null || true
    fi
    if ! rm -rf "${TMPDIR}"; then
        echo "ERROR: failed to remove the E2E temporary directory" >&2
        if [[ "${status}" -eq 0 ]]; then
            status=1
        fi
    fi
    exit "${status}"
}

trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

echo "=== E2E Deploy ==="
# Log image name without registry to avoid exposing internal hostnames
IMAGE_NAME="${IMAGE##*/}"
echo "  IMAGE:      ${IMAGE_NAME}"
echo "  NAMESPACE:  ${NAMESPACE}"
echo "  DEPLOYMENT: ${DEPLOYMENT_NAME}"

# Check prerequisites
for cmd in oc yq; do
    if ! command -v "${cmd}" &>/dev/null; then
        echo "ERROR: ${cmd} is required but not found in PATH" >&2
        exit 1
    fi
done

if [[ "${RUN_E2E}" == "1" ]] && ! command -v go &>/dev/null; then
    echo "ERROR: Go is required when RUN_E2E=1" >&2
    exit 1
fi

if ! oc whoami &>/dev/null; then
    echo "ERROR: not logged in to an OpenShift cluster (run 'oc login' first)" >&2
    exit 1
fi

# Install operator (for CRD) if not already present
if oc get crd agenticruns.agentic.openshift.io &>/dev/null; then
    echo "AgenticRun CRD already installed, skipping operator install"
else
    echo "AgenticRun CRD not found, installing lightspeed-agentic-operator..."
    OPERATOR_DIR="${TMPDIR}/lightspeed-agentic-operator"
    git clone --depth 1 "${OPERATOR_REPO}" "${OPERATOR_DIR}"
    "${OPERATOR_DIR}/hack/quickstart/install.sh"
    echo "Operator installed"
fi

# Copy manifests to temp directory
cp -r "${MANIFESTS_DIR}"/* "${TMPDIR}/"

# Patch deployment image
echo "Patching deployment image to: ${IMAGE}"
yq -i ".spec.template.spec.containers[0].image = \"${IMAGE}\"" "${TMPDIR}/deployment.yaml"
yq -i ".spec.template.spec.containers[0].imagePullPolicy = \"Always\"" "${TMPDIR}/deployment.yaml"

# Patch configmap with test-friendly values
echo "Patching configmap for E2E testing..."
yq -i '
  .data."config.yaml" = "pollInterval: 10s\npostRunDelay: 5m\nfiltering:\n  allowedReceivers:\n    - default\ndeduplication:\n  ignoredLabels:\n    - pod\n    - instance\n    - endpoint\n    - uid\ntools:\n  skills:\n    - image: quay.io/openshiftanalytics/agentic-skills:latest\n      paths:\n        - /skills/cluster-troubleshoot/investigate-alert\n"
' "${TMPDIR}/configmap.yaml"

# Apply manifests
echo "Applying manifests..."
oc apply -f "${TMPDIR}/namespace.yaml"
oc apply -f "${TMPDIR}/serviceaccount.yaml"
oc apply -f "${TMPDIR}/rbac.yaml"
oc apply -f "${TMPDIR}/configmap.yaml"
oc apply -f "${TMPDIR}/service.yaml"
oc apply -f "${TMPDIR}/deployment.yaml"

echo "Restarting deployment to pull the updated image..."
oc rollout restart deployment "${DEPLOYMENT_NAME}" \
    -n "${NAMESPACE}"

echo "Waiting for deployment rollout to complete (timeout: 5m)..."
oc rollout status deployment "${DEPLOYMENT_NAME}" \
    -n "${NAMESPACE}" \
    --timeout="${DEPLOYMENT_TIMEOUT}"

echo "=== E2E Deploy complete ==="

if [[ "${RUN_E2E}" != "1" ]]; then
    exit 0
fi

ADAPTER_API_ADDRESS="${ADAPTER_API_ADDRESS:-127.0.0.1:18080}"
if [[ ! "${ADAPTER_API_ADDRESS}" =~ ^127\.0\.0\.1:([0-9]{1,5})$ ]]; then
    echo "ERROR: ADAPTER_API_ADDRESS must be 127.0.0.1:<port>" >&2
    exit 1
fi
LOCAL_PORT=$((10#${BASH_REMATCH[1]}))
if (( LOCAL_PORT < 1 || LOCAL_PORT > 65535 )); then
    echo "ERROR: investigation port must be between 1 and 65535" >&2
    exit 1
fi
export ADAPTER_API_ADDRESS="127.0.0.1:${LOCAL_PORT}"

if [[ -z "${ALERTMANAGER_URL:-}" ]]; then
    ALERTMANAGER_HOST=$(oc get route alertmanager-main \
        -n openshift-monitoring -o jsonpath='{.spec.host}')
    if [[ -z "${ALERTMANAGER_HOST}" ]]; then
        echo "ERROR: set ALERTMANAGER_URL to a reachable HTTPS endpoint" >&2
        exit 1
    fi
    export ALERTMANAGER_URL="https://${ALERTMANAGER_HOST}"
fi

echo "Starting the investigation API port-forward..."
oc -n "${NAMESPACE}" port-forward \
    service/lightspeed-agentic-alerts-adapter \
    "${LOCAL_PORT}:8080" --address=127.0.0.1 \
    >"${TMPDIR}/port-forward.log" 2>/dev/null &
PORT_FORWARD_PID=$!

ready=0
for _ in $(seq 1 30); do
    if ! kill -0 "${PORT_FORWARD_PID}" 2>/dev/null; then
        echo "ERROR: investigation API port-forward exited before readiness" >&2
        exit 1
    fi
    if grep -Fxq "Forwarding from ${ADAPTER_API_ADDRESS} -> 8080" \
        "${TMPDIR}/port-forward.log"; then
        ready=1
        break
    fi
    sleep 1
done
if [[ "${ready}" != "1" ]]; then
    echo "ERROR: timed out waiting for the investigation API port-forward" >&2
    exit 1
fi

export NAMESPACE DEPLOYMENT_NAME OPERATOR_NAMESPACE OPERATOR_DEPLOYMENT
echo "Running E2E tests..."
test_status=0
(
    cd "${REPO_ROOT}"
    go test ./test/e2e -run '^TestE2E$' -count=1 -v -timeout=35m \
        -args -ginkgo.timeout=30m
) || test_status=$?
exit "${test_status}"
