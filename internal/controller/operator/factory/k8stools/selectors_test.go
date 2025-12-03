package k8stools

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/VictoriaMetrics/operator/internal/config"
)

func Test_discoverNamespacesOk(t *testing.T) {
	type opts struct {
		selectorOpts      SelectorOpts
		predefinedObjects []runtime.Object
		want              *discoverNamespacesResponse
	}
	f := func(opts opts) {
		t.Helper()
		fclient := GetTestClientWithObjects(opts.predefinedObjects)
		got, err := discoverNamespaces(context.TODO(), fclient, &opts.selectorOpts)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if d := cmp.Diff(got, opts.want, cmp.AllowUnexported(discoverNamespacesResponse{})); len(d) > 0 {
			t.Fatalf("unexpected diff: %s", d)
		}
	}

	// match nothing on namespace selector mismatch
	o := opts{
		selectorOpts: SelectorOpts{
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"env": "dev"},
			},
		},
		predefinedObjects: []runtime.Object{&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns1"}}},
	}
	f(o)

	// match everything - non-nil want
	o = opts{
		predefinedObjects: []runtime.Object{&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns1"}}},
		want:              &discoverNamespacesResponse{},
	}
	f(o)

	// select 1 ns with label selector
	o = opts{
		selectorOpts: SelectorOpts{
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"name": "kube-system",
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns1"}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kube-system", Labels: map[string]string{"name": "kube-system"}}},
		},
		want: &discoverNamespacesResponse{namespaces: []string{"kube-system"}},
	}
	f(o)
	// match for object NS only
	o = opts{
		selectorOpts: SelectorOpts{
			DefaultNamespace: "default",
			ObjectSelector: &metav1.LabelSelector{

				MatchLabels: map[string]string{
					"name": "kube-system",
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns1"}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default", Labels: map[string]string{"name": "default-1"}}},
		},
		want: &discoverNamespacesResponse{namespaces: []string{"default"}},
	}
	f(o)
}

func TestVisitSelected(t *testing.T) {
	type opts struct {
		so                *SelectorOpts
		watchNamespaces   []string
		wantPods          []corev1.Pod
		predefinedObjects []runtime.Object
	}

	podFromNameNs := func(name string, namespace string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
		}
	}
	ignoreDiffOpts := cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion")

	f := func(opts opts) {
		cfg := config.MustGetBaseConfig()
		if len(opts.watchNamespaces) > 0 {
			defaultCfg := *cfg
			cfg.WatchNamespaces = opts.watchNamespaces
			defer func() {
				*config.MustGetBaseConfig() = defaultCfg
			}()
		}
		t.Helper()
		ctx := context.Background()
		fclient := GetTestClientWithObjects(opts.predefinedObjects)
		var gotPods []corev1.Pod
		err := VisitSelected(ctx, fclient, opts.so, func(pl *corev1.PodList) {
			gotPods = append(gotPods, pl.Items...)
		})
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if d := cmp.Diff(opts.wantPods, gotPods, ignoreDiffOpts); len(d) > 0 {
			t.Fatalf("unexpected diff: %s", d)
		}
	}
	// empty objects
	o := opts{
		so: &SelectorOpts{
			DefaultNamespace: "default",
		},
	}
	f(o)

	// all objects at single namespace
	o = opts{
		so: &SelectorOpts{
			ObjectSelector:   &metav1.LabelSelector{},
			DefaultNamespace: "default",
		},
		wantPods: []corev1.Pod{
			*podFromNameNs("pod-1", "default"),
			*podFromNameNs("pod-2", "default"),
		},
		predefinedObjects: []runtime.Object{
			podFromNameNs("pod-1", "default"),
			podFromNameNs("pod-2", "default"),
			podFromNameNs("pod-3", "default-1"),
			podFromNameNs("pod-3", "default-2"),

			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default-1"}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default-2"}},
		},
	}
	f(o)

	// all objects at any namespace
	o = opts{
		so: &SelectorOpts{
			SelectAll:        true,
			DefaultNamespace: "default",
		},
		wantPods: []corev1.Pod{
			*podFromNameNs("pod-1", "default"),
			*podFromNameNs("pod-2", "default"),
			*podFromNameNs("pod-3", "default-1"),
			*podFromNameNs("pod-3", "default-2"),
		},
		predefinedObjects: []runtime.Object{
			podFromNameNs("pod-1", "default"),
			podFromNameNs("pod-2", "default"),
			podFromNameNs("pod-3", "default-1"),
			podFromNameNs("pod-3", "default-2"),

			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default-1"}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default-2"}},
		},
	}
	f(o)

	// all objects at any namespace with non-nil selectors
	o = opts{
		so: &SelectorOpts{
			ObjectSelector:    &metav1.LabelSelector{},
			NamespaceSelector: &metav1.LabelSelector{},
			DefaultNamespace:  "default",
		},
		wantPods: []corev1.Pod{
			*podFromNameNs("pod-1", "default"),
			*podFromNameNs("pod-2", "default"),
			*podFromNameNs("pod-3", "default-1"),
			*podFromNameNs("pod-3", "default-2"),
		},
		predefinedObjects: []runtime.Object{
			podFromNameNs("pod-1", "default"),
			podFromNameNs("pod-2", "default"),
			podFromNameNs("pod-3", "default-1"),
			podFromNameNs("pod-3", "default-2"),

			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default-1"}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default-2"}},
		},
	}
	f(o)

	// objects matched selectors at single namespace
	o = opts{
		so: &SelectorOpts{
			ObjectSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"env": "prod", "v": "v1"},
			},
			DefaultNamespace: "default-5",
		},
		wantPods: []corev1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "default-5", Labels: map[string]string{"env": "prod", "v": "v1", "foo": "bar"}}},
		},
		predefinedObjects: []runtime.Object{
			podFromNameNs("pod-1", "default"),
			podFromNameNs("pod-2", "default"),
			podFromNameNs("pod-3", "default-1"),
			podFromNameNs("pod-3", "default-5"),
			&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "default-5", Labels: map[string]string{"env": "prod", "v": "v1", "foo": "bar"}}},

			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default-1"}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default-2"}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default-5"}},
		},
	}
	f(o)

	// all objects at multiple namespaces matched selectors
	o = opts{
		so: &SelectorOpts{
			NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"dev": "env", "foo": "bar"}},
			DefaultNamespace:  "default-5",
		},
		wantPods: []corev1.Pod{
			*podFromNameNs("pod-3", "default-1"),
			*podFromNameNs("pod-1", "default-3"),
		},
		predefinedObjects: []runtime.Object{
			podFromNameNs("pod-1", "default-3"),
			podFromNameNs("pod-2", "default"),
			podFromNameNs("pod-3", "default-1"),
			podFromNameNs("pod-3", "default-2"),
			&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "default-5", Labels: map[string]string{"env": "prod", "v": "v1", "foo": "bar"}}},

			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default-1", Labels: map[string]string{"dev": "env", "foo": "bar"}}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default-3", Labels: map[string]string{"dev": "env", "foo": "bar"}}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default-5"}},
		},
	}
	f(o)

	// objects matched selectors at multiple namespaces matched selectors
	o = opts{
		so: &SelectorOpts{
			NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"dev": "env", "foo": "bar"}},
			ObjectSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"env": "prod", "v": "v1"},
			},
			DefaultNamespace: "default-5",
		},
		wantPods: []corev1.Pod{
			{ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "default-1", Labels: map[string]string{"env": "prod", "v": "v1", "foo": "bar", "baz": "bar"}}},
			{ObjectMeta: metav1.ObjectMeta{Name: "pod-5", Namespace: "default-3", Labels: map[string]string{"env": "prod", "v": "v1", "foo": "bar"}}},
		},
		predefinedObjects: []runtime.Object{
			podFromNameNs("pod-0", "default-3"),
			podFromNameNs("pod-2", "default"),
			podFromNameNs("pod-4", "default-1"),
			podFromNameNs("pod-6", "default-2"),
			&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "default-1", Labels: map[string]string{"env": "prod", "v": "v1", "foo": "bar", "baz": "bar"}}},
			&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-5", Namespace: "default-3", Labels: map[string]string{"env": "prod", "v": "v1", "foo": "bar"}}},

			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default-1", Labels: map[string]string{"dev": "env", "foo": "bar"}}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default-3", Labels: map[string]string{"dev": "env", "foo": "bar"}}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default-5"}},
		},
	}
	f(o)

	// match nothing due to namespace selectors mismatch
	o = opts{
		so: &SelectorOpts{
			NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"dev": "prod", "bar": "baz"}},
			DefaultNamespace:  "default-5",
		},
		predefinedObjects: []runtime.Object{
			podFromNameNs("pod-1", "default-3"),
			podFromNameNs("pod-2", "default"),
			podFromNameNs("pod-3", "default-1"),
			podFromNameNs("pod-3", "default-2"),
			&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "default-5", Labels: map[string]string{"env": "d", "v": "v1", "foo": "bar"}}},

			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default-1", Labels: map[string]string{"dev": "env", "foo": "bar"}}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default-3", Labels: map[string]string{"dev": "env", "foo": "bar"}}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default-5"}},
		},
	}
	f(o)

	// match nothing due to object selectors mismatch
	o = opts{
		so: &SelectorOpts{
			NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"dev": "env", "foo": "bar"}},
			ObjectSelector:    &metav1.LabelSelector{MatchLabels: map[string]string{"bar": "baz"}},
			DefaultNamespace:  "default-5",
		},
		predefinedObjects: []runtime.Object{
			podFromNameNs("pod-1", "default-3"),
			podFromNameNs("pod-2", "default"),
			podFromNameNs("pod-3", "default-1"),
			podFromNameNs("pod-3", "default-2"),
			&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "default-5", Labels: map[string]string{"env": "d", "v": "v1", "foo": "bar"}}},

			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default-1", Labels: map[string]string{"dev": "env", "foo": "bar"}}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default-3", Labels: map[string]string{"dev": "env", "foo": "bar"}}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default-5"}},
		},
	}
	f(o)

	// watch namespace is set for single ns
	o = opts{
		watchNamespaces: []string{"dev"},
		so: &SelectorOpts{
			NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"dev": "env", "foo": "bar"}},
			ObjectSelector:    &metav1.LabelSelector{},
			DefaultNamespace:  "dev",
		},
		wantPods: []corev1.Pod{
			*podFromNameNs("pod-3", "dev"),
		},
		predefinedObjects: []runtime.Object{
			podFromNameNs("pod-1", "default-3"),
			podFromNameNs("pod-2", "default"),
			podFromNameNs("pod-3", "default-1"),
			podFromNameNs("pod-3", "dev"),
			&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "default-5", Labels: map[string]string{"env": "d", "v": "v1", "foo": "bar"}}},

			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "dev"}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default-1", Labels: map[string]string{"dev": "env", "foo": "bar"}}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default-3", Labels: map[string]string{"dev": "env", "foo": "bar"}}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default-5"}},
		},
	}
	f(o)

	// watch namespace is set for multiple namespace with object selector
	o = opts{
		watchNamespaces: []string{"dev", "prod"},
		so: &SelectorOpts{
			ObjectSelector:   &metav1.LabelSelector{MatchLabels: map[string]string{"dev": "env", "foo": "bar"}},
			DefaultNamespace: "dev",
		},
		wantPods: []corev1.Pod{
			{ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "dev", Labels: map[string]string{"dev": "env", "foo": "bar"}}},
			{ObjectMeta: metav1.ObjectMeta{Name: "pod-5", Namespace: "prod", Labels: map[string]string{"dev": "env", "foo": "bar", "baz": "bar"}}},
		},
		predefinedObjects: []runtime.Object{
			podFromNameNs("pod-1", "default-3"),
			podFromNameNs("pod-2", "dev"),
			&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "dev", Labels: map[string]string{"dev": "env", "foo": "bar"}}},
			&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-5", Namespace: "prod", Labels: map[string]string{"dev": "env", "foo": "bar", "baz": "bar"}}},

			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "dev"}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "prod"}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default-1", Labels: map[string]string{"dev": "env", "foo": "bar"}}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default-3", Labels: map[string]string{"dev": "env", "foo": "bar"}}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default-5"}},
		},
	}
	f(o)

}
