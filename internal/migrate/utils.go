package migrate

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// Polling interval/timeout are vars, not consts, so tests can shrink them.
var (
	TargetReadyPollInterval = 5 * time.Second
	TargetReadyTimeout      = 15 * time.Minute

	PodsGonePollInterval = 2 * time.Second
	PodsGoneTimeout      = 5 * time.Minute
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

// bufferAgentCR is the shape the buffer agent CR must satisfy for both single-node and
// cluster-mode migrations, under either strategy.
type bufferAgentCR interface {
	client.Object
	GetStatusMetadata() *vmv1beta1.StatusMetadata
	queueMetricsSource
	SelectorLabels() map[string]string
}

// ensureBufferAgentRunning creates agent if it doesn't exist yet, or adopts a matching existing
// one left over from a resumed attempt, then waits for it to become ready and reachable.
func ensureBufferAgentRunning[Agent bufferAgentCR](ctx context.Context, c client.Client, agent Agent, targetWriteURL string, agentMatches func(existing Agent, wantURL string) bool) (Agent, error) {
	if err := c.Create(ctx, agent); err != nil {
		if !k8serrors.IsAlreadyExists(err) {
			return agent, fmt.Errorf("cannot create buffer agent %s/%s: %w", agent.GetNamespace(), agent.GetName(), err)
		}
		existing := agent.DeepCopyObject().(Agent)
		if err := c.Get(ctx, types.NamespacedName{Name: agent.GetName(), Namespace: agent.GetNamespace()}, existing); err != nil {
			return agent, fmt.Errorf("cannot fetch existing buffer agent %s/%s: %w", agent.GetNamespace(), agent.GetName(), err)
		}
		if !agentMatches(existing, targetWriteURL) {
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

// deleteBufferAgent removes agent, best-effort, at the end of a successful migration.
func deleteBufferAgent(ctx context.Context, c client.Client, agent bufferAgentCR) {
	if err := c.Delete(ctx, agent); err != nil && !k8serrors.IsNotFound(err) {
		fmt.Printf("warning: cannot delete buffer agent %s/%s, please remove it manually: %v\n", agent.GetNamespace(), agent.GetName(), err)
	}
	fmt.Printf("note: buffer agent %s/%s ran as a StatefulSet, so its persistent-queue PVC was not deleted with it — remove it manually if you don't need it\n", agent.GetNamespace(), agent.GetName())
}

func dryRunCreate(ctx context.Context, c client.Client, obj client.Object) error {
	return c.Create(ctx, obj.DeepCopyObject().(client.Object), client.DryRunAll)
}

func dependentConfigsOf(podSpec *corev1.PodSpec, configMaps []corev1.ConfigMap, secrets []corev1.Secret) ([]corev1.ConfigMap, []corev1.Secret) {
	cmNames := map[string]bool{}
	secretNames := map[string]bool{}
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
	}
	for _, v := range podSpec.Volumes {
		addVolume(v)
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

// hasCutoverCandidate reports whether at least one Service's selector matches baseLabels (the
// old workload's own pod labels), i.e. there's something for cutoverServices to repoint.
func hasCutoverCandidate(services []corev1.Service, baseLabels map[string]string) bool {
	for i := range services {
		if !selectorDiffersFromBase(services[i].Spec.Selector, baseLabels) {
			return true
		}
	}
	return false
}

func selectorDiffersFromBase(selector, baseLabels map[string]string) bool {
	if len(selector) == 0 {
		return true
	}
	for k, v := range selector {
		if baseLabels[k] != v {
			return true
		}
	}
	return false
}

func deleteDependentConfigs(ctx context.Context, c client.Client, configMaps []corev1.ConfigMap, secrets []corev1.Secret) error {
	for i := range configMaps {
		if err := c.Delete(ctx, &configMaps[i]); err != nil && !k8serrors.IsNotFound(err) {
			return fmt.Errorf("cannot delete dependent ConfigMap %s/%s: %w", configMaps[i].Namespace, configMaps[i].Name, err)
		}
	}
	for i := range secrets {
		if err := c.Delete(ctx, &secrets[i]); err != nil && !k8serrors.IsNotFound(err) {
			return fmt.Errorf("cannot delete dependent Secret %s/%s: %w", secrets[i].Namespace, secrets[i].Name, err)
		}
	}
	return nil
}

func refuseExistingTarget(ctx context.Context, c client.Client, target client.Object) error {
	existing := target.DeepCopyObject().(client.Object)
	err := c.Get(ctx, types.NamespacedName{Name: target.GetName(), Namespace: target.GetNamespace()}, existing)
	if err == nil {
		return fmt.Errorf("target CR %s/%s already exists — refusing to proceed with a destructive WithDowntime migration; delete it manually first if that's intended", target.GetNamespace(), target.GetName())
	}
	if !k8serrors.IsNotFound(err) {
		return fmt.Errorf("cannot check for an existing target CR %s/%s: %w", target.GetNamespace(), target.GetName(), err)
	}
	return nil
}

// waitForOperational polls until the operator has reconciled target's current generation and
// reports it Operational.
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
		return status.ObservedGeneration == target.GetGeneration() && status.UpdateStatus == vmv1beta1.UpdateStatusOperational, nil
	})
}

func cutoverServices(ctx context.Context, c client.Client, services []*corev1.Service, newSelector map[string]string) error {
	originalSelectors := make([]map[string]string, len(services))
	for i, svc := range services {
		originalSelectors[i] = svc.Spec.Selector
	}
	for i, svc := range services {
		svc.Spec.Selector = newSelector
		if err := c.Update(ctx, svc); err != nil {
			svc.Spec.Selector = originalSelectors[i]
			updateErr := fmt.Errorf("cannot patch Service %s/%s selector: %w", svc.Namespace, svc.Name, err)
			for j := range i {
				services[j].Spec.Selector = originalSelectors[j]
				if revertErr := c.Update(ctx, services[j]); revertErr != nil {
					return fmt.Errorf("%w (additionally failed to revert Service %s/%s: %v)", updateErr, services[j].Namespace, services[j].Name, revertErr)
				}
			}
			return updateErr
		}
	}
	return nil
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
