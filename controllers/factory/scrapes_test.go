package factory

import (
	"context"
	"reflect"
	"sort"
	"testing"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func TestSelectServiceMonitors(t *testing.T) {
	type args struct {
		p *victoriametricsv1beta1.VMAgent
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
			name: "select service scrape inside vmagent namespace",
			args: args{
				p: &victoriametricsv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "default-agent",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMAgentSpec{
						ServiceScrapeSelector: &metav1.LabelSelector{},
					},
				},
				l: logf.Log.WithName("unit-test"),
			},
			predefinedObjects: []runtime.Object{
				&victoriametricsv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "default-monitor"},
					Spec:       victoriametricsv1beta1.VMServiceScrapeSpec{},
				},
			},
			want:    []string{"default/default-monitor"},
			wantErr: false,
		},
		{
			name: "select service scrape from namespace with filter",
			args: args{
				p: &victoriametricsv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "default-agent",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMAgentSpec{
						ServiceScrapeNamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"name": "stage"}},
						ServiceScrapeSelector:          &metav1.LabelSelector{},
					},
				},
				l: logf.Log.WithName("unit-test"),
			},
			predefinedObjects: []runtime.Object{
				&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
				&victoriametricsv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "default-monitor"},
					Spec:       victoriametricsv1beta1.VMServiceScrapeSpec{},
				},
				&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "stg", Labels: map[string]string{"name": "stage"}}},
				&victoriametricsv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{Namespace: "stg", Name: "default-monitor"},
					Spec:       victoriametricsv1beta1.VMServiceScrapeSpec{},
				},
			},
			want:    []string{"stg/default-monitor"},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			got, err := SelectServiceScrapes(context.TODO(), tt.args.p, fclient)
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
		p *victoriametricsv1beta1.VMAgent
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
				p: &victoriametricsv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-vmagent",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMAgentSpec{
						PodScrapeSelector: &metav1.LabelSelector{},
					},
				},
				l: logf.Log.WithName("unit-test"),
			},
			predefinedObjects: []runtime.Object{
				&victoriametricsv1beta1.VMPodScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMPodScrapeSpec{},
				},
			},
			wantErr: false,
			want:    []string{"default/pod1"},
		},
		{
			name: "selector pod scrape at different ns with ns selector",
			args: args{
				p: &victoriametricsv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-vmagent",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMAgentSpec{
						PodScrapeNamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"name": "monitoring"},
						},
					},
				},
				l: logf.Log.WithName("unit-test"),
			},
			predefinedObjects: []runtime.Object{
				&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "monitor", Labels: map[string]string{"name": "monitoring"}}},
				&victoriametricsv1beta1.VMPodScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod2",
						Namespace: "monitor",
					},
					Spec: victoriametricsv1beta1.VMPodScrapeSpec{},
				},
				&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
				&victoriametricsv1beta1.VMPodScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMPodScrapeSpec{},
				},
			},
			wantErr: false,
			want:    []string{"monitor/pod2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			got, err := SelectPodScrapes(context.TODO(), tt.args.p, fclient)
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

func Test_getCredFromConfigMap(t *testing.T) {
	type args struct {
		ns       string
		sel      v1.ConfigMapKeySelector
		cacheKey string
		cache    map[string]*v1.ConfigMap
	}
	tests := []struct {
		name              string
		args              args
		want              string
		wantErr           bool
		predefinedObjects []runtime.Object
	}{
		{
			name: "extract key from cm",
			args: args{
				ns:    "default",
				sel:   v1.ConfigMapKeySelector{Key: "tls-conf", LocalObjectReference: v1.LocalObjectReference{Name: "tls-cm"}},
				cache: map[string]*v1.ConfigMap{},
			},
			predefinedObjects: []runtime.Object{
				&v1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: "tls-cm", Namespace: "default"},
					Data:       map[string]string{"tls-conf": "secret-data"},
				},
			},
			want:    "secret-data",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)

			got, err := getCredFromConfigMap(context.TODO(), fclient, tt.args.ns, tt.args.sel, tt.args.cacheKey, tt.args.cache)
			if (err != nil) != tt.wantErr {
				t.Errorf("getCredFromConfigMap() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("getCredFromConfigMap() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_getCredFromSecret(t *testing.T) {
	type args struct {
		ns       string
		sel      v1.SecretKeySelector
		cacheKey string
		cache    map[string]*v1.Secret
	}
	tests := []struct {
		name              string
		args              args
		want              string
		wantErr           bool
		predefinedObjects []runtime.Object
	}{
		{
			name: "extract tls key data from secret",
			args: args{
				ns: "default",
				sel: v1.SecretKeySelector{LocalObjectReference: v1.LocalObjectReference{
					Name: "tls-secret"},
					Key: "key.pem"},
				cacheKey: "tls-secret",
				cache:    map[string]*v1.Secret{},
			},
			want:    "tls-key-data",
			wantErr: false,
			predefinedObjects: []runtime.Object{
				&v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tls-secret",
						Namespace: "default",
					},
					Data: map[string][]byte{"ca.crt": []byte(`ca-data`), "key.pem": []byte(`tls-key-data`)},
				},
			},
		},
		{
			name: "fail extract missing tls cert data from secret",
			args: args{
				ns: "default",
				sel: v1.SecretKeySelector{LocalObjectReference: v1.LocalObjectReference{
					Name: "tls-secret"},
					Key: "cert.pem"},
				cacheKey: "tls-secret",
				cache:    map[string]*v1.Secret{},
			},
			want:    "",
			wantErr: true,
			predefinedObjects: []runtime.Object{
				&v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tls-secret",
						Namespace: "default",
					},
					Data: map[string][]byte{"ca.crt": []byte(`ca-data`), "key.pem": []byte(`tls-key-data`)},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)

			got, err := getCredFromSecret(context.TODO(), fclient, tt.args.ns, tt.args.sel, tt.args.cacheKey, tt.args.cache)
			if (err != nil) != tt.wantErr {
				t.Errorf("getCredFromSecret() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("getCredFromSecret() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSelectVMProbes(t *testing.T) {
	type args struct {
		cr *victoriametricsv1beta1.VMAgent
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
				cr: &victoriametricsv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-vmagent",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMAgentSpec{
						ProbeSelector: &metav1.LabelSelector{},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&victoriametricsv1beta1.VMProbe{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "static-probe",
					},
					Spec: victoriametricsv1beta1.VMProbeSpec{Targets: victoriametricsv1beta1.VMProbeTargets{StaticConfig: &victoriametricsv1beta1.VMProbeTargetStaticConfig{Targets: []string{"host-1"}}}},
				},
			},
			want: []string{"default/static-probe"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			got, err := SelectVMProbes(context.TODO(), tt.args.cr, fclient)
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
