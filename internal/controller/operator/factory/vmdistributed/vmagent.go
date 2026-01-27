package vmdistributed

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
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
	httpTimeout            = 10 * time.Second
)

// appendHostsFromEndpointSlice generates hosts from EndpointSlices object and appends to hosts.
func appendHostsFromEndpointSlice(hosts []string, es *discoveryv1.EndpointSlice) []string {
	var port int32
	for _, p := range es.Ports {
		if p.Name != nil && *p.Name == "http" && p.Port != nil {
			port = *p.Port
		}
	}
	if port == 0 {
		return hosts
	}
	for _, ep := range es.Endpoints {
		isTerminating := ep.Conditions.Terminating != nil && *ep.Conditions.Terminating
		isReady := ep.Conditions.Ready == nil || *ep.Conditions.Ready
		if !isReady || isTerminating {
			continue
		}
		for _, a := range ep.Addresses {
			if a == "" {
				continue
			}
			if es.AddressType == discoveryv1.AddressTypeIPv6 {
				hosts = append(hosts, fmt.Sprintf("[%s]:%d", a, port))
			} else {
				hosts = append(hosts, fmt.Sprintf("%s:%d", a, port))
			}
		}
	}
	return hosts
}

// waitForEmptyPQ tries to discover pod IPs via an EndpointSlice named the same as the service
// (prefixed name). If discovery yields addresses, it polls each IP. Otherwise, it falls back to the
// single AsURL()+GetMetricPath() behavior.
func waitForEmptyPQ(ctx context.Context, rclient client.Client, vmAgent *vmv1beta1.VMAgent, interval time.Duration) error {
	httpClient := &http.Client{
		Timeout: httpTimeout,
	}

	var hosts []string
	// Poll until pod IPs are discovered
	listOpts := &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{"kubernetes.io/service-name": vmAgent.PrefixedName()}),
	}
	resultErr := wait.PollUntilContextCancel(ctx, interval, true, func(ctx context.Context) (done bool, err error) {
		var esl discoveryv1.EndpointSliceList
		if err = rclient.List(ctx, &esl, listOpts); err != nil {
			if k8serrors.IsNotFound(err) {
				err = nil
			}
			return
		}
		if len(esl.Items) == 0 {
			return false, nil
		}
		for _, es := range esl.Items {
			hosts = appendHostsFromEndpointSlice(hosts, &es)
		}
		return len(hosts) > 0, nil
	})
	if resultErr != nil {
		return resultErr
	}

	logger.WithContext(ctx).Info("found VMAgent hosts", "items", hosts)
	for _, host := range hosts {
		u := &url.URL{
			Host:   host,
			Scheme: strings.ToLower(vmAgent.ProbeScheme()),
			Path:   vmAgent.GetMetricPath(),
		}
		logger.WithContext(ctx).Info("found VMAgent instance metric URL", "url", u.String())

		// Poll until all pod IPs return empty query metric
		resultErr = wait.PollUntilContextCancel(ctx, interval, true, func(ctx context.Context) (done bool, err error) {
			// Query each discovered ip. If any returns non-zero metric, continue polling.
			var metricValues map[string]float64
			if metricValues, err = fetchMetricValues(ctx, httpClient, u.String(), vmagentQueueMetricName, "path"); err != nil {
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
			logger.WithContext(ctx).Info("found VMAgent instance metric value", "url", u.String(), "values", metricValues)
			// All discovered addresses reported zero -> done.
			return true, nil
		})
		if resultErr != nil {
			return fmt.Errorf("failed to wait for VMAgent metrics: %w", resultErr)
		}
	}
	return nil
}

func buildVMAgent(cr *vmv1alpha1.VMDistributed, vmClusters []*vmv1beta1.VMCluster) (*vmv1beta1.VMAgent, error) {
	vmAgent := vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.VMAgentName(),
			Namespace:       cr.Namespace,
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
		},
	}

	key := fmt.Sprintf("%s/%s", cr.Name, cr.Namespace)
	vmagentData, err := json.Marshal(cr.Spec.VMAgent.Spec)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal spec.vmagent.spec of VMDistributed=%s: %w", key, err)
	}

	var vmagentSpec vmv1beta1.VMAgentSpec
	if err := json.Unmarshal(vmagentData, &vmagentSpec); err != nil {
		return nil, fmt.Errorf("failed to unmarshal spec.vmagent.spec of VMDistributed=%s: %w", key, err)
	}
	vmagentSpec.IngestOnlyMode = ptr.To(true)
	if vmagentSpec.RemoteWriteSettings == nil {
		vmagentSpec.RemoteWriteSettings = &vmv1beta1.VMAgentRemoteWriteSettings{}
	}
	vmagentSpec.RemoteWriteSettings.UseMultiTenantMode = true

	remoteWrites := make([]vmv1alpha1.VMDistributedAgentRemoteWriteSpec, len(cr.Spec.Zones))
	for i := range cr.Spec.Zones {
		zone := &cr.Spec.Zones[i]
		remoteWrites[i] = zone.RemoteWrite
	}

	remoteWritesData, err := json.Marshal(remoteWrites)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal.spec.zones[*].remoteWrite of VMDistributed=%s: %w", key, err)
	}
	if err := json.Unmarshal(remoteWritesData, &vmagentSpec.RemoteWrite); err != nil {
		return nil, fmt.Errorf("failed to unmarshal.spec.zones[*].remoteWrite of VMDistributed=%s: %w", key, err)
	}

	for i := range vmClusters {
		vmagentSpec.RemoteWrite[i].URL = remoteWriteURL(vmClusters[i])
	}
	vmAgent.Spec = vmagentSpec
	return &vmAgent, nil
}

func reconcileVMAgent(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VMDistributed, vmClusters []*vmv1beta1.VMCluster) error {
	vmAgent, err := buildVMAgent(cr, vmClusters)
	if err != nil {
		return fmt.Errorf("failed to build VMAgent: %w", err)
	}

	if err := createOrUpdateVMAgent(ctx, rclient, cr, vmAgent); err != nil {
		return fmt.Errorf("failed to update VMAgent: %w", err)
	}
	if err := waitForStatus(ctx, rclient, vmAgent.DeepCopy(), defaultStatusCheckInterval, vmv1beta1.UpdateStatusOperational); err != nil {
		return fmt.Errorf("failed to wait for VMAgent=%s: %w", vmAgent.Name, err)
	}
	logger.WithContext(ctx).Info("fetching VMAgent metrics", "name", vmAgent.Name)
	if err := waitForEmptyPQ(ctx, rclient, vmAgent, defaultMetricsCheckInterval); err != nil {
		return fmt.Errorf("failed to wait for VMAgent=%s metrics to show no pending queue: %w", vmAgent.Name, err)
	}

	return nil
}

// createOrUpdateVMAgent ensures that the VMAgent is updated or created based on the provided VMDistributed.
func createOrUpdateVMAgent(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VMDistributed, vmAgent *vmv1beta1.VMAgent) error {
	// Get existing vmagent obj using Name and cr namespace
	var prevVMAgent vmv1beta1.VMAgent
	nsn := types.NamespacedName{Name: cr.VMAgentName(), Namespace: cr.Namespace}
	if err := rclient.Get(ctx, nsn, &prevVMAgent); err != nil {
		if !k8serrors.IsNotFound(err) {
			return fmt.Errorf("failed to get VMAgent=%s: %w", nsn, err)
		}
		// If it doesn't exist, initialize object for creation.
		logger.WithContext(ctx).Info("creating VMAgent")
		return rclient.Create(ctx, vmAgent)
	}
	// Ensure owner reference is set to current CR
	modifiedOwnerRef, err := setOwnerRefIfNeeded(cr, &prevVMAgent, rclient.Scheme())
	if err != nil {
		return fmt.Errorf("failed to set owner reference for VMAgent=%s: %w", nsn, err)
	}

	orderMap := make(map[string]int)
	for i, rw := range prevVMAgent.Spec.RemoteWrite {
		orderMap[rw.URL] = i
	}
	sort.Slice(vmAgent.Spec.RemoteWrite, func(i, j int) bool {
		idxI, okI := orderMap[vmAgent.Spec.RemoteWrite[i].URL]
		idxJ, okJ := orderMap[vmAgent.Spec.RemoteWrite[j].URL]
		if !okI {
			return false
		}
		if !okJ {
			return true
		}
		return idxI < idxJ
	})

	// Create or update the vmagent object if spec has changed or it doesn't exist yet
	if !modifiedOwnerRef && equality.Semantic.DeepEqual(prevVMAgent.Spec, vmAgent.Spec) {
		return nil
	}
	// TODO[vrutkovs]: add diff
	prevVMAgent.Spec = vmAgent.Spec
	logger.WithContext(ctx).Info("updating VMAgent")
	return rclient.Update(ctx, &prevVMAgent)
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
