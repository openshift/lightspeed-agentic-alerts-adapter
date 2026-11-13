FROM registry.redhat.io/ubi9/go-toolset:9.9-1794518352@sha256:4053e919374d739ee6b0007e412d89256b0bd065e0bc690c2b3905bbaab6500e AS builder
ARG TARGETOS
ARG TARGETARCH

COPY go.mod go.sum* ./
RUN go mod download

COPY cmd/ cmd/
COPY internal/ internal/
# Build with Go's native FIPS 140-3 crypto provider (already pure Go, no
# CGo/OpenSSL). GOFIPS140=certified selects the certified FIPS module; the
# binary defaults to fips140=on (see the //go:debug directive in
# cmd/alerts-adapter/fips.go), keeping FIPS-approved crypto always active
# (matching the previous strictfipsruntime behavior).
# GOAMD64 is only consulted when GOARCH=amd64; it is silently ignored for
# other target architectures (arm64 etc.), so multi-arch builds are unaffected.
RUN CGO_ENABLED=0 GOFIPS140=certified GOAMD64=v2 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -ldflags="-s -w" -o ./alerts-adapter ./cmd/alerts-adapter
# Verify alerts-adapter targets at most x86-64-v2 (no-op on non-amd64)
RUN go build -o check-isa-level ./cmd/check-isa-level && ./check-isa-level ./alerts-adapter

FROM registry.access.redhat.com/ubi9-micro:1794478350@sha256:dbd325b96bcd432dd42816d31412bdf1dc0fb973b79ed310344bc23c76c068db

LABEL name="openshift-lightspeed/lightspeed-agentic-alerts-adapter-rhel9" \
      cpe="cpe:/a:redhat:openshift_lightspeed:2::el9" \      
      com.redhat.component="lightspeed-agentic-alerts-adapter" \
      io.k8s.display-name="OpenShift Lightspeed Agentic Adapter" \
      summary="OpenShift Lightspeed Agentic Alerts Adapter" \
      description="Polls OpenShift AlertManager for firing alerts and creates AgenticRun CRs to trigger automated remediation via the OpenShift Lightspeed Agentic operator." \
      io.k8s.description="Polls OpenShift AlertManager for firing alerts and creates AgenticRun CRs to trigger automated remediation via the OpenShift Lightspeed Agentic operator." \
      io.openshift.tags="openshift-lightspeed,agentic,ols" \
      vendor="Red Hat, Inc."

COPY LICENSE /licenses/LICENSE
COPY --from=builder /opt/app-root/src/alerts-adapter /usr/local/bin/alerts-adapter

USER 1001

ENTRYPOINT ["alerts-adapter"]
