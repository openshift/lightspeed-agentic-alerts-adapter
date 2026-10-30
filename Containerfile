FROM registry.redhat.io/ubi9/go-toolset:9.8-1790174511@sha256:0a4666f7a4eb0644c97a73cba198eb268691b270d97831822689e7a2088f87be AS builder
ARG TARGETOS
ARG TARGETARCH

COPY go.mod go.sum* ./
RUN go mod download

COPY cmd/ cmd/
COPY internal/ internal/
RUN CGO_ENABLED=1 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} GOEXPERIMENT=strictfipsruntime go build -tags strictfipsruntime -ldflags="-s -w" -o ./alerts-adapter ./cmd/alerts-adapter

FROM registry.access.redhat.com/ubi9/ubi-minimal:9.8-1793253163@sha256:bba2b4bbf0ed15cfe7dd72a1e17ae7d2015a141f306b304e1a2785c41400e39c

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
