package factory

import (
	"context"
	"reflect"
	"testing"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestCreateOrUpdateVMAuthService(t *testing.T) {
	type args struct {
		ctx     context.Context
		cr      *victoriametricsv1beta1.VMAuth
		rclient client.Client
	}
	tests := []struct {
		name    string
		args    args
		want    *corev1.Service
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CreateOrUpdateVMAuthService(tt.args.ctx, tt.args.cr, tt.args.rclient)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateOrUpdateVMAuthService() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("CreateOrUpdateVMAuthService() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCreateOrUpdateVMAuth(t *testing.T) {
	type args struct {
		ctx     context.Context
		cr      *victoriametricsv1beta1.VMAuth
		rclient client.Client
		c       *config.BaseOperatorConf
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := CreateOrUpdateVMAuth(tt.args.ctx, tt.args.cr, tt.args.rclient, tt.args.c); (err != nil) != tt.wantErr {
				t.Errorf("CreateOrUpdateVMAuth() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
