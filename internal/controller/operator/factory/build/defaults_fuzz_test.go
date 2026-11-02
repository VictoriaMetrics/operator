package build

import (
	"encoding/json"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/runtime"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

type defaultsKind uint8

const (
	defaultsService defaultsKind = iota
	defaultsDeployment
	defaultsStatefulSet
	defaultsVMAuth
	defaultsVMAgent
	defaultsVMAlert
	defaultsVMSingle
	defaultsVMAlertmanager
	defaultsVMCluster
	defaultsVLogs
	defaultsVLSingle
	defaultsVLCluster
	defaultsVLAgent
	defaultsVTSingle
	defaultsVTAgent
	defaultsVTCluster
	defaultsVMAnomaly
	defaultsVMServiceScrape
	defaultsVMDistributed
	defaultsVLDistributed
	defaultsDaemonSet
	defaultsConfigMap
	defaultsNamespace
	defaultsPVC
	defaultsIngress
	defaultsHTTPRoute
	defaultsSecret
	defaultsKindCount
)

func FuzzAddDefaults(f *testing.F) {
	cases := map[defaultsKind]struct {
		newObject func() runtime.Object
		seed      string
	}{
		defaultsService:         {func() runtime.Object { return &corev1.Service{} }, `{"metadata":{"labels":{"team":"ops"}},"spec":{"type":"LoadBalancer","ports":[{"name":"http","port":8428}]}}`},
		defaultsDeployment:      {func() runtime.Object { return &appsv1.Deployment{} }, `{"spec":{"strategy":{"type":"RollingUpdate"},"template":{"spec":{"containers":[{"name":"vm","image":"vm:latest"}]}}}}`},
		defaultsStatefulSet:     {func() runtime.Object { return &appsv1.StatefulSet{} }, `{"spec":{"updateStrategy":{"type":"RollingUpdate","rollingUpdate":{}},"minReadySeconds":3}}`},
		defaultsVMAuth:          {func() runtime.Object { return &vmv1beta1.VMAuth{} }, `{"spec":{"configSecret":"auth","license":{"key":"enterprise"}}}`},
		defaultsVMAgent:         {func() runtime.Object { return &vmv1beta1.VMAgent{} }, `{"spec":{"remoteWrite":[{"url":"http://localhost:8428/api/v1/write"}],"replicaCount":2}}`},
		defaultsVMAlert:         {func() runtime.Object { return &vmv1beta1.VMAlert{} }, `{"spec":{"remoteRead":{"url":"http://localhost:8428"},"configReloaderImage":"reloader:latest"}}`},
		defaultsVMSingle:        {func() runtime.Object { return &vmv1beta1.VMSingle{} }, `{"spec":{"storage":{"volumeClaimTemplate":{"spec":{"resources":{"requests":{"storage":"1Gi"}}}}},"replicaCount":2}}`},
		defaultsVMAlertmanager:  {func() runtime.Object { return &vmv1beta1.VMAlertmanager{} }, `{"spec":{"configSecret":"alertmanager-config"}}`},
		defaultsVMCluster:       {func() runtime.Object { return &vmv1beta1.VMCluster{} }, `{"spec":{"replicationFactor":2,"vmselect":{"replicaCount":2},"vmstorage":{"replicaCount":3}}}`},
		defaultsVLogs:           {func() runtime.Object { return &vmv1beta1.VLogs{} }, ""},
		defaultsVLSingle:        {func() runtime.Object { return &vmv1.VLSingle{} }, ""},
		defaultsVLCluster:       {func() runtime.Object { return &vmv1.VLCluster{} }, ""},
		defaultsVLAgent:         {func() runtime.Object { return &vmv1.VLAgent{} }, ""},
		defaultsVTSingle:        {func() runtime.Object { return &vmv1.VTSingle{} }, ""},
		defaultsVTAgent:         {func() runtime.Object { return &vmv1.VTAgent{} }, ""},
		defaultsVTCluster:       {func() runtime.Object { return &vmv1.VTCluster{} }, ""},
		defaultsVMAnomaly:       {func() runtime.Object { return &vmv1.VMAnomaly{} }, ""},
		defaultsVMServiceScrape: {func() runtime.Object { return &vmv1beta1.VMServiceScrape{} }, ""},
		defaultsVMDistributed:   {func() runtime.Object { return &vmv1alpha1.VMDistributed{} }, `{"spec":{"backendType":"VMSingle","license":{"key":"enterprise"},"zones":[{"name":"a"}]}}`},
		defaultsVLDistributed:   {func() runtime.Object { return &vmv1alpha1.VLDistributed{} }, `{"spec":{"backendType":"VLSingle","license":{"key":"enterprise"},"zones":[{"name":"a"}]}}`},
		defaultsDaemonSet:       {func() runtime.Object { return &appsv1.DaemonSet{} }, ""},
		defaultsConfigMap:       {func() runtime.Object { return &corev1.ConfigMap{} }, ""},
		defaultsNamespace:       {func() runtime.Object { return &corev1.Namespace{} }, ""},
		defaultsPVC:             {func() runtime.Object { return &corev1.PersistentVolumeClaim{} }, ""},
		defaultsIngress:         {func() runtime.Object { return &networkingv1.Ingress{} }, ""},
		defaultsHTTPRoute:       {func() runtime.Object { return &gwapiv1.HTTPRoute{} }, ""},
		defaultsSecret:          {func() runtime.Object { return &corev1.Secret{} }, ""},
	}
	f.Add(uint8(defaultsVMAuth), []byte(""))
	f.Add(uint8(defaultsVLCluster), []byte("9"))
	for kind := defaultsKind(0); kind < defaultsKindCount; kind++ {
		f.Add(uint8(kind), []byte(`{}`))
		if seed := cases[kind].seed; seed != "" {
			f.Add(uint8(kind), []byte(seed))
		}
	}
	scheme := runtime.NewScheme()
	AddDefaults(scheme)

	f.Fuzz(func(t *testing.T, kind uint8, data []byte) {
		obj := cases[defaultsKind(kind)%defaultsKindCount].newObject()
		if err := json.Unmarshal(data, obj); err != nil {
			fuzztest.New(data).Fill(obj)
		}
		service, isService := obj.(*corev1.Service)
		needsServiceType := isService && service.Spec.Type == ""
		assertStableBuilder(t, func() (any, error) {
			scheme.Default(obj)
			if needsServiceType && service.Spec.Type != corev1.ServiceTypeClusterIP {
				t.Fatalf("service defaulting did not run: type %q", service.Spec.Type)
			}
			return json.Marshal(obj)
		})
	})
}
