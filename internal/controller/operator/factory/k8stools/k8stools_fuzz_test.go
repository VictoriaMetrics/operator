package k8stools

import (
	"encoding/json"
	"fmt"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/version"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
)

const fuzzNamespace = "default"

type k8sToolsInput struct {
	Name              string
	Placeholders      map[string]string
	Major             string
	Minor             string
	Watch             bool
	SelectAll         bool
	NamespaceSelector *metav1.LabelSelector
	ObjectSelector    *metav1.LabelSelector
	PodSelector       map[string]string
	Namespaces        []map[string]string
	Pods              []map[string]string
	Conditions        []vmv1beta1.Condition
	Replicas          *int32
	Child             uint8
}

// objects returns namespaces and pods labeled from input
func (in *k8sToolsInput) objects() []runtime.Object {
	objs := []runtime.Object{&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: fuzzNamespace}}}
	for i, l := range in.Namespaces {
		objs = append(objs, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("ns%d", i), Labels: l}})
	}
	for i, l := range in.Pods {
		objs = append(objs, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("p%d", i), Namespace: fuzzNamespace, Labels: l}})
	}
	return objs
}

// children returns objects handled by test client status interceptors
func (in *k8sToolsInput) children() []client.Object {
	meta := metav1.ObjectMeta{Name: "c", Namespace: fuzzNamespace}
	return []client.Object{
		&appsv1.StatefulSet{ObjectMeta: meta, Spec: appsv1.StatefulSetSpec{Replicas: in.Replicas}},
		&appsv1.Deployment{ObjectMeta: meta, Spec: appsv1.DeploymentSpec{Replicas: in.Replicas}},
		&corev1.PersistentVolumeClaim{ObjectMeta: meta},
		&corev1.ConfigMap{ObjectMeta: meta},
		&vmv1beta1.VMAgent{ObjectMeta: meta},
		&vmv1beta1.VMCluster{ObjectMeta: meta},
		&vmv1beta1.VMAuth{ObjectMeta: meta},
		&vmv1.VLCluster{ObjectMeta: meta},
		&vmv1.VLAgent{ObjectMeta: meta},
		&vmv1.VLSingle{ObjectMeta: meta},
	}
}

func FuzzK8sTools(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"Name":"My_Volume.Name-","Placeholders":{"%NAME%":"x"},"Major":"1","Minor":"34+"}`,
		`{"Name":"%NAME%","Placeholders":{"NAME":"x"},"Major":"bad","Minor":"bad"}`,
		`{"Name":"%NAME%","Placeholders":{"%NAME%":"\""}}`,
		`{"SelectAll":true,"Pods":[{"app":"a"},{}],"PodSelector":{"app":"a"},"Replicas":2}`,
		`{"ObjectSelector":{"matchLabels":{"app":"a"}},"Pods":[{"app":"a"}]}`,
		`{"ObjectSelector":{"matchExpressions":[{"key":"app","operator":"bad"}]}}`,
		`{"NamespaceSelector":{},"Pods":[{}]}`,
		`{"NamespaceSelector":{"matchLabels":{"team":"a"}},"Namespaces":[{"team":"a"},{"team":"b"}]}`,
		`{"NamespaceSelector":{"matchLabels":{"team":"c"}},"Namespaces":[{"team":"a"}]}`,
		`{"NamespaceSelector":{"matchExpressions":[{"key":"team","operator":"bad"}]}}`,
		`{"Watch":true,"SelectAll":true,"Pods":[{}]}`,
		`{"Conditions":[{"type":"a"},{"type":"b"}]}`,
	} {
		f.Add([]byte(seed))
	}
	for child := range len((&k8sToolsInput{}).children()) {
		f.Add(fmt.Appendf(nil, `{"Child":%d,"Replicas":2}`, child))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var in k8sToolsInput
		if err := json.Unmarshal(data, &in); err != nil {
			fuzztest.New(data).Fill(&in)
		}

		_ = SanitizeVolumeName(in.Name)
		_, _ = RenderPlaceholders(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: in.Name}}, in.Placeholders)
		_ = ChildConditionIndexerFunc(&corev1.Pod{})
		rule := &vmv1beta1.VMRule{}
		rule.GetStatusMetadata().Conditions = in.Conditions
		_ = ChildConditionIndexerFunc(rule)

		// version globals are shared, restore them
		major, minor, openShift := ServerMajorVersion, ServerMinorVersion, IsOpenShiftDetected
		cfg := config.MustGetBaseConfig()
		watch := cfg.WatchNamespaces
		t.Cleanup(func() {
			ServerMajorVersion, ServerMinorVersion, IsOpenShiftDetected = major, minor, openShift
			cfg.WatchNamespaces = watch
		})
		SetIsOpenShiftDetected(in.Watch)
		_ = SetKubernetesVersionWithDefaults(&version.Info{Major: in.Major, Minor: in.Minor}, 30, 1)
		_ = IsFSGroupChangePolicySupported()
		_ = IsPodLifecycleSleepActionSupported()
		_ = IsVolumeAttributesClassRemovalStatusScoped()

		// watched namespaces must include reconciled object namespace
		if in.Watch {
			cfg.WatchNamespaces = []string{fuzzNamespace, "ns0"}
		}
		ctx := t.Context()
		rclient := GetTestClientWithOptsActionsAndObjects(in.objects(), &ClientOpts{SetCreationTimestamp: true})
		opts := &SelectorOpts{
			SelectAll:         in.SelectAll,
			NamespaceSelector: in.NamespaceSelector,
			ObjectSelector:    in.ObjectSelector,
			DefaultNamespace:  fuzzNamespace,
		}
		_ = VisitSelected(ctx, rclient, opts, func(*corev1.PodList) {})
		_ = UpdatePodAnnotations(ctx, rclient, in.PodSelector, fuzzNamespace)

		// one child per exec: fake tracker rebuilds REST mapper on each write
		children := in.children()
		_ = rclient.Create(ctx, children[int(in.Child)%len(children)])
	})
}
