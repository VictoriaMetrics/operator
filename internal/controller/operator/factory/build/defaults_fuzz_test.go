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
		defaultsVMDistributed:   {func() runtime.Object { return &vmv1alpha1.VMDistributed{} }, `{"spec":{"backendType":"VMSingle","license":{"key":"enterprise"},"zones":[{"name":"a"}]}}`},
		defaultsVLDistributed:   {func() runtime.Object { return &vmv1alpha1.VLDistributed{} }, `{"spec":{"backendType":"VLSingle","license":{"key":"enterprise"},"zones":[{"name":"a"}]}}`},
		defaultsVMAuth:          {func() runtime.Object { return &vmv1beta1.VMAuth{} }, `{"spec":{"configSecret":"auth","license":{"key":"enterprise"}}}`},
		defaultsVMAgent:         {func() runtime.Object { return &vmv1beta1.VMAgent{} }, `{"spec":{"remoteWrite":[{"url":"http://localhost:8428/api/v1/write"}],"replicaCount":2}}`},
		defaultsVMAlert:         {func() runtime.Object { return &vmv1beta1.VMAlert{} }, `{"spec":{"remoteRead":{"url":"http://localhost:8428"},"configReloaderImage":"reloader:latest"}}`},
		defaultsVMSingle:        {func() runtime.Object { return &vmv1beta1.VMSingle{} }, `{"spec":{"storage":{"resources":{"requests":{"storage":"1Gi"}}},"replicaCount":2}}`},
		defaultsVMAlertmanager:  {func() runtime.Object { return &vmv1beta1.VMAlertmanager{} }, `{"spec":{"configSecret":"alertmanager-config"}}`},
		defaultsVMCluster:       {func() runtime.Object { return &vmv1beta1.VMCluster{} }, `{"spec":{"replicationFactor":2,"vmselect":{"replicaCount":2},"vmstorage":{"replicaCount":3}}}`},
		defaultsVMServiceScrape: {func() runtime.Object { return &vmv1beta1.VMServiceScrape{} }, `{"spec":{"selector":{"matchLabels":{"app":"api"}},"endpoints":[{"port":"http","path":"/metrics"}]}}`},
		defaultsVLSingle:        {func() runtime.Object { return &vmv1.VLSingle{} }, `{"spec":{"storage":{"resources":{"requests":{"storage":"1Gi"}}},"license":{"key":"enterprise"}}}`},
		defaultsVLCluster:       {func() runtime.Object { return &vmv1.VLCluster{} }, `{"spec":{"vlinsert":{"replicaCount":2},"vlstorage":{"replicaCount":3},"vlselect":{"replicaCount":2}}}`},
		defaultsVLAgent:         {func() runtime.Object { return &vmv1.VLAgent{} }, `{"spec":{"remoteWrite":[{"url":"http://localhost:9428/"}],"replicaCount":2}}`},
		defaultsVTSingle:        {func() runtime.Object { return &vmv1.VTSingle{} }, `{"spec":{"storage":{"resources":{"requests":{"storage":"1Gi"}}},"grpcSpec":{"listenPort":10428}}}`},
		defaultsVTAgent:         {func() runtime.Object { return &vmv1.VTAgent{} }, `{"spec":{"remoteWrite":[{"url":"http://localhost:10428"}],"replicaCount":2}}`},
		defaultsVTCluster:       {func() runtime.Object { return &vmv1.VTCluster{} }, `{"spec":{"insert":{"replicaCount":2},"storage":{"replicaCount":3},"select":{"replicaCount":2}}}`},
		defaultsVMAnomaly:       {func() runtime.Object { return &vmv1.VMAnomaly{} }, `{"spec":{"monitoring":{"pull":{"port":"8081"}},"server":{"port":"8490"},"license":{"key":"enterprise"}}}`},
		defaultsDeployment:      {func() runtime.Object { return &appsv1.Deployment{} }, `{"spec":{"strategy":{"type":"RollingUpdate"},"template":{"spec":{"containers":[{"name":"vm","image":"vm:latest"}]}}}}`},
		defaultsStatefulSet:     {func() runtime.Object { return &appsv1.StatefulSet{} }, `{"spec":{"updateStrategy":{"type":"RollingUpdate","rollingUpdate":{}},"minReadySeconds":3}}`},
		defaultsDaemonSet:       {func() runtime.Object { return &appsv1.DaemonSet{} }, `{"metadata":{"labels":{"app":"agent"}},"spec":{"selector":{"matchLabels":{"app":"agent"}},"template":{"metadata":{"labels":{"app":"agent"}},"spec":{"containers":[{"name":"agent","image":"agent:latest"}]}}}}`},
		defaultsService:         {func() runtime.Object { return &corev1.Service{} }, `{"metadata":{"labels":{"team":"ops"}},"spec":{"type":"LoadBalancer","ports":[{"name":"http","port":8428}]}}`},
		defaultsConfigMap:       {func() runtime.Object { return &corev1.ConfigMap{} }, `{"metadata":{"labels":{"team":"ops"}},"data":{"config.yaml":"key: value"}}`},
		defaultsNamespace:       {func() runtime.Object { return &corev1.Namespace{} }, `{"metadata":{"name":"monitoring","labels":{"team":"ops"}}}`},
		defaultsPVC:             {func() runtime.Object { return &corev1.PersistentVolumeClaim{} }, `{"metadata":{"name":"data"},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"1Gi"}}}}`},
		defaultsSecret:          {func() runtime.Object { return &corev1.Secret{} }, `{"metadata":{"name":"auth"},"type":"Opaque","stringData":{"token":"test"}}`},
		defaultsIngress:         {func() runtime.Object { return &networkingv1.Ingress{} }, `{"metadata":{"name":"api"},"spec":{"rules":[{"host":"example.com","http":{"paths":[{"path":"/","pathType":"Prefix","backend":{"service":{"name":"api","port":{"number":8428}}}}]}}]}}`},
		defaultsHTTPRoute:       {func() runtime.Object { return &gwapiv1.HTTPRoute{} }, `{"metadata":{"name":"api"},"spec":{"parentRefs":[{"name":"gateway"}],"rules":[{"matches":[{"path":{"type":"PathPrefix","value":"/api"}}]}]}}`},
	}
	for kind := defaultsKind(0); kind < defaultsKindCount; kind++ {
		f.Add(uint8(kind), []byte(``))
		f.Add(uint8(kind), []byte(`1`))
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
		scheme.Default(obj)
	})
}
