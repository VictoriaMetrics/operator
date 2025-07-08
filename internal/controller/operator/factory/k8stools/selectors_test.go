package k8stools

import (
	"context"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func Test_discoverNamespaces(t *testing.T) {
	f := func(opts *SelectorOpts, predefinedObjects []runtime.Object, want []string) {
		fclient := GetTestClientWithObjects(predefinedObjects)
		got, err := discoverNamespaces(context.TODO(), fclient, opts)
		if err != nil {
			t.Errorf("discoverNamespaces() error = %v", err)
			return
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("discoverNamespaces() got = %v, want %v", got, want)
		}
	}

	// select 1 ns
	f(&SelectorOpts{}, []runtime.Object{
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "ns1",
			},
		},
	}, []string{"ns1"})

	// select 1 ns with label selector
	f(&SelectorOpts{
		NamespaceSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"name": "kube-system",
			},
		},
		SelectAll: true,
	}, []runtime.Object{
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns1"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kube-system", Labels: map[string]string{"name": "kube-system"}}},
	}, []string{"kube-system"})
}
