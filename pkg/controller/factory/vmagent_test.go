package factory

import (
	"context"
	"github.com/VictoriaMetrics/operator/conf"
	monitoringv1 "github.com/VictoriaMetrics/operator/pkg/apis/monitoring/v1"
	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/pkg/apis/victoriametrics/v1beta1"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"testing"
)

func Test_makeSpecForVmAgent(t *testing.T) {
	type args struct {
		cr *victoriametricsv1beta1.VmAgent
		c  *conf.BaseOperatorConf
	}
	tests := []struct {
		name    string
		args    args
		want    *corev1.PodTemplateSpec
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := makeSpecForVmAgent(tt.args.cr, tt.args.c)
			if (err != nil) != tt.wantErr {
				t.Errorf("makeSpecForVmAgent() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("makeSpecForVmAgent() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_newDeployForVmAgent(t *testing.T) {
	type args struct {
		cr *victoriametricsv1beta1.VmAgent
		c  *conf.BaseOperatorConf
	}
	tests := []struct {
		name    string
		args    args
		want    *appsv1.Deployment
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := newDeployForVmAgent(tt.args.cr, tt.args.c)
			if (err != nil) != tt.wantErr {
				t.Errorf("newDeployForVmAgent() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("newDeployForVmAgent() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCreateOrUpdateVmAgent(t *testing.T) {
	type args struct {
		cr      *victoriametricsv1beta1.VmAgent
		rclient client.Client
		c       *conf.BaseOperatorConf
		l       logr.Logger
	}
	tests := []struct {
		name    string
		args    args
		want    reconcile.Result
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CreateOrUpdateVmAgent(tt.args.cr, tt.args.rclient, tt.args.c, tt.args.l)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateOrUpdateVmAgent() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("CreateOrUpdateVmAgent() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_addAddtionalScrapeConfigOwnership(t *testing.T) {
	type args struct {
		cr      *victoriametricsv1beta1.VmAgent
		l       logr.Logger
	}
	tests := []struct {
		name              string
		args              args
		wantErr           bool
		predefinedObjects *corev1.SecretList
	}{
		{
			name: "append ownership to secret",
			args: args{
				cr: &victoriametricsv1beta1.VmAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmagent-1",
						Namespace: "ns-1",
					},
					Spec: victoriametricsv1beta1.VmAgentSpec{
						AdditionalScrapeConfigs: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "secret-1"}}},
				},
				l: logf.Log.WithName("test"),
			},
			predefinedObjects: &corev1.SecretList{
				Items: []corev1.Secret{
					corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "secret-1", Namespace: "ns-1"}},
					corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "secret-2", Namespace: "ns-2"}},
				},
			},
		},
		{
			name: "empty scrape config - nothing todo",
			args: args{
				cr: &victoriametricsv1beta1.VmAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmagent-1",
						Namespace: "ns-1",
					},
					Spec: victoriametricsv1beta1.VmAgentSpec{},
				},
				l: logf.Log.WithName("test"),
			},
			predefinedObjects: &corev1.SecretList{
				Items: []corev1.Secret{
					corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "secret-1", Namespace: "ns-1"}},
					corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "secret-2", Namespace: "ns-2"}},
				},
			},
		},
		{
			name: "ownership exists - nothing todo",
			args: args{
				cr: &victoriametricsv1beta1.VmAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmagent-2",
						Namespace: "ns-2",
					},
					Spec: victoriametricsv1beta1.VmAgentSpec{
						AdditionalScrapeConfigs: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "secret-2"}}},
				},
				l: logf.Log.WithName("test"),
			},
			predefinedObjects: &corev1.SecretList{
				Items: []corev1.Secret{
					corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "secret-1", Namespace: "ns-1"}},
					corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "secret-2", Namespace: "ns-2",OwnerReferences:[]metav1.OwnerReference{
						metav1.OwnerReference{Name:"vmagent-2"},
					}}},
				},
			},
		},

	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			obj := []runtime.Object{}
			for _, secret := range tt.predefinedObjects.Items {
				localSecret := secret
				obj = append(obj, &localSecret)
			}
			s := scheme.Scheme
			s.AddKnownTypes(victoriametricsv1beta1.SchemeGroupVersion, &victoriametricsv1beta1.VmAgent{}, &victoriametricsv1beta1.VmAgentList{})
			s.AddKnownTypes(monitoringv1.SchemeGroupVersion, &monitoringv1.PodMonitor{}, &monitoringv1.PodMonitorList{})
			fclient := fake.NewFakeClientWithScheme(s, obj...)

			if err := addAddtionalScrapeConfigOwnership(tt.args.cr, fclient, tt.args.l); (err != nil) != tt.wantErr {
				t.Errorf("addAddtionalScrapeConfigOwnership() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.args.cr.Spec.AdditionalScrapeConfigs != nil {
				secret := &corev1.Secret{}
				var refFound bool
				err := fclient.Get(context.TODO(), types.NamespacedName{Namespace: tt.args.cr.Namespace, Name: tt.args.cr.Spec.AdditionalScrapeConfigs.Name}, secret)
				if err != nil {
					t.Errorf("cannot find secret for scrape config")
				}
				for _, ownerRef := range secret.OwnerReferences {
					if ownerRef.Name == tt.args.cr.Name {
						refFound = true
					}
				}
				if ! refFound {
					t.Errorf("cannot find secret ownership for vmagent: %s,secret name: %v", tt.args.cr.Name, tt.args.cr.Spec.AdditionalScrapeConfigs.Name)
				}
			}

		})
	}
}
