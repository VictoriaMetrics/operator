package vmagent

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestSelectServiceMonitors(t *testing.T) {
	defaultPredefinedObjects := []runtime.Object{
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

	type opts struct {
		cr                *vmv1beta1.VMAgent
		want              []string
		predefinedObjects []runtime.Object
	}

	f := func(opts opts) {
		if len(opts.predefinedObjects) == 0 {
			opts.predefinedObjects = defaultPredefinedObjects
		}
		fclient := k8stools.GetTestClientWithObjects(opts.predefinedObjects)
		got, err := selectServiceScrapes(context.TODO(), opts.cr, fclient)
		if err != nil {
			t.Errorf("SelectServiceScrapes() error = %v", err)
			return
		}
		gotNames := []string{}
		for _, monitorName := range got {
			gotNames = append(gotNames, fmt.Sprintf("%s/%s", monitorName.Namespace, monitorName.Name))
		}
		sort.Strings(gotNames)
		if !slices.Equal(gotNames, opts.want) {
			t.Errorf("SelectServiceScrapes() got = %v, want %v", gotNames, opts.want)
		}
	}

	// select service scrape inside vmagent namespace
	o := opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-agent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				ServiceScrapeSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{},
				},
			},
		},
		want: []string{"default/default-monitor"},
		predefinedObjects: []runtime.Object{
			&vmv1beta1.VMServiceScrape{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "default-monitor",
				},
				Spec: vmv1beta1.VMServiceScrapeSpec{},
			},
		},
	}
	f(o)

	// select service scrape from namespace with filter
	o = opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-agent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				ServiceScrapeNamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"name": "stage"}},
				ServiceScrapeSelector:          &metav1.LabelSelector{},
			},
		},
		want: []string{"stg/default-monitor"},
		predefinedObjects: []runtime.Object{
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: "default"},
			},
			&vmv1beta1.VMServiceScrape{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "default-monitor"},
				Spec:       vmv1beta1.VMServiceScrapeSpec{},
			},
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: "stg", Labels: map[string]string{"name": "stage"}},
			},
			&vmv1beta1.VMServiceScrape{
				ObjectMeta: metav1.ObjectMeta{Namespace: "stg", Name: "default-monitor"},
				Spec:       vmv1beta1.VMServiceScrapeSpec{},
			},
		},
	}
	f(o)

	// if NamespaceSelector and Selector both undefined, selectAllByDefault=false and empty watchNS
	o = opts{
		cr: &vmv1beta1.VMAgent{
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
	}
	f(o)

	// if NamespaceSelector and Selector both undefined, selectAllByDefault=true and empty watchNS
	o = opts{
		cr: &vmv1beta1.VMAgent{
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
		want: []string{"default/ss1", "default/ss2", "other1/ss1", "other1/ss2", "other2/ss1", "other2/ss2"},
	}
	f(o)

	// if NamespaceSelector defined, Selector undefined, selectAllByDefault=false and empty watchNS
	o = opts{
		cr: &vmv1beta1.VMAgent{
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
		want: []string{"other1/ss1", "other1/ss2"},
	}
	f(o)

	// if NamespaceSelector defined, Selector undefined, selectAllByDefault=true and empty watchNS
	o = opts{
		cr: &vmv1beta1.VMAgent{
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
		want: []string{"other1/ss1", "other1/ss2"},
	}
	f(o)

	// if NamespaceSelector undefined, Selector defined, selectAllByDefault=false and empty watchNS
	o = opts{
		cr: &vmv1beta1.VMAgent{
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
		want: []string{"default/ss1"},
	}
	f(o)

	// if NamespaceSelector undefined, Selector defined, selectAllByDefault=true and empty watchNS
	o = opts{
		cr: &vmv1beta1.VMAgent{
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
		want: []string{"default/ss1"},
	}
	f(o)

	// if NamespaceSelector and Selector both defined, selectAllByDefault=false and empty watchNS
	o = opts{
		cr: &vmv1beta1.VMAgent{
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
		want: []string{"other1/ss1"},
	}
	f(o)

	// if NamespaceSelector and Selector both defined, selectAllByDefault=true and empty watchNS
	o = opts{
		cr: &vmv1beta1.VMAgent{
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
		want: []string{"other2/ss2"},
	}
	f(o)
}

func TestSelectPodMonitors(t *testing.T) {
	f := func(cr *vmv1beta1.VMAgent, want []string, predefinedObjects []runtime.Object) {
		fclient := k8stools.GetTestClientWithObjects(predefinedObjects)
		got, err := selectPodScrapes(context.TODO(), cr, fclient)
		if err != nil {
			t.Errorf("SelectPodScrapes() error = %v", err)
			return
		}
		var gotNames []string

		for _, k := range got {
			gotNames = append(gotNames, fmt.Sprintf("%s/%s", k.Namespace, k.Name))
		}
		sort.Strings(gotNames)
		if !reflect.DeepEqual(gotNames, want) {
			t.Errorf("SelectPodScrapes() got = %v, want %v", gotNames, want)
		}
	}

	// selector pod scrape at vmagent ns
	f(&vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example-vmagent",
			Namespace: "default",
		},
		Spec: vmv1beta1.VMAgentSpec{
			PodScrapeSelector: &metav1.LabelSelector{},
		},
	}, []string{"default/pod1"}, []runtime.Object{
		&vmv1beta1.VMPodScrape{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod1",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMPodScrapeSpec{},
		},
	})

	// selector pod scrape at different ns with ns selector
	f(&vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example-vmagent",
			Namespace: "default",
		},
		Spec: vmv1beta1.VMAgentSpec{
			PodScrapeNamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"name": "monitoring"},
			},
		},
	}, []string{"monitor/pod2"}, []runtime.Object{
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "monitor", Labels: map[string]string{"name": "monitoring"}},
		},
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
	})
}

func TestSelectVMProbes(t *testing.T) {
	f := func(cr *vmv1beta1.VMAgent, want []string, predefinedObjects []runtime.Object) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(predefinedObjects)
		got, err := selectVMProbes(context.TODO(), cr, fclient)
		if err != nil {
			t.Errorf("SelectVMProbes() error = %v", err)
			return
		}
		var result []string
		for _, k := range got {
			result = append(result, fmt.Sprintf("%s/%s", k.Namespace, k.Name))
		}
		sort.Strings(result)
		if !reflect.DeepEqual(result, want) {
			t.Errorf("SelectVMProbes() got = %v, want %v", got, want)
		}
	}

	// select vmProbe with static conf
	f(&vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example-vmagent",
			Namespace: "default",
		},
		Spec: vmv1beta1.VMAgentSpec{
			ProbeSelector: &metav1.LabelSelector{},
		},
	}, []string{"default/static-probe"}, []runtime.Object{
		&vmv1beta1.VMProbe{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "static-probe",
			},
			Spec: vmv1beta1.VMProbeSpec{Targets: vmv1beta1.VMProbeTargets{StaticConfig: &vmv1beta1.VMProbeTargetStaticConfig{Targets: []string{"host-1"}}}},
		},
	})
}
