package reconcile

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	v2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

// object kinds, used as reconcileTargets index and in seeds
const (
	configMapKind uint8 = iota
	secretKind
	serviceKind
	serviceAccountKind
	pdbKind
	hpaKind
	networkPolicyKind
	ingressKind
	httpRouteKind
	roleKind
	roleBindingKind
	clusterRoleKind
	clusterRoleBindingKind
	pvcKind
	deploymentKind
	statefulSetKind
	daemonSetKind
	vpaKind
	vlAgentKind
	vlClusterKind
	vlSingleKind
	vmAgentKind
	vmAlertmanagerConfigKind
	vmAuthKind
	vmClusterKind
	vmPodScrapeKind
	vmProbeKind
	vmRuleKind
	vmScrapeConfigKind
	vmServiceScrapeKind
	vmSingleKind
	objectKindCount
)

type reconcileFunc[T client.Object] func(ctx context.Context, rclient client.Client, newObj, prevObj T, owner *metav1.OwnerReference, flag bool) error

type reconcileTarget func(t *testing.T, data []byte, flag bool)

// isNil reports typed nil pointers
func isNil(v any) bool {
	rv := reflect.ValueOf(v)
	return !rv.IsValid() || rv.IsNil()
}

// newReconcileTarget decodes new, prev and existing objects and reconciles new over existing
func newReconcileTarget[T client.Object](run reconcileFunc[T]) reconcileTarget {
	return func(t *testing.T, data []byte, flag bool) {
		var in struct {
			New      T            `json:"new"`
			Prev     T            `json:"prev"`
			Existing T            `json:"existing"`
			Pods     []corev1.Pod `json:"pods"`
		}
		if err := json.Unmarshal(data, &in); err != nil {
			fuzztest.New(data).Fill(&in)
		}
		if isNil(in.New) {
			return
		}

		// fake client rejects managed fields, deletion without finalizers and preset versions
		var objects []runtime.Object
		for _, obj := range []T{in.New, in.Prev, in.Existing} {
			if isNil(obj) {
				continue
			}
			obj.SetName("fuzz")
			obj.SetNamespace("default")
			obj.SetResourceVersion("")
			obj.SetManagedFields(nil)
			obj.SetDeletionTimestamp(nil)
		}
		if !isNil(in.Existing) {
			objects = append(objects, in.Existing)
		}
		for i := range in.Pods {
			p := &in.Pods[i]
			if p.Name == "" {
				continue
			}
			p.Namespace = "default"
			p.ResourceVersion = ""
			p.ManagedFields = nil
			p.DeletionTimestamp = nil
			objects = append(objects, p)
		}

		var owner *metav1.OwnerReference
		if flag {
			owner = &metav1.OwnerReference{APIVersion: "v1", Kind: "Pod", Name: "owner", UID: "uid", Controller: ptr.To(true)}
		}
		rclient := k8stools.GetTestClientWithObjects(objects)

		// fake clock makes polling time out instantly, CR status waits poll until ctx is done
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			_ = run(ctx, rclient, in.New, in.Prev, owner, flag)
		})
	}
}

// prevMeta returns prev object metadata or nil
func prevMeta(obj client.Object) *metav1.ObjectMeta {
	if isNil(obj) {
		return nil
	}
	m := metav1.ObjectMeta{}
	reflect.ValueOf(&m).Elem().Set(reflect.ValueOf(obj).Elem().FieldByName("ObjectMeta"))
	return &m
}

var reconcileTargets = [...]reconcileTarget{
	configMapKind: newReconcileTarget(func(ctx context.Context, c client.Client, n, p *corev1.ConfigMap, o *metav1.OwnerReference, _ bool) error {
		_, err := ConfigMap(ctx, c, n, prevMeta(p), o)
		return err
	}),
	secretKind: newReconcileTarget(func(ctx context.Context, c client.Client, n, p *corev1.Secret, o *metav1.OwnerReference, _ bool) error {
		return Secret(ctx, c, n, prevMeta(p), o)
	}),
	serviceKind: newReconcileTarget(func(ctx context.Context, c client.Client, n, p *corev1.Service, o *metav1.OwnerReference, _ bool) error {
		return Service(ctx, c, n, p, o)
	}),
	serviceAccountKind: newReconcileTarget(func(ctx context.Context, c client.Client, n, p *corev1.ServiceAccount, o *metav1.OwnerReference, _ bool) error {
		return ServiceAccount(ctx, c, n, p, o)
	}),
	pdbKind: newReconcileTarget(func(ctx context.Context, c client.Client, n, p *policyv1.PodDisruptionBudget, o *metav1.OwnerReference, _ bool) error {
		return PDB(ctx, c, n, p, o)
	}),
	hpaKind: newReconcileTarget(func(ctx context.Context, c client.Client, n, p *v2.HorizontalPodAutoscaler, o *metav1.OwnerReference, _ bool) error {
		return HPA(ctx, c, n, p, o)
	}),
	networkPolicyKind: newReconcileTarget(func(ctx context.Context, c client.Client, n, p *networkingv1.NetworkPolicy, o *metav1.OwnerReference, _ bool) error {
		return NetworkPolicy(ctx, c, n, p, o)
	}),
	ingressKind: newReconcileTarget(func(ctx context.Context, c client.Client, n, p *networkingv1.Ingress, o *metav1.OwnerReference, _ bool) error {
		return Ingress(ctx, c, n, p, o)
	}),
	httpRouteKind: newReconcileTarget(func(ctx context.Context, c client.Client, n, p *gwapiv1.HTTPRoute, o *metav1.OwnerReference, _ bool) error {
		return HTTPRoute(ctx, c, n, p, o)
	}),
	roleKind: newReconcileTarget(func(ctx context.Context, c client.Client, n, p *rbacv1.Role, o *metav1.OwnerReference, _ bool) error {
		return Role(ctx, c, n, p, o)
	}),
	roleBindingKind: newReconcileTarget(func(ctx context.Context, c client.Client, n, p *rbacv1.RoleBinding, o *metav1.OwnerReference, _ bool) error {
		return RoleBinding(ctx, c, n, p, o)
	}),
	clusterRoleKind: newReconcileTarget(func(ctx context.Context, c client.Client, n, p *rbacv1.ClusterRole, _ *metav1.OwnerReference, _ bool) error {
		return ClusterRole(ctx, c, n, p)
	}),
	clusterRoleBindingKind: newReconcileTarget(func(ctx context.Context, c client.Client, n, p *rbacv1.ClusterRoleBinding, _ *metav1.OwnerReference, _ bool) error {
		return ClusterRoleBinding(ctx, c, n, p)
	}),
	pvcKind: newReconcileTarget(func(ctx context.Context, c client.Client, n, p *corev1.PersistentVolumeClaim, o *metav1.OwnerReference, _ bool) error {
		return PersistentVolumeClaim(ctx, c, n, p, o)
	}),
	deploymentKind: newReconcileTarget(func(ctx context.Context, c client.Client, n, p *appsv1.Deployment, o *metav1.OwnerReference, _ bool) error {
		return Deployment(ctx, c, n, p, o, &DeploymentOpts{})
	}),
	statefulSetKind: newReconcileTarget(func(ctx context.Context, c client.Client, n, p *appsv1.StatefulSet, o *metav1.OwnerReference, flag bool) error {
		opts := &StatefulSetOpts{}
		if flag {
			opts.UpdateBehavior = &vmv1beta1.StatefulSetUpdateStrategyBehavior{MaxUnavailable: ptr.To(intstr.FromInt32(1))}
		}
		return StatefulSet(ctx, c, n, p, o, opts)
	}),
	daemonSetKind: newReconcileTarget(func(ctx context.Context, c client.Client, n, p *appsv1.DaemonSet, o *metav1.OwnerReference, _ bool) error {
		return DaemonSet(ctx, c, n, p, o)
	}),
	vpaKind: newReconcileTarget(func(ctx context.Context, c client.Client, n, p *vpav1.VerticalPodAutoscaler, o *metav1.OwnerReference, _ bool) error {
		return VPA(ctx, c, n, p, o)
	}),
	vlAgentKind: newReconcileTarget(func(ctx context.Context, c client.Client, n, p *vmv1.VLAgent, o *metav1.OwnerReference, _ bool) error {
		return VLAgent(ctx, c, n, p, o)
	}),
	vlClusterKind: newReconcileTarget(func(ctx context.Context, c client.Client, n, p *vmv1.VLCluster, o *metav1.OwnerReference, _ bool) error {
		return VLCluster(ctx, c, n, p, o)
	}),
	vlSingleKind: newReconcileTarget(func(ctx context.Context, c client.Client, n, p *vmv1.VLSingle, o *metav1.OwnerReference, _ bool) error {
		return VLSingle(ctx, c, n, p, o)
	}),
	vmAgentKind: newReconcileTarget(func(ctx context.Context, c client.Client, n, p *vmv1beta1.VMAgent, o *metav1.OwnerReference, _ bool) error {
		return VMAgent(ctx, c, n, p, o)
	}),
	vmAlertmanagerConfigKind: newReconcileTarget(func(ctx context.Context, c client.Client, n, p *vmv1beta1.VMAlertmanagerConfig, o *metav1.OwnerReference, flag bool) error {
		return VMAlertmanagerConfig(ctx, c, n, p, o, flag)
	}),
	vmAuthKind: newReconcileTarget(func(ctx context.Context, c client.Client, n, p *vmv1beta1.VMAuth, o *metav1.OwnerReference, _ bool) error {
		return VMAuth(ctx, c, n, p, o)
	}),
	vmClusterKind: newReconcileTarget(func(ctx context.Context, c client.Client, n, p *vmv1beta1.VMCluster, o *metav1.OwnerReference, _ bool) error {
		return VMCluster(ctx, c, n, p, o)
	}),
	vmPodScrapeKind: newReconcileTarget(func(ctx context.Context, c client.Client, n, p *vmv1beta1.VMPodScrape, o *metav1.OwnerReference, flag bool) error {
		return VMPodScrape(ctx, c, n, p, o, flag)
	}),
	vmProbeKind: newReconcileTarget(func(ctx context.Context, c client.Client, n, p *vmv1beta1.VMProbe, o *metav1.OwnerReference, flag bool) error {
		return VMProbe(ctx, c, n, p, o, flag)
	}),
	vmRuleKind: newReconcileTarget(func(ctx context.Context, c client.Client, n, p *vmv1beta1.VMRule, o *metav1.OwnerReference, flag bool) error {
		return VMRule(ctx, c, n, p, o, flag)
	}),
	vmScrapeConfigKind: newReconcileTarget(func(ctx context.Context, c client.Client, n, p *vmv1beta1.VMScrapeConfig, o *metav1.OwnerReference, flag bool) error {
		return VMScrapeConfig(ctx, c, n, p, o, flag)
	}),
	vmServiceScrapeKind: newReconcileTarget(func(ctx context.Context, c client.Client, n, p *vmv1beta1.VMServiceScrape, o *metav1.OwnerReference, flag bool) error {
		return VMServiceScrape(ctx, c, n, p, o, flag)
	}),
	vmSingleKind: newReconcileTarget(func(ctx context.Context, c client.Client, n, p *vmv1beta1.VMSingle, o *metav1.OwnerReference, _ bool) error {
		return VMSingle(ctx, c, n, p, o)
	}),
}

type objectSeed struct {
	kind uint8
	data string
}

// meta changes labels, annotations and finalizers between prev, existing and new
const metaSeed = `{"new":{"metadata":{"labels":{"a":"new"},"annotations":{"x":"new"}}},` +
	`"prev":{"metadata":{"labels":{"a":"old","gone":"1"},"annotations":{"gone":"1"}}},` +
	`"existing":{"metadata":{"labels":{"a":"cur","gone":"1","ext":"1"},"annotations":{"ext":"1"},"finalizers":["apps.victoriametrics.com/finalizer"]}}}`

func FuzzReconcileObjects(f *testing.F) {
	seeds := []objectSeed{
		{configMapKind, `{"new":{"data":{"a":"1","b":"2"}},"existing":{"data":{"a":"0"},"binaryData":{"c":"AA=="}}}`},
		{secretKind, `{"new":{"data":{"a":"MQ=="}},"existing":{"data":{"a":"MA=="}}}`},
		{serviceKind, `{"new":{"spec":{"type":"LoadBalancer","ports":[{"name":"http","port":80,"nodePort":30000}]}},"existing":{"spec":{"type":"ClusterIP","clusterIP":"None","ports":[{"name":"http","port":81}]}}}`},
		{serviceKind, `{"new":{"spec":{"type":"NodePort","clusterIP":"10.0.0.1","ports":[{"port":80}]}},"prev":{"spec":{"type":"ClusterIP"}},"existing":{"spec":{"type":"NodePort","clusterIP":"10.0.0.2","ports":[{"port":80,"nodePort":30001}],"healthCheckNodePort":3}}}`},
		{serviceAccountKind, `{"new":{"automountServiceAccountToken":false},"existing":{"secrets":[{"name":"s"}],"imagePullSecrets":[{"name":"p"}]}}`},
		{pdbKind, `{"new":{"spec":{"minAvailable":1,"selector":{"matchLabels":{"a":"b"}}}},"existing":{"spec":{"maxUnavailable":"50%"}}}`},
		{hpaKind, `{"new":{"spec":{"minReplicas":1,"maxReplicas":3,"scaleTargetRef":{"kind":"StatefulSet","name":"s"}}},"existing":{"spec":{"maxReplicas":2}}}`},
		{networkPolicyKind, `{"new":{"spec":{"podSelector":{"matchLabels":{"a":"b"}},"ingress":[{}]}},"existing":{"spec":{"policyTypes":["Egress"]}}}`},
		{ingressKind, `{"new":{"spec":{"ingressClassName":"nginx","rules":[{"host":"a"}]}},"existing":{"spec":{"tls":[{"hosts":["b"]}]}}}`},
		{httpRouteKind, `{"new":{"spec":{"hostnames":["a"],"parentRefs":[{"name":"gw"}]}},"existing":{"spec":{"hostnames":["b"]}}}`},
		{roleKind, `{"new":{"rules":[{"verbs":["get"],"resources":["pods"],"apiGroups":[""]}]},"existing":{"rules":[]}}`},
		{roleBindingKind, `{"new":{"roleRef":{"kind":"Role","name":"r"},"subjects":[{"kind":"ServiceAccount","name":"s"}]},"existing":{"roleRef":{"kind":"Role","name":"other"}}}`},
		{clusterRoleKind, `{"new":{"rules":[{"verbs":["list"],"nonResourceURLs":["/metrics"]}],"aggregationRule":{}},"existing":{}}`},
		{clusterRoleBindingKind, `{"new":{"roleRef":{"kind":"ClusterRole","name":"r"},"subjects":[{"kind":"User","name":"u"}]},"existing":{}}`},
		{pvcKind, `{"new":{"spec":{"resources":{"requests":{"storage":"2Gi"}},"storageClassName":"sc"}},"existing":{"spec":{"resources":{"requests":{"storage":"1Gi"}}},"status":{"phase":"Bound"}}}`},
		{pvcKind, `{"new":{"spec":{"resources":{"requests":{"storage":"1Gi"}}}},"existing":{"spec":{"resources":{"requests":{"storage":"2Gi"}}}}}`},
		{deploymentKind, `{"new":{"spec":{"replicas":2,"selector":{"matchLabels":{"a":"b"}},"template":{"metadata":{"labels":{"a":"b"},"annotations":{"x":"y"}}}}},"prev":{"spec":{"template":{"metadata":{"annotations":{"old":"1"}}}}},"existing":{"spec":{"replicas":1},"status":{"replicas":1,"readyReplicas":1,"updatedReplicas":1}}}`},
		{statefulSetKind, `{"new":{"spec":{"replicas":2,"serviceName":"s","selector":{"matchLabels":{"a":"b"}},"template":{"metadata":{"labels":{"a":"b"}}},"volumeClaimTemplates":[{"metadata":{"name":"data"},"spec":{"resources":{"requests":{"storage":"2Gi"}}}}],"persistentVolumeClaimRetentionPolicy":{"whenDeleted":"Delete"}}},` +
			`"prev":{"spec":{"volumeClaimTemplates":[{"metadata":{"name":"data"},"spec":{"resources":{"requests":{"storage":"1Gi"}}}}]}},` +
			`"existing":{"spec":{"replicas":1,"serviceName":"old","volumeClaimTemplates":[{"metadata":{"name":"data"},"spec":{"resources":{"requests":{"storage":"1Gi"}}}}]},"status":{"replicas":1,"readyReplicas":1,"updatedReplicas":1,"currentRevision":"a","updateRevision":"b"}}}`},
		{statefulSetKind, `{"new":{"spec":{"updateStrategy":{"type":"OnDelete"},"selector":{"matchLabels":{"a":"b"}}}},"existing":{"spec":{"updateStrategy":{"type":"RollingUpdate"}}}}`},
		{statefulSetKind, `{"new":{"spec":{"replicas":2,"updateStrategy":{"type":"OnDelete"},"selector":{"matchLabels":{"a":"b"}},"template":{"metadata":{"labels":{"a":"b"}}}}},` +
			`"existing":{"spec":{"replicas":2,"updateStrategy":{"type":"OnDelete"},"selector":{"matchLabels":{"a":"b"}},"template":{"metadata":{"labels":{"a":"b"}}}},"status":{"replicas":2,"currentRevision":"v1","updateRevision":"v2"}},` +
			`"pods":[{"metadata":{"name":"fuzz-0","labels":{"a":"b","controller-revision-hash":"v1"},"ownerReferences":[{"apiVersion":"apps/v1","kind":"StatefulSet","name":"fuzz","uid":"u","controller":true}]},"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]}},{"metadata":{"name":"fuzz-1","labels":{"a":"b","controller-revision-hash":"v2"},"ownerReferences":[{"apiVersion":"apps/v1","kind":"StatefulSet","name":"fuzz","uid":"u","controller":true}]},"status":{"phase":"Pending","containerStatuses":[{"name":"c","state":{"waiting":{"reason":"CrashLoopBackOff"}}}]}}]}`},
		{deploymentKind, `{"new":{"spec":{"replicas":2,"selector":{"matchLabels":{"a":"b"}},"template":{"metadata":{"labels":{"a":"b"}}}}},` +
			`"pods":[{"metadata":{"name":"fuzz-0","labels":{"a":"b","controller-revision-hash":"v1"},"ownerReferences":[{"apiVersion":"apps/v1","kind":"StatefulSet","name":"fuzz","uid":"u","controller":true}]},"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]}},{"metadata":{"name":"fuzz-1","labels":{"a":"b","controller-revision-hash":"v2"},"ownerReferences":[{"apiVersion":"apps/v1","kind":"StatefulSet","name":"fuzz","uid":"u","controller":true}]},"status":{"phase":"Pending","containerStatuses":[{"name":"c","state":{"waiting":{"reason":"CrashLoopBackOff"}}}]}}]}`},
		{daemonSetKind, `{"new":{"spec":{"selector":{"matchLabels":{"a":"b"}},"template":{"metadata":{"labels":{"a":"b"}}}}},"existing":{"status":{"desiredNumberScheduled":1,"numberReady":1}}}`},
		{vpaKind, `{"new":{"spec":{"targetRef":{"kind":"Deployment","name":"d"},"updatePolicy":{"updateMode":"Auto"}}},"existing":{"spec":{"targetRef":{"kind":"Deployment","name":"x"}}}}`},
		{vlAgentKind, `{"new":{"spec":{"remoteWrite":[{"url":"http://a"}]}},"existing":{"spec":{"replicaCount":2}}}`},
		{vlClusterKind, `{"new":{"spec":{"vlstorage":{"replicaCount":2}}},"existing":{"spec":{}}}`},
		{vlSingleKind, `{"new":{"spec":{"retentionPeriod":"1d"}},"existing":{"spec":{}}}`},
		{vmAgentKind, `{"new":{"spec":{"remoteWrite":[{"url":"http://a"}]}},"existing":{"spec":{"remoteWrite":[{"url":"http://b"}]},"status":{"updateStatus":"operational"}}}`},
		{vmAlertmanagerConfigKind, `{"new":{"spec":{"route":{"receiver":"r"},"receivers":[{"name":"r"}]}},"existing":{"spec":{}}}`},
		{vmAuthKind, `{"new":{"spec":{"selectAllByDefault":true}},"existing":{"spec":{}}}`},
		{vmClusterKind, `{"new":{"spec":{"retentionPeriod":"1","vmstorage":{"replicaCount":2}}},"existing":{"spec":{"retentionPeriod":"2"}}}`},
		{vmPodScrapeKind, `{"new":{"spec":{"podMetricsEndpoints":[{"port":"http"}]}},"existing":{"spec":{}}}`},
		{vmProbeKind, `{"new":{"spec":{"vmProberSpec":{"url":"http://p"}}},"existing":{"spec":{}}}`},
		{vmRuleKind, `{"new":{"spec":{"groups":[{"name":"g","rules":[{"record":"r","expr":"up"}]}]}},"existing":{"spec":{}}}`},
		{vmScrapeConfigKind, `{"new":{"spec":{"staticConfigs":[{"targets":["a"]}]}},"existing":{"spec":{}}}`},
		{vmServiceScrapeKind, `{"new":{"spec":{"endpoints":[{"port":"http"}]}},"existing":{"spec":{}}}`},
		{vmSingleKind, `{"new":{"spec":{"retentionPeriod":"1"}},"existing":{"spec":{"retentionPeriod":"2"}}}`},
	}
	for kind := range objectKindCount {
		seeds = append(seeds, objectSeed{kind, `{"new":{}}`}, objectSeed{kind, metaSeed})
	}
	for _, seed := range seeds {
		f.Add(seed.kind, false, []byte(seed.data))
		f.Add(seed.kind, true, []byte(seed.data))
	}
	f.Fuzz(func(t *testing.T, kind uint8, flag bool, data []byte) {
		reconcileTargets[int(kind)%len(reconcileTargets)](t, data, flag)
	})
}
