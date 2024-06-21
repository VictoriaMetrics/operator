package vmagent

import (
	"context"
	"reflect"
	"sort"
	"testing"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/factory/k8stools"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func TestSelectServiceMonitors(t *testing.T) {
	type args struct {
		p *vmv1beta1.VMAgent
		l logr.Logger
	}

	predefinedObjects := []runtime.Object{
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&vmv1beta1.VMServiceScrape{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "ss1", Labels: map[string]string{"name": "ss1"}},
			Spec:       vmv1beta1.VMServiceScrapeSpec{},
		},
		&vmv1beta1.VMServiceScrape{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "ss2", Labels: map[string]string{"name": "ss2"}},
			Spec:       vmv1beta1.VMServiceScrapeSpec{},
		},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "other1", Labels: map[string]string{"name": "other1"}}},
		&vmv1beta1.VMServiceScrape{
			ObjectMeta: metav1.ObjectMeta{Namespace: "other1", Name: "ss1", Labels: map[string]string{"name": "ss1"}},
			Spec:       vmv1beta1.VMServiceScrapeSpec{},
		},
		&vmv1beta1.VMServiceScrape{
			ObjectMeta: metav1.ObjectMeta{Namespace: "other1", Name: "ss2", Labels: map[string]string{"name": "ss2"}},
			Spec:       vmv1beta1.VMServiceScrapeSpec{},
		},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "other2", Labels: map[string]string{"name": "other2"}}},
		&vmv1beta1.VMServiceScrape{
			ObjectMeta: metav1.ObjectMeta{Namespace: "other2", Name: "ss1", Labels: map[string]string{"name": "ss1"}},
			Spec:       vmv1beta1.VMServiceScrapeSpec{},
		},
		&vmv1beta1.VMServiceScrape{
			ObjectMeta: metav1.ObjectMeta{Namespace: "other2", Name: "ss2", Labels: map[string]string{"name": "ss2"}},
			Spec:       vmv1beta1.VMServiceScrapeSpec{},
		},
	}

	tests := []struct {
		name              string
		args              args
		want              []string
		wantErr           bool
		predefinedObjects []runtime.Object
	}{
		{
			name: "select service scrape inside vmagent namespace",
			args: args{
				p: &vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "default-agent",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAgentSpec{
						ServiceScrapeSelector: &metav1.LabelSelector{},
					},
				},
				l: logf.Log.WithName("unit-test"),
			},
			predefinedObjects: []runtime.Object{
				&vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "default-monitor"},
					Spec:       vmv1beta1.VMServiceScrapeSpec{},
				},
			},
			want:    []string{"default/default-monitor"},
			wantErr: false,
		},
		{
			name: "select service scrape from namespace with filter",
			args: args{
				p: &vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "default-agent",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAgentSpec{
						ServiceScrapeNamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"name": "stage"}},
						ServiceScrapeSelector:          &metav1.LabelSelector{},
					},
				},
				l: logf.Log.WithName("unit-test"),
			},
			predefinedObjects: []runtime.Object{
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
				&vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "default-monitor"},
					Spec:       vmv1beta1.VMServiceScrapeSpec{},
				},
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "stg", Labels: map[string]string{"name": "stage"}}},
				&vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{Namespace: "stg", Name: "default-monitor"},
					Spec:       vmv1beta1.VMServiceScrapeSpec{},
				},
			},
			want:    []string{"stg/default-monitor"},
			wantErr: false,
		},
		{
			name: "If NamespaceSelector and Selector both undefined, selectAllByDefault=false and empty watchNS",
			args: args{
				p: &vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "default-agent",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAgentSpec{
						ServiceScrapeNamespaceSelector: nil,
						ServiceScrapeSelector:          nil,
						SelectAllByDefault:             false,
					},
				},
				l: logf.Log.WithName("unit-test"),
			},
			predefinedObjects: predefinedObjects,
			want:              []string{},
			wantErr:           false,
		},
		{
			name: "If NamespaceSelector and Selector both undefined, selectAllByDefault=true and empty watchNS",
			args: args{
				p: &vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "default-agent",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAgentSpec{
						ServiceScrapeNamespaceSelector: nil,
						ServiceScrapeSelector:          nil,
						SelectAllByDefault:             true,
					},
				},
				l: logf.Log.WithName("unit-test"),
			},
			predefinedObjects: predefinedObjects,
			want:              []string{"default/ss1", "default/ss2", "other1/ss1", "other1/ss2", "other2/ss1", "other2/ss2"},
			wantErr:           false,
		},
		{
			name: "If NamespaceSelector defined, Selector undefined, selectAllByDefault=false and empty watchNS",
			args: args{
				p: &vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "default-agent",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAgentSpec{
						ServiceScrapeNamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"name": "other1"}},
						ServiceScrapeSelector:          nil,
						SelectAllByDefault:             false,
					},
				},
				l: logf.Log.WithName("unit-test"),
			},
			predefinedObjects: predefinedObjects,
			want:              []string{"other1/ss1", "other1/ss2"},
			wantErr:           false,
		},
		{
			name: "If NamespaceSelector defined, Selector undefined, selectAllByDefault=true and empty watchNS",
			args: args{
				p: &vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "default-agent",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAgentSpec{
						ServiceScrapeNamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"name": "other1"}},
						ServiceScrapeSelector:          nil,
						SelectAllByDefault:             true,
					},
				},
				l: logf.Log.WithName("unit-test"),
			},
			predefinedObjects: predefinedObjects,
			want:              []string{"other1/ss1", "other1/ss2"},
			wantErr:           false,
		},
		{
			name: "If NamespaceSelector undefined, Selector defined, selectAllByDefault=false and empty watchNS",
			args: args{
				p: &vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "default-agent",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAgentSpec{
						ServiceScrapeNamespaceSelector: nil,
						ServiceScrapeSelector:          &metav1.LabelSelector{MatchLabels: map[string]string{"name": "ss1"}},
						SelectAllByDefault:             false,
					},
				},
				l: logf.Log.WithName("unit-test"),
			},
			predefinedObjects: predefinedObjects,
			want:              []string{"default/ss1"},
			wantErr:           false,
		},
		{
			name: "If NamespaceSelector undefined, Selector defined, selectAllByDefault=true and empty watchNS",
			args: args{
				p: &vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "default-agent",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAgentSpec{
						ServiceScrapeNamespaceSelector: nil,
						ServiceScrapeSelector:          &metav1.LabelSelector{MatchLabels: map[string]string{"name": "ss1"}},
						SelectAllByDefault:             true,
					},
				},
				l: logf.Log.WithName("unit-test"),
			},
			predefinedObjects: predefinedObjects,
			want:              []string{"default/ss1"},
			wantErr:           false,
		},
		{
			name: "If NamespaceSelector and Selector both defined, selectAllByDefault=false and empty watchNS",
			args: args{
				p: &vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "default-agent",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAgentSpec{
						ServiceScrapeNamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"name": "other1"}},
						ServiceScrapeSelector:          &metav1.LabelSelector{MatchLabels: map[string]string{"name": "ss1"}},
						SelectAllByDefault:             false,
					},
				},
				l: logf.Log.WithName("unit-test"),
			},
			predefinedObjects: predefinedObjects,
			want:              []string{"other1/ss1"},
			wantErr:           false,
		},
		{
			name: "If NamespaceSelector and Selector both defined, selectAllByDefault=true and empty watchNS",
			args: args{
				p: &vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "default-agent",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAgentSpec{
						ServiceScrapeNamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"name": "other2"}},
						ServiceScrapeSelector:          &metav1.LabelSelector{MatchLabels: map[string]string{"name": "ss2"}},
						SelectAllByDefault:             true,
					},
				},
				l: logf.Log.WithName("unit-test"),
			},
			predefinedObjects: predefinedObjects,
			want:              []string{"other2/ss2"},
			wantErr:           false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			got, err := selectServiceScrapes(context.TODO(), tt.args.p, fclient)
			if (err != nil) != tt.wantErr {
				t.Errorf("SelectServiceScrapes() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			gotNames := []string{}
			for monitorName := range got {
				gotNames = append(gotNames, monitorName)
			}
			sort.Strings(gotNames)
			if !reflect.DeepEqual(gotNames, tt.want) {
				t.Errorf("SelectServiceScrapes() got = %v, want %v", gotNames, tt.want)
			}
		})
	}
}

func TestSelectPodMonitors(t *testing.T) {
	type args struct {
		p *vmv1beta1.VMAgent
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
			name: "selector pod scrape at vmagent ns",
			args: args{
				p: &vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-vmagent",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAgentSpec{
						PodScrapeSelector: &metav1.LabelSelector{},
					},
				},
				l: logf.Log.WithName("unit-test"),
			},
			predefinedObjects: []runtime.Object{
				&vmv1beta1.VMPodScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMPodScrapeSpec{},
				},
			},
			wantErr: false,
			want:    []string{"default/pod1"},
		},
		{
			name: "selector pod scrape at different ns with ns selector",
			args: args{
				p: &vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-vmagent",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAgentSpec{
						PodScrapeNamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"name": "monitoring"},
						},
					},
				},
				l: logf.Log.WithName("unit-test"),
			},
			predefinedObjects: []runtime.Object{
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "monitor", Labels: map[string]string{"name": "monitoring"}}},
				&vmv1beta1.VMPodScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod2",
						Namespace: "monitor",
					},
					Spec: vmv1beta1.VMPodScrapeSpec{},
				},
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
				&vmv1beta1.VMPodScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMPodScrapeSpec{},
				},
			},
			wantErr: false,
			want:    []string{"monitor/pod2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			got, err := selectPodScrapes(context.TODO(), tt.args.p, fclient)
			if (err != nil) != tt.wantErr {
				t.Errorf("SelectPodScrapes() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			gotNames := []string{}
			for podName := range got {
				gotNames = append(gotNames, podName)
			}
			sort.Strings(gotNames)
			if !reflect.DeepEqual(gotNames, tt.want) {
				t.Errorf("SelectPodScrapes() got = %v, want %v", gotNames, tt.want)
			}
		})
	}
}

func TestSelectVMProbes(t *testing.T) {
	type args struct {
		cr *vmv1beta1.VMAgent
	}
	tests := []struct {
		name              string
		args              args
		want              []string
		wantErr           bool
		predefinedObjects []runtime.Object
	}{
		{
			name: "select vmProbe with static conf",
			args: args{
				cr: &vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-vmagent",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAgentSpec{
						ProbeSelector: &metav1.LabelSelector{},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&vmv1beta1.VMProbe{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "static-probe",
					},
					Spec: vmv1beta1.VMProbeSpec{Targets: vmv1beta1.VMProbeTargets{StaticConfig: &vmv1beta1.VMProbeTargetStaticConfig{Targets: []string{"host-1"}}}},
				},
			},
			want: []string{"default/static-probe"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			got, err := selectVMProbes(context.TODO(), tt.args.cr, fclient)
			if (err != nil) != tt.wantErr {
				t.Errorf("SelectVMProbes() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			var result []string
			for k := range got {
				result = append(result, k)
			}
			sort.Strings(result)
			if !reflect.DeepEqual(result, tt.want) {
				t.Errorf("SelectVMProbes() got = %v, want %v", got, tt.want)
			}
		})
	}
}
