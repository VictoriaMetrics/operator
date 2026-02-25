package reconcile

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

const podRevisionLabel = "controller-revision-hash"

// STSOptions options for StatefulSet update
// HPA and UpdateReplicaCount optional
type STSOptions struct {
	HasClaim           bool
	SelectorLabels     func() map[string]string
	HPA                *vmv1beta1.EmbeddedHPA
	UpdateReplicaCount func(count *int32)
	UpdateBehavior     *vmv1beta1.StatefulSetUpdateStrategyBehavior
}

func waitForStatefulSetReady(ctx context.Context, rclient client.Client, newObj *appsv1.StatefulSet) error {
	if newObj.Spec.Replicas == nil {
		return nil
	}
	err := wait.PollUntilContextTimeout(ctx, podWaitReadyIntervalCheck, appWaitReadyDeadline, true, func(ctx context.Context) (done bool, err error) {
		var existingObj appsv1.StatefulSet
		if err := rclient.Get(ctx, types.NamespacedName{Namespace: newObj.Namespace, Name: newObj.Name}, &existingObj); err != nil {
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		if existingObj.Generation > existingObj.Status.ObservedGeneration ||
			// special case to prevent possible race condition between updated object and local cache
			// See this issue https://github.com/VictoriaMetrics/operator/issues/1579
			newObj.Generation > existingObj.Generation {
			return false, nil
		}
		if *newObj.Spec.Replicas != existingObj.Status.ReadyReplicas || *newObj.Spec.Replicas != existingObj.Status.UpdatedReplicas {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return reportFirstNotReadyPodOnError(ctx, rclient, fmt.Errorf("cannot wait for statefulSet=%s to become ready: %w", newObj.Name, err), newObj.Namespace, labels.SelectorFromSet(newObj.Spec.Selector.MatchLabels), newObj.Spec.MinReadySeconds)
	}
	return nil
}

// StatefulSet performs create and update operations for given statefulSet with STSOptions
func StatefulSet(ctx context.Context, rclient client.Client, cr STSOptions, newObj, prevObj *appsv1.StatefulSet, owner *metav1.OwnerReference) error {
	if err := validateStatefulSet(newObj); err != nil {
		return err
	}
	var mustRecreatePod bool
	var prevMeta *metav1.ObjectMeta
	var prevTemplateAnnotations map[string]string
	if prevObj != nil {
		prevMeta = &prevObj.ObjectMeta
		prevTemplateAnnotations = prevObj.Spec.Template.Annotations
	}

	rclient.Scheme().Default(newObj)
	updateStrategy := newObj.Spec.UpdateStrategy.Type
	nsn := types.NamespacedName{Name: newObj.Name, Namespace: newObj.Namespace}
	var recreateSTS func() error
	err := retryOnConflict(func() error {
		var existingObj appsv1.StatefulSet
		if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating new StatefulSet=%s", nsn))
				if err = rclient.Create(ctx, newObj); err != nil {
					return fmt.Errorf("cannot create new StatefulSet=%s: %w", nsn, err)
				}
				updateStrategy = appsv1.RollingUpdateStatefulSetStrategyType
				return nil
			}
			return fmt.Errorf("cannot get StatefulSet=%s: %w", nsn, err)
		}
		if err := collectGarbage(ctx, rclient, &existingObj); err != nil {
			return err
		}

		spec := &newObj.Spec
		// will update the original cr replicaCount to propagate right num,
		// for now, it's only used in vmselect
		if cr.UpdateReplicaCount != nil {
			cr.UpdateReplicaCount(existingObj.Spec.Replicas)
		}

		// do not change replicas count.
		if cr.HPA != nil {
			spec.Replicas = existingObj.Spec.Replicas
		}

		var mustRecreateSTS bool
		mustRecreateSTS, mustRecreatePod = isSTSRecreateRequired(ctx, newObj, &existingObj)
		if mustRecreateSTS {
			recreateSTS = func() error {
				return removeStatefulSetKeepPods(ctx, rclient, newObj, &existingObj)
			}
			return nil
		}
		metaChanged, err := mergeMeta(&existingObj, newObj, prevMeta, owner)
		if err != nil {
			return err
		}
		logMessageMetadata := []string{fmt.Sprintf("name=%s, is_prev_nil=%t", nsn, prevObj == nil)}
		spec.Template.Annotations = mergeMaps(existingObj.Spec.Template.Annotations, newObj.Spec.Template.Annotations, prevTemplateAnnotations)
		specDiff := diffDeepDerivative(newObj.Spec, existingObj.Spec)
		needsUpdate := metaChanged || len(specDiff) > 0
		logMessageMetadata = append(logMessageMetadata, fmt.Sprintf("spec_diff=%s", specDiff))
		if !needsUpdate {
			return nil
		}
		existingObj.Spec = newObj.Spec
		logger.WithContext(ctx).Info(fmt.Sprintf("updating Statefulset %s", strings.Join(logMessageMetadata, ", ")))
		if err := rclient.Update(ctx, &existingObj); err != nil {
			return fmt.Errorf("cannot perform update on StatefulSet=%s: %w", nsn, err)
		}
		// check if pvcs need to resize
		if cr.HasClaim {
			return updateSTSPVC(ctx, rclient, &existingObj, owner)
		}
		return nil
	})
	if err != nil {
		return err
	}

	if recreateSTS != nil {
		if err = recreateSTS(); err != nil {
			return err
		}
	}

	// perform manual update only with OnDelete policy, which is default.
	switch updateStrategy {
	case appsv1.OnDeleteStatefulSetStrategyType:
		opts := rollingUpdateOpts{
			recreate:       mustRecreatePod,
			selector:       cr.SelectorLabels(),
			maxUnavailable: 1,
		}
		if cr.UpdateBehavior != nil {
			if cr.UpdateBehavior.MaxUnavailable.String() == "100%" {
				opts.delete = true
			}
			opts.maxUnavailable, err = intstr.GetScaledValueFromIntOrPercent(cr.UpdateBehavior.MaxUnavailable, int(*newObj.Spec.Replicas), false)
			if err != nil {
				return err
			}
		}
		if err := performRollingUpdateOnSts(ctx, rclient, newObj, opts); err != nil {
			return fmt.Errorf("cannot handle rolling-update on StatefulSet=%s: %w", nsn, err)
		}
		return nil
	default:
		logger.WithContext(ctx).Info(fmt.Sprintf("ignoring custom update behavior settings with update strategy=%s on StatefulSet=%s", updateStrategy, nsn))
		if err := waitForStatefulSetReady(ctx, rclient, newObj); err != nil {
			return fmt.Errorf("cannot ensure that statefulset is ready with strategy=%q: %w", updateStrategy, err)
		}
	}
	return err
}

// this change is needed to properly handle revision version fields
// object was processed by controller-manager
// if ObservedGeneration matches current generation
func getLatestStsState(ctx context.Context, rclient client.Client, targetSTS types.NamespacedName) (*appsv1.StatefulSet, error) {
	var sts appsv1.StatefulSet
	err := wait.PollUntilContextTimeout(ctx, podWaitReadyIntervalCheck,
		appWaitReadyDeadline, true, func(ctx context.Context) (done bool, err error) {
			if err := rclient.Get(ctx, targetSTS, &sts); err != nil {
				return true, err
			}
			if sts.Generation > sts.Status.ObservedGeneration {
				return false, nil
			}
			return true, nil
		})

	if err != nil {
		return nil, fmt.Errorf("cannot wait for deployment Generation status transition to=%d, current generation=%d", sts.Generation, sts.Status.ObservedGeneration)
	}
	return &sts, nil
}

type rollingUpdateOpts struct {
	recreate       bool
	maxUnavailable int
	selector       map[string]string
	delete         bool
}

// we perform rolling update on sts by manually evicting pods one by one or in batches
// we check sts revision (kubernetes controller-manager is responsible for that)
// and compare pods revision label with sts revision
// if it doesn't match - updated is needed
//
// we always check if sts.Status.CurrentRevision needs update, to keep it equal to UpdateRevision
// see https://github.com/kubernetes/kube-state-metrics/issues/1324#issuecomment-1779751992
func performRollingUpdateOnSts(ctx context.Context, rclient client.Client, obj *appsv1.StatefulSet, o rollingUpdateOpts) error {
	time.Sleep(podWaitReadyIntervalCheck)
	nsn := types.NamespacedName{
		Name:      obj.Name,
		Namespace: obj.Namespace,
	}
	sts, err := getLatestStsState(ctx, rclient, nsn)
	if err != nil {
		return err
	}
	neededPodCount := 0
	if sts.Spec.Replicas != nil {
		neededPodCount = int(*sts.Spec.Replicas)
	}

	stsVersion := sts.Status.UpdateRevision
	if stsVersion == "" {
		return fmt.Errorf("sts.Status.UpdateRevision is empty. Update cannot be performed. Please check logs of Kubernetes controller-manager or change rollingUpdateStrategy to RollingUpdate")
	}
	l := logger.WithContext(ctx)
	// fast path
	if neededPodCount < 1 {
		l.Info("sts has 0 replicas configured, nothing to update")
		return nil
	}
	l.Info(fmt.Sprintf("check if pod update needed to desiredVersion=%s, podMustRecreate=%v", stsVersion, o.recreate))
	var podList corev1.PodList
	opts := &client.ListOptions{
		Namespace:     obj.Namespace,
		LabelSelector: labels.SelectorFromSet(o.selector),
	}
	if err := rclient.List(ctx, &podList, opts); err != nil {
		return fmt.Errorf("cannot list pods for statefulset rolling update: %w", err)
	}
	if err := sortStsPodsByID(podList.Items); err != nil {
		return fmt.Errorf("cannot sort statefulset pods: %w", err)
	}
	readyPods, updatedPods, podsForUpdate := filterSTSPods(podList.Items, stsVersion, sts.Spec.MinReadySeconds, o.recreate)
	totalPodsCount := len(readyPods) + len(updatedPods) + len(podsForUpdate)

	switch {
	// sanity check, should help to catch possible bugs
	case totalPodsCount > neededPodCount:
		l.Info(fmt.Sprintf("unexpected count of pods=%d, want pod count=%d for sts. "+
			"It seems like configuration of stateful wasn't correct and kubernetes cannot create pod,"+
			" check kubectl events to find out source of problem", totalPodsCount, neededPodCount))
	// usual case when some param misconfigured
	// or kubernetes for some reason cannot create pod
	// it's better to fail fast
	case totalPodsCount < neededPodCount:
		return fmt.Errorf("actual pod count: %d less than needed: %d, possible statefulset misconfiguration", totalPodsCount, neededPodCount)
	}

	updatedNeeded := len(podsForUpdate) != 0 || len(updatedPods) != 0
	if !updatedNeeded {
		l.Info("no pod needs to be updated")
		return nil
	}

	l.Info(fmt.Sprintf("discovered already updated pods=%d, pods needed to be update=%d", len(updatedPods), len(podsForUpdate)))

	// check updated, by not ready pods
	for _, pod := range updatedPods {
		l.Info(fmt.Sprintf("checking ready status for already updated pod %s to revision version=%q", pod.Name, stsVersion))
		podNsn := types.NamespacedName{Namespace: obj.Namespace, Name: pod.Name}
		if err := waitForPodReady(ctx, rclient, podNsn, stsVersion, sts.Spec.MinReadySeconds); err != nil {
			return fmt.Errorf("cannot wait for pod ready state for already updated pod: %w", err)
		}
	}

	// perform update for not updated pods in batches according to podMaxUnavailable
	for batchStart := 0; batchStart < len(podsForUpdate); batchStart += o.maxUnavailable {
		var batch []corev1.Pod

		// determine batch of pods to update
		batchClose := batchStart + o.maxUnavailable
		if batchClose > len(podsForUpdate) {
			batchClose = len(podsForUpdate)
		}
		batch = podsForUpdate[batchStart:batchClose]

		errG, ctx := errgroup.WithContext(ctx)
		for _, pod := range batch {
			errG.Go(func() error {
				l.Info(fmt.Sprintf("updating pod=%s revision label=%q", pod.Name, pod.Labels[podRevisionLabel]))
				// eviction may fail due to podDisruption budget and it's unexpected
				// so retry pod eviction
				evictErr := wait.PollUntilContextTimeout(ctx, podWaitReadyIntervalCheck, podWaitReadyTimeout, true, func(ctx context.Context) (done bool, err error) {
					if o.delete {
						if err := rclient.Delete(ctx, &pod); err != nil {
							if k8serrors.IsNotFound(err) {
								return true, nil
							}
							return false, fmt.Errorf("failed to delete pod %s: %w", pod.Name, err)
						}
						return true, nil
					}

					// evict pod to trigger re-creation
					podEviction := policyv1.Eviction{ObjectMeta: pod.ObjectMeta}
					if err := rclient.SubResource("eviction").Create(ctx, &pod, &podEviction); err != nil {
						// retry distruption interrupt error:
						// https://github.com/kubernetes/kubernetes/blob/9a50e306361ea936e57fb6eb8c635f971e7bb707/pkg/registry/core/pod/storage/eviction.go#L418
						if strings.Contains(err.Error(), "Cannot evict pod as it would violate the pod's disruption budget") {
							return false, nil
						}
						return false, fmt.Errorf("cannot evict pod %s: %w", pod.Name, err)
					}
					return true, nil
				})
				if evictErr != nil {
					return fmt.Errorf("cannot perform pod eviction: %w", evictErr)
				}
				// wait for pod to be re-created
				podNsn := types.NamespacedName{Namespace: obj.Namespace, Name: pod.Name}
				if err := waitForPodReady(ctx, rclient, podNsn, stsVersion, sts.Spec.MinReadySeconds); err != nil {
					return fmt.Errorf("cannot wait for pod ready state during re-creation for pod %s: %w", pod.Name, err)
				}
				l.Info(fmt.Sprintf("pod %s was updated successfully", pod.Name))
				return nil
			})
		}
		if err := errG.Wait(); err != nil {
			return fmt.Errorf("fail to perform batch update with size: %d: %w", len(batch), err)
		}
	}

	l.Info(fmt.Sprintf("finished statefulset update from revision=%q to revision=%q", sts.Status.CurrentRevision, stsVersion))

	return nil
}

// PodIsReady check is pod is ready
func PodIsReady(pod *corev1.Pod, minReadySeconds int32) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == "True" {
			if minReadySeconds > 0 {
				return time.Since(cond.LastTransitionTime.Time) > time.Duration(minReadySeconds)*time.Second
			}
			return true
		}
	}
	return false
}

func waitForPodReady(ctx context.Context, rclient client.Client, nsn types.NamespacedName, desiredRevision string, minReadySeconds int32) error {
	var pod corev1.Pod
	if err := wait.PollUntilContextTimeout(ctx, podWaitReadyIntervalCheck, podWaitReadyTimeout, true, func(ctx context.Context) (done bool, err error) {
		if err := rclient.Get(ctx, nsn, &pod); err != nil {
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, fmt.Errorf("cannot get pod %s: %w", nsn, err)
		}
		if !pod.DeletionTimestamp.IsZero() {
			return false, nil
		}
		revision := pod.Labels[podRevisionLabel]
		if revision != desiredRevision {
			logger.WithContext(ctx).Info(fmt.Sprintf("unexpected pod label %s=%s, want revision=%s", podRevisionLabel, revision, desiredRevision))
			return false, nil
		}
		return PodIsReady(&pod, minReadySeconds), nil
	}); err != nil {
		if len(pod.Name) == 0 {
			return err
		}
		return podStatusesToError(err, &pod)
	}
	return nil
}

func podStatusesToError(origin error, pod *corev1.Pod) error {
	var hasCrashedContainers bool
	var conditions []string
	for _, cond := range pod.Status.Conditions {
		conditions = append(conditions, fmt.Sprintf("name=%s,status=%s,message=%s", cond.Type, cond.Status, cond.Message))
	}

	stateToString := func(state corev1.ContainerState) string {
		switch {
		case state.Running != nil:
			return fmt.Sprintf("running since: %s", state.Running.StartedAt)
		case state.Terminated != nil:
			return fmt.Sprintf("terminated message=%s, exit_code=%d, reason=%s", state.Terminated.Message, state.Terminated.ExitCode, state.Terminated.Reason)
		case state.Waiting != nil:
			return fmt.Sprintf("waiting with reason=%s, message=%s", state.Waiting.Reason, state.Waiting.Message)
		}
		return ""
	}
	isCrashed := func(st corev1.ContainerStatus) bool {
		if st.RestartCount > 0 && st.LastTerminationState.Terminated != nil && st.State.Waiting != nil {
			return true
		}
		if st.State.Waiting != nil && st.State.Waiting.Reason != "PodInitializing" && st.State.Waiting.Message != "" {
			return true
		}
		return false
	}
	var containerStates []string
	addContainerStatus := func(namePrefix string, css []corev1.ContainerStatus) {
		for _, condStatus := range css {
			stateMsg := stateToString(condStatus.LastTerminationState)
			if stateMsg == "" {
				stateMsg = stateToString(condStatus.State)
			}
			if stateMsg == "" {
				continue
			}
			if isCrashed(condStatus) {
				hasCrashedContainers = true
			}
			containerStates = append(containerStates, fmt.Sprintf("%sname=[%s],is_ready=%v,restart_count=%d,state=%s", namePrefix, condStatus.Name, condStatus.Ready, condStatus.RestartCount, stateMsg))
		}
	}
	addContainerStatus("", pod.Status.ContainerStatuses)
	addContainerStatus("init_container_", pod.Status.InitContainerStatuses)
	msg := fmt.Sprintf("pod phase=%s, pod conditions=%s, pod statuses=%s", pod.Status.Phase, strings.Join(conditions, ","), strings.Join(containerStates, ","))
	if hasCrashedContainers {
		return fmt.Errorf("%s: pod has crashed containers", msg)
	}
	return fmt.Errorf("%s: %w", msg, origin)
}

func sortStsPodsByID(src []corev1.Pod) error {
	var firstParseError error
	sort.Slice(src, func(i, j int) bool {
		if firstParseError != nil {
			return false
		}
		pID := func(name string) uint64 {
			n := strings.LastIndexByte(name, '-')
			if n <= 0 {
				firstParseError = fmt.Errorf("cannot find - at the pod name: %s", name)
				return 0
			}
			id, err := strconv.ParseUint(name[n+1:], 10, 64)
			if err != nil {
				firstParseError = fmt.Errorf("cannot parse pod id number: %s from name: %s", name[n+1:], name)
				return 0
			}
			return id
		}
		return pID(src[i].Name) < pID(src[j].Name)
	})
	return firstParseError
}

func isOwnedBySTS(pod *corev1.Pod) bool {
	for _, ref := range pod.OwnerReferences {
		if ref.Kind == "StatefulSet" {
			return true
		}
	}
	return false
}

func filterSTSPods(pods []corev1.Pod, revision string, minReadySeconds int32, recreate bool) ([]corev1.Pod, []corev1.Pod, []corev1.Pod) {
	var readyPods, updatedPods, podsForUpdate []corev1.Pod
	for _, pod := range pods {
		// pod could be owned by Deployment due to Deployment -> StatefulSet transition
		isSameRevision := pod.Labels[podRevisionLabel] == revision
		switch {
		case !isOwnedBySTS(&pod):
		case !pod.DeletionTimestamp.IsZero() || recreate:
			podsForUpdate = append(podsForUpdate, pod)
		case PodIsReady(&pod, minReadySeconds) && isSameRevision:
			readyPods = append(readyPods, pod)
		case isSameRevision:
			updatedPods = append(updatedPods, pod)
		default:
			podsForUpdate = append(podsForUpdate, pod)
		}
	}
	return readyPods, updatedPods, podsForUpdate
}

// validateStatefulSet performs validation Statefulset spec
// Kubernetes doesn't perform some checks and produces runtime error
// during Pod creation.
// VolumeMounts validation is missing:
// https://github.com/kubernetes/kubernetes/blob/b15dfce6cbd0d5bbbcd6172cf7e2082f4d31055e/pkg/apis/apps/validation/validation.go#L66
func validateStatefulSet(sts *appsv1.StatefulSet) error {
	volumeNames := sets.New[string]()
	var joinedNames string
	for _, vl := range sts.Spec.Template.Spec.Volumes {
		if volumeNames.Has(vl.Name) {
			return fmt.Errorf("duplicate Volume.Name=%s", vl.Name)
		}
		volumeNames.Insert(vl.Name)
		joinedNames += vl.Name + ","
	}
	for _, vct := range sts.Spec.VolumeClaimTemplates {
		if volumeNames.Has(vct.Name) {
			return fmt.Errorf("duplicate VolumeClaimTemplate.Name=%s", vct.Name)
		}
		volumeNames.Insert(vct.Name)
		joinedNames += vct.Name + ","
	}
	for _, cnt := range sts.Spec.Template.Spec.Containers {
		for _, vm := range cnt.VolumeMounts {
			if !volumeNames.Has(vm.Name) {
				return fmt.Errorf("cannot find volumeMount.name=%s link for container=%s at volumes and claimTemplates names=%s", vm.Name, cnt.Name, joinedNames)
			}
		}
	}
	for _, cnt := range sts.Spec.Template.Spec.InitContainers {
		for _, vm := range cnt.VolumeMounts {
			if !volumeNames.Has(vm.Name) {
				return fmt.Errorf("cannot find volumeMount.name=%s link for initContainer=%s at volumes and claimTemplates names=%s", vm.Name, cnt.Name, joinedNames)
			}
		}
	}

	return nil
}
