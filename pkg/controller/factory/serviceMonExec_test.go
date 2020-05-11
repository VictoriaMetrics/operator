package factory

import (
	monitoringv1 "github.com/VictoriaMetrics/operator/pkg/apis/monitoring/v1"
	monitoringv1beta1 "github.com/VictoriaMetrics/operator/pkg/apis/monitoring/v1beta1"
	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sort"
	"testing"
)

func TestSelectServiceMonitors(t *testing.T) {
	type args struct {
		p *monitoringv1beta1.VmAgent
		l logr.Logger
	}
	tests := []struct {
		name             string
		args             args
		want             []string
		wantErr          bool
		predefinedObjest []runtime.Object
	}{
		{
			name: "select service monitor inside vmagent namespace",
			args: args{
				p: &monitoringv1beta1.VmAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "default-agent",
						Namespace: "default",
					},
					Spec: monitoringv1beta1.VmAgentSpec{
						ServiceMonitorSelector: &metav1.LabelSelector{},
					},
				},
				l: logf.Log.WithName("unit-test"),
			},
			predefinedObjest: []runtime.Object{
				&monitoringv1.ServiceMonitor{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "default-monitor"},
					Spec:       monitoringv1.ServiceMonitorSpec{},
				},
			},
			want:    []string{"default/default-monitor"},
			wantErr: false,
		},
		{
			name: "select service monitor from namespace with filter",
			args: args{
				p: &monitoringv1beta1.VmAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "default-agent",
						Namespace: "default",
					},
					Spec: monitoringv1beta1.VmAgentSpec{
						ServiceMonitorNamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"name": "stage"}},
						ServiceMonitorSelector:          &metav1.LabelSelector{},
					},
				},
				l: logf.Log.WithName("unit-test"),
			},
			predefinedObjest: []runtime.Object{
				&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
				&monitoringv1.ServiceMonitor{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "default-monitor"},
					Spec:       monitoringv1.ServiceMonitorSpec{},
				},
				&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "stg", Labels: map[string]string{"name": "stage"}}},
				&monitoringv1.ServiceMonitor{
					ObjectMeta: metav1.ObjectMeta{Namespace: "stg", Name: "default-monitor"},
					Spec:       monitoringv1.ServiceMonitorSpec{},
				},
			},
			want:    []string{"stg/default-monitor"},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := []runtime.Object{}
			obj = append(obj, tt.predefinedObjest...)
			s := scheme.Scheme

			s.AddKnownTypes(monitoringv1.SchemeGroupVersion, &monitoringv1.ServiceMonitor{}, &monitoringv1.ServiceMonitorList{})
			s.AddKnownTypes(monitoringv1beta1.SchemeGroupVersion, &monitoringv1beta1.VmAgent{}, &monitoringv1beta1.VmAgentList{})
			fclient := fake.NewFakeClientWithScheme(s, obj...)
			got, err := SelectServiceMonitors(tt.args.p, fclient, tt.args.l)
			if (err != nil) != tt.wantErr {
				t.Errorf("SelectServiceMonitors() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			gotNames := []string{}
			for monitorName := range got {
				gotNames = append(gotNames, monitorName)
			}
			sort.Strings(gotNames)
			if !reflect.DeepEqual(gotNames, tt.want) {
				t.Errorf("SelectServiceMonitors() got = %v, want %v", gotNames, tt.want)
			}
		})
	}
}

func TestSelectPodMonitors(t *testing.T) {
	type args struct {
		p *monitoringv1beta1.VmAgent
		l logr.Logger
	}
	tests := []struct {
		name              string
		args              args
		want              []string
		wantErr           bool
		predefinedObjects []runtime.Object
	}{
		{
			name: "selector pod monitor at vmagent ns",
			args: args{
				p: &monitoringv1beta1.VmAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-vmagent",
						Namespace: "default",
					},
					Spec: monitoringv1beta1.VmAgentSpec{
						PodMonitorSelector: &metav1.LabelSelector{},
					},
				},
				l: logf.Log.WithName("unit-test"),
			},
			predefinedObjects: []runtime.Object{
				&monitoringv1.PodMonitor{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
					},
					Spec: monitoringv1.PodMonitorSpec{},
				},
			},
			wantErr: false,
			want:    []string{"default/pod1"},
		},
		{
			name: "selector pod monitor at different ns with ns selector",
			args: args{
				p: &monitoringv1beta1.VmAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-vmagent",
						Namespace: "default",
					},
					Spec: monitoringv1beta1.VmAgentSpec{
						PodMonitorNamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"name": "monitoring"},
						},
					},
				},
				l: logf.Log.WithName("unit-test"),
			},
			predefinedObjects: []runtime.Object{
				&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "monitor", Labels: map[string]string{"name": "monitoring"}},},
				&monitoringv1.PodMonitor{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod2",
						Namespace: "monitor",
					},
					Spec: monitoringv1.PodMonitorSpec{},
				},
				&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
				&monitoringv1.PodMonitor{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
					},
					Spec: monitoringv1.PodMonitorSpec{},
				},
			},
			wantErr: false,
			want:    []string{"monitor/pod2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := []runtime.Object{}
			obj = append(obj, tt.predefinedObjects...)
			s := scheme.Scheme
			s.AddKnownTypes(monitoringv1beta1.SchemeGroupVersion, &monitoringv1beta1.VmAgent{}, &monitoringv1beta1.VmAgentList{})
			s.AddKnownTypes(monitoringv1.SchemeGroupVersion, &monitoringv1.PodMonitor{}, &monitoringv1.PodMonitorList{})
			fclient := fake.NewFakeClientWithScheme(s, obj...)
			got, err := SelectPodMonitors(tt.args.p, fclient, tt.args.l)
			if (err != nil) != tt.wantErr {
				t.Errorf("SelectPodMonitors() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			gotNames := []string{}
			for podName := range got {
				gotNames = append(gotNames, podName)
			}
			sort.Strings(gotNames)
			if !reflect.DeepEqual(gotNames, tt.want) {
				t.Errorf("SelectPodMonitors() got = %v, want %v", gotNames, tt.want)
			}
		})
	}
}
