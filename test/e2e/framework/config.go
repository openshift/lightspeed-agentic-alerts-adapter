package framework

import "os"

const (
	defaultNamespace         = "openshift-lightspeed"
	defaultDeploymentName    = "lightspeed-agentic-alerts-adapter"
	defaultOperatorNamespace = "openshift-lightspeed"
	defaultOperatorDeployment = "lightspeed-agentic-operator"
)

type Config struct {
	Namespace           string
	DeploymentName      string
	Image               string
	OperatorNamespace   string
	OperatorDeployment  string
}

func LoadConfig() Config {
	return Config{
		Namespace:          envOrDefault("NAMESPACE", defaultNamespace),
		DeploymentName:     envOrDefault("DEPLOYMENT_NAME", defaultDeploymentName),
		Image:              os.Getenv("IMAGE"),
		OperatorNamespace:  envOrDefault("OPERATOR_NAMESPACE", defaultOperatorNamespace),
		OperatorDeployment: envOrDefault("OPERATOR_DEPLOYMENT", defaultOperatorDeployment),
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
