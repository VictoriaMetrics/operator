package e2e

import (
	"context"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
	"github.com/VictoriaMetrics/operator/test/e2e/suite"
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
			return fmt.Sprintf("pod isn't ready: %s,\n status: %s", pod.Name, pod.Status.String())
		}
	}
	return ""
}

func getRevisionHistoryLimit(rclient client.Client, name types.NamespacedName) int32 {
	app := &appsv1.Deployment{}
	if err := rclient.Get(context.TODO(), name, app); err != nil {
		return 0
	}
	return *app.Spec.RevisionHistoryLimit
}

func expectObjectStatusExpanding(ctx context.Context,
	rclient client.Client,
	object client.Object,
	name types.NamespacedName) error {
	return suite.ExpectObjectStatus(ctx, rclient, object, name, vmv1beta1.UpdateStatusExpanding)
}
func expectObjectStatusOperational(ctx context.Context,
	rclient client.Client,
	object client.Object,
	name types.NamespacedName) error {
	return suite.ExpectObjectStatus(ctx, rclient, object, name, vmv1beta1.UpdateStatusOperational)
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
response_code=$(curl --write-out %%{http_code} -X %s '%s' -d '%s' -o /tmp/curl_log --connect-timeout 5 --max-time 6 --silent --show-error 2>>/tmp/curl_log)
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
	nsn := types.NamespacedName{Name: job.Name, Namespace: job.Namespace}
	defer func() {
		Expect(k8sClient.Delete(ctx, job, &client.DeleteOptions{
			PropagationPolicy: ptr.To(metav1.DeletePropagationForeground),
		})).To(Succeed())
		waitResourceDeleted(ctx, k8sClient, nsn, &batchv1.Job{})
	}()
	Eventually(func() error {
		var jb batchv1.Job
		if err := k8sClient.Get(ctx, nsn, &jb); err != nil {
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
}

//nolint:dupl,lll
func assertAnnotationsOnObjects(ctx context.Context, nsn types.NamespacedName, objects []client.Object, annotations map[string]string) {
	for idx, obj := range objects {
		Expect(k8sClient.Get(ctx, nsn, obj)).To(Succeed())
		gotAnnotations := obj.GetAnnotations()
		for k, v := range annotations {
			gv, ok := gotAnnotations[k]
			if v == "" {
				Expect(ok).To(BeFalse(), "annotation key=%q,value=%q must not exist for object at idx=%d, object=%q", k, gv, idx, nsn.String())
			} else {
				Expect(ok).To(BeTrue(), "annotation key=%s must present for object at idx=%d, object=%q", k, idx, nsn.String())
				Expect(gv).To(Equal(v), "annotation key=%s must equal for object at idx=%d, object=%q", k, idx, nsn.String())

			}

		}
	}
}

//nolint:dupl,lll
func assertLabelsOnObjects(ctx context.Context, nsn types.NamespacedName, objects []client.Object, wantLabels map[string]string) {
	for idx, obj := range objects {
		Expect(k8sClient.Get(ctx, nsn, obj)).To(Succeed())
		gotAnnotations := obj.GetLabels()
		for k, v := range wantLabels {
			gv, ok := gotAnnotations[k]
			if v == "" {
				Expect(ok).NotTo(BeTrue(), "label key=%s must not exist for object at idx=%d", k, idx)
			} else {
				Expect(ok).To(BeTrue(), "label key=%s must present for object at idx=%d", k, idx)
				Expect(gv).To(Equal(v), "label key=%s must equal for object at idx=%d", k, idx)

			}

		}
	}
}

//nolint:dupl,lll,unparam
func hasVolume(volumes []corev1.Volume, volumeName string) error {
	var existVolumes []string
	for _, v := range volumes {
		if v.Name == volumeName {
			return nil
		}
		existVolumes = append(existVolumes, fmt.Sprintf("name=%s", v.Name))
	}
	return fmt.Errorf("volumes=%d with names=%s; must have=%s", len(volumes), strings.Join(existVolumes, ","), volumeName)
}

//nolint:dupl,lll,unparam
func hasVolumeMount(volumeMounts []corev1.VolumeMount, volumeMountName string) error {
	var existVolumes []string
	for _, vm := range volumeMounts {
		if vm.MountPath == volumeMountName {
			return nil
		}
		existVolumes = append(existVolumes, fmt.Sprintf("volume=%s,mountPath=%s", vm.Name, vm.MountPath))
	}
	return fmt.Errorf("volumes mounts=%d with paths=%s; must have=%s", len(volumeMounts), strings.Join(existVolumes, ","), volumeMountName)
}

//nolint:dupl,lll
func mustGetFirstPod(rclient client.Client, ns string, lbs map[string]string) *corev1.Pod {
	var podList corev1.PodList
	Expect(rclient.List(context.TODO(), &podList, &client.ListOptions{
		Namespace:     ns,
		LabelSelector: labels.SelectorFromSet(lbs),
	})).To(Succeed())
	Expect(podList.Items).ToNot(BeEmpty())
	return &podList.Items[0]
}

//nolint:dupl,lll
func waitResourceDeleted(ctx context.Context, rclient client.Client, nsn types.NamespacedName, r client.Object) {
	GinkgoHelper()
	Eventually(func() error {
		return rclient.Get(ctx, nsn, r)
	}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"), fmt.Sprintf("unexpected resource found: %#v", r))
}
