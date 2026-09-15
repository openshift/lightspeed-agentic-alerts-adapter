package alertmanager

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/prometheus/alertmanager/api/v2/models"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	monitoringNamespace        = "openshift-monitoring"
	alertmanagerRoute          = "alertmanager-main"
	alertmanagerServiceAccount = "prometheus-k8s"
	ingressCANamespace         = "openshift-config-managed"
	ingressCAConfigMap         = "default-ingress-cert"
	ingressCAKey               = "ca-bundle.crt"
	tokenRefreshWindow         = time.Minute
)

var routeGVR = schema.GroupVersionResource{
	Group:    "route.openshift.io",
	Version:  "v1",
	Resource: "routes",
}

// SpokeConfig holds settings for querying Alertmanager on a spoke cluster.
type SpokeConfig struct {
	Kubeconfig []byte
}

// SpokeClient queries Alertmanager on a spoke OpenShift cluster.
type SpokeClient struct {
	kubeClient  kubernetes.Interface
	routeClient dynamic.Interface
	tokenMu     sync.Mutex
	token       string
	tokenExpiry time.Time
}

// NewSpokeFromKubeconfig creates a SpokeClient from a kubeconfig file.
func NewSpokeFromKubeconfig(cfg SpokeConfig) (*SpokeClient, error) {
	if len(cfg.Kubeconfig) == 0 {
		return nil, fmt.Errorf("alertmanager: kubeconfig is empty")
	}

	restCfg, err := clientcmd.RESTConfigFromKubeConfig(cfg.Kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("alertmanager: loading kubeconfig: %w", err)
	}

	kubeClient, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("alertmanager: creating kubeconfig client: %w", err)
	}
	routeClient, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("alertmanager: creating route client: %w", err)
	}

	return &SpokeClient{
		kubeClient:  kubeClient,
		routeClient: routeClient,
	}, nil
}

// GetAlerts retrieves alerts from Alertmanager using its OpenShift Route.
func (c *SpokeClient) GetAlerts(ctx context.Context) (alerts models.GettableAlerts, retErr error) {
	route, err := c.routeClient.Resource(routeGVR).Namespace(monitoringNamespace).Get(ctx, alertmanagerRoute, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("alertmanager: getting route: %w", err)
	}
	host, found, err := unstructured.NestedString(route.Object, "spec", "host")
	if err != nil || !found || host == "" {
		return nil, fmt.Errorf("alertmanager: route has no host")
	}

	token, err := c.bearerToken(ctx)
	if err != nil {
		return nil, err
	}

	caConfigMap, err := c.kubeClient.CoreV1().ConfigMaps(ingressCANamespace).Get(ctx, ingressCAConfigMap, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("alertmanager: getting ingress ca bundle: %w", err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM([]byte(caConfigMap.Data[ingressCAKey])) {
		return nil, fmt.Errorf("alertmanager: no valid certificates in ingress ca bundle")
	}

	u, err := url.Parse("https://" + host + "/api/v2/alerts")
	if err != nil {
		return nil, fmt.Errorf("alertmanager: parsing route url: %w", err)
	}
	q := u.Query()
	q.Set("active", "true")
	q.Set("silenced", "false")
	q.Set("inhibited", "false")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("alertmanager: creating route request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    caPool,
				MinVersion: tls.VersionTLS12,
			},
		},
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("alertmanager: querying route: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil && retErr == nil {
			retErr = fmt.Errorf("alertmanager: closing route response: %w", err)
		}
	}()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		if resp.StatusCode == http.StatusUnauthorized {
			c.invalidateToken()
		}
		return nil, fmt.Errorf("alertmanager: route query failed: status %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(&alerts); err != nil {
		return nil, fmt.Errorf("alertmanager: decoding route response: %w", err)
	}

	return alerts, nil
}

func (c *SpokeClient) bearerToken(ctx context.Context) (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	if c.token != "" && time.Until(c.tokenExpiry) > tokenRefreshWindow {
		return c.token, nil
	}

	response, err := c.kubeClient.CoreV1().ServiceAccounts(monitoringNamespace).CreateToken(
		ctx,
		alertmanagerServiceAccount,
		&authenticationv1.TokenRequest{},
		metav1.CreateOptions{},
	)
	if err != nil {
		return "", fmt.Errorf("alertmanager: creating token: %w", err)
	}
	if response.Status.Token == "" {
		return "", fmt.Errorf("alertmanager: token response is empty")
	}

	c.token = response.Status.Token
	c.tokenExpiry = response.Status.ExpirationTimestamp.Time
	return c.token, nil
}

func (c *SpokeClient) invalidateToken() {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	c.token = ""
	c.tokenExpiry = time.Time{}
}
