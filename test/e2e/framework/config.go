package framework

import "os"

const (
	defaultNamespace          = "openshift-lightspeed"
	defaultDeploymentName     = "lightspeed-agentic-alerts-adapter"
	defaultOperatorNamespace  = "openshift-lightspeed"
	defaultOperatorDeployment = "lightspeed-agentic-operator"
	defaultAdapterApiAddress  = "127.0.0.1:8080"
)

type Config struct {
	Namespace          string
	DeploymentName     string
	Image              string
	OperatorNamespace  string
	OperatorDeployment string
	AdapterApiAddress  string
}

func LoadConfig() Config {
	return Config{
		Namespace:          envOrDefault("NAMESPACE", defaultNamespace),
		DeploymentName:     envOrDefault("DEPLOYMENT_NAME", defaultDeploymentName),
		Image:              os.Getenv("IMAGE"),
		OperatorNamespace:  envOrDefault("OPERATOR_NAMESPACE", defaultOperatorNamespace),
		OperatorDeployment: envOrDefault("OPERATOR_DEPLOYMENT", defaultOperatorDeployment),
		AdapterApiAddress:  envOrDefault("ADAPTER_API_ADDRESS", defaultAdapterApiAddress),
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
