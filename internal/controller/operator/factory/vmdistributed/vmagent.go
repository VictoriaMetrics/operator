package vmdistributed

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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
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
func waitForVMClusterVMAgentMetrics(ctx context.Context, httpClient *http.Client, vmAgent VMAgentWithStatus, deadline, interval time.Duration, rclient client.Client) error {
	// Wait for vmAgent to become ready
	err := wait.PollUntilContextTimeout(ctx, 5*time.Second, deadline, true, func(ctx context.Context) (done bool, err error) {
		vmAgentObj := &vmv1beta1.VMAgent{}
		namespacedName := types.NamespacedName{Name: vmAgent.GetName(), Namespace: vmAgent.GetNamespace()}
		err = rclient.Get(ctx, namespacedName, vmAgentObj)
		if err != nil {
			return false, err
		}
		return vmAgentObj.GetGeneration() == vmAgentObj.Status.ObservedGeneration && vmAgentObj.Status.UpdateStatus == vmv1beta1.UpdateStatusOperational, nil
	})
	if err != nil {
		return err
	}

	// Base values for fallback URL
	baseURL := vmAgent.AsURL()
	metricPath := vmAgent.GetMetricPath()

	logger.WithContext(ctx).Info("Found VMAgent metrics path", "metricPath", metricPath)

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
		logger.WithContext(ctx).Info("Found VMAgent endpoint", "endpoint", &endpointList.Items[0])
		hosts = parseEndpointSliceAddresses(&endpointList.Items[0])
		return len(hosts) > 0, nil
	})
	if err != nil {
		return err
	}

	logger.WithContext(ctx).Info("Found VMAgent hosts", "hosts", hosts)
	for _, ip := range hosts {
		metricsURL := buildPerIPMetricURL(baseURL, metricPath, ip)
		logger.WithContext(ctx).Info("Found VMAgent instance metric URL", "url", metricsURL)

		// Poll until all pod IPs return empty query metric
		err = wait.PollUntilContextTimeout(ctx, interval, deadline, true, func(ctx context.Context) (done bool, err error) {
			// Query each discovered ip. If any returns non-zero metric, continue polling.
			metricValue, ferr := fetchVMAgentDiskBufferMetric(ctx, httpClient, metricsURL)
			logger.WithContext(ctx).Info("Found VMAgent instance metric value", "url", metricsURL, "value", metricValue)
			if ferr != nil {
				// Treat fetch errors as transient -> not ready, continue polling.
				return false, nil
			}
			if metricValue != 0 {
				return false, nil
			}
			// All discovered addresses reported zero -> done.
			return true, nil
		})
		if err != nil {
			return fmt.Errorf("failed to wait for VMAgent metrics: %w", err)
		}
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
			values := strings.Split(value, " ")
			res, err := strconv.ParseFloat(values[len(values)-1], 64)
			if err != nil {
				return 0, fmt.Errorf("could not parse metric value %s: %w", metric, err)
			}
			return int64(res), nil
		}
	}
	return 0, fmt.Errorf("metric %s not found", VMAgentQueueMetricName)
}

// updateOrCreateVMAgent ensures that the VMAgent is updated or created based on the provided VMDistributed.
func updateOrCreateVMAgent(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VMDistributed, vmClusters []*vmv1beta1.VMCluster) (*vmv1beta1.VMAgent, error) {
	logger.WithContext(ctx).Info("Reconciling VMAgent")

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

	// Preserve existing VMAgent RemoteWrite order when possible to avoid unnecessary updates.
	// New VMCluster URLs will be appended to the end of the list.
	remoteWriteURLs := make([]string, 0, len(vmClusters))
	for _, vmCluster := range vmClusters {
		remoteWriteURLs = append(remoteWriteURLs, remoteWriteURL(vmCluster))
	}

	// Map CR-provided remoteWrite entries by URL so we can preserve auth/config if present.
	writeSpecMap := map[string]vmv1alpha1.CustomVMAgentRemoteWriteSpec{}
	for _, rw := range desiredVMAgentSpec.RemoteWrite {
		writeSpecMap[rw.URL] = rw
	}

	if !vmAgentExists {
		// New VMAgent
		desiredVMAgentSpec.RemoteWrite = make([]vmv1alpha1.CustomVMAgentRemoteWriteSpec, len(remoteWriteURLs))
		for i, url := range remoteWriteURLs {
			if crrw, ok := writeSpecMap[url]; ok {
				desiredVMAgentSpec.RemoteWrite[i] = crrw
			}
			desiredVMAgentSpec.RemoteWrite[i].URL = url
		}
	} else {
		// Existing VMAgent
		preserveVMAgentOrder(&desiredVMAgentSpec, remoteWriteURLs, vmAgentObj.Spec.RemoteWrite, writeSpecMap)
	}

	// Assemble new VMAgentSpec
	newVMAgentSpec := vmv1beta1.VMAgentSpec{}
	newVMAgentSpec.PodMetadata = desiredVMAgentSpec.PodMetadata
	newVMAgentSpec.ManagedMetadata = desiredVMAgentSpec.ManagedMetadata
	newVMAgentSpec.LogLevel = desiredVMAgentSpec.LogLevel
	newVMAgentSpec.LogFormat = desiredVMAgentSpec.LogFormat
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
	// If License is not set in VMAgent spec but is set in VMDistributed, use the VMDistributed License
	if newVMAgentSpec.License == nil && cr.Spec.License != nil {
		newVMAgentSpec.License = cr.Spec.License
	}
	newVMAgentSpec.ServiceAccountName = desiredVMAgentSpec.ServiceAccountName
	newVMAgentSpec.CommonDefaultableParams = desiredVMAgentSpec.CommonDefaultableParams
	newVMAgentSpec.CommonApplicationDeploymentParams = desiredVMAgentSpec.CommonApplicationDeploymentParams

	if desiredVMAgentSpec.RemoteWriteSettings == nil {
		desiredVMAgentSpec.RemoteWriteSettings = &vmv1beta1.VMAgentRemoteWriteSettings{}
	}
	desiredVMAgentSpec.RemoteWriteSettings.UseMultiTenantMode = true

	newVMAgentSpec.IngestOnlyMode = true
	newVMAgentSpec.RemoteWriteSettings = desiredVMAgentSpec.RemoteWriteSettings
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

	// Compare current spec with desired spec and apply if different
	if !reflect.DeepEqual(vmAgentObj.Spec, newVMAgentSpec) {
		vmAgentObj.Spec = *newVMAgentSpec.DeepCopy()
		vmAgentNeedsUpdate = true
	}

	// Ensure owner reference is set to current CR
	if modifiedOwnerRef, err := setOwnerRefIfNeeded(cr, vmAgentObj, rclient.Scheme()); err != nil {
		// setOwnerRefIfNeeded already returns wrapped error
		return nil, fmt.Errorf("failed to set owner reference for VMAgent %s: %w", vmAgentObj.Name, err)
	} else if modifiedOwnerRef {
		vmAgentNeedsUpdate = true
	}

	// Create or update the vmagent object if spec has changed or it doesn't exist yet
	switch {
	case !vmAgentExists:
		// TODO[vrutkovs]: add diff
		logger.WithContext(ctx).Info("Creating VMAgent")
		if err := rclient.Create(ctx, vmAgentObj); err != nil {
			return nil, fmt.Errorf("failed to create VMAgent %s/%s: %w", vmAgentObj.Namespace, vmAgentObj.Name, err)
		}
	case vmAgentNeedsUpdate:
		// TODO[vrutkovs]: add diff
		logger.WithContext(ctx).Info("Updating VMAgent")

		if err := rclient.Update(ctx, vmAgentObj); err != nil {
			return nil, fmt.Errorf("failed to update VMAgent %s/%s: %w", vmAgentObj.Namespace, vmAgentObj.Name, err)
		}
	default:
		logger.WithContext(ctx).Info("VMAgent is up to date")
	}
	return vmAgentObj, nil
}

// remoteWriteURL generates the remote write URL based on the provided VMCluster and tenant.
func remoteWriteURL(vmCluster *vmv1beta1.VMCluster) string {
	var defaultVMInsertPort = config.MustGetBaseConfig().VMClusterDefault.VMInsertDefault.Port
	vmHost := vmCluster.PrefixedName(vmv1beta1.ClusterComponentInsert)
	vmInsertPort := ""
	if vmCluster.Spec.VMInsert != nil {
		vmInsertPort = vmCluster.Spec.VMInsert.Port
	}
	if vmCluster.Spec.RequestsLoadBalancer.Enabled && !vmCluster.Spec.RequestsLoadBalancer.DisableInsertBalancing {
		vmInsertPort = vmCluster.Spec.RequestsLoadBalancer.Spec.Port
		vmHost = vmCluster.PrefixedName(vmv1beta1.ClusterComponentBalancer)
	}
	if vmInsertPort == "" {
		vmInsertPort = defaultVMInsertPort
	}
	return fmt.Sprintf("http://%s.%s.svc.cluster.local.:%s/insert/multitenant/prometheus/api/v1/write", vmHost, vmCluster.Namespace, vmInsertPort)
}

func preserveVMAgentOrder(desiredVMAgentSpec *vmv1alpha1.CustomVMAgentSpec, remoteWriteURLs []string, vmAgentWriteSpec []vmv1beta1.VMAgentRemoteWriteSpec, writeSpecMap map[string]vmv1alpha1.CustomVMAgentRemoteWriteSpec) {
	desiredVMAgentSpec.RemoteWrite = make([]vmv1alpha1.CustomVMAgentRemoteWriteSpec, 0, len(remoteWriteURLs))
	used := map[string]bool{}
	// Build a map to lookup desired URLs
	desiredSet := map[string]bool{}
	for _, u := range remoteWriteURLs {
		desiredSet[u] = true
	}
	// First, keep URLs in the same order as existing VMAgent.Spec.RemoteWrite
	for _, existing := range vmAgentWriteSpec {
		if !desiredSet[existing.URL] {
			continue
		}
		customSpec := vmv1alpha1.CustomVMAgentRemoteWriteSpec{
			URL: existing.URL,
		}
		// Use existing spec if available, always rewrite URL
		if crrw, ok := writeSpecMap[existing.URL]; ok {
			customSpec = *crrw.DeepCopy()
			customSpec.URL = existing.URL
		}
		desiredVMAgentSpec.RemoteWrite = append(desiredVMAgentSpec.RemoteWrite, customSpec)
		used[existing.URL] = true
	}
	// Append any new URLs that were not present in the existing spec
	for _, newURL := range remoteWriteURLs {
		if used[newURL] {
			continue
		}
		desiredVMAgentSpec.RemoteWrite = append(desiredVMAgentSpec.RemoteWrite, vmv1alpha1.CustomVMAgentRemoteWriteSpec{
			URL: newURL,
		})
	}
}

func listVMAgents(ctx context.Context, rclient client.Client, namespace string, labelSelector *metav1.LabelSelector) ([]*vmv1beta1.VMAgent, error) {
	labelsSelector, err := metav1.LabelSelectorAsSelector(labelSelector)
	if err != nil {
		return nil, fmt.Errorf("cannot parse selector as labelSelector: %w", err)
	}

	vmAgentList := &vmv1beta1.VMAgentList{}
	if err := rclient.List(ctx, vmAgentList, client.InNamespace(namespace), client.MatchingLabelsSelector{Selector: labelsSelector}); err != nil {
		return nil, err
	}
	vmAgents := make([]*vmv1beta1.VMAgent, 0, len(vmAgentList.Items))
	for i := range vmAgentList.Items {
		vmAgents = append(vmAgents, &vmAgentList.Items[i])
	}
	return vmAgents, nil
}
