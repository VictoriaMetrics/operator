package migrate

import (
	"bufio"
	"context"
	"fmt"
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

// waitPodsGone polls until no Pod in namespace matches selector.
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

// operationalCR is the minimal shape WaitForOperational needs. Kept separate from
// singleNodeCR since VMCluster/VLCluster's PrefixedName/SelectorLabels take a component-kind
// argument, so they satisfy this but not singleNodeCR.
type operationalCR interface {
	client.Object
	GetStatusMetadata() *vmv1beta1.StatusMetadata
}

// singleNodeCR is the shape shared by VMSingle and VLSingle that WithDowntimeSingleNode needs.
type singleNodeCR interface {
	operationalCR
	PrefixedName() string
	SelectorLabels() map[string]string
}

// WithDowntimeSingleNode runs the WithDowntime strategy for a single-node chart: delete the
// old Deployment, rebind its PV under the target CR's PVC name, create the target CR, wait
// for it to become ready, then repoint the release's Service at the new pods.
func WithDowntimeSingleNode(ctx context.Context, c client.Client, opts Options, target singleNodeCR) error {
	d, err := Discover(ctx, c, opts.Namespace, opts.ReleaseName)
	if err != nil {
		return fmt.Errorf("discovery failed: %w", err)
	}
	if d.Deployment == nil {
		return fmt.Errorf("no Deployment found for release %q in namespace %q", opts.ReleaseName, opts.Namespace)
	}
	pvc, err := d.SingleNodePVC()
	if err != nil {
		return fmt.Errorf("storage discovery failed: %w", err)
	}

	targetPVCName := target.PrefixedName()
	fmt.Printf("plan: delete Deployment %s/%s, rebind PVC %s/%s -> %s/%s (PV %s), create %s %s/%s, repoint Service selector to %v\n",
		d.Deployment.Namespace, d.Deployment.Name,
		pvc.Namespace, pvc.Name, target.GetNamespace(), targetPVCName, pvc.Spec.VolumeName,
		target.GetObjectKind().GroupVersionKind().Kind, target.GetNamespace(), target.GetName(),
		target.SelectorLabels())

	if opts.DryRun {
		fmt.Println("dry-run: stopping before any mutation")
		return nil
	}
	if !opts.Yes {
		if !Confirm("proceed with the above plan?") {
			return fmt.Errorf("aborted by user")
		}
	}

	dependentConfigMaps, dependentSecrets := dependentConfigsOf(&d.Deployment.Spec.Template.Spec, d.ConfigMaps, d.Secrets)

	if err := c.Delete(ctx, d.Deployment); err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("cannot delete old Deployment %s/%s: %w", d.Deployment.Namespace, d.Deployment.Name, err)
	}
	if err := waitPodsGone(ctx, c, d.Deployment.Namespace, d.Deployment.Spec.Selector.MatchLabels); err != nil {
		return err
	}
	if err := deleteDependentConfigs(ctx, c, dependentConfigMaps, dependentSecrets); err != nil {
		return err
	}

	if _, err := RebindPVC(ctx, c, pvc, targetPVCName, target.GetNamespace()); err != nil {
		return fmt.Errorf("storage rebind failed: %w", err)
	}

	if err := c.Create(ctx, target); err != nil {
		return fmt.Errorf("cannot create target CR %s/%s: %w", target.GetNamespace(), target.GetName(), err)
	}

	if err := WaitForOperational(ctx, c, target); err != nil {
		return fmt.Errorf("target CR did not become ready: %w", err)
	}

	baseLabels := d.Deployment.Spec.Selector.MatchLabels
	var svcPtrs []*corev1.Service
	for i := range d.Services {
		if hasExtraSelectorKeys(d.Services[i].Spec.Selector, baseLabels) {
			fmt.Printf("warning: Service %s/%s has a selector beyond the release's own pod labels, skipping cutover — update it manually\n", d.Services[i].Namespace, d.Services[i].Name)
			continue
		}
		svcPtrs = append(svcPtrs, &d.Services[i])
	}
	if err := CutoverServices(ctx, c, svcPtrs, target.SelectorLabels()); err != nil {
		return fmt.Errorf("traffic cutover failed: %w", err)
	}

	fmt.Println("migration complete")
	return nil
}

// dependentConfigsOf returns only the ConfigMaps/Secrets podSpec actually references, so a
// co-labeled but unrelated object never gets deleted.
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

func hasExtraSelectorKeys(selector, baseLabels map[string]string) bool {
	for k := range selector {
		if _, ok := baseLabels[k]; !ok {
			return true
		}
	}
	return false
}

// deleteDependentConfigs deletes the given ConfigMaps/Secrets, tolerating them already gone.
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

// WaitForOperational polls until the operator has reconciled target's current generation and
// reports it Operational.
func WaitForOperational(ctx context.Context, c client.Client, target operationalCR) error {
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

// CutoverServices patches every Service's selector to point at the new pods, preserving the
// Service's name/DNS entry. Services are passed by pointer so a caller invoking this twice on
// the same Service sees each Update's refreshed ResourceVersion rather than conflicting.
func CutoverServices(ctx context.Context, c client.Client, services []*corev1.Service, newSelector map[string]string) error {
	originalSelectors := make([]map[string]string, len(services))
	for i, svc := range services {
		originalSelectors[i] = svc.Spec.Selector
	}
	for i, svc := range services {
		svc.Spec.Selector = newSelector
		if err := c.Update(ctx, svc); err != nil {
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

// RevertSelectorAndFail reverts svc's selector back to origSelector before returning err,
// since traffic may be flowing through a not-yet-proven new setup.
func RevertSelectorAndFail(ctx context.Context, c client.Client, svc *corev1.Service, origSelector map[string]string, err error) error {
	if revertErr := CutoverServices(ctx, c, []*corev1.Service{svc}, origSelector); revertErr != nil {
		return fmt.Errorf("%w (additionally failed to revert Service selector: %v)", err, revertErr)
	}
	return err
}

// Confirm prompts the user on stdin/stdout for an interactive yes/no confirmation.
func Confirm(prompt string) bool {
	fmt.Printf("%s [y/N]: ", prompt)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}
