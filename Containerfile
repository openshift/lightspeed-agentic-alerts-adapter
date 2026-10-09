FROM registry.redhat.io/ubi9/go-toolset:9.8-1791479310@sha256:6f246e8913d082df463b62a74c72f0d2b410583e1b2ac48add39cd7ede59ce62 AS builder

COPY go.mod go.sum* ./
RUN go mod download

COPY cmd/ cmd/
COPY internal/ internal/
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o ./alerts-adapter ./cmd/alerts-adapter

FROM registry.access.redhat.com/ubi9-micro:9.8-1789345812@sha256:7a0454cbd9bd847e8f6a63b6f0254a6efbeb6e0ed71a5d824a4f6cccbe626650

LABEL com.redhat.component="lightspeed-agentic-alerts-adapter" \
      name="openshift-lightspeed/lightspeed-agentic-alerts-adapter-rhel9" \
      version="0.1.0" \
      release="1" \
      summary="Lightspeed Agentic Alerts Adapter" \
      description="Polls OpenShift AlertManager for firing alerts and creates AgenticRun CRs to trigger automated remediation via the Lightspeed Agentic operator." \
      io.k8s.description="Polls OpenShift AlertManager for firing alerts and creates AgenticRun CRs to trigger automated remediation via the Lightspeed Agentic operator." \
      url="https://github.com/openshift/lightspeed-agentic-alerts-adapter" \
      vendor="Red Hat, Inc." \
      distribution-scope="public" \
      cpe="cpe:/a:redhat:openshift_lightspeed:1::el9"

COPY LICENSE /licenses/LICENSE
COPY --from=builder /opt/app-root/src/alerts-adapter /usr/local/bin/alerts-adapter

USER 1001

ENTRYPOINT ["alerts-adapter"]
