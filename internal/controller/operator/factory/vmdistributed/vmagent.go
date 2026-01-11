package vmdistributed

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"time"

	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

const (
	vmagentQueueMetricName = "vm_persistentqueue_bytes_pending"
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
	nsn := types.NamespacedName{Name: vmAgent.GetName(), Namespace: vmAgent.GetNamespace()}
	resultErr := wait.PollUntilContextTimeout(ctx, 5*time.Second, deadline, true, func(ctx context.Context) (done bool, err error) {
		vmagentObj := &vmv1beta1.VMAgent{}
		if err = rclient.Get(ctx, nsn, vmagentObj); err != nil {
			if k8serrors.IsNotFound(err) {
				err = nil
			}
			return
		}
		return vmagentObj.GetGeneration() == vmagentObj.Status.ObservedGeneration && vmagentObj.Status.UpdateStatus == vmv1beta1.UpdateStatusOperational, nil
	})
	if resultErr != nil {
		return resultErr
	}

	// Base values for fallback URL
	baseURL := vmAgent.AsURL()
	metricPath := vmAgent.GetMetricPath()

	logger.WithContext(ctx).Info("Found VMAgent metrics path", "metricPath", metricPath)

	var hosts []string
	// Poll until pod IPs are discovered
	listOpts := &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{"kubernetes.io/service-name": vmAgent.PrefixedName()}),
	}
	resultErr = wait.PollUntilContextTimeout(ctx, 1*time.Second, deadline, true, func(ctx context.Context) (done bool, err error) {
		endpointList := &discoveryv1.EndpointSliceList{}
		if err = rclient.List(ctx, endpointList, listOpts); err != nil {
			if k8serrors.IsNotFound(err) {
				err = nil
			}
			return
		}
		if len(endpointList.Items) == 0 {
			return false, nil
		}
		for _, es := range endpointList.Items {
			logger.WithContext(ctx).Info("Found VMAgent endpoint", "endpoint", es)
			hosts = append(hosts, parseEndpointSliceAddresses(&es)...)
		}
		return len(hosts) > 0, nil
	})
	if resultErr != nil {
		return resultErr
	}

	logger.WithContext(ctx).Info("Found VMAgent hosts", "hosts", hosts)
	for _, ip := range hosts {
		metricsURL := buildPerIPMetricURL(baseURL, metricPath, ip)
		logger.WithContext(ctx).Info("Found VMAgent instance metric URL", "url", metricsURL)

		// Poll until all pod IPs return empty query metric
		resultErr = wait.PollUntilContextTimeout(ctx, interval, deadline, true, func(ctx context.Context) (done bool, err error) {
			// Query each discovered ip. If any returns non-zero metric, continue polling.
			var metricValues map[string]float64
			if metricValues, err = fetchMetricValues(ctx, httpClient, metricsURL, vmagentQueueMetricName, "path"); err != nil {
				// Treat fetch errors as transient -> not ready, continue polling.
				return false, nil
			}
			if len(metricValues) == 0 {
				return false, nil
			}
			for _, v := range metricValues {
				if v > 0 {
					return false, nil
				}
			}
			logger.WithContext(ctx).Info("Found VMAgent instance metric value", "url", metricsURL, "values", metricValues)
			// All discovered addresses reported zero -> done.
			return true, nil
		})
		if resultErr != nil {
			return fmt.Errorf("failed to wait for VMAgent metrics: %w", resultErr)
		}
	}
	return nil
}

// updateOrCreateVMAgent ensures that the VMAgent is updated or created based on the provided VMDistributed.
func updateOrCreateVMAgent(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VMDistributed, vmClusters []*vmv1beta1.VMCluster) (*vmv1beta1.VMAgent, error) {
	logger.WithContext(ctx).Info("Reconciling VMAgent")
	vmagentData, err := json.Marshal(cr.Spec.VMAgent.Spec)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal spec.vmagent.spec of VMDistributed=%s/%s: %w", cr.Name, cr.Namespace, err)
	}

	var vmagentSpec vmv1beta1.VMAgentSpec
	if err := json.Unmarshal(vmagentData, &vmagentSpec); err != nil {
		return nil, fmt.Errorf("failed to unmarshal spec.vmagent.spec of VMDistributed=%s/%s: %w", cr.Name, cr.Namespace, err)
	}
	vmagentSpec.IngestOnlyMode = ptr.To(true)
	if vmagentSpec.RemoteWriteSettings == nil {
		vmagentSpec.RemoteWriteSettings = &vmv1beta1.VMAgentRemoteWriteSettings{}
	}
	vmagentSpec.RemoteWriteSettings.UseMultiTenantMode = true
	if vmagentSpec.License == nil && cr.Spec.License != nil {
		vmagentSpec.License = cr.Spec.License.DeepCopy()
	}

	remoteWrites := make([]vmv1alpha1.VMDistributedAgentRemoteWriteSpec, len(cr.Spec.Zones))
	for i := range cr.Spec.Zones {
		zone := &cr.Spec.Zones[i]
		if zone.RemoteWrite != nil {
			remoteWrites[i] = *zone.RemoteWrite
		}
	}
	remoteWritesData, err := json.Marshal(remoteWrites)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal.spec.zones[*].remoteWrite of VMDistributed=%s/%s: %w", cr.Name, cr.Namespace, err)
	}
	if err := json.Unmarshal(remoteWritesData, &vmagentSpec.RemoteWrite); err != nil {
		return nil, fmt.Errorf("failed to unmarshal.spec.zones[*].remoteWrite of VMDistributed=%s/%s: %w", cr.Name, cr.Namespace, err)
	}

	for i := range vmClusters {
		vmagentSpec.RemoteWrite[i].URL = remoteWriteURL(vmClusters[i])
	}

	// Get existing vmagent obj using Name and cr namespace
	var vmagentObj vmv1beta1.VMAgent
	nsn := types.NamespacedName{Name: cr.Spec.VMAgent.Name, Namespace: cr.Namespace}
	if err := rclient.Get(ctx, nsn, &vmagentObj); err != nil {
		if !k8serrors.IsNotFound(err) {
			return nil, fmt.Errorf("failed to get VMAgent %s: %w", nsn.String(), err)
		}
		// If it doesn't exist, initialize object for creation.
		vmagentObj.Name = cr.Spec.VMAgent.Name
		vmagentObj.Namespace = cr.Namespace
		vmagentObj.OwnerReferences = []metav1.OwnerReference{cr.AsOwner()}
		vmagentObj.Spec = vmagentSpec
		logger.WithContext(ctx).Info("Creating VMAgent")
		if err := rclient.Create(ctx, &vmagentObj); err != nil {
			return nil, fmt.Errorf("failed to create VMAgent %s/%s: %w", vmagentObj.Namespace, vmagentObj.Name, err)
		}
		return &vmagentObj, nil
	}
	orderMap := make(map[string]int)
	for i, rw := range vmagentObj.Spec.RemoteWrite {
		orderMap[rw.URL] = i
	}
	sort.Slice(vmagentSpec.RemoteWrite, func(i, j int) bool {
		return orderMap[vmagentSpec.RemoteWrite[i].URL] < orderMap[vmagentSpec.RemoteWrite[j].URL]
	})

	// Ensure owner reference is set to current CR
	modifiedOwnerRef, err := setOwnerRefIfNeeded(cr, &vmagentObj, rclient.Scheme())
	if err != nil {
		return nil, fmt.Errorf("failed to set owner reference for VMAgent %s: %w", vmagentObj.Name, err)
	}

	// Create or update the vmagent object if spec has changed or it doesn't exist yet
	if modifiedOwnerRef || !equality.Semantic.DeepEqual(vmagentObj.Spec, vmagentSpec) {
		// TODO[vrutkovs]: add diff
		vmagentObj.Spec = vmagentSpec
		logger.WithContext(ctx).Info("Updating VMAgent")
		if err := rclient.Update(ctx, &vmagentObj); err != nil {
			return nil, fmt.Errorf("failed to update VMAgent %s/%s: %w", vmagentObj.Namespace, vmagentObj.Name, err)
		}
	} else {
		logger.WithContext(ctx).Info("VMAgent is up to date")
	}
	return &vmagentObj, nil
}

// remoteWriteURL generates the remote write URL based on the provided VMCluster and tenant.
func remoteWriteURL(vmCluster *vmv1beta1.VMCluster) string {
	insertBaseURL := vmCluster.AsURL(vmv1beta1.ClusterComponentInsert)
	return fmt.Sprintf("%s/insert/multitenant/prometheus/api/v1/write", insertBaseURL)
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
