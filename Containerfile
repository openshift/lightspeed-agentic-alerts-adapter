FROM registry.redhat.io/ubi9/go-toolset:9.8-1783442369 AS builder

COPY go.mod go.sum* ./
RUN go mod download

COPY cmd/ cmd/
COPY internal/ internal/
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o ./alerts-adapter ./cmd/alerts-adapter

FROM registry.access.redhat.com/ubi9-micro:latest

LABEL com.redhat.component="lightspeed-agentic-alerts-adapter" \
      name="openshift-lightspeed-1/lightspeed-agentic-alerts-adapter" \
      version="0.1.0" \
      release="1" \
      summary="Lightspeed Agentic Alerts Adapter" \
      description="Polls OpenShift AlertManager for firing alerts and creates Proposal CRs to trigger automated remediation via the Lightspeed Agentic operator." \
      io.k8s.description="Polls OpenShift AlertManager for firing alerts and creates Proposal CRs to trigger automated remediation via the Lightspeed Agentic operator." \
      url="https://github.com/openshift/lightspeed-agentic-alerts-adapter" \
      vendor="Red Hat, Inc." \
      distribution-scope="public" \
      cpe="cpe:/a:redhat:openshift_lightspeed:1::el9"

COPY LICENSE /licenses/LICENSE
COPY --from=builder /opt/app-root/src/alerts-adapter /usr/local/bin/alerts-adapter

USER 1001

ENTRYPOINT ["alerts-adapter"]
