package factory

import (
	"github.com/VictoriaMetrics/operator/conf"
	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/pkg/apis/victoriametrics/v1beta1"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
