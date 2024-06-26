package e2e

import (
	"context"
	"fmt"

	v1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func expectPodCount(rclient client.Client, count int, ns string, lbs map[string]string) string {
	podList := &corev1.PodList{}
	if err := rclient.List(context.TODO(), podList, &client.ListOptions{Namespace: ns, LabelSelector: labels.SelectorFromSet(lbs)}); err != nil {
		return err.Error()
	}
	if len(podList.Items) != count {
		return fmt.Sprintf("pod count mismatch, expect: %d, got: %d", count, len(podList.Items))
	}
	for _, pod := range podList.Items {
		if !reconcile.PodIsReady(&pod, 0) {
			return fmt.Sprintf("pod isnt ready: %s,\n status: %s", pod.Name, pod.Status.String())
		}
	}
	return ""
}

func getRevisionHistoryLimit(rclient client.Client, name types.NamespacedName) int32 {
	deployment := &v1.Deployment{}
	if err := rclient.Get(context.TODO(), name, deployment); err != nil {
		return 0
	}
	return *deployment.Spec.RevisionHistoryLimit
}
