package factory

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"testing"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	"github.com/go-test/deep"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

func Test_reconcileServiceForCRD(t *testing.T) {
	type args struct {
		ctx        context.Context
		newService *v1.Service
	}
	tests := []struct {
		name              string
		args              args
		predefinedObjects []runtime.Object
		validate          func(svc *v1.Service) error
		wantErr           bool
	}{
		{
			name: "create new svc",
			args: args{
				newService: &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prefixed-1",
						Namespace: "default",
					},
					Spec: v1.ServiceSpec{
						Type: v1.ServiceTypeNodePort,
					},
				},
				ctx: context.TODO(),
			},
			validate: func(svc *v1.Service) error {
				if svc.Name != "prefixed-1" {
					return fmt.Errorf("unexpected name, got: %v, want: prefixed-1", svc.Name)
				}
				return nil
			},
		},
		{
			name: "update svc from headless to clusterIP",
			args: args{
				newService: &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prefixed-1",
						Namespace: "default",
					},
					Spec: v1.ServiceSpec{
						Type: v1.ServiceTypeClusterIP,
					},
				},
				ctx: context.TODO(),
			},
			predefinedObjects: []runtime.Object{
				&v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prefixed-1",
						Namespace: "default",
					},
					Spec: v1.ServiceSpec{
						Type:      v1.ServiceTypeClusterIP,
						ClusterIP: "None",
					},
				},
			},
			validate: func(svc *v1.Service) error {
				if svc.Name != "prefixed-1" {
					return fmt.Errorf("unexpected name, got: %v, want: prefixed-1", svc.Name)
				}
				if svc.Spec.ClusterIP == "None" {
					return fmt.Errorf("unexpected value for clusterIP, want ip, got: %v", svc.Spec.ClusterIP)
				}
				return nil
			},
		},
		{
			name: "update svc from clusterIP to headless",
			args: args{
				newService: &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prefixed-1",
						Namespace: "default",
					},
					Spec: v1.ServiceSpec{
						Type:      v1.ServiceTypeClusterIP,
						ClusterIP: "None",
					},
				},
				ctx: context.TODO(),
			},
			predefinedObjects: []runtime.Object{
				&v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prefixed-1",
						Namespace: "default",
					},
					Spec: v1.ServiceSpec{
						Type:      v1.ServiceTypeClusterIP,
						ClusterIP: "192.168.1.5",
					},
				},
			},
			validate: func(svc *v1.Service) error {
				if svc.Name != "prefixed-1" {
					return fmt.Errorf("unexpected name, got: %v, want: prefixed-1", svc.Name)
				}
				if svc.Spec.ClusterIP != "None" {
					return fmt.Errorf("unexpected value for clusterIP, want ip, got: %v", svc.Spec.ClusterIP)
				}
				return nil
			},
		},
		{
			name: "update svc clusterIP value",
			args: args{
				newService: &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prefixed-1",
						Namespace: "default",
					},
					Spec: v1.ServiceSpec{
						Type:      v1.ServiceTypeClusterIP,
						ClusterIP: "192.168.1.5",
					},
				},
				ctx: context.TODO(),
			},
			predefinedObjects: []runtime.Object{
				&v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prefixed-1",
						Namespace: "default",
					},
					Spec: v1.ServiceSpec{
						Type:      v1.ServiceTypeClusterIP,
						ClusterIP: "192.168.1.4",
					},
				},
			},
			validate: func(svc *v1.Service) error {
				if svc.Name != "prefixed-1" {
					return fmt.Errorf("unexpected name, got: %v, want: prefixed-1", svc.Name)
				}
				if svc.Spec.ClusterIP != "192.168.1.5" {
					return fmt.Errorf("unexpected value for clusterIP, want ip, got: %v", svc.Spec.ClusterIP)
				}
				return nil
			},
		},
		{
			name: "update svc from nodePort to clusterIP with value",
			args: args{
				newService: &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prefixed-1",
						Namespace: "default",
					},
					Spec: v1.ServiceSpec{
						Type:      v1.ServiceTypeClusterIP,
						ClusterIP: "192.168.1.5",
					},
				},
				ctx: context.TODO(),
			},
			predefinedObjects: []runtime.Object{
				&v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prefixed-1",
						Namespace: "default",
					},
					Spec: v1.ServiceSpec{
						Type:      v1.ServiceTypeNodePort,
						ClusterIP: "192.168.1.1",
					},
				},
			},
			validate: func(svc *v1.Service) error {
				if svc.Name != "prefixed-1" {
					return fmt.Errorf("unexpected name, got: %v, want: prefixed-1", svc.Name)
				}
				if svc.Spec.Type != v1.ServiceTypeClusterIP {
					return fmt.Errorf("unexpected type: %v", svc.Spec.Type)
				}
				if svc.Spec.ClusterIP != "192.168.1.5" {
					return fmt.Errorf("unexpected value for clusterIP, want ip, got: %v", svc.Spec.ClusterIP)
				}
				return nil
			},
		},
		{
			name: "keep node port",
			args: args{
				newService: &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prefixed-1",
						Namespace: "default",
					},
					Spec: v1.ServiceSpec{
						Type:      v1.ServiceTypeNodePort,
						ClusterIP: "192.168.1.5",
						Ports: []v1.ServicePort{
							{
								Name:     "web",
								Protocol: "TCP",
							},
						},
					},
				},
				ctx: context.TODO(),
			},
			predefinedObjects: []runtime.Object{
				&v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prefixed-1",
						Namespace: "default",
					},
					Spec: v1.ServiceSpec{
						Type:      v1.ServiceTypeNodePort,
						ClusterIP: "192.168.1.5",
						Ports: []v1.ServicePort{
							{
								Name:     "web",
								Protocol: "TCP",
								NodePort: 331,
							},
						},
					},
				},
			},
			validate: func(svc *v1.Service) error {
				if svc.Name != "prefixed-1" {
					return fmt.Errorf("unexpected name, got: %v, want: prefixed-1", svc.Name)
				}
				if svc.Spec.Type != v1.ServiceTypeNodePort {
					return fmt.Errorf("unexpected type: %v", svc.Spec.Type)
				}
				if svc.Spec.ClusterIP != "192.168.1.5" {
					return fmt.Errorf("unexpected value for clusterIP, want ip, got: %v", svc.Spec.ClusterIP)
				}
				if svc.Spec.Ports[0].NodePort != 331 {
					return fmt.Errorf("unexpected value for node port: %v", svc.Spec.Ports[0])
				}
				return nil
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			_, err := reconcileServiceForCRD(tt.args.ctx, cl, tt.args.newService)
			if (err != nil) != tt.wantErr {
				t.Errorf("reconcileServiceForCRD() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			var updatedSvc v1.Service
			if err := cl.Get(tt.args.ctx, types.NamespacedName{Namespace: tt.args.newService.Namespace, Name: tt.args.newService.Name}, &updatedSvc); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if err := tt.validate(&updatedSvc); err != nil {
				t.Errorf("reconcileServiceForCRD() unexpected error: %v.", err)
			}
		})
	}
}

func Test_mergeServiceSpec(t *testing.T) {
	type args struct {
		svc     *v1.Service
		svcSpec *victoriametricsv1beta1.ServiceSpec
	}
	tests := []struct {
		name     string
		args     args
		validate func(svc *v1.Service) error
	}{
		{
			name: "override ports",
			args: args{
				svc: &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name: "some-name",
					},
					Spec: v1.ServiceSpec{
						Ports: []v1.ServicePort{
							{Name: "web"},
						},
					},
				},
				svcSpec: &victoriametricsv1beta1.ServiceSpec{
					Spec: v1.ServiceSpec{
						Ports: []v1.ServicePort{
							{Name: "metrics"},
						},
					},
				},
			},
			validate: func(svc *v1.Service) error {
				if svc.Name != "some-name-additional-service" {
					return fmt.Errorf("expect name to be empty, got: %v", svc.Name)
				}
				if len(svc.Spec.Ports) != 1 && svc.Spec.Ports[0].Name != "metrics" {
					return fmt.Errorf("unexpected value for ports: %v", svc.Spec.Ports)
				}
				return nil
			},
		},
		{
			name: "change clusterIP ports",
			args: args{
				svc: &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name: "some-name",
					},
					Spec: v1.ServiceSpec{
						Ports: []v1.ServicePort{
							{Name: "metrics"},
						},
					},
				},
				svcSpec: &victoriametricsv1beta1.ServiceSpec{
					Spec: v1.ServiceSpec{
						Type: v1.ServiceTypeNodePort,
					},
				},
			},
			validate: func(svc *v1.Service) error {
				if svc.Spec.Type != v1.ServiceTypeNodePort {
					return fmt.Errorf("unexpected value for spec.type want nodePort, got: %v", svc.Spec.Type)
				}
				if len(svc.Spec.Ports) != 1 && svc.Spec.Ports[0].Name != "metrics" {
					return fmt.Errorf("unexpected value for ports: %v", svc.Spec.Ports)
				}
				return nil
			},
		},
		{
			name: "change selector",
			args: args{
				svc: &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name: "some-name",
					},
					Spec: v1.ServiceSpec{
						Type: v1.ServiceTypeNodePort,
						Ports: []v1.ServicePort{
							{Name: "metrics"},
						},
						Selector: map[string]string{
							"app": "value",
						},
					},
				},
				svcSpec: &victoriametricsv1beta1.ServiceSpec{
					Spec: v1.ServiceSpec{
						Type: v1.ServiceTypeNodePort,
						Selector: map[string]string{
							"app-2": "value-3",
						},
					},
				},
			},
			validate: func(svc *v1.Service) error {
				if svc.Spec.Type != v1.ServiceTypeNodePort {
					return fmt.Errorf("unexpected value for spec.type want nodePort, got: %v", svc.Spec.Type)
				}
				if len(svc.Spec.Ports) != 1 && svc.Spec.Ports[0].Name != "metrics" {
					return fmt.Errorf("unexpected value for ports: %v", svc.Spec.Ports)
				}
				if diff := deep.Equal(svc.Spec.Selector, map[string]string{"app-2": "value-3"}); len(diff) > 0 {
					return fmt.Errorf("unexpected value for selector: %v", svc.Spec.Selector)
				}
				return nil
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mergeServiceSpec(tt.args.svc, tt.args.svcSpec)
			if err := tt.validate(tt.args.svc); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func Test_buildProbe(t *testing.T) {
	type args struct {
		container       v1.Container
		ep              *victoriametricsv1beta1.EmbeddedProbes
		probePath       func() string
		port            string
		needAddLiveness bool
	}
	tests := []struct {
		name     string
		args     args
		validate func(v1.Container) error
	}{
		{
			name: "build default probe with empty ep",
			args: args{
				probePath: func() string {
					return "/health"
				},
				container:       v1.Container{},
				port:            "8051",
				needAddLiveness: true,
			},
			validate: func(container v1.Container) error {
				if container.LivenessProbe == nil {
					return fmt.Errorf("want liveness to be not nil")
				}
				if container.ReadinessProbe == nil {
					return fmt.Errorf("want readinessProbe to be not nil")
				}
				return nil
			},
		},
		{
			name: "build default probe with ep",
			args: args{
				probePath: func() string {
					return "/health"
				},
				container:       v1.Container{},
				port:            "8051",
				needAddLiveness: true,
				ep: &victoriametricsv1beta1.EmbeddedProbes{
					ReadinessProbe: &v1.Probe{
						ProbeHandler: v1.ProbeHandler{
							Exec: &v1.ExecAction{
								Command: []string{"echo", "1"},
							},
						},
					},
					StartupProbe: &v1.Probe{
						ProbeHandler: v1.ProbeHandler{
							HTTPGet: &v1.HTTPGetAction{
								Host: "some",
							},
						},
					},
					LivenessProbe: &v1.Probe{
						ProbeHandler: v1.ProbeHandler{
							HTTPGet: &v1.HTTPGetAction{
								Path: "/live1",
							},
						},
						TimeoutSeconds:      15,
						InitialDelaySeconds: 20,
					},
				},
			},
			validate: func(container v1.Container) error {
				if container.LivenessProbe == nil {
					return fmt.Errorf("want liveness to be not nil")
				}
				if container.ReadinessProbe == nil {
					return fmt.Errorf("want readinessProbe to be not nil")
				}
				if container.StartupProbe == nil {
					return fmt.Errorf("want startupProbe to be not nil")
				}
				if len(container.ReadinessProbe.Exec.Command) != 2 {
					return fmt.Errorf("want exec args: %d, got: %v", 2, container.ReadinessProbe.Exec.Command)
				}
				if container.StartupProbe.HTTPGet.Host != "some" {
					return fmt.Errorf("want host: %s, got: %s", "some", container.StartupProbe.HTTPGet.Host)
				}
				if container.LivenessProbe.HTTPGet.Path != "/live1" {
					return fmt.Errorf("unexpected path, got: %s, want: %v", container.LivenessProbe.HTTPGet.Path, "/live1")
				}
				if container.LivenessProbe.InitialDelaySeconds != 20 {
					return fmt.Errorf("unexpected delay, got: %d, want: %d", container.LivenessProbe.InitialDelaySeconds, 20)
				}
				if container.LivenessProbe.TimeoutSeconds != 15 {
					return fmt.Errorf("unexpected timeout, got: %d, want: %d", container.LivenessProbe.TimeoutSeconds, 15)
				}
				return nil
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildProbe(tt.args.container, tt.args.ep, tt.args.probePath, tt.args.port, tt.args.needAddLiveness)
			if err := tt.validate(got); err != nil {
				t.Errorf("buildProbe() unexpected error: %v", err)
			}
		})
	}
}

func Test_addExtraArgsOverrideDefaults(t *testing.T) {
	type args struct {
		args      []string
		extraArgs map[string]string
		dashes    string
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{
			name: "no changes",
			args: args{
				args:   []string{"-http.ListenAddr=:8081"},
				dashes: "-",
			},
			want: []string{"-http.ListenAddr=:8081"},
		},
		{
			name: "override default",
			args: args{
				args:      []string{"-http.ListenAddr=:8081"},
				extraArgs: map[string]string{"http.ListenAddr": "127.0.0.1:8085"},
				dashes:    "-",
			},
			want: []string{"-http.ListenAddr=127.0.0.1:8085"},
		},
		{
			name: "override default, add to the end",
			args: args{
				args:      []string{"-http.ListenAddr=:8081", "-promscrape.config=/opt/vmagent.yml"},
				extraArgs: map[string]string{"http.ListenAddr": "127.0.0.1:8085"},
				dashes:    "-",
			},
			want: []string{"-promscrape.config=/opt/vmagent.yml", "-http.ListenAddr=127.0.0.1:8085"},
		},
		{
			name: "two dashes, extend",
			args: args{
				args:      []string{"--web.timeout=0"},
				extraArgs: map[string]string{"log.level": "debug"},
				dashes:    "--",
			},
			want: []string{"--web.timeout=0", "--log.level=debug"},
		},
		{
			name: "two dashes, override default",
			args: args{
				args:      []string{"--log.level=info"},
				extraArgs: map[string]string{"log.level": "debug"},
				dashes:    "--",
			},
			want: []string{"--log.level=debug"},
		},
		{
			name: "two dashes, alertmanager migration",
			args: args{
				args:      []string{"--log.level=info"},
				extraArgs: map[string]string{"-web.externalURL": "http://domain.example"},
				dashes:    "--",
			},
			want: []string{"--log.level=info", "--web.externalURL=http://domain.example"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equalf(
				t,
				tt.want,
				addExtraArgsOverrideDefaults(tt.args.args, tt.args.extraArgs, tt.args.dashes),
				"addExtraArgsOverrideDefaults(%v, %v)", tt.args.args, tt.args.extraArgs)
		})
	}
}
