package reconcile

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
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
}

func waitForStatefulSetReady(ctx context.Context, rclient client.Client, newSts *appsv1.StatefulSet) error {
	err := wait.PollUntilContextTimeout(ctx, podWaitReadyIntervalCheck, appWaitReadyDeadline, false, func(ctx context.Context) (done bool, err error) {
		// fast path
		if newSts.Spec.Replicas == nil {
			return true, nil
		}
		var stsForStatus appsv1.StatefulSet
		if err := rclient.Get(ctx, types.NamespacedName{Namespace: newSts.Namespace, Name: newSts.Name}, &stsForStatus); err != nil {
			return false, err
		}
		if stsForStatus.Generation > stsForStatus.Status.ObservedGeneration {
			return false, nil
		}
		if *newSts.Spec.Replicas != stsForStatus.Status.ReadyReplicas || *newSts.Spec.Replicas != stsForStatus.Status.UpdatedReplicas {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return reportFirstNotReadyPodOnError(ctx, rclient, fmt.Errorf("cannot wait for statefulSet=%s to become ready: %w", newSts.Name, err), newSts.Namespace, labels.SelectorFromSet(newSts.Spec.Selector.MatchLabels), newSts.Spec.MinReadySeconds)
	}
	return nil
}

// HandleSTSUpdate performs create and update operations for given statefulSet with STSOptions
func HandleSTSUpdate(ctx context.Context, rclient client.Client, cr STSOptions, newSts, prevSts *appsv1.StatefulSet) error {
	if err := validateStatefulSet(newSts); err != nil {
		return err
	}
	var isPrevEqual bool
	var prevSpecDiff string
	if prevSts != nil {
		isPrevEqual = equality.Semantic.DeepDerivative(prevSts.Spec, newSts.Spec)
		if !isPrevEqual {
			prevSpecDiff = diffDeepDerivative(prevSts.Spec, newSts.Spec)
		}
	}
	rclient.Scheme().Default(newSts)

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var currentSts appsv1.StatefulSet
		if err := rclient.Get(ctx, types.NamespacedName{Name: newSts.Name, Namespace: newSts.Namespace}, &currentSts); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating new StatefulSet %s", newSts.Name))
				if err = rclient.Create(ctx, newSts); err != nil {
					return fmt.Errorf("cannot create new sts %s under namespace %s: %w", newSts.Name, newSts.Namespace, err)
				}
				return waitForStatefulSetReady(ctx, rclient, newSts)
			}
			return fmt.Errorf("cannot get sts %s under namespace %s: %w", newSts.Name, newSts.Namespace, err)
		}
		if err := finalize.FreeIfNeeded(ctx, rclient, &currentSts); err != nil {
			return err
		}

		// will update the original cr replicaCount to propagate right num,
		// for now, it's only used in vmselect
		if cr.UpdateReplicaCount != nil {
			cr.UpdateReplicaCount(currentSts.Spec.Replicas)
		}

		// do not change replicas count.
		if cr.HPA != nil {
			newSts.Spec.Replicas = currentSts.Spec.Replicas
		}
		// hack for kubernetes 1.18
		newSts.Status.Replicas = currentSts.Status.Replicas

		stsRecreated, podMustRecreate, err := recreateSTSIfNeed(ctx, rclient, newSts, &currentSts)
		if err != nil {
			return err
		}

		// if sts wasn't recreated, update it first
		// before making call for performRollingUpdateOnSts
		if !stsRecreated {
			var prevAnnotations, prevTemplateAnnotations map[string]string
			if prevSts != nil {
				prevAnnotations = prevSts.Annotations
				prevTemplateAnnotations = prevSts.Spec.Template.Annotations
			}
			isEqual := equality.Semantic.DeepDerivative(newSts.Spec, currentSts.Spec)
			shouldSkipUpdate := isPrevEqual &&
				isEqual &&
				equality.Semantic.DeepEqual(newSts.Labels, currentSts.Labels) &&
				isAnnotationsEqual(currentSts.Annotations, newSts.Annotations, prevAnnotations)

			if !shouldSkipUpdate {

				vmv1beta1.AddFinalizer(newSts, &currentSts)
				newSts.Annotations = mergeAnnotations(currentSts.Annotations, newSts.Annotations, prevAnnotations)
				newSts.Spec.Template.Annotations = mergeAnnotations(currentSts.Spec.Template.Annotations, newSts.Spec.Template.Annotations, prevTemplateAnnotations)
				cloneSignificantMetadata(newSts, &currentSts)

				logMsg := fmt.Sprintf("updating statefulset %s configuration, is_current_equal=%v,is_prev_equal=%v,is_prev_nil=%v",
					newSts.Name, isEqual, isPrevEqual, prevSts == nil)
				if !isEqual {
					logMsg += fmt.Sprintf(", current_spec_diff=%s", diffDeepDerivative(newSts.Spec, currentSts.Spec))
				}
				if len(prevSpecDiff) > 0 {
					logMsg += fmt.Sprintf(", prev_spec_diff=%s", prevSpecDiff)
				}

				logger.WithContext(ctx).Info(logMsg)

				if err := rclient.Update(ctx, newSts); err != nil {
					return fmt.Errorf("cannot perform update on sts: %s, err: %w", newSts.Name, err)
				}
			}
		}

		// perform manual update only with OnDelete policy, which is default.
		if newSts.Spec.UpdateStrategy.Type == appsv1.OnDeleteStatefulSetStrategyType {
			if err := performRollingUpdateOnSts(ctx, podMustRecreate, rclient, newSts.Name, newSts.Namespace, cr.SelectorLabels()); err != nil {
				return fmt.Errorf("cannot handle rolling-update on sts: %s, err: %w", newSts.Name, err)
			}
		} else {
			if err := waitForStatefulSetReady(ctx, rclient, newSts); err != nil {
				return fmt.Errorf("cannot ensure that statefulset is ready with strategy=%q: %w", newSts.Spec.UpdateStrategy.Type, err)
			}
		}

		// check if pvcs need to resize
		if cr.HasClaim {
			err = growSTSPVC(ctx, rclient, newSts)
		}

		return err
	})
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

// we perform rolling update on sts by manually deleting pods one by one
// we check sts revision (kubernetes controller-manager is responsible for that)
// and compare pods revision label with sts revision
// if it doesn't match - updated is needed
//
// we always check if sts.Status.CurrentRevision needs update, to keep it equal to UpdateRevision
// see https://github.com/kubernetes/kube-state-metrics/issues/1324#issuecomment-1779751992
func performRollingUpdateOnSts(ctx context.Context, podMustRecreate bool, rclient client.Client, stsName string, ns string, podLabels map[string]string) error {
	time.Sleep(podWaitReadyIntervalCheck)
	sts, err := getLatestStsState(ctx, rclient, types.NamespacedName{Name: stsName, Namespace: ns})
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
	l.Info(fmt.Sprintf("check if pod update needed to desiredVersion=%s, podMustRecreate=%v", stsVersion, podMustRecreate))
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(podLabels)
	listOps := &client.ListOptions{Namespace: ns, LabelSelector: labelSelector}
	if err := rclient.List(ctx, podList, listOps); err != nil {
		return fmt.Errorf("cannot list pods for statefulset rolling update: %w", err)
	}
	keepOnlyStsPods(podList)
	if err := sortStsPodsByID(podList.Items); err != nil {
		return fmt.Errorf("cannot sort statefulset pods: %w", err)
	}
	switch {
	// sanity check, should help to catch possible bugs
	case len(podList.Items) > neededPodCount:
		l.Info(fmt.Sprintf("unexpected count of pods=%d, want pod count=%d for sts. "+
			"It seems like configuration of stateful wasn't correct and kubernetes cannot create pod,"+
			" check kubectl events to find out source of problem", len(podList.Items), neededPodCount))
	// usual case when some param misconfigured
	// or kubernetes for some reason cannot create pod
	// it's better to fail fast
	case len(podList.Items) < neededPodCount:
		return fmt.Errorf("actual pod count: %d less than needed: %d, possible statefulset misconfiguration", len(podList.Items), neededPodCount)
	}

	// first we must ensure, that already updated pods in ready status
	// then we can update other pods
	// if pod is not ready
	// it must be at first place for update
	podsForUpdate := make([]corev1.Pod, 0, len(podList.Items))
	// if pods were already updated to some version, we have to wait its readiness
	updatedPods := make([]corev1.Pod, 0, len(podList.Items))

	if podMustRecreate {
		podsForUpdate = podList.Items
	} else {
		for _, pod := range podList.Items {
			podRev := pod.Labels[podRevisionLabel]
			if podRev == stsVersion {
				// wait for readiness only for not ready pods
				if !PodIsReady(&pod, sts.Spec.MinReadySeconds) {
					updatedPods = append(updatedPods, pod)
				}
				continue
			}

			// move unready pods to the begging of list for update
			if !PodIsReady(&pod, sts.Spec.MinReadySeconds) {
				podsForUpdate = append([]corev1.Pod{pod}, podsForUpdate...)
				continue
			}

			podsForUpdate = append(podsForUpdate, pod)
		}
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
		podNsn := types.NamespacedName{Namespace: ns, Name: pod.Name}
		err := waitForPodReady(ctx, rclient, podNsn, stsVersion, sts.Spec.MinReadySeconds)
		if err != nil {
			return fmt.Errorf("cannot wait for pod ready state for already updated pod: %w", err)
		}
	}

	// perform update for not updated pods
	for _, pod := range podsForUpdate {
		l.Info(fmt.Sprintf("updating pod=%s revision label=%q", pod.Name, pod.Labels[podRevisionLabel]))
		// we have to delete pod and wait for it readiness
		err := rclient.Delete(ctx, &pod)
		if err != nil {
			return err
		}
		podNsn := types.NamespacedName{Namespace: ns, Name: pod.Name}
		if err = waitForPodReady(ctx, rclient, podNsn, stsVersion, sts.Spec.MinReadySeconds); err != nil {
			return fmt.Errorf("cannot wait for pod ready state during re-creation: %w", err)
		}
		l.Info(fmt.Sprintf("pod %s was updated successfully", pod.Name))
	}

	l.Info(fmt.Sprintf("finished statefulset update from revision=%q to revision=%q", sts.Status.CurrentRevision, stsVersion))

	return nil
}

// PodIsReady check is pod is ready
func PodIsReady(pod *corev1.Pod, minReadySeconds int32) bool {
	if pod.DeletionTimestamp != nil {
		return false
	}

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
	var pod *corev1.Pod
	if err := wait.PollUntilContextTimeout(ctx, podWaitReadyIntervalCheck, podWaitReadyTimeout, false, func(_ context.Context) (done bool, err error) {
		pod = &corev1.Pod{}
		err = rclient.Get(ctx, nsn, pod)
		if err != nil {
			return false, fmt.Errorf("cannot get pod: %q: %w", nsn, err)
		}
		revision := pod.Labels[podRevisionLabel]
		if revision != desiredRevision {
			return true, fmt.Errorf("unexpected pod label %s=%s, want revision=%s", podRevisionLabel, revision, desiredRevision)
		}
		if PodIsReady(pod, minReadySeconds) {
			return true, nil
		}
		return false, nil
	}); err != nil {
		if pod == nil {
			return err
		}
		return podStatusesToError(err, pod)
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
	err := fmt.Errorf("origin_Err=%w,podPhase=%s,pod conditions=%s,pod statuses = %s", origin, pod.Status.Phase, strings.Join(conditions, ","), strings.Join(containerStates, ","))
	if hasCrashedContainers {
		return err
	}
	return &errWaitReady{origin: err}
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

func keepOnlyStsPods(podList *corev1.PodList) {
	var cnt int
	for _, pod := range podList.Items {
		var ownedBySts bool
		for _, ow := range pod.OwnerReferences {
			if ow.Kind == "StatefulSet" {
				ownedBySts = true
				break
			}
		}
		// pod could be owned by Deployment due to Deployment -> StatefulSet transition
		if !ownedBySts {
			continue
		}
		podList.Items[cnt] = pod
		cnt++
	}
	podList.Items = podList.Items[:cnt]
}

// validateStatefulSet performs validation Statefulset spec
// Kubernetes doesn't perform some checks and produces runtime error
// during Pod creation.
// VolumeMounts validation is missing:
// https://github.com/kubernetes/kubernetes/blob/b15dfce6cbd0d5bbbcd6172cf7e2082f4d31055e/pkg/apis/apps/validation/validation.go#L66
func validateStatefulSet(sts *appsv1.StatefulSet) error {
	volumeNames := make(map[string]struct{}, len(sts.Spec.Template.Spec.Volumes))
	var joinedNames string
	for _, vl := range sts.Spec.Template.Spec.Volumes {
		if _, ok := volumeNames[vl.Name]; ok {
			return fmt.Errorf("duplicate Volume.Name=%s", vl.Name)
		}
		volumeNames[vl.Name] = struct{}{}
		joinedNames += vl.Name + ","
	}
	for _, vct := range sts.Spec.VolumeClaimTemplates {
		if _, ok := volumeNames[vct.Name]; ok {
			return fmt.Errorf("duplicate VolumeClaimTemplate.Name=%s", vct.Name)
		}
		volumeNames[vct.Name] = struct{}{}
		joinedNames += vct.Name + ","
	}
	for _, cnt := range sts.Spec.Template.Spec.Containers {
		for _, vm := range cnt.VolumeMounts {
			if _, ok := volumeNames[vm.Name]; !ok {
				return fmt.Errorf("cannot find volumeMount.name=%s link for container=%s at volumes and claimTemplates names=%s", vm.Name, cnt.Name, joinedNames)
			}
		}
	}
	for _, cnt := range sts.Spec.Template.Spec.InitContainers {
		for _, vm := range cnt.VolumeMounts {
			if _, ok := volumeNames[vm.Name]; !ok {
				return fmt.Errorf("cannot find volumeMount.name=%s link for initContainer=%s at volumes and claimTemplates names=%s", vm.Name, cnt.Name, joinedNames)
			}
		}
	}

	return nil
}
