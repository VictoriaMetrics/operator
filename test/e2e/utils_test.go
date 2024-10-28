package e2e

import (
	"context"
	"encoding/json"
	"fmt"

	operator "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
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

func expectObjectStatusExpanding(ctx context.Context,
	rclient client.Client,
	object client.Object,
	name types.NamespacedName) error {
	return expectObjectStatus(ctx, rclient, object, name, operator.UpdateStatusExpanding)
}
func expectObjectStatusOperational(ctx context.Context,
	rclient client.Client,
	object client.Object,
	name types.NamespacedName) error {
	return expectObjectStatus(ctx, rclient, object, name, operator.UpdateStatusOperational)
}

func expectObjectStatus(ctx context.Context,
	rclient client.Client,
	object client.Object,
	name types.NamespacedName,
	status operator.UpdateStatus) error {
	if err := rclient.Get(ctx, name, object); err != nil {
		return err
	}
	jsD, err := json.Marshal(object)
	if err != nil {
		return err
	}
	type objectStatus struct {
		Status struct {
			CurrentStatus      string `json:"clusterStatus"`
			UpdateStatus       string `json:"updateStatus"`
			SingleUpdateStatus string `json:"singleStatus,omitempty"`
		} `json:"status"`
	}
	var obs objectStatus
	if err := json.Unmarshal(jsD, &obs); err != nil {
		return err
	}
	if obs.Status.UpdateStatus != string(status) &&
		obs.Status.CurrentStatus != string(status) &&
		obs.Status.SingleUpdateStatus != string(status) {
		return fmt.Errorf("not expected object status=%q cluster status=%q,single_status=%q",
			obs.Status.UpdateStatus,
			obs.Status.CurrentStatus,
			obs.Status.SingleUpdateStatus)
	}

	return nil
}

type httpRequestOpts struct {
	dstURL       string
	method       string
	expectedCode int
	payload      string
}

type httpRequestCRDObject interface {
	GetName() string
	GetNamespace() string
}

//nolint:dupl,lll
func expectHTTPRequestToSucceed(ctx context.Context, object httpRequestCRDObject, opts httpRequestOpts) {
	By("making http request to: " + opts.dstURL)
	if opts.method == "" {
		opts.method = "GET"
	}
	if opts.expectedCode == 0 {
		opts.expectedCode = 200
	}
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-http-request-for-" + object.GetName(),
			Namespace: object.GetNamespace(),
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    "curl",
							Image:   "curlimages/curl:7.85.0",
							Command: []string{"sh", "-c"},
							Args: []string{
								fmt.Sprintf(`
set -e 
set -o pipefail
set -x
response_code=$(curl --write-out %%{http_code} -X %s %s -d "%s" -o /tmp/curl_log --connect-timeout 5 --max-time 6 --silent --show-error 2>>/tmp/curl_log)
if [[ "$response_code" -ne %d ]] ; then
  echo "unexpected status code: $response_code" | tee >> /tmp/curl_log
  cat /tmp/curl_log | tr '\n' ' ' | tee > /dev/termination-log
  exit 1
else
  echo "status code: $response_code is ok"
  cat /tmp/curl_log
  exit 0
fi
`, opts.method, opts.dstURL, opts.payload, opts.expectedCode),
							},
						},
					},
				},
			},
		},
	}
	Expect(k8sClient.Create(ctx, job)).To(Succeed())
	defer k8sClient.Delete(ctx, job, &client.DeleteOptions{PropagationPolicy: ptr.To(metav1.DeletePropagationForeground)})
	nss := types.NamespacedName{Name: job.Name, Namespace: job.Namespace}
	Eventually(func() error {
		var jb batchv1.Job
		if err := k8sClient.Get(ctx, nss, &jb); err != nil {
			return err
		}
		if jb.Status.Succeeded > 0 {
			return nil
		}
		var pds corev1.PodList
		if err := k8sClient.List(ctx, &pds, &client.ListOptions{
			LabelSelector: labels.SelectorFromSet(jb.Spec.Selector.MatchLabels),
		}); err != nil {
			return err
		}
		var firstStatus string
		for _, pd := range pds.Items {
			for _, ps := range pd.Status.ContainerStatuses {
				if ps.State.Terminated != nil && firstStatus == "" {
					firstStatus = fmt.Sprintf("exit_code=%d with message: %s", ps.State.Terminated.ExitCode, ps.State.Terminated.Message)
					break
				}
			}
		}
		return fmt.Errorf("unexpected job status, want Succeeded pod > 0 , pod status: %s", firstStatus)
	}, 60).Should(Succeed())
	Expect(k8sClient.Delete(ctx, job, &client.DeleteOptions{PropagationPolicy: ptr.To(metav1.DeletePropagationForeground)})).To(Succeed())
	Eventually(func() error {
		var jb batchv1.Job
		return k8sClient.Get(ctx, nss, &jb)
	}, eventualDeletionTimeout).Should(MatchError(errors.IsNotFound, "IsNotFound"))

}
