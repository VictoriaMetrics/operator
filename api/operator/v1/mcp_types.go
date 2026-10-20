package v1

import (
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// MCPServerMode defines transport of the MCP server
// +kubebuilder:validation:Enum=http;sse
type MCPServerMode string

const (
	// MCPServerModeHTTP serves Streamable HTTP transport at /mcp path
	MCPServerModeHTTP MCPServerMode = "http"
	// MCPServerModeSSE serves Server-Sent Events transport at /sse and /message paths
	MCPServerModeSSE MCPServerMode = "sse"
)

const (
	// MCPLivenessPath defines path of the MCP server liveness check
	MCPLivenessPath  = "/health/liveness"
	mcpReadinessPath = "/health/readiness"
	mcpDefaultPort   = "8080"
)

// CommonMCPSpec defines the desired state of the MCP server, which is shared by CRDs of MCP servers
type CommonMCPSpec struct {
	// ComponentVersion defines default image tag for the MCP server.
	// It can be overwritten with image.tag value.
	// +optional
	ComponentVersion string `json:"componentVersion,omitempty"`
	// PodMetadata configures Labels and Annotations which are propagated to the MCP server pods.
	// +optional
	PodMetadata *vmv1beta1.EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// ManagedMetadata defines metadata that will be added to the all objects
	// created by operator for the given CustomResource
	// +optional
	ManagedMetadata *vmv1beta1.ManagedObjectsMetadata `json:"managedMetadata,omitempty"`
	// ServiceAccountName is the name of the ServiceAccount to use to run the pods
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`
	// ServiceSpec that will be added to the MCP server service spec
	// +optional
	ServiceSpec *vmv1beta1.AdditionalServiceSpec `json:"serviceSpec,omitempty"`
	// ServiceScrapeSpec that will be added to the MCP server VMServiceScrape spec
	// +optional
	// +kubebuilder:validation:Type=object
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	ServiceScrapeSpec *vmv1beta1.VMServiceScrapeSpec `json:"serviceScrapeSpec,omitempty"`
	// PodDisruptionBudget created by operator
	// +optional
	PodDisruptionBudget *vmv1beta1.EmbeddedPodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`
	// UpdateStrategy - overrides default update strategy.
	// +kubebuilder:validation:Enum=Recreate;RollingUpdate
	// +optional
	UpdateStrategy *appsv1.DeploymentStrategyType `json:"updateStrategy,omitempty"`
	// RollingUpdate - overrides deployment update params.
	// +optional
	RollingUpdate *appsv1.RollingUpdateDeployment `json:"rollingUpdate,omitempty"`
	// Configures vertical pod autoscaling.
	// +optional
	VPA *vmv1beta1.EmbeddedVPA `json:"vpa,omitempty"`
	// NetworkPolicy defines network access rules for pods created by this CR.
	// +optional
	NetworkPolicy *vmv1beta1.EmbeddedNetworkPolicy `json:"networkPolicy,omitempty"`

	// ServerMode defines transport of the MCP server:
	// http serves Streamable HTTP transport at /mcp path,
	// sse serves Server-Sent Events transport at /sse and /message paths.
	// Defaults to http.
	// See https://modelcontextprotocol.io/specification/2025-03-26/basic/transports
	// +optional
	ServerMode MCPServerMode `json:"serverMode,omitempty"`
	// DisabledTools defines names of MCP tools, which are not exposed to MCP clients.
	// If it's not set, the MCP server uses its default list of disabled tools.
	// +optional
	DisabledTools []string `json:"disabledTools,omitempty"`
	// HeartbeatInterval defines how often the MCP server sends heartbeats over Streamable HTTP connections,
	// which keeps them from being closed by gateways and load balancers.
	// Defaults to 30s.
	// +kubebuilder:validation:Pattern:="[0-9]+(ms|s|m|h)"
	// +optional
	HeartbeatInterval string `json:"heartbeatInterval,omitempty"`
	// LogLevel defines minimum log level of the MCP server.
	// +kubebuilder:validation:Enum=debug;info;warn;error
	// +optional
	LogLevel string `json:"logLevel,omitempty"`
	// LogFormat defines log output format of the MCP server.
	// +kubebuilder:validation:Enum=text;json
	// +optional
	LogFormat string `json:"logFormat,omitempty"`

	vmv1beta1.CommonAppsParams `json:",inline,omitempty"`
}

// validate checks if common params of the MCP server are correct
func (s *CommonMCPSpec) validate(prefixedName string) error {
	if s.ServiceSpec != nil && s.ServiceSpec.Name == prefixedName {
		return fmt.Errorf("spec.serviceSpec.Name cannot be equal to prefixed name=%q", prefixedName)
	}
	if len(s.ExtraArgs) > 0 {
		return fmt.Errorf("spec.extraArgs is not supported, since the MCP server is configured with environment variables, use spec.extraEnvs instead")
	}
	if s.HeartbeatInterval != "" {
		d, err := time.ParseDuration(s.HeartbeatInterval)
		if err != nil {
			return fmt.Errorf("cannot parse spec.heartbeatInterval=%q: %w", s.HeartbeatInterval, err)
		}
		if d < 0 {
			return fmt.Errorf("spec.heartbeatInterval=%q cannot be negative", s.HeartbeatInterval)
		}
	}
	if err := validateMCPListItems("disabledTools", s.DisabledTools); err != nil {
		return err
	}
	if s.VPA != nil {
		if err := s.VPA.Validate(); err != nil {
			return err
		}
	}
	// validates params of the embedded CommonAppsParams
	return s.Validate()
}

// validateMCPListItems checks items of the list - headers or tools, which is passed to the MCP server as a comma-separated env var value
func validateMCPListItems(field string, items []string) error {
	for i, item := range items {
		if strings.TrimSpace(item) == "" {
			return fmt.Errorf("spec.%s[%d] cannot be empty", field, i)
		}
		if strings.Contains(item, ",") {
			return fmt.Errorf("spec.%s[%d]=%q cannot contain commas", field, i, item)
		}
	}
	return nil
}

// validateMCPHeaders checks custom headers, which are passed to the MCP server as a comma-separated list of key=value pairs
func validateMCPHeaders(headers []string) error {
	if err := validateMCPListItems("headers", headers); err != nil {
		return err
	}
	for i, h := range headers {
		k, v, ok := strings.Cut(h, "=")
		if !ok || strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
			return fmt.Errorf("spec.headers[%d]=%q must be in key=value format", i, h)
		}
	}
	return nil
}

func mcpSelectorLabels(appName, name string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      appName,
		"app.kubernetes.io/instance":  name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

func (s *CommonMCPSpec) podLabels(selectorLabels map[string]string) map[string]string {
	if s.PodMetadata == nil {
		return selectorLabels
	}
	return labels.Merge(s.PodMetadata.Labels, selectorLabels)
}

func (s *CommonMCPSpec) podAnnotations() map[string]string {
	annotations := map[string]string{}
	if s.PodMetadata != nil {
		for annotation, value := range s.PodMetadata.Annotations {
			annotations[annotation] = value
		}
	}
	return annotations
}

func (s *CommonMCPSpec) finalAnnotations() map[string]string {
	var v map[string]string
	if s.ManagedMetadata != nil {
		v = labels.Merge(s.ManagedMetadata.Annotations, v)
	}
	return v
}

func (s *CommonMCPSpec) resourceLabels(selectorLabels, input map[string]string) map[string]string {
	var v map[string]string
	if s.ManagedMetadata != nil {
		v = s.ManagedMetadata.Labels
	}
	v = labels.Merge(v, input)
	return labels.Merge(v, selectorLabels)
}

func (s *CommonMCPSpec) serviceAccountName(prefixedName string) string {
	if s.ServiceAccountName == "" {
		return prefixedName
	}
	return s.ServiceAccountName
}

func (s *CommonMCPSpec) asURL(prefixedName, namespace string, isExtra bool) string {
	specPort := s.Port
	if specPort == "" {
		specPort = mcpDefaultPort
	}
	svcName, port := vmv1beta1.ResolveServiceURL(prefixedName, specPort, "http", s.ServiceSpec, isExtra)
	return fmt.Sprintf("http://%s.%s.svc:%s", svcName, namespace, port)
}

func mcpOwnerReference(tm metav1.TypeMeta, om metav1.ObjectMeta) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion:         tm.APIVersion,
		Kind:               tm.Kind,
		Name:               om.Name,
		UID:                om.UID,
		Controller:         ptr.To(true),
		BlockOwnerDeletion: ptr.To(true),
	}
}
