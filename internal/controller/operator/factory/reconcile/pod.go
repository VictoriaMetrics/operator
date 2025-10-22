package reconcile

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

func reportFirstNotReadyPodOnError(ctx context.Context, rclient client.Client, origin error, ns string, selector labels.Selector, minReadySeconds int32) error {
	// list pods and join statuses
	var podList corev1.PodList
	if err := rclient.List(ctx, &podList, &client.ListOptions{
		LabelSelector: selector,
		Namespace:     ns,
	}); err != nil {
		return fmt.Errorf("cannot list pods for selector=%q: %w", selector.String(), err)
	}
	for _, dp := range podList.Items {
		if PodIsReady(&dp, minReadySeconds) {
			continue
		}
		return podStatusesToError(origin, &dp)
	}
	return &finalize.ErrWaitReady{
		Err: fmt.Errorf("cannot find any pod for selector=%q, check kubernetes events, origin err: %w", selector.String(), origin),
	}
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
	return &finalize.ErrWaitReady{Err: err}
}

func waitForPodReady(ctx context.Context, rclient client.Client, nsn types.NamespacedName, desiredRevision string, minReadySeconds int32) error {
	var pod *corev1.Pod
	if err := wait.PollUntilContextTimeout(ctx, podWaitReadyIntervalCheck, podWaitReadyTimeout, true, func(_ context.Context) (done bool, err error) {
		pod = &corev1.Pod{}
		err = rclient.Get(ctx, nsn, pod)
		if err != nil {
			return false, fmt.Errorf("cannot get pod: %q: %w", nsn, err)
		}
		revision := pod.Labels[podRevisionLabel]
		if revision != desiredRevision {
			logger.WithContext(ctx).Info(fmt.Sprintf("unexpected pod label %s=%s, want revision=%s", podRevisionLabel, revision, desiredRevision))
			return false, nil
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
