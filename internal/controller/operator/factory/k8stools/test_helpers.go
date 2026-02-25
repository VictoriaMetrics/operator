package k8stools

import (
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func testGetScheme() *runtime.Scheme {
	s := scheme.Scheme
	s.AddKnownTypes(vmv1beta1.SchemeGroupVersion,
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
	s.AddKnownTypes(vmv1alpha1.SchemeGroupVersion,
		&vmv1alpha1.VMDistributedList{},
		&vmv1alpha1.VMDistributed{},
	)
	s.AddKnownTypes(vmv1.SchemeGroupVersion,
		&vmv1.VLSingleList{},
		&vmv1.VTSingleList{},
		&vmv1.VLClusterList{},
		&vmv1.VTClusterList{},
		&vmv1.VMAnomalyList{},
		&vmv1.VLAgentList{},
		&vmv1.VLSingle{},
		&vmv1.VLCluster{},
		&vmv1.VTSingle{},
		&vmv1.VTCluster{},
		&vmv1.VMAnomaly{},
		&vmv1.VLAgent{},
	)
	s.AddKnownTypes(gwapiv1.SchemeGroupVersion,
		&gwapiv1.HTTPRouteList{},
		&gwapiv1.HTTPRoute{},
	)
	s.AddKnownTypes(apiextensionsv1.SchemeGroupVersion,
		&apiextensionsv1.CustomResourceDefinitionList{},
		&apiextensionsv1.CustomResourceDefinition{},
	)
	s.AddKnownTypes(vpav1.SchemeGroupVersion,
		&vpav1.VerticalPodAutoscaler{},
		&vpav1.VerticalPodAutoscalerList{},
	)
	return s
}

// GetTestClientWithObjectsAndInterceptors returns testing client with optional predefined objects and interceptors
func GetTestClientWithObjectsAndInterceptors(predefinedObjects []runtime.Object, fns interceptor.Funcs) client.Client {
	return getTestClient(predefinedObjects, &fns)
}

// GetTestClientWithObjects returns testing client with optional predefined objects
func GetTestClientWithObjects(predefinedObjects []runtime.Object) client.Client {
	fns := GetInterceptorsWithObjects()
	return getTestClient(predefinedObjects, &fns)
}

func getTestClient(predefinedObjects []runtime.Object, fns *interceptor.Funcs) client.Client {
	builder := fake.NewClientBuilder().
		WithScheme(testGetScheme()).
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
			&vmv1alpha1.VMDistributed{},
			&vmv1.VLSingle{},
			&vmv1.VLCluster{},
			&vmv1.VTSingle{},
			&vmv1.VTCluster{},
			&vmv1.VMAnomaly{},
			&vmv1.VLAgent{},
			&gwapiv1.HTTPRoute{},
			&vpav1.VerticalPodAutoscaler{},
		).
		WithRuntimeObjects(predefinedObjects...)
	if fns != nil {
		builder = builder.WithInterceptorFuncs(*fns)
	}
	return builder.Build()
}

// ClientAction represents a client action
type ClientAction struct {
	Verb     string
	Kind     string
	Resource types.NamespacedName
}

// ClientWithActions is a client that tracks actions
type ClientWithActions struct {
	client.Client
	Actions []ClientAction
}

// GetTestClientWithActions returns a client that tracks actions
func GetTestClientWithActions(predefinedObjects []runtime.Object) *ClientWithActions {
	var cwa ClientWithActions

	cwa.Client = GetTestClientWithObjectsAndInterceptors(predefinedObjects, NewActionRecordingInterceptor(&cwa.Actions, nil))
	return &cwa
}

// GetTestClientWithActionsAndObjects returns a client that tracks actions and updates status/objects
func GetTestClientWithActionsAndObjects(predefinedObjects []runtime.Object) *ClientWithActions {
	var cwa ClientWithActions
	objectInterceptors := GetInterceptorsWithObjects()

	cwa.Client = GetTestClientWithObjectsAndInterceptors(predefinedObjects, NewActionRecordingInterceptor(&cwa.Actions, &objectInterceptors))
	return &cwa
}

// CompareObjectMeta compares metadata objects
func CompareObjectMeta(t *testing.T, got, want metav1.ObjectMeta) {
	assert.Equal(t, got.Labels, want.Labels)
	assert.Equal(t, got.Annotations, want.Annotations)
	assert.Equal(t, got.Name, want.Name)
	assert.Equal(t, got.Namespace, want.Namespace)
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
			UpdatedReplicas: 1,
			ReadyReplicas:   1,
			Replicas:        1,
		},
	}
}
