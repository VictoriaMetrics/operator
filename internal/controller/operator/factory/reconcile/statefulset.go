package reconcile

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const podRevisionLabel = "controller-revision-hash"

// STSOptions options for StatefulSet update
// HPA and UpdateReplicaCount optional
type STSOptions struct {
	HasClaim           bool
	SelectorLabels     func() map[string]string
	VolumeName         func() string
	HPA                *vmv1beta1.EmbeddedHPA
	UpdateReplicaCount func(count *int32)
}

func waitForStatefulSetReady(ctx context.Context, rclient client.Client, newSts *appsv1.StatefulSet, c *config.BaseOperatorConf) error {
	err := wait.PollUntilContextTimeout(ctx, c.PodWaitReadyIntervalCheck, c.PodWaitReadyTimeout, false, func(ctx context.Context) (done bool, err error) {
		// fast path
		if newSts.Spec.Replicas == nil {
			return true, nil
		}
		var stsForStatus appsv1.StatefulSet
		if err := rclient.Get(ctx, types.NamespacedName{Namespace: newSts.Namespace, Name: newSts.Name}, &stsForStatus); err != nil {
			return false, err
		}
		if *newSts.Spec.Replicas != stsForStatus.Status.ReadyReplicas || *newSts.Spec.Replicas != stsForStatus.Status.UpdatedReplicas {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return reportFirstNotReadyPodOnError(ctx, rclient, fmt.Errorf("cannot wait for statefulSet to become ready: %w", err), newSts.Namespace, labels.SelectorFromSet(newSts.Spec.Selector.MatchLabels), newSts.Spec.MinReadySeconds)
	}
	return nil
}

// HandleSTSUpdate performs create and update operations for given statefulSet with STSOptions
func HandleSTSUpdate(ctx context.Context, rclient client.Client, cr STSOptions, newSts *appsv1.StatefulSet, c *config.BaseOperatorConf) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var currentSts appsv1.StatefulSet
		if err := rclient.Get(ctx, types.NamespacedName{Name: newSts.Name, Namespace: newSts.Namespace}, &currentSts); err != nil {
			if errors.IsNotFound(err) {
				if err = rclient.Create(ctx, newSts); err != nil {
					return fmt.Errorf("cannot create new sts %s under namespace %s: %w", newSts.Name, newSts.Namespace, err)
				}
				return waitForStatefulSetReady(ctx, rclient, newSts, c)
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
		newSts.Spec.Template.Annotations = labels.Merge(currentSts.Spec.Template.Annotations, newSts.Spec.Template.Annotations)
		vmv1beta1.AddFinalizer(newSts, &currentSts)

		stsRecreated, podMustRecreate, err := recreateSTSIfNeed(ctx, rclient, newSts, &currentSts)
		if err != nil {
			return err
		}

		// if sts wasn't recreated, update it first
		// before making call for performRollingUpdateOnSts
		if !stsRecreated {
			// TODO think about compare to conditionally apply updates
			// it may be tricky due to default values at kubernetes side
			if err := rclient.Update(ctx, newSts); err != nil {
				return fmt.Errorf("cannot perform update on sts: %s, err: %w", newSts.Name, err)
			}
		}

		// perform manual update only with OnDelete policy, which is default.
		if newSts.Spec.UpdateStrategy.Type == appsv1.OnDeleteStatefulSetStrategyType {
			if err := performRollingUpdateOnSts(ctx, podMustRecreate, rclient, newSts.Name, newSts.Namespace, cr.SelectorLabels(), c); err != nil {
				return fmt.Errorf("cannot handle rolling-update on sts: %s, err: %w", newSts.Name, err)
			}
		} else {
			if err := waitForStatefulSetReady(ctx, rclient, newSts, c); err != nil {
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

// we perform rolling update on sts by manually deleting pods one by one
// we check sts revision (kubernetes controller-manager is responsible for that)
// and compare pods revision label with sts revision
// if it doesn't match - updated is needed
//
// we always check if sts.Status.CurrentRevision needs update, to keep it equal to UpdateRevision
// see https://github.com/kubernetes/kube-state-metrics/issues/1324#issuecomment-1779751992
func performRollingUpdateOnSts(ctx context.Context, podMustRecreate bool, rclient client.Client, stsName string, ns string, podLabels map[string]string, c *config.BaseOperatorConf) error {
	time.Sleep(time.Second * 2)
	sts := &appsv1.StatefulSet{}
	err := rclient.Get(ctx, types.NamespacedName{Name: stsName, Namespace: ns}, sts)
	if err != nil {
		return err
	}
	neededPodCount := 0
	if sts.Spec.Replicas != nil {
		neededPodCount = int(*sts.Spec.Replicas)
	}

	stsVersion := sts.Status.UpdateRevision
	l := logger.WithContext(ctx).WithValues("controller", "sts.rollingupdate", "desiredVersion", stsVersion, "podMustRecreate", podMustRecreate, "sts.name", sts.Name)
	// fast path
	if neededPodCount < 1 {
		l.Info("sts has 0 replicas configured, nothing to update")
		return nil
	}
	l.Info("check if pod update needed")
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(podLabels)
	listOps := &client.ListOptions{Namespace: ns, LabelSelector: labelSelector}
	if err := rclient.List(ctx, podList, listOps); err != nil {
		return fmt.Errorf("cannot list pods for statefulset rolling update: %w", err)
	}

	if err := sortStsPodsByID(podList.Items); err != nil {
		return fmt.Errorf("cannot sort statefulset pods: %w", err)
	}
	switch {
	// sanity check, should help to catch possible bugs
	case len(podList.Items) > neededPodCount:
		l.Info("unexpected count of pods for sts, seems like configuration of stateful wasn't correct and kubernetes cannot create pod,"+
			" check kubectl events to find out source of problem", "sts", sts.Name, "wantCount", neededPodCount, "actualCount", len(podList.Items), "namespace", ns)
	// usual case when some param misconfigured
	// or kubernetes for some reason cannot create pod
	// it's better to fail fast
	case len(podList.Items) < neededPodCount:
		return fmt.Errorf("actual pod count: %d less then needed: %d, possible statefulset misconfiguration", len(podList.Items), neededPodCount)
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
		if sts.Status.UpdateRevision != sts.Status.CurrentRevision {
			logger.WithContext(ctx).Info("update sts.Status.CurrentRevision", "sts", sts.Name, "currentRevision", sts.Status.CurrentRevision, "desiredRevision", sts.Status.UpdateRevision)
			sts.Status.CurrentRevision = sts.Status.UpdateRevision
			if err := rclient.Status().Update(ctx, sts); err != nil {
				return fmt.Errorf("cannot update sts currentRevision after sts updated finished, err: %w", err)
			}
		}
		return nil
	}

	l.Info("check with updated but not ready pods", "updated pods count", len(updatedPods), "desired version", stsVersion)
	// check updated, by not ready pods
	for _, pod := range updatedPods {
		l.Info("checking ready status for already updated pod to desired version", "pod", pod.Name)
		err := waitForPodReady(ctx, rclient, ns, pod.Name, c, sts.Spec.MinReadySeconds)
		if err != nil {
			return fmt.Errorf("cannot wait for pod ready state for already updated pod: %w", err)
		}
	}

	l.Info("update outdated pods", "updated pods count", len(podsForUpdate), "desired version", stsVersion)
	// perform update for not updated pods
	for _, pod := range podsForUpdate {
		l.Info("updating pod", "pod", pod.Name)
		// we have to delete pod and wait for it readiness
		err := rclient.Delete(ctx, &pod, &client.DeleteOptions{GracePeriodSeconds: ptr.To(int64(30))})
		if err != nil {
			return err
		}
		err = waitForPodReady(ctx, rclient, ns, pod.Name, c, sts.Spec.MinReadySeconds)
		if err != nil {
			return fmt.Errorf("cannot wait for pod ready state during re-creation: %w", err)
		}
		l.Info("pod was updated successfully", "pod", pod.Name)
	}

	if sts.Status.CurrentRevision != sts.Status.UpdateRevision {
		logger.WithContext(ctx).Info("update sts.Status.CurrentRevision", "sts", sts.Name, "currentRevision", sts.Status.CurrentRevision, "desiredRevision", sts.Status.UpdateRevision)
		sts.Status.CurrentRevision = sts.Status.UpdateRevision
		if err := rclient.Status().Update(ctx, sts); err != nil {
			return fmt.Errorf("cannot update sts currentRevision after sts updated finished, err: %w", err)
		}
	}

	return nil
}

// PodIsReady check is pod is ready
func PodIsReady(pod *corev1.Pod, minReadySeconds int32) bool {
	if pod.ObjectMeta.DeletionTimestamp != nil {
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

func waitForPodReady(ctx context.Context, rclient client.Client, ns, podName string, c *config.BaseOperatorConf, minReadySeconds int32) error {
	// we need some delay
	time.Sleep(c.PodWaitReadyInitDelay)
	var pod *corev1.Pod
	if err := wait.PollUntilContextTimeout(context.TODO(), c.PodWaitReadyIntervalCheck, c.PodWaitReadyTimeout, false, func(_ context.Context) (done bool, err error) {
		pod = &corev1.Pod{}
		err = rclient.Get(ctx, types.NamespacedName{Namespace: ns, Name: podName}, pod)
		if err != nil {
			return false, fmt.Errorf("cannot get pod: %q: %w", podName, err)
		}
		if PodIsReady(pod, minReadySeconds) {
			logger.WithContext(ctx).Info("pod update finished with revision", "pod", pod.Name, "revision", pod.Labels[podRevisionLabel])
			return true, nil
		}
		return false, nil
	}); err != nil {
		return podStatusesToError(err, pod)
	}
	return nil
}

func podStatusesToError(origin error, pod *corev1.Pod) error {
	var conditions []string
	for _, cond := range pod.Status.Conditions {
		conditions = append(conditions, fmt.Sprintf("name=%q,status=%q,message=%q", cond.Type, cond.Status, cond.Message))
	}

	stateToString := func(state corev1.ContainerState) string {
		switch {
		case state.Running != nil:
			return fmt.Sprintf("running since: %s", state.Running.StartedAt)
		case state.Terminated != nil:
			return fmt.Sprintf("terminated reason=%q, exit_code=%d", state.Terminated.Message, state.Terminated.ExitCode)
		case state.Waiting != nil:
			return fmt.Sprintf("waiting with reason=%q", state.Waiting.Reason)
		}
		return "container at waiting state"
	}
	for _, condStatus := range pod.Status.ContainerStatuses {
		conditions = append(conditions, fmt.Sprintf("name=%q,is_ready=%v,restart_count=%d,state=%s", condStatus.Name, condStatus.Ready, condStatus.RestartCount, stateToString(condStatus.State)))
	}
	for _, condStatus := range pod.Status.InitContainerStatuses {
		conditions = append(conditions, fmt.Sprintf("name=%q,is_ready=%v,restart_count=%d,state=%s", condStatus.Name, condStatus.Ready, condStatus.RestartCount, stateToString(condStatus.State)))
	}

	return fmt.Errorf("origin_Err=%w,podPhase=%q,conditions=%s", origin, pod.Status.Phase, strings.Join(conditions, ","))
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
