package migrate

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

const testAgentAddr = "10.0.0.1"

func testAdminHandler() http.Handler {
	var scrapes int
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			scrapes++
			var queued int
			if scrapes <= 2 {
				queued = 1024
			}
			fmt.Fprintf(w, "%s{url=\"/tmp/q\"} %d\n", vmv1beta1.VMAgentQueueMetricName, queued)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}

func handlerClient(handler http.Handler) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Result(), nil
	})}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func vmSingleNodeEngine() SingleNodeEngine[*vmv1beta1.VMSingle, *vmv1beta1.VMAgent] {
	return SingleNodeEngine[*vmv1beta1.VMSingle, *vmv1beta1.VMAgent]{
		QueueMetricName: vmv1beta1.VMAgentQueueMetricName,
		HasStorage: func(target *vmv1beta1.VMSingle) bool {
			return target.Spec.Storage != nil && !target.Spec.Storage.Resources.Requests.Storage().IsZero()
		},
		BuildAgent: func(name, namespace, writeURL, bufferSize string) (*vmv1beta1.VMAgent, error) {
			size, err := ParseAgentBufferSize(bufferSize)
			if err != nil {
				return nil, err
			}
			return &vmv1beta1.VMAgent{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
				Spec: vmv1beta1.VMAgentSpec{
					CommonScrapeParams: vmv1beta1.CommonScrapeParams{IngestOnlyMode: ptr.To(true)},
					StatefulMode:       true,
					StatefulStorage: &vmv1beta1.StorageSpec{
						VolumeClaimTemplate: vmv1beta1.EmbeddedPersistentVolumeClaim{
							Spec: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{corev1.ResourceStorage: size},
								},
							},
						},
					},
					RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{{URL: writeURL}},
				},
			}, nil
		},
		AgentMatches: func(existing *vmv1beta1.VMAgent, wantURL string) bool {
			return len(existing.Spec.RemoteWrite) == 1 && existing.Spec.RemoteWrite[0].URL == wantURL &&
				existing.Spec.StatefulMode && existing.Spec.StatefulStorage != nil && existing.Spec.StatefulStorage.EmptyDir == nil &&
				!existing.Spec.StatefulStorage.VolumeClaimTemplate.Spec.Resources.Requests.Storage().IsZero() &&
				existing.Spec.IngestOnlyMode != nil && *existing.Spec.IngestOnlyMode
		},
	}
}

func simulateOperational[T interface {
	client.Object
	GetStatusMetadata() *vmv1beta1.StatusMetadata
}](ctx context.Context, c client.Client, nsn types.NamespacedName, newObj func() T) {
	for {
		obj := newObj()
		if err := c.Get(ctx, nsn, obj); err == nil {
			status := obj.GetStatusMetadata()
			status.UpdateStatus = vmv1beta1.UpdateStatusOperational
			status.ObservedGeneration = obj.GetGeneration()
			_ = c.Status().Update(ctx, obj)
			return
		} else if !k8serrors.IsNotFound(err) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func simulateTargetEndpointsReady(ctx context.Context, c client.Client, serviceName string) {
	es := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName + "-endpoints",
			Namespace: "default",
			Labels:    map[string]string{discoveryv1.LabelServiceName: serviceName},
		},
		Endpoints: []discoveryv1.Endpoint{
			{Addresses: []string{"10.0.0.1"}, Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true)}},
		},
		Ports: []discoveryv1.EndpointPort{
			{Name: ptr.To("http"), Port: ptr.To(int32(8428))},
		},
	}
	_ = c.Create(ctx, es)
}

func simulateVMAgentEndpointsReady(ctx context.Context, c client.Client, namespace, agentName string) {
	for {
		var agent vmv1beta1.VMAgent
		if err := c.Get(ctx, types.NamespacedName{Name: agentName, Namespace: namespace}, &agent); err == nil {
			es := &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agent.PrefixedName() + "-endpoints",
					Namespace: namespace,
					Labels:    map[string]string{discoveryv1.LabelServiceName: agent.PrefixedName()},
				},
				Endpoints: []discoveryv1.Endpoint{
					{Addresses: []string{testAgentAddr}, Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true)}},
				},
				Ports: []discoveryv1.EndpointPort{
					{Name: ptr.To("http"), Port: ptr.To(int32(8429))},
				},
			}
			_ = c.Create(ctx, es)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestWithDowntimeSingleNode(t *testing.T) {
	synctest.Test(t, testWithDowntimeSingleNode)
}

func testWithDowntimeSingleNode(t *testing.T) {
	release := "myrelease"
	ns := "default"
	labels := helmLabels(release)

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-data"},
		Spec: corev1.PersistentVolumeSpec{
			Capacity:    corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			ClaimRef:    &corev1.ObjectReference{Kind: "PersistentVolumeClaim", Namespace: ns, Name: release},
		},
	}
	oldPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns, Labels: labels},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			VolumeName:  "pv-data",
		},
		Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
	}
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": release}},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{Name: "data", VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: release},
						}},
						{Name: "config", VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: release + "-config"}},
						}},
					},
				},
			},
		},
	}
	oldSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns, Labels: labels},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": release}},
	}
	dependentCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: release + "-config", Namespace: ns, Labels: labels},
	}
	unrelatedCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: release + "-unrelated", Namespace: ns, Labels: labels},
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(vmv1beta1.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&vmv1beta1.VMSingle{}, &vmv1beta1.VMAgent{}, &corev1.PersistentVolumeClaim{}).
		WithRuntimeObjects(pv, oldPVC, dep, oldSvc, dependentCM, unrelatedCM).
		Build()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	target := &vmv1beta1.VMSingle{
		ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns},
		Spec: vmv1beta1.VMSingleSpec{
			Storage: &corev1.PersistentVolumeClaimSpec{
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
				},
			},
		},
	}
	agentName := target.PrefixedName() + "-migration-buffer"

	go simulateBind(ctx, t, c, types.NamespacedName{Name: target.PrefixedName(), Namespace: ns})
	go simulateOperational(ctx, c, types.NamespacedName{Name: release, Namespace: ns}, func() *vmv1beta1.VMSingle { return &vmv1beta1.VMSingle{} })
	go simulateOperational(ctx, c, types.NamespacedName{Name: agentName, Namespace: ns}, func() *vmv1beta1.VMAgent { return &vmv1beta1.VMAgent{} })
	go simulateVMAgentEndpointsReady(ctx, c, ns, agentName)
	simulateTargetEndpointsReady(ctx, c, target.PrefixedName())

	err := SingleNode(ctx, c, handlerClient(testAdminHandler()), Options{
		Chart:       ChartVMSingle,
		Strategy:    StrategyWithDowntime,
		Namespace:   ns,
		ReleaseName: release,
		Yes:         true,
	}, target, vmSingleNodeEngine(), false)
	require.NoError(t, err)

	var checkDep appsv1.Deployment
	err = c.Get(ctx, types.NamespacedName{Name: release, Namespace: ns}, &checkDep)
	assert.True(t, k8serrors.IsNotFound(err))

	var checkCM corev1.ConfigMap
	err = c.Get(ctx, types.NamespacedName{Name: release + "-config", Namespace: ns}, &checkCM)
	assert.True(t, k8serrors.IsNotFound(err))
	assert.NoError(t, c.Get(ctx, types.NamespacedName{Name: release + "-unrelated", Namespace: ns}, &checkCM))

	var newPVC corev1.PersistentVolumeClaim
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "vmsingle-" + release, Namespace: ns}, &newPVC))
	assert.Equal(t, "pv-data", newPVC.Spec.VolumeName)

	var createdTarget vmv1beta1.VMSingle
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: release, Namespace: ns}, &createdTarget))

	var svc corev1.Service
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: release, Namespace: ns}, &svc))
	assert.Equal(t, target.SelectorLabels(), svc.Spec.Selector)

	var agent vmv1beta1.VMAgent
	err = c.Get(ctx, types.NamespacedName{Name: agentName, Namespace: ns}, &agent)
	assert.True(t, k8serrors.IsNotFound(err))
}

func TestWithDowntimeSingleNode_ResumesWhenTargetAlreadyExists(t *testing.T) {
	synctest.Test(t, testWithDowntimeSingleNode_ResumesWhenTargetAlreadyExists)
}

func testWithDowntimeSingleNode_ResumesWhenTargetAlreadyExists(t *testing.T) {
	release := "myrelease"
	ns := "default"
	labels := helmLabels(release)

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-data"},
		Spec: corev1.PersistentVolumeSpec{
			Capacity:    corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			ClaimRef:    &corev1.ObjectReference{Kind: "PersistentVolumeClaim", Namespace: ns, Name: "vmsingle-" + release},
		},
	}
	reboundPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: "vmsingle-" + release, Namespace: ns,
			Annotations: map[string]string{reboundFromAnnotation: ns + "/" + release},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			VolumeName:  "pv-data",
		},
		Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
	}
	oldSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: release, Namespace: ns, Labels: labels,
			Annotations: map[string]string{serviceCutoverAnnotation: "true"},
		},
		Spec: corev1.ServiceSpec{Selector: map[string]string{"app": release}},
	}

	target := &vmv1beta1.VMSingle{
		ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns},
		Spec: vmv1beta1.VMSingleSpec{
			Storage: &corev1.PersistentVolumeClaimSpec{
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
				},
			},
		},
	}
	existingTarget := target.DeepCopy()

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(vmv1beta1.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&vmv1beta1.VMSingle{}, &vmv1beta1.VMAgent{}, &corev1.PersistentVolumeClaim{}).
		WithRuntimeObjects(pv, reboundPVC, oldSvc, existingTarget).
		Build()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	agentName := target.PrefixedName() + "-migration-buffer"

	go simulateOperational(ctx, c, types.NamespacedName{Name: release, Namespace: ns}, func() *vmv1beta1.VMSingle { return &vmv1beta1.VMSingle{} })
	go simulateOperational(ctx, c, types.NamespacedName{Name: agentName, Namespace: ns}, func() *vmv1beta1.VMAgent { return &vmv1beta1.VMAgent{} })
	go simulateVMAgentEndpointsReady(ctx, c, ns, agentName)
	simulateTargetEndpointsReady(ctx, c, target.PrefixedName())

	err := SingleNode(ctx, c, handlerClient(testAdminHandler()), Options{
		Chart:       ChartVMSingle,
		Strategy:    StrategyWithDowntime,
		Namespace:   ns,
		ReleaseName: release,
		Yes:         true,
	}, target, vmSingleNodeEngine(), false)
	require.NoError(t, err)

	var svc corev1.Service
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: release, Namespace: ns}, &svc))
	assert.Equal(t, target.SelectorLabels(), svc.Spec.Selector)

	var agent vmv1beta1.VMAgent
	err = c.Get(ctx, types.NamespacedName{Name: agentName, Namespace: ns}, &agent)
	assert.True(t, k8serrors.IsNotFound(err))
}

func TestWithDowntimeSingleNode_ResumeRejectsUnstampedService(t *testing.T) {
	synctest.Test(t, testWithDowntimeSingleNode_ResumeRejectsUnstampedService)
}

func testWithDowntimeSingleNode_ResumeRejectsUnstampedService(t *testing.T) {
	release := "myrelease"
	ns := "default"
	labels := helmLabels(release)

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-data"},
		Spec: corev1.PersistentVolumeSpec{
			Capacity:    corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			ClaimRef:    &corev1.ObjectReference{Kind: "PersistentVolumeClaim", Namespace: ns, Name: "vmsingle-" + release},
		},
	}
	reboundPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: "vmsingle-" + release, Namespace: ns,
			Annotations: map[string]string{reboundFromAnnotation: ns + "/" + release},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			VolumeName:  "pv-data",
		},
		Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
	}
	// same Helm labels as the release, but never touched by cutoverServices — this tool didn't
	// create the resumable state; something else deleted the Deployment.
	oldSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns, Labels: labels},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": release}},
	}

	target := &vmv1beta1.VMSingle{
		ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns},
		Spec: vmv1beta1.VMSingleSpec{
			Storage: &corev1.PersistentVolumeClaimSpec{
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
				},
			},
		},
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(vmv1beta1.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&vmv1beta1.VMSingle{}, &vmv1beta1.VMAgent{}, &corev1.PersistentVolumeClaim{}).
		WithRuntimeObjects(pv, reboundPVC, oldSvc).
		Build()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	err := SingleNode(ctx, c, handlerClient(testAdminHandler()), Options{
		Chart:       ChartVMSingle,
		Strategy:    StrategyWithDowntime,
		Namespace:   ns,
		ReleaseName: release,
		Yes:         true,
	}, target, vmSingleNodeEngine(), false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "wasn't previously redirected")

	// Service is untouched.
	var svc corev1.Service
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: release, Namespace: ns}, &svc))
	assert.Equal(t, oldSvc.Spec.Selector, svc.Spec.Selector)
}

func TestSingleNode_RejectsCrossNamespaceTarget(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(vmv1beta1.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	ctx := context.Background()

	target := &vmv1beta1.VMSingle{
		ObjectMeta: metav1.ObjectMeta{Name: "myrelease", Namespace: "other-namespace"},
		Spec: vmv1beta1.VMSingleSpec{
			Storage: &corev1.PersistentVolumeClaimSpec{
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
				},
			},
		},
	}

	err := SingleNode(ctx, c, http.DefaultClient, Options{
		Chart:       ChartVMSingle,
		Strategy:    StrategyWithDowntime,
		Namespace:   "default",
		ReleaseName: "myrelease",
		Yes:         true,
	}, target, vmSingleNodeEngine(), false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "different namespace")
}

func TestSingleNode_RejectsTLSEnabledTarget(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(vmv1beta1.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	ctx := context.Background()

	target := &vmv1beta1.VMSingle{
		ObjectMeta: metav1.ObjectMeta{Name: "myrelease", Namespace: "default"},
		Spec: vmv1beta1.VMSingleSpec{
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				ExtraArgs: map[string]string{"tls": "true"},
			},
			Storage: &corev1.PersistentVolumeClaimSpec{
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
				},
			},
		},
	}

	err := SingleNode(ctx, c, http.DefaultClient, Options{
		Chart:       ChartVMSingle,
		Strategy:    StrategyWithDowntime,
		Namespace:   "default",
		ReleaseName: "myrelease",
		Yes:         true,
	}, target, vmSingleNodeEngine(), false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TLS enabled")
}
