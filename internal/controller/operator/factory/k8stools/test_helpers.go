package k8stools

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/go-test/deep"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func testGetScheme() *runtime.Scheme {
	s := scheme.Scheme
	s.AddKnownTypes(vmv1beta1.GroupVersion,
		&vmv1beta1.VMAgent{},
		&vmv1beta1.VMAgentList{},
		&vmv1beta1.VMAlert{},
		&vmv1beta1.VMAlertList{},
		&vmv1beta1.VMSingle{},
		&vmv1beta1.VMSingleList{},
		&vmv1beta1.VMAlertmanager{},
		&vmv1beta1.VMAlertmanagerList{},
		&vmv1beta1.VMNodeScrapeList{},
		&vmv1beta1.VMStaticScrapeList{},
		&vmv1beta1.VMUserList{},
		&vmv1beta1.VMAuthList{},
		&vmv1beta1.VMAlertmanagerConfigList{},
		&vmv1beta1.VMScrapeConfigList{},
		&vmv1beta1.VMClusterList{},
		&vmv1beta1.VLogsList{},
		&vmv1.VLSingleList{},
		&vmv1.VTSingleList{},
		&vmv1.VLClusterList{},
		&vmv1.VTClusterList{},
		&vmv1.VMAnomalyList{},
		&vmv1.VLAgentList{},
		&gwapiv1.HTTPRouteList{},
	)
	s.AddKnownTypes(vmv1beta1.GroupVersion,
		&vmv1beta1.VMPodScrape{},
		&vmv1beta1.VMPodScrapeList{},
		&vmv1beta1.VMServiceScrapeList{},
		&vmv1beta1.VMServiceScrape{},
		&vmv1beta1.VMServiceScrapeList{},
		&vmv1beta1.VMRule{},
		&vmv1beta1.VMRuleList{},
		&vmv1beta1.VMProbe{},
		&vmv1beta1.VMProbeList{},
		&vmv1beta1.VMNodeScrape{},
		&vmv1beta1.VMStaticScrape{},
		&vmv1beta1.VMUser{},
		&vmv1beta1.VMAuth{},
		&vmv1beta1.VMAlertmanagerConfig{},
		&vmv1beta1.VMScrapeConfig{},
		&vmv1beta1.VMCluster{},
		&vmv1beta1.VLogs{},
		&vmv1.VLSingle{},
		&vmv1.VLCluster{},
		&vmv1.VTSingle{},
		&vmv1.VTCluster{},
		&vmv1.VMAnomaly{},
		&vmv1.VLAgent{},
		&gwapiv1.HTTPRoute{},
	)
	return s
}

// GetTestClientWithObjects returns testing client with optional predefined objects
func GetTestClientWithObjects(predefinedObjects []runtime.Object) client.Client {
	obj := make([]client.Object, 0, len(predefinedObjects))
	for _, o := range predefinedObjects {
		obj = append(obj, o.(client.Object))
	}
	return GetTestClientWithClientObjects(obj)
}

// GetTestClientWithClientObjects returns testing client with optional predefined objects
func GetTestClientWithClientObjects(predefinedObjects []client.Object) client.Client {
	fclient := fake.NewClientBuilder().WithScheme(testGetScheme()).
		WithStatusSubresource(
			&vmv1beta1.VMRule{},
			&vmv1beta1.VMAlert{},
			&vmv1beta1.VMAuth{},
			&vmv1beta1.VMUser{},
			&vmv1beta1.VMCluster{},
			&vmv1beta1.VMSingle{},
			&vmv1beta1.VMAgent{},
			&vmv1beta1.VMAlertmanager{},
			&vmv1beta1.VMAlertmanagerConfig{},
			&vmv1beta1.VLogs{},
			&vmv1beta1.VMServiceScrape{},
			&vmv1beta1.VMPodScrape{},
			&vmv1beta1.VMProbe{},
			&vmv1beta1.VMScrapeConfig{},
			&vmv1beta1.VMStaticScrape{},
			&vmv1beta1.VMNodeScrape{},
			&vmv1.VLSingle{},
			&vmv1.VLCluster{},
			&vmv1.VTSingle{},
			&vmv1.VTCluster{},
			&vmv1.VMAnomaly{},
			&vmv1.VLAgent{},
			&gwapiv1.HTTPRoute{},
		).
		WithObjects(predefinedObjects...).Build()
	withStats := TestClientWithStatsTrack{
		origin: fclient,
	}
	return &withStats
}

// CompareObjectMeta compares metadata objects
func CompareObjectMeta(t *testing.T, got, want metav1.ObjectMeta) {
	if diff := deep.Equal(got.Labels, want.Labels); len(diff) > 0 {
		t.Fatalf("objects not match, labels diff: %v", diff)
	}
	if diff := deep.Equal(got.Annotations, want.Annotations); len(diff) > 0 {
		t.Fatalf("objects not match, annotations diff: %v", diff)
	}

	if diff := deep.Equal(got.Name, want.Name); len(diff) > 0 {
		t.Fatalf("objects not match, Name diff: %v", diff)
	}
	if diff := deep.Equal(got.Namespace, want.Namespace); len(diff) > 0 {
		t.Fatalf("objects not match, namespace diff: %v", diff)
	}
}

// NewReadyDeployment returns a new deployment with ready status condition
func NewReadyDeployment(name, namespace string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Status: appsv1.DeploymentStatus{
			Conditions: []appsv1.DeploymentCondition{
				{
					Reason: "NewReplicaSetAvailable",
					Type:   appsv1.DeploymentProgressing,
					Status: "True",
				},
			},
			UpdatedReplicas:   1,
			AvailableReplicas: 1,
			Replicas:          1,
		},
	}
}

// TestClientWithStatsTrack helps to track actual requests to the api server
type TestClientWithStatsTrack struct {
	origin         client.Client
	ApplyCalls     atomic.Int64
	GetCalls       atomic.Int64
	DeleteCalls    atomic.Int64
	CreateCalls    atomic.Int64
	UpdateCalls    atomic.Int64
	ListCalls      atomic.Int64
	PatchCalls     atomic.Int64
	DeleteAllCalls atomic.Int64
}

func (tcs *TestClientWithStatsTrack) Apply(ctx context.Context, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
	tcs.ApplyCalls.Add(1)
	return tcs.origin.Apply(ctx, obj, opts...)
}

func (tcs *TestClientWithStatsTrack) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	tcs.GetCalls.Add(1)
	return tcs.origin.Get(ctx, key, obj, opts...)
}

func (tcs *TestClientWithStatsTrack) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	tcs.ListCalls.Add(1)
	return tcs.origin.List(ctx, list, opts...)
}

func (tcs *TestClientWithStatsTrack) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	tcs.CreateCalls.Add(1)
	return tcs.origin.Create(ctx, obj, opts...)
}

func (tcs *TestClientWithStatsTrack) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	tcs.DeleteCalls.Add(1)
	return tcs.origin.Delete(ctx, obj, opts...)
}

func (tcs *TestClientWithStatsTrack) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	tcs.UpdateCalls.Add(1)
	return tcs.origin.Update(ctx, obj, opts...)
}

func (tcs *TestClientWithStatsTrack) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	tcs.PatchCalls.Add(1)
	return tcs.origin.Patch(ctx, obj, patch, opts...)
}

// DeleteAllOf deletes all objects of the given type matching the given options.
func (tcs *TestClientWithStatsTrack) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	tcs.DeleteAllCalls.Add(1)
	return tcs.origin.DeleteAllOf(ctx, obj, opts...)
}

func (tcs *TestClientWithStatsTrack) Status() client.SubResourceWriter {
	return tcs.origin.Status()
}

func (tcs *TestClientWithStatsTrack) SubResource(subResource string) client.SubResourceClient {
	return tcs.origin.SubResource(subResource)
}

func (tcs *TestClientWithStatsTrack) Scheme() *runtime.Scheme {
	return tcs.origin.Scheme()
}

func (tcs *TestClientWithStatsTrack) RESTMapper() meta.RESTMapper {
	return tcs.origin.RESTMapper()
}

func (tcs *TestClientWithStatsTrack) GroupVersionKindFor(obj runtime.Object) (schema.GroupVersionKind, error) {
	return tcs.origin.GroupVersionKindFor(obj)
}

func (tcs *TestClientWithStatsTrack) IsObjectNamespaced(obj runtime.Object) (bool, error) {
	return tcs.origin.IsObjectNamespaced(obj)
}
