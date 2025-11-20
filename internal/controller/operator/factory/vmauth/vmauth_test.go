package vmauth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestCreateOrUpdate(t *testing.T) {
	mutateConf := func(cb func(c *config.BaseOperatorConf)) *config.BaseOperatorConf {
		c := config.MustGetBaseConfig()
		cb(c)
		return c
	}
	type args struct {
		cr *vmv1beta1.VMAuth
		c  *config.BaseOperatorConf
	}
	tests := []struct {
		name              string
		args              args
		wantErr           bool
		predefinedObjects []runtime.Object
	}{
		{
			name: "simple-unmanaged",
			args: args{
				cr: &vmv1beta1.VMAuth{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "default",
					},
				},
				c: config.MustGetBaseConfig(),
			},
			predefinedObjects: []runtime.Object{
				k8stools.NewReadyDeployment("vmauth-test", "default"),
			},
		},
		{
			name: "simple-with-httproute",
			args: args{
				cr: &vmv1beta1.VMAuth{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAuthSpec{
						CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
							Port: "8427",
						},
						HTTPRoute: &vmv1beta1.EmbeddedHTTPRoute{
							Spec: gwapiv1.HTTPRouteSpec{
								CommonRouteSpec: gwapiv1.CommonRouteSpec{
									ParentRefs: []gwapiv1.ParentReference{
										{
											Group:     ptr.To(gwapiv1.Group("gateway.networking.k8s.io")),
											Kind:      ptr.To(gwapiv1.Kind("Gateway")),
											Namespace: ptr.To(gwapiv1.Namespace("default")),
											Name:      gwapiv1.ObjectName("test"),
										},
									},
								},
								Rules: []gwapiv1.HTTPRouteRule{{
									Matches: []gwapiv1.HTTPRouteMatch{
										{
											Path: &gwapiv1.HTTPPathMatch{
												Type:  ptr.To(gwapiv1.PathMatchPathPrefix),
												Value: ptr.To("/"),
											},
										},
									},
								}},
							},
						},
					},
				},
				c: config.MustGetBaseConfig(),
			},
			predefinedObjects: []runtime.Object{
				k8stools.NewReadyDeployment("vmauth-test", "default"),
				&gwapiv1.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmauth-test",
						Namespace: "default",
					},
					Spec: gwapiv1.HTTPRouteSpec{
						CommonRouteSpec: gwapiv1.CommonRouteSpec{
							ParentRefs: []gwapiv1.ParentReference{
								{
									Group:     ptr.To(gwapiv1.Group("gateway.networking.k8s.io")),
									Kind:      ptr.To(gwapiv1.Kind("Gateway")),
									Namespace: ptr.To(gwapiv1.Namespace("default")),
									Name:      gwapiv1.ObjectName("test"),
								},
							},
						},
						Rules: []gwapiv1.HTTPRouteRule{{
							Matches: []gwapiv1.HTTPRouteMatch{
								{
									Path: &gwapiv1.HTTPPathMatch{
										Type:  ptr.To(gwapiv1.PathMatchPathPrefix),
										Value: ptr.To("/"),
									},
								},
							},
						}},
					},
				},
			},
		},
		{
			name: "simple-remove-httproute",
			args: args{
				cr: &vmv1beta1.VMAuth{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAuthSpec{
						CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
							Port: "8427",
						},
					},
					ParsedLastAppliedSpec: &vmv1beta1.VMAuthSpec{
						CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
							Port: "8427",
						},
						HTTPRoute: &vmv1beta1.EmbeddedHTTPRoute{
							Spec: gwapiv1.HTTPRouteSpec{
								CommonRouteSpec: gwapiv1.CommonRouteSpec{
									ParentRefs: []gwapiv1.ParentReference{
										{
											Group:     ptr.To(gwapiv1.Group("gateway.networking.k8s.io")),
											Kind:      ptr.To(gwapiv1.Kind("Gateway")),
											Namespace: ptr.To(gwapiv1.Namespace("default")),
											Name:      gwapiv1.ObjectName("test"),
										},
									},
								},
								Rules: []gwapiv1.HTTPRouteRule{{
									Matches: []gwapiv1.HTTPRouteMatch{
										{
											Path: &gwapiv1.HTTPPathMatch{
												Type:  ptr.To(gwapiv1.PathMatchPathPrefix),
												Value: ptr.To("/"),
											},
										},
									},
								}},
							},
						},
					},
				},
				c: config.MustGetBaseConfig(),
			},
			predefinedObjects: []runtime.Object{
				k8stools.NewReadyDeployment("vmauth-test", "default"),
			},
		},
		{
			name: "simple-with-external-config",
			args: args{
				cr: &vmv1beta1.VMAuth{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAuthSpec{
						ExternalConfig: vmv1beta1.ExternalConfig{
							SecretRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "external-cfg",
								},
							},
						},
					},
				},
				c: config.MustGetBaseConfig(),
			},
			predefinedObjects: []runtime.Object{
				k8stools.NewReadyDeployment("vmauth-test", "default"),
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "external-cfg",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"config.yaml": {},
					},
				},
			},
		},
		{
			name: "with-match-all",
			args: args{
				cr: &vmv1beta1.VMAuth{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAuthSpec{
						SelectAllByDefault: true,
					},
				},
				c: config.MustGetBaseConfig(),
			},
			predefinedObjects: []runtime.Object{
				k8stools.NewReadyDeployment("vmauth-test", "default"),
				&vmv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-1",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMUserSpec{
						UserName: ptr.To("user-1"),
						Password: ptr.To("password-1"),
						TargetRefs: []vmv1beta1.TargetRef{
							{
								Static: &vmv1beta1.StaticRef{
									URLs: []string{"http://url-1", "http://url-2"},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "with customer config reloader",
			args: args{
				cr: &vmv1beta1.VMAuth{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAuthSpec{
						SelectAllByDefault: true,
					},
				},
				c: mutateConf(func(c *config.BaseOperatorConf) {
					c.UseVMConfigReloader = true
				}),
			},
			predefinedObjects: []runtime.Object{
				k8stools.NewReadyDeployment("vmauth-test", "default"),
				&vmv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-1",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMUserSpec{
						UserName: ptr.To("user-1"),
						Password: ptr.To("password-1"),
						TargetRefs: []vmv1beta1.TargetRef{
							{
								Static: &vmv1beta1.StaticRef{
									URLs: []string{"http://url-1", "http://url-2"},
								},
							},
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			tc := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			// TODO fix
			if err := CreateOrUpdate(ctx, tt.args.cr, tc); (err != nil) != tt.wantErr {
				t.Errorf("CreateOrUpdate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMakeSpecForAuthOk(t *testing.T) {
	f := func(t *testing.T, cr *vmv1beta1.VMAuth, wantYaml string) {
		t.Helper()

		scheme := k8stools.GetTestClientWithObjects(nil).Scheme()
		build.AddDefaults(scheme)
		scheme.Default(cr)
		var wantSpec corev1.PodSpec
		if err := yaml.Unmarshal([]byte(wantYaml), &wantSpec); err != nil {
			t.Fatalf("not expected wantYaml: %q: \n%q", wantYaml, err)
		}
		wantYAMLForCompare, err := yaml.Marshal(wantSpec)
		if err != nil {
			t.Fatalf("BUG: cannot parse as yaml: %q", err)
		}
		got, err := makeSpecForVMAuth(cr)
		if err != nil {
			t.Fatalf("not expected error=%q", err)
		}
		gotYAML, err := yaml.Marshal(got.Spec)
		if err != nil {
			t.Fatalf("cannot parse got as yaml: %q", err)
		}
		assert.Equal(t, string(wantYAMLForCompare), string(gotYAML))
	}

	t.Run("custom-loader", func(t *testing.T) {
		f(t, &vmv1beta1.VMAuth{
			ObjectMeta: metav1.ObjectMeta{Name: "auth", Namespace: "default"},
			Spec: vmv1beta1.VMAuthSpec{
				CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
					UseDefaultResources: ptr.To(false),
					Image: vmv1beta1.Image{
						Repository: "vm-repo",
						Tag:        "v1.97.1",
					},
					Port: "8429",
				},
				CommonConfigReloaderParams: vmv1beta1.CommonConfigReloaderParams{
					UseVMConfigReloader:    ptr.To(true),
					ConfigReloaderImageTag: "vmcustom:config-reloader-v0.35.0",
				},
			},
		}, `
volumes:
  - name: config-out
    volumesource:
      emptydir:
        medium: ""
        sizelimit: null
initcontainers:
  - name: config-init
    image: vmcustom:config-reloader-v0.35.0
    args:
      - --reload-url=http://localhost:8429/-/reload
      - --config-envsubst-file=/opt/vmauth/config.yaml
      - --config-secret-name=default/vmauth-config-auth
      - --only-init-config
    resources:
      limits: {}
      requests: {}
    volumemounts:
      - name: config-out
        readonly: false
        mountpath: /opt/vmauth
containers:
  - name: vmauth
    image: vm-repo:v1.97.1
    imagepullpolicy: IfNotPresent
    args:
      - -auth.config=/opt/vmauth/config.yaml
      - -httpListenAddr=:8429
    ports:
      - name: http
        hostport: 0
        containerport: 8429
        protocol: TCP
    resources:
      limits: {}
      requests: {}
    volumemounts:
      - name: config-out
        readonly: false
        mountpath: /opt/vmauth
    livenessprobe:
      probehandler:
        httpget:
          path: /health
          port:
            type: 0
            intval: 8429
            strval: ""
          scheme: HTTP
      timeoutseconds: 5
      periodseconds: 5
      successthreshold: 1
      failurethreshold: 10
    readinessprobe:
      probehandler:
        httpget:
          path: /health
          port:
            intval: 8429
          scheme: HTTP
      initialdelayseconds: 0
      timeoutseconds: 5
      periodseconds: 5
      successthreshold: 1
      failurethreshold: 10
    terminationmessagepolicy: FallbackToLogsOnError
  - name: config-reloader
    image: vmcustom:config-reloader-v0.35.0
    args:
      - --reload-url=http://localhost:8429/-/reload
      - --config-envsubst-file=/opt/vmauth/config.yaml
      - --config-secret-name=default/vmauth-config-auth
    env:
      - name: POD_NAME
        valuefrom:
          fieldref:
            apiversion: ""
            fieldpath: metadata.name
    ports:
      - name: reloader-http
        containerport: 8435
        protocol: TCP
    resources:
      limits: {}
      requests: {}
    volumemounts:
      - name: config-out
        readonly: false
        mountpath: /opt/vmauth
    livenessprobe:
      probehandler:
        httpget:
          path: /health
          port:
            type: 0
            intval: 8435
            strval: ""
          scheme: HTTP
      timeoutseconds: 1
      periodseconds: 10
      successthreshold: 1
      failurethreshold: 3
    readinessprobe:
      probehandler:
        httpget:
          path: /health
          port:
            intval: 8435
          scheme: HTTP
      initialdelayseconds: 5
      timeoutseconds: 1
      periodseconds: 10
      successthreshold: 1
      failurethreshold: 3
    terminationmessagepolicy: FallbackToLogsOnError
serviceaccountname: vmauth-auth

`)
	})
	t.Run("no-custom-loader", func(t *testing.T) {
		f(t, &vmv1beta1.VMAuth{
			ObjectMeta: metav1.ObjectMeta{Name: "auth", Namespace: "default"},
			Spec: vmv1beta1.VMAuthSpec{
				CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
					UseDefaultResources: ptr.To(false),
					Image: vmv1beta1.Image{
						Repository: "vm-repo",
						Tag:        "v1.97.1",
					},
					Port: "8429",
				},
				CommonConfigReloaderParams: vmv1beta1.CommonConfigReloaderParams{
					ConfigReloaderImageTag: "quay.io/prometheus-operator/prometheus-config-reloader:v1",
					UseVMConfigReloader:    ptr.To(false),
				},
			},
		}, `
volumes:
  - name: config-out
    volumesource:
      emptydir:
        medium: ""
        sizelimit: null
  - name: config
    volumesource:
      secret:
        secretname: vmauth-config-auth
initcontainers:
  - name: config-init
    image: quay.io/prometheus-operator/prometheus-config-reloader:v1
    command:
      - /bin/sh
    args:
      - -c
      - "gunzip -c /opt/vmauth-config-gz/config.yaml.gz > /opt/vmauth/config.yaml"
    resources:
      limits: {}
      requests: {}
    volumemounts:
      - name: config
        mountpath: /opt/vmauth-config-gz
      - name: config-out
        mountpath: /opt/vmauth
containers:
  - name: vmauth
    image: vm-repo:v1.97.1
    imagepullpolicy: IfNotPresent
    args:
      - -auth.config=/opt/vmauth/config.yaml
      - -httpListenAddr=:8429
    ports:
      - name: http
        hostport: 0
        containerport: 8429
        protocol: TCP
    resources:
      limits: {}
      requests: {}
    volumemounts:
      - name: config
        mountpath: /opt/vmauth/config
      - name: config-out
        mountpath: /opt/vmauth
    livenessprobe:
      probehandler:
        httpget:
          path: /health
          port:
            type: 0
            intval: 8429
            strval: ""
          scheme: HTTP
      timeoutseconds: 5
      periodseconds: 5
      successthreshold: 1
      failurethreshold: 10
    readinessprobe:
      probehandler:
        httpget:
          path: /health
          port:
            intval: 8429
          scheme: HTTP
      initialdelayseconds: 0
      timeoutseconds: 5
      periodseconds: 5
      successthreshold: 1
      failurethreshold: 10
    terminationmessagepolicy: FallbackToLogsOnError
  - name: config-reloader
    image: quay.io/prometheus-operator/prometheus-config-reloader:v1
    command:
      - /bin/prometheus-config-reloader
    args:
      - --reload-url=http://localhost:8429/-/reload
      - --config-envsubst-file=/opt/vmauth/config.yaml
      - --config-file=/opt/vmauth-config-gz/config.yaml.gz
    env:
      - name: POD_NAME
        valuefrom:
          fieldref:
            apiversion: ""
            fieldpath: metadata.name
    resources:
      limits: {}
      requests: {}
    volumemounts:
      - name: config-out
        readonly: false
        mountpath: /opt/vmauth
      - name: config
        mountpath: /opt/vmauth-config-gz
    terminationmessagepolicy: FallbackToLogsOnError
serviceaccountname: vmauth-auth

`)
	})
}
