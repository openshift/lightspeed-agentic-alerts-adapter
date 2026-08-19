#!/usr/bin/env bash
set -euo pipefail

NAMESPACE="${NAMESPACE:-openshift-lightspeed}"
DEPLOYMENT_NAME="${DEPLOYMENT_NAME:-lightspeed-agentic-alerts-adapter}"
OPERATOR_NAMESPACE="${OPERATOR_NAMESPACE:-openshift-lightspeed}"

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
trap 'rm -rf "${TMPDIR}"' EXIT

echo "=== E2E Deploy ==="
echo "  IMAGE:      ${IMAGE}"
echo "  NAMESPACE:  ${NAMESPACE}"
echo "  DEPLOYMENT: ${DEPLOYMENT_NAME}"

# Check prerequisites
for cmd in oc yq; do
    if ! command -v "${cmd}" &>/dev/null; then
        echo "ERROR: ${cmd} is required but not found in PATH" >&2
        exit 1
    fi
done

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
oc apply -f "${TMPDIR}/deployment.yaml"

# Wait for deployment to be available
echo "Waiting for deployment to be available (timeout: 5m)..."
oc wait deployment "${DEPLOYMENT_NAME}" \
    -n "${NAMESPACE}" \
    --for=condition=available \
    --timeout=300s

echo "=== E2E Deploy complete ==="
