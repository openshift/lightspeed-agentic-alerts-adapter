package framework

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

// CurrentToken reads oc's current token without exposing command diagnostics.
func CurrentToken(ctx context.Context) (string, error) {
	output, err := exec.CommandContext(ctx, "oc", "whoami", "-t").Output()
	if err != nil {
		return "", errors.New("cannot obtain the current Kubernetes token")
	}
	token := strings.TrimSpace(string(output))
	if token == "" {
		return "", errors.New("oc returned an empty Kubernetes token")
	}
	return token, nil
}

type InvestigationClient struct {
	client   *http.Client
	endpoint string
}

func NewInvestigationClient(
	address, namespace string,
) (*InvestigationClient, error) {
	endpoint, err := url.Parse("https://" + address)
	if err != nil {
		return nil, errors.New("invalid investigation address")
	}
	if endpoint.Hostname() != "127.0.0.1" || endpoint.User != nil {
		return nil, errors.New("investigation address must use 127.0.0.1 without user information")
	}
	if endpoint.Path != "" || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, errors.New("investigation address must contain only a host and optional port")
	}
	endpoint.Path = "/api/v1/investigations"
	return &InvestigationClient{
		endpoint: endpoint.String(),
		client: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					MinVersion:         tls.VersionTLS12,
					InsecureSkipVerify: true, // E2E traffic uses the loopback oc port-forward.
					ServerName: fmt.Sprintf(
						"lightspeed-agentic-alerts-adapter.%s.svc", namespace,
					),
				},
			},
		},
	}, nil
}

// Request sends a bearer token only for this request; callers must close the body.
func (client *InvestigationClient) Request(
	ctx context.Context, token, body string,
) (*http.Response, error) {
	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, client.endpoint, strings.NewReader(body),
	)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	return client.client.Do(request)
}

func (client *InvestigationClient) Close() {
	client.client.CloseIdleConnections()
}
