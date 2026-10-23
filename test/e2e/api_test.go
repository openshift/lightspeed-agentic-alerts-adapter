package e2e_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	"github.com/prometheus/alertmanager/api/v2/models"
	"github.com/stretchr/testify/assert"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/agenticrun"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/alertmanager"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/config"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/test/e2e/framework"
)

var _ = Describe("Investigation API", Label("investigations"), func() {
	var client *framework.InvestigationClient

	BeforeEach(func() {
		var err error
		client, err = framework.NewInvestigationClient(cfg.AdapterApiAddress, cfg.Namespace)
		if !assert.NoError(GinkgoT(), err) {
			return
		}
		DeferCleanup(client.Close)
	})

	It("should reject an invalid bearer token", func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		response, err := client.Request(ctx, "e2e-invalid-token", `{"fingerprint":"e2e-missing"}`)
		if !assert.NoError(GinkgoT(), err) {
			return
		}
		assertInvestigationError(response, http.StatusUnauthorized,
			"unauthorized", "A valid bearer token is required")
	})

	It("should reject invalid request bodies for an authenticated caller", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		token, err := framework.CurrentToken(ctx)
		if !assert.NoError(GinkgoT(), err) {
			return
		}
		tests := []struct {
			name string
			body string
		}{
			{name: "empty body"},
			{name: "malformed JSON", body: `{"fingerprint":`},
			{name: "missing fingerprint", body: `{}`},
			{name: "empty fingerprint", body: `{"fingerprint":""}`},
			{name: "null fingerprint", body: `{"fingerprint":null}`},
			{name: "numeric fingerprint", body: `{"fingerprint":123}`},
			{name: "boolean fingerprint", body: `{"fingerprint":true}`},
			{name: "object fingerprint", body: `{"fingerprint":{}}`},
			{name: "array fingerprint", body: `{"fingerprint":[]}`},
			{name: "unknown field", body: `{"fingerprint":"e2e-missing","extra":true}`},
			{name: "null body", body: `null`},
			{name: "array body", body: `[]`},
			{name: "second object", body: `{"fingerprint":"e2e-missing"} {}`},
			{name: "trailing garbage", body: `{"fingerprint":"e2e-missing"} garbage`},
		}
		for _, tt := range tests {
			By(tt.name)
			response, err := client.Request(ctx, token, tt.body)
			if !assert.NoError(GinkgoT(), err) {
				return
			}
			assertInvestigationError(response, http.StatusBadRequest,
				"invalid_request", "Request must contain only a non-empty fingerprint string")
		}
	})

	PIt("should enforce the body limit before JSON decoding", func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		token, err := framework.CurrentToken(ctx)
		if !assert.NoError(GinkgoT(), err) {
			return
		}
		valid := `{"fingerprint":"e2e-missing"}`
		for _, body := range []string{
			valid + strings.Repeat(" ", 4097-len(valid)),
			strings.Repeat("x", 4097),
		} {
			response, err := client.Request(ctx, token, body)
			if !assert.NoError(GinkgoT(), err) {
				return
			}
			assertInvestigationError(response, http.StatusRequestEntityTooLarge,
				"request_too_large", "Request body exceeds the size limit")
		}
	})

	It("should return not found for an unknown fingerprint including a body at the size limit", func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		token, err := framework.CurrentToken(ctx)
		if !assert.NoError(GinkgoT(), err) {
			return
		}
		valid := `{"fingerprint":"e2e-missing"}`
		for _, body := range []string{
			valid,
			valid + "\n\t ",
			valid + strings.Repeat(" ", 4096-len(valid)),
		} {
			response, err := client.Request(ctx, token, body)
			if !assert.NoError(GinkgoT(), err) {
				return
			}
			assertInvestigationError(response, http.StatusNotFound,
				"alert_not_found", "No eligible alert matches the fingerprint")
		}
	})

	It("should create an investigation for a PrometheusRule alert ignored by the poller", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		token, err := framework.CurrentToken(ctx)
		if !assert.NoError(GinkgoT(), err) {
			return
		}

		alerts, err := alertmanager.New(alertmanager.Config{
			URL: os.Getenv("ALERTMANAGER_URL"), Token: token,
			InsecureSkipVerify: true,
		})
		if !assert.True(GinkgoT(), err == nil, "cannot initialize the AlertManager client") {
			return
		}
		ruleName := "e2einvestigation" + strings.ToLower(rand.Text())
		selector := fmt.Sprintf(
			"agentic.openshift.io/source=alertmanager,agentic.openshift.io/alert-name=%s",
			ruleName,
		)
		DeferCleanup(func() {
			cleanup, stop := context.WithTimeout(context.Background(), 2*time.Minute)
			defer stop()
			_, err := framework.DeleteResourcesByLabel(cleanup, agenticRunAPIResource, cfg.Namespace, selector)
			assert.True(GinkgoT(), err == nil, "failed to delete this test's AgenticRuns")
		})
		DeferCleanup(func() {
			cleanup, stop := context.WithTimeout(context.Background(), 2*time.Minute)
			defer stop()
			_, err := framework.DeleteResource(cleanup, "prometheusrules.monitoring.coreos.com", ruleName, "openshift-monitoring")
			assert.True(GinkgoT(), err == nil, "failed to delete this test's PrometheusRule")
		})

		By("Creating an always-firing rule with a unique test identity")
		_, err = framework.CreateFromYAML(ctx,
			framework.PrometheusRuleManifest(ruleName, cfg.Namespace, "vector(1) > 0"),
		)
		if !assert.True(GinkgoT(), err == nil, "failed to create the test PrometheusRule") {
			return
		}
		labels := map[string]string{
			"alertname": ruleName, "severity": "critical",
			"namespace": cfg.Namespace, "e2e_test_id": ruleName,
		}
		var canonical *models.GettableAlert
		By("Waiting for the canonical AlertManager snapshot and fingerprint")
		if !assert.Eventually(GinkgoT(), func() bool {
			found, err := framework.FindAlert(ctx, alerts, labels)
			if err != nil || found == nil {
				return false
			}
			canonical = found
			return true
		}, 2*time.Minute, 2*time.Second, "test alert did not become eligible in AlertManager") {
			return
		}
		if !assert.NotEmpty(GinkgoT(), canonical.Receivers) {
			return
		}
		for _, receiver := range canonical.Receivers {
			if !assert.True(GinkgoT(), receiver != nil && receiver.Name != nil,
				"test alert must have named receivers") {
				return
			}
			if !assert.False(GinkgoT(), strings.EqualFold(*receiver.Name, "default"),
				"test requires a receiver excluded by the E2E polling allowlist") {
				return
			}
		}

		By("Checking that polling creates no run over multiple E2E poll cycles")
		if !assert.Never(GinkgoT(), func() bool {
			names, err := framework.GetResourcesByLabel(ctx, agenticRunAPIResource, cfg.Namespace, selector, "{.items[*].metadata.name}")
			return err != nil || names != ""
		}, 30*time.Second, 5*time.Second, "poller created a run or run listing failed") {
			return
		}
		payload, err := json.Marshal(map[string]string{"fingerprint": *canonical.Fingerprint})
		if !assert.NoError(GinkgoT(), err) {
			return
		}
		By("Requesting the investigation with the canonical fingerprint")
		response, err := client.Request(ctx, token, string(payload))
		if !assert.NoError(GinkgoT(), err) {
			return
		}
		defer response.Body.Close()
		if !assert.Equal(GinkgoT(), http.StatusCreated, response.StatusCode) {
			return
		}
		assert.True(GinkgoT(), response.Header.Get("Content-Type") == "application/json")
		var created map[string]string
		decoder := json.NewDecoder(io.LimitReader(response.Body, 64<<10))
		if !assert.True(GinkgoT(), decoder.Decode(&created) == nil, "invalid creation response") {
			return
		}
		if !assert.True(GinkgoT(), len(created) == 2 && created["namespace"] == cfg.Namespace && created["name"] != "",
			"creation response must contain the expected namespace and a name") {
			return
		}
		if !assert.False(GinkgoT(), strings.Contains(created["name"], token), "creation response exposed credentials") {
			return
		}
		assert.True(GinkgoT(), decoder.Decode(new(any)) == io.EOF, "unexpected trailing response data")

		By("Reading the created resource and checking its canonical alert metadata")
		output, err := framework.Run(ctx, "get", agenticRunAPIResource, created["name"], "-n", cfg.Namespace, "-o", "json")
		if !assert.True(GinkgoT(), err == nil, "cannot read the created AgenticRun") {
			return
		}
		var run agenticv1alpha1.AgenticRun
		if !assert.True(GinkgoT(), json.Unmarshal([]byte(output), &run) == nil, "invalid AgenticRun response") {
			return
		}
		assert.True(GinkgoT(), run.Namespace == cfg.Namespace && run.Name == created["name"])
		assert.True(GinkgoT(), run.Labels["agentic.openshift.io/trigger"] == "api")
		assert.True(GinkgoT(), run.Labels[agenticrun.LabelSource] == "alertmanager")
		assert.True(GinkgoT(), run.Labels[agenticrun.LabelAlertName] == ruleName)
		assert.True(GinkgoT(), run.Labels[agenticrun.LabelDedupFingerprint] == agenticrun.StableFingerprint(canonical.Labels, config.DefaultIgnoredLabels))
		fingerprint := *canonical.Fingerprint
		assert.True(GinkgoT(), run.Labels[agenticrun.LabelFingerprint] == fingerprint[:min(len(fingerprint), 8)])
		if assert.NotNil(GinkgoT(), canonical.StartsAt) {
			assert.True(GinkgoT(), run.Annotations[agenticrun.AnnotStartsAt] == time.Time(*canonical.StartsAt).UTC().Format(time.RFC3339))
		}
		assert.True(GinkgoT(), run.Annotations[agenticrun.AnnotSummary] == canonical.Annotations["summary"])
		assert.True(GinkgoT(), strings.Contains(run.Spec.Request, ruleName))
	})
})

func assertInvestigationError(response *http.Response, status int, code, message string) {
	GinkgoHelper()
	defer response.Body.Close()
	assert.Equal(GinkgoT(), status, response.StatusCode)
	assert.True(GinkgoT(), response.Header.Get("Content-Type") == "application/json",
		"unexpected response content type")
	challenge := ""
	if status == http.StatusUnauthorized {
		challenge = "Bearer"
	}
	assert.True(GinkgoT(), response.Header.Get("WWW-Authenticate") == challenge,
		"unexpected bearer challenge")
	decoder := json.NewDecoder(io.LimitReader(response.Body, 64<<10))
	var actual any
	if !assert.True(GinkgoT(), decoder.Decode(&actual) == nil, "response must be valid JSON") {
		return
	}
	expected := map[string]any{
		"error": map[string]any{"code": code, "message": message},
	}
	// Never include response bodies in failures; a broken server could expose credentials.
	assert.True(GinkgoT(), reflect.DeepEqual(expected, actual), "unexpected API error response")
	assert.True(GinkgoT(), decoder.Decode(new(any)) == io.EOF, "unexpected trailing response data")
}
