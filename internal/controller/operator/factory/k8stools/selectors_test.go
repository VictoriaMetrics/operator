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
	tests := []struct {
		name         string
		selectorOpts *SelectorOpts
		predefinedNs []runtime.Object
		want         []string
		wantErr      bool
	}{
		{
			name:         "select 1 ns",
			selectorOpts: &SelectorOpts{},
			predefinedNs: []runtime.Object{&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns1"}}},
			want:         []string{"ns1"},
			wantErr:      false,
		},
		{
			name: "select 1 ns with label selector",
			selectorOpts: &SelectorOpts{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"name": "kube-system",
					},
				},
				SelectAll: true,
			},
			predefinedNs: []runtime.Object{
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns1"}},
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kube-system", Labels: map[string]string{"name": "kube-system"}}},
			},
			want:    []string{"kube-system"},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := GetTestClientWithObjects(tt.predefinedNs)
			got, err := discoverNamespaces(context.TODO(), fclient, tt.selectorOpts)
			if (err != nil) != tt.wantErr {
				t.Errorf("discoverNamespaces() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("discoverNamespaces() got = %v, want %v", got, tt.want)
			}
		})
	}
}
