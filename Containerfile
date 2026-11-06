FROM registry.redhat.io/ubi9/go-toolset:9.8-1793736700@sha256:c2bd0309bf78ae317024fdfc5602a31704e0dba9572f6d4b0bd4b681033dc965 AS builder
ARG TARGETOS
ARG TARGETARCH

COPY go.mod go.sum* ./
RUN go mod download

COPY cmd/ cmd/
COPY internal/ internal/
RUN CGO_ENABLED=1 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} GOEXPERIMENT=strictfipsruntime go build -tags strictfipsruntime -ldflags="-s -w" -o ./alerts-adapter ./cmd/alerts-adapter

FROM registry.access.redhat.com/ubi9/ubi-minimal:9.8-1793578971@sha256:dfa0eb04abc59221544e74862432496add1f491f915c9b531880f91affc0e12c

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
