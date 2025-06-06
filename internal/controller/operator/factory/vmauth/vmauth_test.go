package vmauth

import (
	"context"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestCreateOrUpdateVMAuth(t *testing.T) {
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
					c.UseCustomConfigReloader = true
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
			if err := CreateOrUpdateVMAuth(ctx, tt.args.cr, tc); (err != nil) != tt.wantErr {
				t.Errorf("CreateOrUpdateVMAuth() error = %v, wantErr %v", err, tt.wantErr)
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
    emptyDir: {}
initContainers:
  - name: config-init
    image: vmcustom:config-reloader-v0.35.0
    args:
      - --reload-url=http://localhost:8429/-/reload
      - --config-envsubst-file=/opt/vmauth/config.yaml
      - --config-secret-name=default/vmauth-config-auth
      - --only-init-config
    volumeMounts:
      - name: config-out
        mountPath: /opt/vmauth
containers:
  - name: vmauth
    image: vm-repo:v1.97.1
    imagePullPolicy: IfNotPresent
    args:
      - -auth.config=/opt/vmauth/config.yaml
      - -httpListenAddr=:8429
    ports:
      - name: http
        containerPort: 8429
        protocol: TCP
    volumeMounts:
      - name: config-out
        mountPath: /opt/vmauth
    livenessProbe:
      httpGet:
        path: /health
        port:
          intval: 8429
        scheme: HTTP
      timeoutSeconds: 5
      periodSeconds: 5
      successThreshold: 1
      failureThreshold: 10
    readinessProbe:
      httpGet:
        path: /health
        port:
          intval: 8429
        scheme: HTTP
      initialDelaySeconds: 0
      timeoutSeconds: 5
      periodSeconds: 5
      successThreshold: 1
      failureThreshold: 10
    terminationMessagePolicy: FallbackToLogsOnError
  - name: config-reloader
    image: vmcustom:config-reloader-v0.35.0
    args:
      - --reload-url=http://localhost:8429/-/reload
      - --config-envsubst-file=/opt/vmauth/config.yaml
      - --config-secret-name=default/vmauth-config-auth
    env:
      - name: POD_NAME
        valueFrom:
          fieldRef:
            fieldPath: metadata.name
    ports:
      - name: reloader-http
        containerPort: 8435
        protocol: TCP
    volumeMounts:
      - name: config-out
        mountPath: /opt/vmauth
    livenessProbe:
      httpGet:
        path: /health
        port:
          intval: 8435
        scheme: HTTP
      timeoutSeconds: 1
      periodSeconds: 10
      successThreshold: 1
      failureThreshold: 3
    readinessProbe:
      httpGet:
        path: /health
        port:
          intval: 8435
        scheme: HTTP
      initialDelaySeconds: 5
      timeoutSeconds: 1
      periodSeconds: 10
      successThreshold: 1
      failureThreshold: 3
    terminationMessagePolicy: FallbackToLogsOnError
serviceAccountName: vmauth-auth

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
    emptyDir: {}
  - name: config
    secret:
      secretName: vmauth-config-auth
initContainers:
  - name: config-init
    image: quay.io/prometheus-operator/prometheus-config-reloader:v1
    command:
      - /bin/sh
    args:
      - -c
      - "gunzip -c /opt/vmauth-config-gz/config.yaml.gz > /opt/vmauth/config.yaml"
    volumeMounts:
      - name: config
        mountPath: /opt/vmauth-config-gz
      - name: config-out
        mountPath: /opt/vmauth
containers:
  - name: vmauth
    image: vm-repo:v1.97.1
    imagePullPolicy: IfNotPresent
    args:
      - -auth.config=/opt/vmauth/config.yaml
      - -httpListenAddr=:8429
    ports:
      - name: http
        containerPort: 8429
        protocol: TCP
    volumeMounts:
      - name: config
        mountPath: /opt/vmauth/config
      - name: config-out
        mountPath: /opt/vmauth
    livenessProbe:
      httpGet:
        path: /health
        port:
          intval: 8429
        scheme: HTTP
      timeoutSeconds: 5
      periodSeconds: 5
      successThreshold: 1
      failureThreshold: 10
    readinessProbe:
      httpGet:
        path: /health
        port:
          intval: 8429
        scheme: HTTP
      initialDelaySeconds: 0
      timeoutSeconds: 5
      periodSeconds: 5
      successThreshold: 1
      failureThreshold: 10
    terminationMessagePolicy: FallbackToLogsOnError
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
        valueFrom:
          fieldRef:
            fieldPath: metadata.name
    volumeMounts:
      - name: config-out
        mountPath: /opt/vmauth
      - name: config
        mountPath: /opt/vmauth-config-gz
    terminationMessagePolicy: FallbackToLogsOnError
serviceAccountName: vmauth-auth

`)
	})
}
