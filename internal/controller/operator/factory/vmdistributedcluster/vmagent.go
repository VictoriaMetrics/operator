package vmdistributedcluster

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	discoveryv1 "k8s.io/api/discovery/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

const (
	VMAgentQueueMetricName = "vm_persistentqueue_bytes_pending"
)

// VMAgentMetrics defines the interface for VMAgent objects that can provide metrics URLs
type VMAgentMetrics interface {
	AsURL() string
	GetMetricPath() string
}

// VMAgentWithStatus extends VMAgentMetrics to include status checking
type VMAgentWithStatus interface {
	VMAgentMetrics
	PrefixedName() string
	GetName() string
	GetNamespace() string
}

// parseEndpointSliceAddresses extracts IPv4/IPv6 addresses from an EndpointSlice object.
func parseEndpointSliceAddresses(es *discoveryv1.EndpointSlice) []string {
	addrs := make([]string, 0)
	for _, ep := range es.Endpoints {
		for _, a := range ep.Addresses {
			if a != "" {
				addrs = append(addrs, a)
			}
		}
	}
	return addrs
}

// buildPerIPMetricURL constructs a per-IP metrics URL using the base service URL to
// infer scheme and (optionally) port. This centralizes scheme/port detection so the
// polling logic remains concise.
func buildPerIPMetricURL(baseURL, metricPath, ip string) string {
	scheme := "http"
	port := ""
	if u, err := url.Parse(baseURL); err == nil {
		if u.Scheme != "" {
			scheme = u.Scheme
		}
		if p := u.Port(); p != "" {
			port = p
		}
	}
	if port == "" {
		// Default VMAgent metrics port
		port = "8429"
	}
	return fmt.Sprintf("%s://%s:%s%s", scheme, ip, port, metricPath)
}

// waitForVMClusterVMAgentMetrics accepts a VMAgentWithStatus interface plus a client.Client.
// This allows callers that already have an implementation of VMAgentWithStatus (e.g., tests' mocks)
// to call this function directly. Callers with a concrete *vmv1beta1.VMAgent can pass it directly
// since it implements the required methods.
//
// The function will try to discover pod IPs via an EndpointSlice named the same as the service
// (prefixed name). If discovery yields addresses, it polls each IP. Otherwise, it falls back to the
// single AsURL()+GetMetricPath() behavior.
func waitForVMClusterVMAgentMetrics(ctx context.Context, httpClient *http.Client, vmAgent VMAgentWithStatus, deadline time.Duration, rclient client.Client) error {
	// Wait for vmAgent to become ready
	err := wait.PollUntilContextTimeout(ctx, 5*time.Second, deadline, true, func(ctx context.Context) (done bool, err error) {
		vmAgentObj := &vmv1beta1.VMAgent{}
		namespacedName := types.NamespacedName{Name: vmAgent.GetName(), Namespace: vmAgent.GetNamespace()}
		err = rclient.Get(ctx, namespacedName, vmAgentObj)
		if err != nil {
			return false, err
		}
		return vmAgentObj.Status.UpdateStatus == vmv1beta1.UpdateStatusOperational, nil
	})
	if err != nil {
		return err
	}

	// Base values for fallback URL
	baseURL := vmAgent.AsURL()
	metricPath := vmAgent.GetMetricPath()

	var hosts []string
	// Poll until pod IPs are discovered
	err = wait.PollUntilContextTimeout(ctx, 1*time.Second, deadline, true, func(ctx context.Context) (done bool, err error) {
		endpointList := &discoveryv1.EndpointSliceList{}
		if err := rclient.List(ctx, endpointList, &client.ListOptions{
			LabelSelector: labels.SelectorFromSet(labels.Set{"kubernetes.io/service-name": vmAgent.PrefixedName()}),
		}); err != nil {
			return false, err
		}
		if len(endpointList.Items) == 0 {
			return false, nil
		}
		hosts = parseEndpointSliceAddresses(&endpointList.Items[0])
		return len(hosts) > 0, nil
	})
	if err != nil {
		return err
	}

	// Poll until all pod IPs return empty query metric
	err = wait.PollUntilContextTimeout(ctx, 1*time.Second, deadline, true, func(ctx context.Context) (done bool, err error) {
		// Query each discovered ip. If any returns non-zero metric, continue polling.
		for _, ip := range hosts {
			metricsURL := buildPerIPMetricURL(baseURL, metricPath, ip)
			metricValue, ferr := fetchVMAgentDiskBufferMetric(ctx, httpClient, metricsURL)
			if ferr != nil {
				// Treat fetch errors as transient -> not ready, continue polling.
				return false, nil
			}
			if metricValue != 0 {
				return false, nil
			}
		}
		// All discovered addresses reported zero -> done.
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("failed to wait for VMAgent metrics: %w", err)
	}
	return nil
}

func fetchVMAgentDiskBufferMetric(ctx context.Context, httpClient *http.Client, metricsPath string) (int64, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, metricsPath, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create request for VMAgent at %s: %w", metricsPath, err)
	}
	resp, err := httpClient.Do(request)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch metrics from VMAgent at %s: %w", metricsPath, err)
	}
	defer resp.Body.Close()
	metricBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read VMAgent metrics at %s: %w", metricsPath, err)
	}
	metrics := string(metricBytes)
	for _, metric := range strings.Split(metrics, "\n") {
		value, found := strings.CutPrefix(metric, VMAgentQueueMetricName)
		if found {
			value = strings.Trim(value, " ")
			res, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return 0, fmt.Errorf("could not parse metric value %s: %w", metric, err)
			}
			return int64(res), nil
		}
	}
	return 0, fmt.Errorf("metric %s not found", VMAgentQueueMetricName)
}

// updateOrCreateVMAgent ensures that the VMAgent is updated or created based on the provided VMDistributedCluster.
func updateOrCreateVMAgent(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VMDistributedCluster, scheme *runtime.Scheme, vmClusters []*vmv1beta1.VMCluster) (*vmv1beta1.VMAgent, error) {
	// Get existing vmagent obj using Name and cr namespace
	vmAgentExists := true
	vmAgentNeedsUpdate := false
	vmAgentObj := &vmv1beta1.VMAgent{}
	namespacedName := types.NamespacedName{Name: cr.Spec.VMAgent.Name, Namespace: cr.Namespace}
	if err := rclient.Get(ctx, namespacedName, vmAgentObj); err != nil {
		if k8serrors.IsNotFound(err) {
			vmAgentExists = false
			// If it doesn't exist, initialize object for creation.
			vmAgentObj = &vmv1beta1.VMAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cr.Spec.VMAgent.Name,
					Namespace: cr.Namespace,
				},
			}
		} else {
			return nil, fmt.Errorf("failed to get VMAgent %s/%s: %w", cr.Namespace, cr.Spec.VMAgent.Name, err)
		}
	}

	// Prepare the desired spec for VMAgent.
	desiredVMAgentSpec := vmv1alpha1.CustomVMAgentSpec{}
	if cr.Spec.VMAgent.Spec != nil {
		desiredVMAgentSpec = *cr.Spec.VMAgent.Spec.DeepCopy()
	}

	// Determine tenant id (default to "0" when not provided)
	tenantID := "0"
	if cr.Spec.VMAgent.TenantID != nil && *cr.Spec.VMAgent.TenantID != "" {
		tenantID = *cr.Spec.VMAgent.TenantID
	}

	tenantPtr := &tenantID

	// Point VMAgent to all VMClusters by constructing RemoteWrite entries
	if len(vmClusters) > 0 {
		desiredVMAgentSpec.RemoteWrite = make([]vmv1alpha1.CustomVMAgentRemoteWriteSpec, len(vmClusters))
		for i, vmCluster := range vmClusters {
			desiredVMAgentSpec.RemoteWrite[i].URL = remoteWriteURL(vmCluster, tenantPtr)
		}
	}

	// Assemble new VMAgentSpec
	newVMAgentSpec := vmv1beta1.VMAgentSpec{}
	newVMAgentSpec.PodMetadata = desiredVMAgentSpec.PodMetadata
	newVMAgentSpec.ManagedMetadata = desiredVMAgentSpec.ManagedMetadata
	newVMAgentSpec.LogLevel = desiredVMAgentSpec.LogLevel
	newVMAgentSpec.LogFormat = desiredVMAgentSpec.LogFormat
	newVMAgentSpec.RemoteWriteSettings = desiredVMAgentSpec.RemoteWriteSettings
	newVMAgentSpec.ShardCount = desiredVMAgentSpec.ShardCount
	newVMAgentSpec.UpdateStrategy = desiredVMAgentSpec.UpdateStrategy
	newVMAgentSpec.RollingUpdate = desiredVMAgentSpec.RollingUpdate
	newVMAgentSpec.PodDisruptionBudget = desiredVMAgentSpec.PodDisruptionBudget
	newVMAgentSpec.EmbeddedProbes = desiredVMAgentSpec.EmbeddedProbes
	newVMAgentSpec.StatefulMode = desiredVMAgentSpec.StatefulMode
	newVMAgentSpec.StatefulStorage = desiredVMAgentSpec.StatefulStorage
	newVMAgentSpec.StatefulRollingUpdateStrategy = desiredVMAgentSpec.StatefulRollingUpdateStrategy
	newVMAgentSpec.PersistentVolumeClaimRetentionPolicy = desiredVMAgentSpec.PersistentVolumeClaimRetentionPolicy
	newVMAgentSpec.ClaimTemplates = desiredVMAgentSpec.ClaimTemplates
	newVMAgentSpec.License = desiredVMAgentSpec.License
	// If License is not set in VMAgent spec but is set in VMDistributedCluster, use the VMDistributedCluster License
	if newVMAgentSpec.License == nil && cr.Spec.License != nil {
		newVMAgentSpec.License = cr.Spec.License
	}
	newVMAgentSpec.ServiceAccountName = desiredVMAgentSpec.ServiceAccountName
	newVMAgentSpec.VMAgentSecurityEnforcements = desiredVMAgentSpec.VMAgentSecurityEnforcements
	newVMAgentSpec.CommonDefaultableParams = desiredVMAgentSpec.CommonDefaultableParams
	newVMAgentSpec.CommonConfigReloaderParams = desiredVMAgentSpec.CommonConfigReloaderParams
	newVMAgentSpec.CommonApplicationDeploymentParams = desiredVMAgentSpec.CommonApplicationDeploymentParams

	if len(desiredVMAgentSpec.RemoteWrite) > 0 {
		newVMAgentSpec.RemoteWrite = make([]vmv1beta1.VMAgentRemoteWriteSpec, len(desiredVMAgentSpec.RemoteWrite))
		for i, remoteWrite := range desiredVMAgentSpec.RemoteWrite {
			vmAgentRemoteWrite := vmv1beta1.VMAgentRemoteWriteSpec{}
			vmAgentRemoteWrite.URL = remoteWrite.URL
			vmAgentRemoteWrite.BasicAuth = remoteWrite.BasicAuth
			vmAgentRemoteWrite.BearerTokenSecret = remoteWrite.BearerTokenSecret
			vmAgentRemoteWrite.OAuth2 = remoteWrite.OAuth2
			vmAgentRemoteWrite.TLSConfig = remoteWrite.TLSConfig
			vmAgentRemoteWrite.SendTimeout = remoteWrite.SendTimeout
			vmAgentRemoteWrite.Headers = remoteWrite.Headers
			vmAgentRemoteWrite.MaxDiskUsage = remoteWrite.MaxDiskUsage
			vmAgentRemoteWrite.ForceVMProto = remoteWrite.ForceVMProto
			vmAgentRemoteWrite.ProxyURL = remoteWrite.ProxyURL
			vmAgentRemoteWrite.AWS = remoteWrite.AWS
			newVMAgentSpec.RemoteWrite[i] = vmAgentRemoteWrite
		}
	}

	// Compare current spec with desired spec and apply if different
	if !reflect.DeepEqual(vmAgentObj.Spec, newVMAgentSpec) {
		vmAgentObj.Spec = *newVMAgentSpec.DeepCopy()
		vmAgentNeedsUpdate = true
	}

	// Ensure owner reference is set to current CR
	if modifiedOwnerRef, err := setOwnerRefIfNeeded(cr, vmAgentObj, scheme); err != nil {
		// setOwnerRefIfNeeded already returns wrapped error
		return nil, fmt.Errorf("failed to set owner reference for VMAgent %s: %w", vmAgentObj.Name, err)
	} else if modifiedOwnerRef {
		vmAgentNeedsUpdate = true
	}

	// Create or update the vmagent object if spec has changed or it doesn't exist yet
	if !vmAgentExists {
		if err := rclient.Create(ctx, vmAgentObj); err != nil {
			return nil, fmt.Errorf("failed to create VMAgent %s/%s: %w", vmAgentObj.Namespace, vmAgentObj.Name, err)
		}
	} else if vmAgentNeedsUpdate {
		if err := rclient.Update(ctx, vmAgentObj); err != nil {
			return nil, fmt.Errorf("failed to update VMAgent %s/%s: %w", vmAgentObj.Namespace, vmAgentObj.Name, err)
		}
	}
	return vmAgentObj, nil
}

// remoteWriteURL generates the remote write URL based on the provided VMCluster and tenant.
func remoteWriteURL(vmCluster *vmv1beta1.VMCluster, tenant *string) string {
	return fmt.Sprintf("http://%s.%s.svc.cluster.local.:8480/insert/%s/prometheus/api/v1/write", vmCluster.PrefixedName(vmv1beta1.ClusterComponentInsert), vmCluster.Namespace, *tenant)
}
