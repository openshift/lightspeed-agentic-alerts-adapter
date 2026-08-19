#!/usr/bin/env bash
set -euo pipefail

IMAGE_REF="${1:?Usage: deploy-e2e-local.sh <image-ref> <internal-registry>}"
INTERNAL_REGISTRY="${2:?Usage: deploy-e2e-local.sh <image-ref> <internal-registry>}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Ensure the image registry default route is exposed
if ! oc get route default-route -n openshift-image-registry &>/dev/null; then
    echo "Exposing image registry default route..."
    oc patch configs.imageregistry.operator.openshift.io/cluster \
        --patch '{"spec":{"defaultRoute":true}}' --type=merge
fi

echo "Waiting for registry route..."
until oc get route default-route -n openshift-image-registry -o jsonpath='{.spec.host}' 2>/dev/null | grep -q .; do
    sleep 2
done

REGISTRY=$(oc get route default-route -n openshift-image-registry -o jsonpath='{.spec.host}')
echo "Registry: ${REGISTRY}"

# Ensure the target namespace exists (required for image push)
NAMESPACE="${IMAGE_REF%%/*}"
oc get namespace "${NAMESPACE}" &>/dev/null || oc create namespace "${NAMESPACE}"

podman login --tls-verify=false -u unused -p "$(oc whoami -t)" "${REGISTRY}"
podman build --no-cache -t "${REGISTRY}/${IMAGE_REF}" -f Containerfile .
podman push --tls-verify=false "${REGISTRY}/${IMAGE_REF}"

export IMAGE="${INTERNAL_REGISTRY}/${IMAGE_REF}"
exec "${SCRIPT_DIR}/deploy-e2e.sh"
