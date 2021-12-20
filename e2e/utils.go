package e2e

import (
	"context"
	"fmt"

	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
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
		if !k8stools.PodIsReady(pod) {
			return fmt.Sprintf("pod isnt ready: %s,\n status: %s", pod.Name, pod.Status.String())
		}
	}
	return ""
}
