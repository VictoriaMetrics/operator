package k8stools

import (
	"context"
	"fmt"
	"time"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const podRevisionLabel = "controller-revision-hash"

type STSOptions struct {
	SelectorLabels func() map[string]string
	VolumeName     func() string
	UpdateStrategy func() appsv1.StatefulSetUpdateStrategyType
}

func HandleSTSUpdate(ctx context.Context, rclient client.Client, cr STSOptions, newSts, currentSts *appsv1.StatefulSet, c *config.BaseOperatorConf) error {

	// special case, that allows app restart
	newSts.Spec.Template.Annotations = MergeAnnotations(currentSts.Spec.Template.Annotations, newSts.Spec.Template.Annotations)
	newSts.Finalizers = victoriametricsv1beta1.MergeFinalizers(currentSts, victoriametricsv1beta1.FinalizerName)

	isRecreated, err := wasCreatedSTS(ctx, rclient, cr.VolumeName(), newSts, currentSts)
	if err != nil {
		return err
	}

	if !isRecreated {
		if err := rclient.Update(ctx, newSts); err != nil {
			return fmt.Errorf("cannot perform update on sts: %s, err: %w", newSts.Name, err)
		}
	}

	// perform manual update only with OnDelete policy, which is default.
	if cr.UpdateStrategy() == appsv1.OnDeleteStatefulSetStrategyType {

		if err := performRollingUpdateOnSts(ctx, isRecreated, rclient, newSts.Name, newSts.Namespace, cr.SelectorLabels(), c); err != nil {
			return fmt.Errorf("cannot update statefulset for vmalertmanager: %w", err)
		}
	}

	if err := growSTSPVC(ctx, rclient, newSts, cr.VolumeName()); err != nil {
		return err
	}

	return nil
}

// we perform rolling update on sts by manually deleting pods one by one
// we check sts revision (kubernetes controller-manager is responsible for that)
// and compare pods revision label with sts revision
// if it doesnt match - updated is needed
// there is corner case, when statefulset was removed
// See details at https://github.com/VictoriaMetrics/operator/issues/344
func performRollingUpdateOnSts(ctx context.Context, wasRecreated bool, rclient client.Client, stsName string, ns string, podLabels map[string]string, c *config.BaseOperatorConf) error {
	time.Sleep(time.Second * 2)
	sts := &appsv1.StatefulSet{}
	err := rclient.Get(ctx, types.NamespacedName{Name: stsName, Namespace: ns}, sts)
	if err != nil {
		return err
	}

	stsVersion := sts.Status.CurrentRevision

	if sts.Status.UpdateRevision != sts.Status.CurrentRevision || wasRecreated {
		log.Info("sts update is needed", "sts", sts.Name, "currentVersion", sts.Status.CurrentRevision, "desiredVersion", sts.Status.UpdateRevision)
		stsVersion = sts.Status.UpdateRevision
	}

	l := log.WithValues("controller", "sts.rollingupdate", "desiredVersion", stsVersion, "wasRecreated", wasRecreated)

	l.Info("checking if update needed")
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(podLabels)
	listOps := &client.ListOptions{Namespace: ns, LabelSelector: labelSelector}
	if err := rclient.List(ctx, podList, listOps); err != nil {
		return err
	}
	neededPodCount := 1
	if sts.Spec.Replicas != nil {
		neededPodCount = int(*sts.Spec.Replicas)
	}
	if len(podList.Items) != neededPodCount {
		l.Info("unexpected count of pods for sts: %s, want: %d, got: %d, seems like configuration of stateful wasn't correct and kubernetes cannot create pod,"+
			" check kubectl events for namespace: %s, to find out source of problem", sts.Name, neededPodCount, len(podList.Items), sts.Namespace)
	}

	// first we must ensure, that already updated pods in ready status
	// then we can update other pods
	// if pod is not ready
	// it must be at first place for update
	podsForUpdate := make([]corev1.Pod, 0, len(podList.Items))
	// if pods were already updated to some version, we have to wait its readiness
	updatedPods := make([]corev1.Pod, 0, len(podList.Items))

	// in case of re-creation, remove and create all pods
	if wasRecreated {
		podsForUpdate = podList.Items
	} else {
		for _, pod := range podList.Items {
			podRev := pod.Labels[podRevisionLabel]

			if podRev == stsVersion {
				// wait for readiness only for not ready pods
				if !PodIsReady(pod) {
					updatedPods = append(updatedPods, pod)
				}
				continue
			}

			// move unready pods to the begging of list for update
			if !PodIsReady(pod) {
				podsForUpdate = append([]corev1.Pod{pod}, podsForUpdate...)
				continue
			}

			podsForUpdate = append(podsForUpdate, pod)

		}
	}

	updatedNeeded := len(podsForUpdate) != 0 || len(updatedPods) != 0

	if !updatedNeeded {
		l.Info("update isn't needed")
		return nil
	}

	l.Info("starting pods update, checking updated, by not ready pods", "updated pods count", len(updatedPods), "desired version", stsVersion)

	// check updated, by not ready pods
	for _, pod := range updatedPods {
		l.Info("checking ready status for already updated pod to desired version", "pod", pod.Name)
		err := waitForPodReady(ctx, rclient, ns, pod.Name, c, nil)
		if err != nil {
			l.Error(err, "cannot get ready status for already updated pod", "pod", pod.Name)
			return err
		}
	}

	// perform update for not updated pods
	for _, pod := range podsForUpdate {
		l.Info("updating pod", "pod", pod.Name)
		// we have to delete pod and wait for it readiness
		err := rclient.Delete(ctx, &pod, &client.DeleteOptions{GracePeriodSeconds: pointer.Int64Ptr(30)})
		if err != nil {
			return err
		}
		err = waitForPodReady(ctx, rclient, ns, pod.Name, c, func(pod *corev1.Pod) error {
			// its special hack
			// we check first pod revision label after re-creation
			// it must contain valid statefulset revision
			// See more at https://github.com/VictoriaMetrics/operator/issues/344
			updatedPodRev := pod.Labels[podRevisionLabel]
			var newRev string
			// cases:
			// - sts was recreated
			// - sts was recreated with different version
			if sts.Status.UpdateRevision == "" || sts.Status.UpdateRevision != updatedPodRev {
				newRev = updatedPodRev
			}

			if len(newRev) > 0 {
				l.Info("updating stateful set revision from pod", "sts update", sts.Status.UpdateRevision, "pod rev", updatedPodRev)
				sts.Status.UpdateRevision = updatedPodRev
				if err := rclient.Status().Update(ctx, sts); err != nil {
					return fmt.Errorf("cannot update sts pod revision: %w", err)
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		l.Info("pod was updated", "pod", pod.Name)
		time.Sleep(time.Second * 1)
	}

	return nil

}

func PodIsReady(pod corev1.Pod) bool {
	if pod.ObjectMeta.DeletionTimestamp != nil {
		return false
	}

	if pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == "True" {
			return true
		}
	}
	return false
}

func waitForPodReady(ctx context.Context, rclient client.Client, ns, podName string, c *config.BaseOperatorConf, cb func(pod *corev1.Pod) error) error {
	// we need some delay
	time.Sleep(c.PodWaitReadyInitDelay)
	return wait.Poll(c.PodWaitReadyIntervalCheck, c.PodWaitReadyTimeout, func() (done bool, err error) {
		pod := &corev1.Pod{}
		err = rclient.Get(ctx, types.NamespacedName{Namespace: ns, Name: podName}, pod)
		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			log.Error(err, "cannot get pod", "pod", podName)
			return false, err
		}
		if PodIsReady(*pod) {
			log.Info("pod update finished with revision", "pod", pod.Name, "revision", pod.Labels[podRevisionLabel])
			if cb != nil {
				if err := cb(pod); err != nil {
					return true, fmt.Errorf("errror occured at callback execution: %w", err)
				}
			}
			return true, nil
		}
		return false, nil
	})
}
