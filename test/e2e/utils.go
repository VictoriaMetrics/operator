package e2e

import (
	"context"
	"encoding/json"
	"fmt"

	operator "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	v1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func expectPodCount(rclient client.Client, count int, ns string, lbs map[string]string) string {
	podList := &corev1.PodList{}
	if err := rclient.List(context.TODO(), podList, &client.ListOptions{
		Namespace:     ns,
		LabelSelector: labels.SelectorFromSet(lbs),
	}); err != nil {
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

func expectObjectStatusExpanding(ctx context.Context, rclient client.Client, object client.Object, name types.NamespacedName) error {
	return expectObjectStatus(ctx, rclient, object, name, operator.UpdateStatusExpanding)
}
func expectObjectStatusOperational(ctx context.Context, rclient client.Client, object client.Object, name types.NamespacedName) error {
	return expectObjectStatus(ctx, rclient, object, name, operator.UpdateStatusOperational)
}

func expectObjectStatus(ctx context.Context, rclient client.Client, object client.Object, name types.NamespacedName, status operator.UpdateStatus) error {
	if err := rclient.Get(ctx, name, object); err != nil {
		return err
	}
	jsD, err := json.Marshal(object)
	if err != nil {
		return err
	}
	type objectStatus struct {
		Status struct {
			CurrentStatus string `json:"clusterStatus"`
			UpdateStatus  string `json:"updateStatus"`
		} `json:"status"`
	}
	var obs objectStatus
	if err := json.Unmarshal(jsD, &obs); err != nil {
		return err
	}
	if obs.Status.UpdateStatus != string(status) && obs.Status.CurrentStatus != string(status) {
		return fmt.Errorf("not expected object status: %s current status %s", obs.Status.UpdateStatus, obs.Status.CurrentStatus)
	}

	return nil
}
