package vmagent

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestSelectServiceMonitors(t *testing.T) {
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
	type opts struct {
		cr                *vmv1beta1.VMAgent
		want              []string
		predefinedObjects []runtime.Object
	}

	f := func(o opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		sp := &o.cr.Spec.CommonScrapeParams
		pos := &parsedObjects{Namespace: o.cr.Namespace}
		assert.NoError(t, pos.selectServiceScrapes(context.TODO(), fclient, sp))
		gotNames := []string{}
		for _, monitorName := range pos.serviceScrapes.All() {
			gotNames = append(gotNames, fmt.Sprintf("%s/%s", monitorName.Namespace, monitorName.Name))
		}
		sort.Strings(gotNames)
		if !reflect.DeepEqual(gotNames, o.want) {
			t.Errorf("SelectServiceScrapes(): %s", cmp.Diff(gotNames, o.want))
		}
	}

	// select service scrape inside vmagent namespace
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-agent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					ServiceScrapeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{},
					},
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&vmv1beta1.VMServiceScrape{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "default-monitor",
				},
				Spec: vmv1beta1.VMServiceScrapeSpec{},
			},
		},
		want: []string{"default/default-monitor"},
	})

	// select service scrape from namespace with filter
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-agent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					ServiceScrapeNamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"name": "stage"}},
					ServiceScrapeSelector:          &metav1.LabelSelector{},
				},
			},
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
		want: []string{"stg/default-monitor"},
	})

	// if NamespaceSelector and Selector both undefined, selectAllByDefault=false and empty watchNS
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-agent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					ServiceScrapeNamespaceSelector: nil,
					ServiceScrapeSelector:          nil,
					SelectAllByDefault:             false,
				},
			},
		},
		predefinedObjects: predefinedObjects,
		want:              []string{},
	})

	// if NamespaceSelector and Selector both undefined, selectAllByDefault=true and empty watchNS
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-agent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					ServiceScrapeNamespaceSelector: nil,
					ServiceScrapeSelector:          nil,
					SelectAllByDefault:             true,
				},
			},
		},
		predefinedObjects: predefinedObjects,
		want:              []string{"default/ss1", "default/ss2", "other1/ss1", "other1/ss2", "other2/ss1", "other2/ss2"},
	})

	// If NamespaceSelector defined, Selector undefined, selectAllByDefault=false and empty watchNS
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-agent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					ServiceScrapeNamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"name": "other1"}},
					ServiceScrapeSelector:          nil,
					SelectAllByDefault:             false,
				},
			},
		},
		predefinedObjects: predefinedObjects,
		want:              []string{"other1/ss1", "other1/ss2"},
	})

	// if NamespaceSelector defined, Selector undefined, selectAllByDefault=true and empty watchNS
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-agent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					ServiceScrapeNamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"name": "other1"}},
					ServiceScrapeSelector:          nil,
					SelectAllByDefault:             true,
				},
			},
		},
		predefinedObjects: predefinedObjects,
		want:              []string{"other1/ss1", "other1/ss2"},
	})

	// if NamespaceSelector undefined, Selector defined, selectAllByDefault=false and empty watchNS
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-agent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					ServiceScrapeNamespaceSelector: nil,
					ServiceScrapeSelector:          &metav1.LabelSelector{MatchLabels: map[string]string{"name": "ss1"}},
					SelectAllByDefault:             false,
				},
			},
		},
		predefinedObjects: predefinedObjects,
		want:              []string{"default/ss1"},
	})

	// if NamespaceSelector undefined, Selector defined, selectAllByDefault=true and empty watchNS
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-agent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					ServiceScrapeNamespaceSelector: nil,
					ServiceScrapeSelector:          &metav1.LabelSelector{MatchLabels: map[string]string{"name": "ss1"}},
					SelectAllByDefault:             true,
				},
			},
		},
		predefinedObjects: predefinedObjects,
		want:              []string{"default/ss1"},
	})

	// if NamespaceSelector and Selector both defined, selectAllByDefault=false and empty watchNS
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-agent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					ServiceScrapeNamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"name": "other1"}},
					ServiceScrapeSelector:          &metav1.LabelSelector{MatchLabels: map[string]string{"name": "ss1"}},
					SelectAllByDefault:             false,
				},
			},
		},
		predefinedObjects: predefinedObjects,
		want:              []string{"other1/ss1"},
	})

	// if NamespaceSelector and Selector both defined, selectAllByDefault=true and empty watchNS
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-agent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					ServiceScrapeNamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"name": "other2"}},
					ServiceScrapeSelector:          &metav1.LabelSelector{MatchLabels: map[string]string{"name": "ss2"}},
					SelectAllByDefault:             true,
				},
			},
		},
		predefinedObjects: predefinedObjects,
		want:              []string{"other2/ss2"},
	})
}

func TestSelectPodMonitors(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMAgent
		want              []string
		predefinedObjects []runtime.Object
	}
	f := func(o opts) {
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		sp := &o.cr.Spec.CommonScrapeParams
		pos := &parsedObjects{Namespace: o.cr.Namespace}
		assert.NoError(t, pos.selectPodScrapes(context.TODO(), fclient, sp))
		var gotNames []string

		for _, k := range pos.podScrapes.All() {
			gotNames = append(gotNames, fmt.Sprintf("%s/%s", k.Namespace, k.Name))
		}
		sort.Strings(gotNames)
		if !reflect.DeepEqual(gotNames, o.want) {
			t.Errorf("SelectPodScrapes(): %s", cmp.Diff(gotNames, o.want))
		}
	}

	// selector pod scrape at vmagent ns
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "example-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					PodScrapeSelector: &metav1.LabelSelector{},
				},
			},
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
		want: []string{"default/pod1"},
	})

	// selector pod scrape at different ns with ns selector
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "example-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					PodScrapeNamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"name": "monitoring"},
					},
				},
			},
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
		want: []string{"monitor/pod2"},
	})
}

func TestSelectProbes(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMAgent
		want              []string
		predefinedObjects []runtime.Object
	}

	f := func(o opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		sp := &o.cr.Spec.CommonScrapeParams
		pos := &parsedObjects{Namespace: o.cr.Namespace}
		assert.NoError(t, pos.selectProbes(context.TODO(), fclient, sp))
		var result []string
		for _, k := range pos.probes.All() {
			result = append(result, fmt.Sprintf("%s/%s", k.Namespace, k.Name))
		}
		sort.Strings(result)
		if !reflect.DeepEqual(result, o.want) {
			t.Errorf("SelectProbes(): %s", cmp.Diff(result, o.want))
		}
	}

	// select vmProbe with static conf
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "example-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
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
	})
}
