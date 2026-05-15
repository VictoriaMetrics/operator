package e2e

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/rest"
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
	owners := sets.New[string]()
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
			owners.Insert(ref.Name)
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

type httpRequestOpts struct {
	dstURL       string
	method       string
	expectedCode int
	payload      string
}

func expectHTTPRequestToSucceed(ctx context.Context, opts httpRequestOpts) {
	GinkgoHelper()
	By("making http request to: " + opts.dstURL)
	if opts.method == "" {
		if opts.payload != "" {
			opts.method = "POST"
		} else {
			opts.method = "GET"
		}
	}
	if opts.expectedCode == 0 {
		opts.expectedCode = 200
	}
	proxyURL, err := buildServiceProxyURL(&k8sCfg, opts.dstURL)
	Expect(err).ToNot(HaveOccurred())
	hc, err := rest.HTTPClientFor(&k8sCfg)
	Expect(err).ToNot(HaveOccurred())
	hc.Timeout = 10 * time.Second
	Eventually(func() error {
		var body io.Reader
		if opts.payload != "" {
			body = strings.NewReader(opts.payload)
		}
		req, err := http.NewRequestWithContext(ctx, opts.method, proxyURL, body)
		if err != nil {
			return err
		}
		resp, err := hc.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != opts.expectedCode {
			b, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("unexpected status code: got %d, want %d, body: %s", resp.StatusCode, opts.expectedCode, b)
		}
		return nil
	}, 60*time.Second).ShouldNot(HaveOccurred())
}

// buildServiceProxyURL converts a cluster-internal service URL to a Kubernetes
// API server proxy URL so tests can reach in-cluster services without a Job.
// Input:  "http://svcname.namespace.svc:port/path?query"
// Output: "{apiserver}/api/v1/namespaces/{namespace}/services/{svcname}:{port}/proxy/{path}?query"
func buildServiceProxyURL(cfg *rest.Config, svcURL string) (string, error) {
	u, err := url.Parse(svcURL)
	if err != nil {
		return "", err
	}
	parts := strings.SplitN(u.Hostname(), ".", 3)
	if len(parts) < 2 {
		return "", fmt.Errorf("cannot parse namespace from service URL: %s", svcURL)
	}
	proxyPath := u.Path
	if u.RawQuery != "" {
		proxyPath += "?" + u.RawQuery
	}
	return fmt.Sprintf("%s/api/v1/namespaces/%s/services/%s:%s/proxy%s",
		strings.TrimRight(cfg.Host, "/"), parts[1], parts[0], u.Port(), proxyPath), nil
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

// expectStatusAfterAction starts a watch on the named object, runs action, then waits for
// each status in sequence. Start the watch before the action to capture transient states.
// After the action, we fetch the current generation so WatchUntilStatusSeen can ignore
// stale pre-action ADDED events that satisfy the target status but predate the action.
func expectStatusAfterAction(ctx context.Context, list client.ObjectList, nsn types.NamespacedName, timeout time.Duration, action func(), statuses ...vmv1beta1.UpdateStatus) {
	GinkgoHelper()
	watcher, err := k8sClient.Watch(ctx, list, &client.ListOptions{
		Namespace:     nsn.Namespace,
		FieldSelector: fields.OneTermEqualSelector("metadata.name", nsn.Name),
	})
	Expect(err).ToNot(HaveOccurred())
	defer watcher.Stop()
	action()
	// Fetch the post-action generation to skip stale pre-action watch events.
	var minGen int64
	if err := k8sClient.List(ctx, list, &client.ListOptions{
		Namespace:     nsn.Namespace,
		FieldSelector: fields.OneTermEqualSelector("metadata.name", nsn.Name),
	}); err == nil {
		if items, err := k8smeta.ExtractList(list); err == nil && len(items) > 0 {
			if obj, ok := items[0].(client.Object); ok {
				minGen = obj.GetGeneration()
			}
		}
	}
	for _, status := range statuses {
		watchCtx, cancel := context.WithTimeout(ctx, timeout)
		err := suite.WatchUntilStatusSeen(watchCtx, watcher, nsn.Name, minGen, status)
		cancel()
		Expect(err).ToNot(HaveOccurred())
	}
}
