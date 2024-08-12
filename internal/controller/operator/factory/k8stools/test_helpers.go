package k8stools

import (
	"testing"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/go-test/deep"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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
	)
	return s
}

// GetTestClientWithObjects returns testing client with optional predefined objects
func GetTestClientWithObjects(predefinedObjects []runtime.Object) client.Client {
	obj := make([]client.Object, 0, len(predefinedObjects))
	for _, o := range predefinedObjects {
		obj = append(obj, o.(client.Object))
	}
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
		).
		WithObjects(obj...).Build()
	return fclient
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
		},
	}
}
