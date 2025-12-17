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

func getLatestPods(ctx context.Context, rclient client.Client, obj client.Object) ([]corev1.Pod, error) {
	GinkgoHelper()
	var podList corev1.PodList
	if err := rclient.List(ctx, &podList, &client.ListOptions{
		Namespace:     obj.GetNamespace(),
		LabelSelector: labels.SelectorFromSet(obj.GetLabels()),
	}); err != nil {
		return nil, err
	}
	podsByHash := make(map[string][]corev1.Pod)
	var labelName, kind string
	owners := make(map[string]struct{})
	switch obj.(type) {
	case *appsv1.StatefulSet:
		labelName = "controller-revision-hash"
		kind = "StatefulSet"
	case *appsv1.ReplicaSet:
		labelName = "pod-template-hash"
		kind = "ReplicaSet"
	default:
		return nil, fmt.Errorf("kind=%T is not supported", obj)
	}
	for _, pod := range podList.Items {
		if !pod.DeletionTimestamp.IsZero() {
			continue
		}
		labelValue, ok := pod.Labels[labelName]
		if !ok {
			continue
		}
		for _, ref := range pod.OwnerReferences {
			if ref.Kind != kind {
				continue
			}
			if _, ok := owners[ref.Name]; !ok {
				owners[ref.Name] = struct{}{}
			}
		}
		podsByHash[labelValue] = append(podsByHash[labelValue], pod)
	}
	var creationTimestamp metav1.Time
	var currentHash string
	for owner := range owners {
		nsn := types.NamespacedName{
			Name:      owner,
			Namespace: obj.GetNamespace(),
		}
		if err := rclient.Get(ctx, nsn, obj); err != nil {
			return nil, fmt.Errorf("failed to get %T=%s", obj, nsn)
		}
		ts := obj.GetCreationTimestamp()
		if creationTimestamp.IsZero() || (!ts.IsZero() && ts.After(creationTimestamp.Time)) {
			creationTimestamp = ts
			switch v := obj.(type) {
			case *appsv1.StatefulSet:
				currentHash = v.Status.UpdateRevision
			case *appsv1.ReplicaSet:
				currentHash = v.Labels[labelName]
			default:
				return nil, fmt.Errorf("kind=%T is not supported", obj)
			}
		}
	}
	var pods []corev1.Pod
	if len(currentHash) > 0 {
		pods = podsByHash[currentHash]
	}
	return pods, nil
}

func expectPodCount(ctx context.Context, rclient client.Client, obj client.Object, count int) error {
	GinkgoHelper()
	pods, err := getLatestPods(ctx, rclient, obj)
	if err != nil {
		return err
	}
	if len(pods) != count {
		return fmt.Errorf("pod count mismatch, expect: %d, got: %d", count, len(pods))
	}
	for _, pod := range pods {
		if !reconcile.PodIsReady(&pod, 0) {
			return fmt.Errorf("pod isn't ready: %s,\n status: %s", pod.Name, pod.Status.String())
		}
	}
	return nil
}

func getRevisionHistoryLimit(ctx context.Context, rclient client.Client, name types.NamespacedName) int32 {
	app := &appsv1.Deployment{}
	if err := rclient.Get(ctx, name, app); err != nil {
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

func expectObjectStatusPaused(ctx context.Context,
	rclient client.Client,
	object client.Object,
	name types.NamespacedName) error {
	return suite.ExpectObjectStatus(ctx, rclient, object, name, vmv1beta1.UpdateStatusPaused)
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
	GinkgoHelper()
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
	Expect(k8sClient.Create(ctx, job)).ToNot(HaveOccurred())
	nss := types.NamespacedName{Name: job.Name, Namespace: job.Namespace}
	defer func() {
		Expect(k8sClient.Delete(ctx, job, &client.DeleteOptions{
			PropagationPolicy: ptr.To(metav1.DeletePropagationForeground),
		})).ToNot(HaveOccurred())
		waitResourceDeleted(ctx, k8sClient, nss, &batchv1.Job{})
	}()
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
	}, 60).ShouldNot(HaveOccurred())
}

//nolint:dupl,lll
func assertAnnotationsOnObjects(ctx context.Context, nss types.NamespacedName, objects []client.Object, annotations map[string]string) {
	GinkgoHelper()
	for idx, obj := range objects {
		Expect(k8sClient.Get(ctx, nss, obj)).ToNot(HaveOccurred())
		gotAnnotations := obj.GetAnnotations()
		for k, v := range annotations {
			gv, ok := gotAnnotations[k]
			if v == "" {
				Expect(ok).To(BeFalse(), "annotation key=%q,value=%q must not exist for %T at idx=%d, object=%q", k, gv, obj, idx, nss.String())
			} else {
				Expect(ok).To(BeTrue(), "annotation key=%s must present for %T at idx=%d, object=%q", k, obj, idx, nss.String())
				Expect(gv).To(Equal(v), "annotation key=%s must equal for %T at idx=%d, object=%q", k, obj, idx, nss.String())

			}

		}
	}
}

//nolint:dupl,lll
func assertLabelsOnObjects(ctx context.Context, nss types.NamespacedName, objects []client.Object, wantLabels map[string]string) {
	GinkgoHelper()
	for idx, obj := range objects {
		Expect(k8sClient.Get(ctx, nss, obj)).ToNot(HaveOccurred())
		gotLabels := obj.GetLabels()
		for k, v := range wantLabels {
			gv, ok := gotLabels[k]
			if v == "" {
				Expect(ok).NotTo(BeTrue(), "label key=%s must not exist for %T at idx=%d", k, obj, idx)
			} else {
				Expect(ok).To(BeTrue(), "label key=%s must present for %T at idx=%d", k, obj, idx)
				Expect(gv).To(Equal(v), "label key=%s must equal for %T at idx=%d", k, obj, idx)

			}

		}
	}
}

//nolint:dupl,lll,unparam
func hasVolume(volumes []corev1.Volume, volumeName string) error {
	GinkgoHelper()
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
	GinkgoHelper()
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
func mustGetFirstPod(ctx context.Context, rclient client.Client, obj client.Object) *corev1.Pod {
	GinkgoHelper()
	pods, err := getLatestPods(ctx, rclient, obj)
	Expect(err).ToNot(HaveOccurred())
	Expect(pods).ToNot(BeEmpty())
	return &pods[0]
}

//nolint:dupl,lll
func waitResourceDeleted(ctx context.Context, rclient client.Client, nss types.NamespacedName, r client.Object) {
	GinkgoHelper()
	Eventually(func() error {
		return rclient.Get(ctx, nss, r)
	}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))
}
