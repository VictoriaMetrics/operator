package migrate

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/podutil"
)

// Polling interval/timeout are vars, not consts, so tests can shrink them.
var (
	TargetReadyPollInterval = 5 * time.Second
	TargetReadyTimeout      = 15 * time.Minute
	PodsGonePollInterval    = 2 * time.Second
	PodsGoneTimeout         = 5 * time.Minute
)

func waitPodsGone(ctx context.Context, c client.Client, namespace string, selector map[string]string) error {
	err := wait.PollUntilContextTimeout(ctx, PodsGonePollInterval, PodsGoneTimeout, true, func(ctx context.Context) (bool, error) {
		var pods corev1.PodList
		if err := c.List(ctx, &pods, client.InNamespace(namespace), client.MatchingLabels(selector)); err != nil {
			return false, err
		}
		return len(pods.Items) == 0, nil
	})
	if err != nil {
		return fmt.Errorf("waiting for pods in %s matching %v to terminate: %w", namespace, selector, err)
	}
	return nil
}

type bufferAgentCR interface {
	client.Object
	GetStatusMetadata() *vmv1beta1.StatusMetadata
	queueMetricsSource
	SelectorLabels() map[string]string
	GetExtraArgs() map[string]string
}

const bufferAgentOwnershipLabel = "migrate.victoriametrics.com/buffer-agent"

func ensureBufferAgentRunning[Agent bufferAgentCR](ctx context.Context, c client.Client, agent Agent, targetWriteURL string, agentMatches func(existing, want Agent) bool) (Agent, error) {
	want := agent.DeepCopyObject().(Agent)
	agentLabels := agent.GetLabels()
	if agentLabels == nil {
		agentLabels = map[string]string{}
	}
	agentLabels[bufferAgentOwnershipLabel] = "true"
	agent.SetLabels(agentLabels)
	if err := c.Create(ctx, agent); err != nil {
		if !k8serrors.IsAlreadyExists(err) {
			return agent, fmt.Errorf("cannot create buffer agent %s/%s: %w", agent.GetNamespace(), agent.GetName(), err)
		}
		existing := agent.DeepCopyObject().(Agent)
		if err := c.Get(ctx, types.NamespacedName{Name: agent.GetName(), Namespace: agent.GetNamespace()}, existing); err != nil {
			return agent, fmt.Errorf("cannot fetch existing buffer agent %s/%s: %w", agent.GetNamespace(), agent.GetName(), err)
		}
		if existing.GetLabels()[bufferAgentOwnershipLabel] != "true" {
			return agent, fmt.Errorf("buffer agent %s/%s already exists but wasn't created by this tool — "+
				"refusing to reuse an unrelated resource as the migration buffer; delete it manually before retrying", agent.GetNamespace(), agent.GetName())
		}
		if !agentMatches(existing, want) {
			return agent, fmt.Errorf("buffer agent %s/%s already exists but isn't configured as a persistent buffer forwarding to %q — "+
				"likely left over from a previous migration attempt; delete it manually before retrying", agent.GetNamespace(), agent.GetName(), targetWriteURL)
		}
		agent = existing
	}
	if err := waitForOperational(ctx, c, agent); err != nil {
		return agent, fmt.Errorf("buffer agent did not become ready: %w", err)
	}
	if err := waitEndpointsReady(ctx, c, agent.GetNamespace(), agent.PrefixedName()); err != nil {
		return agent, fmt.Errorf("buffer agent's Service did not become ready: %w", err)
	}
	return agent, nil
}

func deleteBufferAgent(ctx context.Context, c client.Client, agent bufferAgentCR) {
	if err := c.Delete(ctx, agent); err != nil && !k8serrors.IsNotFound(err) {
		fmt.Printf("warning: cannot delete buffer agent %s/%s, please remove it manually: %v\n", agent.GetNamespace(), agent.GetName(), err)
	}
	fmt.Printf("note: buffer agent %s/%s ran as a StatefulSet, so its persistent-queue PVC was not deleted with it — remove it manually if you don't need it\n", agent.GetNamespace(), agent.GetName())
}

func dryRunCreate(ctx context.Context, c client.Client, obj client.Object) error {
	return c.Create(ctx, obj.DeepCopyObject().(client.Object), client.DryRunAll)
}

func podSpecConfigRefs(podSpec *corev1.PodSpec) (cmNames, secretNames map[string]bool) {
	cmNames = map[string]bool{}
	secretNames = map[string]bool{}
	addVolume := func(v corev1.Volume) {
		if v.ConfigMap != nil {
			cmNames[v.ConfigMap.Name] = true
		}
		if v.Secret != nil {
			secretNames[v.Secret.SecretName] = true
		}
		if v.Projected != nil {
			for _, src := range v.Projected.Sources {
				if src.ConfigMap != nil {
					cmNames[src.ConfigMap.Name] = true
				}
				if src.Secret != nil {
					secretNames[src.Secret.Name] = true
				}
			}
		}
		if v.CSI != nil && v.CSI.NodePublishSecretRef != nil {
			secretNames[v.CSI.NodePublishSecretRef.Name] = true
		}
		if v.AzureFile != nil {
			secretNames[v.AzureFile.SecretName] = true
		}
		if v.CephFS != nil && v.CephFS.SecretRef != nil {
			secretNames[v.CephFS.SecretRef.Name] = true
		}
		if v.Cinder != nil && v.Cinder.SecretRef != nil {
			secretNames[v.Cinder.SecretRef.Name] = true
		}
		if v.FlexVolume != nil && v.FlexVolume.SecretRef != nil {
			secretNames[v.FlexVolume.SecretRef.Name] = true
		}
		if v.RBD != nil && v.RBD.SecretRef != nil {
			secretNames[v.RBD.SecretRef.Name] = true
		}
		if v.ScaleIO != nil && v.ScaleIO.SecretRef != nil {
			secretNames[v.ScaleIO.SecretRef.Name] = true
		}
		if v.ISCSI != nil && v.ISCSI.SecretRef != nil {
			secretNames[v.ISCSI.SecretRef.Name] = true
		}
		if v.StorageOS != nil && v.StorageOS.SecretRef != nil {
			secretNames[v.StorageOS.SecretRef.Name] = true
		}
	}
	for _, v := range podSpec.Volumes {
		addVolume(v)
	}
	for _, ips := range podSpec.ImagePullSecrets {
		secretNames[ips.Name] = true
	}
	addEnvRefs := func(envFrom []corev1.EnvFromSource, env []corev1.EnvVar) {
		for _, ef := range envFrom {
			if ef.ConfigMapRef != nil {
				cmNames[ef.ConfigMapRef.Name] = true
			}
			if ef.SecretRef != nil {
				secretNames[ef.SecretRef.Name] = true
			}
		}
		for _, e := range env {
			if e.ValueFrom == nil {
				continue
			}
			if e.ValueFrom.ConfigMapKeyRef != nil {
				cmNames[e.ValueFrom.ConfigMapKeyRef.Name] = true
			}
			if e.ValueFrom.SecretKeyRef != nil {
				secretNames[e.ValueFrom.SecretKeyRef.Name] = true
			}
		}
	}
	for _, cnt := range podSpec.Containers {
		addEnvRefs(cnt.EnvFrom, cnt.Env)
	}
	for _, cnt := range podSpec.InitContainers {
		addEnvRefs(cnt.EnvFrom, cnt.Env)
	}
	for _, cnt := range podSpec.EphemeralContainers {
		addEnvRefs(cnt.EnvFrom, cnt.Env)
	}
	return cmNames, secretNames
}

func dependentConfigsOf(podSpec *corev1.PodSpec, configMaps []corev1.ConfigMap, secrets []corev1.Secret) ([]corev1.ConfigMap, []corev1.Secret) {
	cmNames, secretNames := podSpecConfigRefs(podSpec)
	var cms []corev1.ConfigMap
	for _, cm := range configMaps {
		if cmNames[cm.Name] {
			cms = append(cms, cm)
		}
	}
	var secs []corev1.Secret
	for _, s := range secrets {
		if secretNames[s.Name] {
			secs = append(secs, s)
		}
	}
	return cms, secs
}

func selectorDiffersFromBase(selector, baseLabels map[string]string) bool {
	if len(selector) == 0 {
		return true
	}
	return !maps.Equal(selector, baseLabels)
}

func selectorMatchesBase(selector, baseLabels map[string]string) bool {
	if len(selector) == 0 {
		return false
	}
	for k, v := range selector {
		if baseLabels[k] != v {
			return false
		}
	}
	return true
}

func deleteDependentConfigs(ctx context.Context, c client.Client, target client.Object, configMaps []corev1.ConfigMap, secrets []corev1.Secret) error {
	if len(configMaps) == 0 && len(secrets) == 0 {
		return nil
	}
	targetJSON, err := json.Marshal(target)
	if err != nil {
		return fmt.Errorf("cannot check target CR %s/%s for shared dependent config references: %w", target.GetNamespace(), target.GetName(), err)
	}
	referencedInTarget := func(name string) bool {
		quoted, err := json.Marshal(name)
		if err != nil {
			return true
		}
		return bytes.Contains(targetJSON, quoted)
	}

	namespace := target.GetNamespace()
	liveCMNames := map[string]bool{}
	liveSecretNames := map[string]bool{}
	collect := func(podSpec *corev1.PodSpec) {
		cmNames, secretNames := podSpecConfigRefs(podSpec)
		for n := range cmNames {
			liveCMNames[n] = true
		}
		for n := range secretNames {
			liveSecretNames[n] = true
		}
	}
	var deps appsv1.DeploymentList
	if err := c.List(ctx, &deps, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("cannot list Deployments in %s to check for shared dependent config references: %w", namespace, err)
	}
	for i := range deps.Items {
		collect(&deps.Items[i].Spec.Template.Spec)
	}
	var stss appsv1.StatefulSetList
	if err := c.List(ctx, &stss, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("cannot list StatefulSets in %s to check for shared dependent config references: %w", namespace, err)
	}
	for i := range stss.Items {
		collect(&stss.Items[i].Spec.Template.Spec)
	}
	var dss appsv1.DaemonSetList
	if err := c.List(ctx, &dss, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("cannot list DaemonSets in %s to check for shared dependent config references: %w", namespace, err)
	}
	for i := range dss.Items {
		collect(&dss.Items[i].Spec.Template.Spec)
	}
	var rss appsv1.ReplicaSetList
	if err := c.List(ctx, &rss, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("cannot list ReplicaSets in %s to check for shared dependent config references: %w", namespace, err)
	}
	for i := range rss.Items {
		collect(&rss.Items[i].Spec.Template.Spec)
	}
	var rcs corev1.ReplicationControllerList
	if err := c.List(ctx, &rcs, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("cannot list ReplicationControllers in %s to check for shared dependent config references: %w", namespace, err)
	}
	for i := range rcs.Items {
		if rcs.Items[i].Spec.Template != nil {
			collect(&rcs.Items[i].Spec.Template.Spec)
		}
	}
	var sas corev1.ServiceAccountList
	if err := c.List(ctx, &sas, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("cannot list ServiceAccounts in %s to check for shared dependent config references: %w", namespace, err)
	}
	for i := range sas.Items {
		for _, ips := range sas.Items[i].ImagePullSecrets {
			liveSecretNames[ips.Name] = true
		}
		for _, s := range sas.Items[i].Secrets {
			liveSecretNames[s.Name] = true
		}
	}
	var ingresses networkingv1.IngressList
	if err := c.List(ctx, &ingresses, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("cannot list Ingresses in %s to check for shared dependent config references: %w", namespace, err)
	}
	for i := range ingresses.Items {
		for _, tls := range ingresses.Items[i].Spec.TLS {
			if tls.SecretName != "" {
				liveSecretNames[tls.SecretName] = true
			}
		}
	}
	var jobs batchv1.JobList
	if err := c.List(ctx, &jobs, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("cannot list Jobs in %s to check for shared dependent config references: %w", namespace, err)
	}
	for i := range jobs.Items {
		collect(&jobs.Items[i].Spec.Template.Spec)
	}
	var cronJobs batchv1.CronJobList
	if err := c.List(ctx, &cronJobs, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("cannot list CronJobs in %s to check for shared dependent config references: %w", namespace, err)
	}
	for i := range cronJobs.Items {
		collect(&cronJobs.Items[i].Spec.JobTemplate.Spec.Template.Spec)
	}
	var pods corev1.PodList
	if err := c.List(ctx, &pods, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("cannot list Pods in %s to check for shared dependent config references: %w", namespace, err)
	}
	for i := range pods.Items {
		collect(&pods.Items[i].Spec)
	}

	for i := range configMaps {
		if referencedInTarget(configMaps[i].Name) || liveCMNames[configMaps[i].Name] {
			continue
		}
		if err := c.Delete(ctx, &configMaps[i]); err != nil && !k8serrors.IsNotFound(err) {
			return fmt.Errorf("cannot delete dependent ConfigMap %s/%s: %w", configMaps[i].Namespace, configMaps[i].Name, err)
		}
	}
	for i := range secrets {
		if referencedInTarget(secrets[i].Name) || liveSecretNames[secrets[i].Name] {
			continue
		}
		if err := c.Delete(ctx, &secrets[i]); err != nil && !k8serrors.IsNotFound(err) {
			return fmt.Errorf("cannot delete dependent Secret %s/%s: %w", secrets[i].Namespace, secrets[i].Name, err)
		}
	}
	return nil
}

func waitForOperational(ctx context.Context, c client.Client, target interface {
	client.Object
	GetStatusMetadata() *vmv1beta1.StatusMetadata
}) error {
	nsn := types.NamespacedName{Name: target.GetName(), Namespace: target.GetNamespace()}
	return wait.PollUntilContextTimeout(ctx, TargetReadyPollInterval, TargetReadyTimeout, true, func(ctx context.Context) (bool, error) {
		if err := c.Get(ctx, nsn, target); err != nil {
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		status := target.GetStatusMetadata()
		if status.ObservedGeneration != target.GetGeneration() {
			return false, nil
		}
		if status.UpdateStatus == vmv1beta1.UpdateStatusFailed {
			reason := status.Reason
			if reason == "" {
				reason = "no reason given"
			}
			return false, fmt.Errorf("operator reports %s/%s as failed: %s", target.GetNamespace(), target.GetName(), reason)
		}
		return status.UpdateStatus == vmv1beta1.UpdateStatusOperational, nil
	})
}

const serviceCutoverAnnotation = "migrate.victoriametrics.com/cutover"

func cutoverServices(ctx context.Context, c client.Client, services []*corev1.Service, newSelector map[string]string) error {
	originalSelectors := make([]map[string]string, len(services))
	originalAnnotations := make([]map[string]string, len(services))
	for i, svc := range services {
		originalSelectors[i] = svc.Spec.Selector
		originalAnnotations[i] = svc.Annotations
	}
	for i, svc := range services {
		svc.Spec.Selector = newSelector
		annotations := maps.Clone(svc.Annotations)
		if annotations == nil {
			annotations = map[string]string{}
		}
		annotations[serviceCutoverAnnotation] = "true"
		svc.Annotations = annotations
		if err := c.Update(ctx, svc); err != nil {
			svc.Spec.Selector = originalSelectors[i]
			svc.Annotations = originalAnnotations[i]
			updateErr := fmt.Errorf("cannot patch Service %s/%s selector: %w", svc.Namespace, svc.Name, err)
			for j := range i {
				services[j].Spec.Selector = originalSelectors[j]
				services[j].Annotations = originalAnnotations[j]
				if revertErr := c.Update(ctx, services[j]); revertErr != nil {
					return fmt.Errorf("%w (additionally failed to revert Service %s/%s: %v)", updateErr, services[j].Namespace, services[j].Name, revertErr)
				}
			}
			return updateErr
		}
	}
	return nil
}

func verifyAgentPortCompatible(ctx context.Context, c client.Client, svc *corev1.Service, agent bufferAgentCR) error {
	needsAgentPort := false
	for i := range svc.Spec.Ports {
		port := &svc.Spec.Ports[i]
		if portIsTCP(port) && port.TargetPort.Type == intstr.String && port.TargetPort.StrVal == "http" {
			continue
		}
		needsAgentPort = true
	}
	if !needsAgentPort {
		return nil
	}

	var agentPort int
	addrs, err := podutil.DiscoverEndpointAddrs(ctx, c, agent.GetNamespace(), agent.PrefixedName(), "http", "http", "/")
	if err != nil {
		return fmt.Errorf("cannot discover buffer agent %s/%s's endpoint port: %w", agent.GetNamespace(), agent.PrefixedName(), err)
	}
	found := false
	for addr := range addrs {
		u, err := url.Parse(addr)
		if err != nil {
			return fmt.Errorf("cannot parse buffer agent %s/%s's discovered endpoint %q: %w", agent.GetNamespace(), agent.PrefixedName(), addr, err)
		}
		agentPort, err = strconv.Atoi(u.Port())
		if err != nil {
			return fmt.Errorf("buffer agent %s/%s's discovered endpoint %q has no numeric port: %w", agent.GetNamespace(), agent.PrefixedName(), addr, err)
		}
		found = true
		break
	}
	if !found {
		return fmt.Errorf("buffer agent %s/%s has no discovered endpoint to check its port against", agent.GetNamespace(), agent.PrefixedName())
	}

	for i := range svc.Spec.Ports {
		port := &svc.Spec.Ports[i]
		if !portIsTCP(port) {
			return fmt.Errorf("service %s/%s's port %q uses protocol %q, but the buffer agent only accepts TCP — a selector-only cutover would send this port's traffic nowhere; this port can't be migrated this way",
				svc.Namespace, svc.Name, port.Name, port.Protocol)
		}
		if port.TargetPort.Type == intstr.String {
			if port.TargetPort.StrVal == "http" {
				continue
			}
			return fmt.Errorf("service %s/%s's port %q has targetPort %q, but the buffer agent's pods only expose a port named \"http\" — a selector-only cutover would send this port's traffic nowhere; the buffer agent only handles remote-write, so this port can't be migrated this way",
				svc.Namespace, svc.Name, port.Name, port.TargetPort.StrVal)
		}
		wantTargetPort := port.TargetPort.IntValue()
		if wantTargetPort == 0 {
			wantTargetPort = int(port.Port)
		}
		if wantTargetPort != agentPort {
			return fmt.Errorf("service %s/%s's port %q routes to a fixed pod port %d, but buffer agent %s/%s listens on %d — a selector-only cutover would send this port's traffic nowhere; use a named targetPort \"http\" or align the port numbers before migrating",
				svc.Namespace, svc.Name, port.Name, wantTargetPort, agent.GetNamespace(), agent.PrefixedName(), agentPort)
		}
	}
	return nil
}

func portIsTCP(port *corev1.ServicePort) bool {
	return port.Protocol == "" || port.Protocol == corev1.ProtocolTCP
}

func confirm(prompt string) bool {
	fmt.Printf("%s [y/N]: ", prompt)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

var forceMergeTimeout = 10 * time.Minute

func forceMerge(ctx context.Context, httpClient *http.Client, url string) error {
	ctx, cancel := context.WithTimeout(ctx, forceMergeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("cannot create force-merge request for %s: %w", url, err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("force-merge request to %s failed: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected response status=%d from force-merge request to %s", resp.StatusCode, url)
	}
	return nil
}
