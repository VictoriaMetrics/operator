package vmauth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/utils/ptr"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestCreateOrUpdate(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMAuth
		cfgMutator        func(*config.BaseOperatorConf)
		wantErr           bool
		predefinedObjects []runtime.Object
	}

	f := func(o opts) {
		cfg := config.MustGetBaseConfig()
		if o.cfgMutator != nil {
			defaultCfg := *cfg
			o.cfgMutator(cfg)
			defer func() {
				*config.MustGetBaseConfig() = defaultCfg
			}()
		}
		ctx := context.Background()
		tc := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		if err := CreateOrUpdate(ctx, o.cr, tc); (err != nil) != o.wantErr {
			t.Errorf("CreateOrUpdate() error = %v, wantErr %v", err, o.wantErr)
		}
	}

	// simple-with-httproute
	f(opts{
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
					ParentRefs: []gwapiv1.ParentReference{
						{
							Group:     ptr.To(gwapiv1.Group("gateway.networking.k8s.io")),
							Kind:      ptr.To(gwapiv1.Kind("Gateway")),
							Namespace: ptr.To(gwapiv1.Namespace("default")),
							Name:      gwapiv1.ObjectName("test"),
						},
					},
				},
			},
		},
		cfgMutator: func(c *config.BaseOperatorConf) {
			c.GatewayAPIEnabled = true
		},
		predefinedObjects: []runtime.Object{
			k8stools.NewReadyDeployment("vmauth-test", "default"),
			&apiextensionsv1.CustomResourceDefinition{
				ObjectMeta: metav1.ObjectMeta{
					Name: "httproutes.gateway.networking.k8s.io",
				},
			},
		},
	})

	// simple-remove-httproute
	f(opts{
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
					ParentRefs: []gwapiv1.ParentReference{
						{
							Group:     ptr.To(gwapiv1.Group("gateway.networking.k8s.io")),
							Kind:      ptr.To(gwapiv1.Kind("Gateway")),
							Namespace: ptr.To(gwapiv1.Namespace("default")),
							Name:      gwapiv1.ObjectName("test"),
						},
					},
				},
			},
		},
		predefinedObjects: []runtime.Object{
			k8stools.NewReadyDeployment("vmauth-test", "default"),
			&apiextensionsv1.CustomResourceDefinition{
				ObjectMeta: metav1.ObjectMeta{
					Name: "httproutes.gateway.networking.k8s.io",
				},
			},
		},
	})

	// simple-with-external-config
	f(opts{
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
	})

	// with-match-all
	f(opts{
		cr: &vmv1beta1.VMAuth{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAuthSpec{
				SelectAllByDefault: true,
			},
		},
		predefinedObjects: []runtime.Object{
			k8stools.NewReadyDeployment("vmauth-test", "default"),
			&vmv1beta1.VMUser{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "user-1",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMUserSpec{
					Username: ptr.To("user-1"),
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
	})

	// with customer config reloader
	f(opts{
		cr: &vmv1beta1.VMAuth{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAuthSpec{
				SelectAllByDefault: true,
			},
		},
		predefinedObjects: []runtime.Object{
			k8stools.NewReadyDeployment("vmauth-test", "default"),
			&vmv1beta1.VMUser{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "user-1",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMUserSpec{
					Username: ptr.To("user-1"),
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
	})
}

func TestVMAuthVPACreate(t *testing.T) {
	updateModeInitial := vpav1.UpdateModeInitial

	ctx := context.Background()
	fclient := k8stools.GetTestClientWithObjects([]runtime.Object{
		k8stools.NewReadyDeployment("vmauth-test", "default"),
	})
	build.AddDefaults(fclient.Scheme())

	cr := &vmv1beta1.VMAuth{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: vmv1beta1.VMAuthSpec{
			VPA: &vmv1beta1.EmbeddedVPA{
				UpdatePolicy: &vpav1.PodUpdatePolicy{
					UpdateMode: &updateModeInitial,
				},
				ResourcePolicy: &vpav1.PodResourcePolicy{
					ContainerPolicies: []vpav1.ContainerResourcePolicy{
						{ContainerName: "vmauth"},
					},
				},
			},
		},
	}
	if err := CreateOrUpdate(ctx, cr, fclient); err != nil {
		t.Fatalf("CreateOrUpdate() error = %v", err)
	}

	var vpa vpav1.VerticalPodAutoscaler
	if err := fclient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "vmauth-test"}, &vpa); err != nil {
		t.Fatalf("VPA not found: %v", err)
	}
	assert.Equal(t, vpav1.UpdateModeInitial, *vpa.Spec.UpdatePolicy.UpdateMode)
	assert.Equal(t, "vmauth-test", vpa.Spec.TargetRef.Name)
	assert.Equal(t, "Deployment", vpa.Spec.TargetRef.Kind)
}

func TestVMAuthVPAUpdate(t *testing.T) {
	updateModeInitial := vpav1.UpdateModeInitial
	updateModeRecreate := vpav1.UpdateModeRecreate

	ctx := context.Background()
	fclient := k8stools.GetTestClientWithObjects([]runtime.Object{
		k8stools.NewReadyDeployment("vmauth-test", "default"),
	})
	build.AddDefaults(fclient.Scheme())

	// Create initial VPA
	cr := &vmv1beta1.VMAuth{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: vmv1beta1.VMAuthSpec{
			VPA: &vmv1beta1.EmbeddedVPA{
				UpdatePolicy: &vpav1.PodUpdatePolicy{
					UpdateMode: &updateModeInitial,
				},
				ResourcePolicy: &vpav1.PodResourcePolicy{
					ContainerPolicies: []vpav1.ContainerResourcePolicy{
						{ContainerName: "vmauth"},
					},
				},
			},
		},
	}
	if err := CreateOrUpdate(ctx, cr, fclient); err != nil {
		t.Fatalf("initial CreateOrUpdate() error = %v", err)
	}

	var vpa vpav1.VerticalPodAutoscaler
	if err := fclient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "vmauth-test"}, &vpa); err != nil {
		t.Fatalf("VPA not found after create: %v", err)
	}
	assert.Equal(t, vpav1.UpdateModeInitial, *vpa.Spec.UpdatePolicy.UpdateMode)

	// Update VPA to Recreate
	updatedCR := cr.DeepCopy()
	updatedCR.Spec.VPA.UpdatePolicy.UpdateMode = &updateModeRecreate
	updatedCR.ParsedLastAppliedSpec = cr.Spec.DeepCopy()
	if err := CreateOrUpdate(ctx, updatedCR, fclient); err != nil {
		t.Fatalf("update CreateOrUpdate() error = %v", err)
	}

	if err := fclient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "vmauth-test"}, &vpa); err != nil {
		t.Fatalf("VPA not found after update: %v", err)
	}
	assert.Equal(t, vpav1.UpdateModeRecreate, *vpa.Spec.UpdatePolicy.UpdateMode)
}

func TestVMAuthVPARemoval(t *testing.T) {
	updateModeInitial := vpav1.UpdateModeInitial

	ctx := context.Background()
	fclient := k8stools.GetTestClientWithObjects([]runtime.Object{
		k8stools.NewReadyDeployment("vmauth-test", "default"),
	})
	build.AddDefaults(fclient.Scheme())

	// Create with VPA
	cr := &vmv1beta1.VMAuth{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: vmv1beta1.VMAuthSpec{
			VPA: &vmv1beta1.EmbeddedVPA{
				UpdatePolicy: &vpav1.PodUpdatePolicy{
					UpdateMode: &updateModeInitial,
				},
				ResourcePolicy: &vpav1.PodResourcePolicy{
					ContainerPolicies: []vpav1.ContainerResourcePolicy{
						{ContainerName: "vmauth"},
					},
				},
			},
		},
	}
	if err := CreateOrUpdate(ctx, cr, fclient); err != nil {
		t.Fatalf("initial CreateOrUpdate() error = %v", err)
	}

	var vpa vpav1.VerticalPodAutoscaler
	if err := fclient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "vmauth-test"}, &vpa); err != nil {
		t.Fatalf("VPA not found after create: %v", err)
	}

	// Remove VPA
	removedCR := cr.DeepCopy()
	removedCR.ParsedLastAppliedSpec = cr.Spec.DeepCopy()
	removedCR.Spec.VPA = nil
	if err := CreateOrUpdate(ctx, removedCR, fclient); err != nil {
		t.Fatalf("removal CreateOrUpdate() error = %v", err)
	}

	err := fclient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "vmauth-test"}, &vpa)
	assert.True(t, k8serrors.IsNotFound(err), "VPA should be removed, got error: %v", err)
}

func TestMakeSpecForAuthOk(t *testing.T) {
	f := func(cr *vmv1beta1.VMAuth, wantYaml string) {
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

	// with custom-loader
	f(&vmv1beta1.VMAuth{
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
				ConfigReloaderImage: "vmcustom:config-reloader-v0.35.0",
			},
		},
	}, `
volumes:
  - name: config-out
    volumesource:
      emptydir: {}
initcontainers:
  - name: config-init
    image: vmcustom:config-reloader-v0.35.0
    args:
      - --config-envsubst-file=/opt/vmauth/config.yaml
      - --config-secret-key=config.yaml.gz
      - --config-secret-name=default/vmauth-config-auth
      - --only-init-config
      - --reload-url=http://127.0.0.1:8429/-/reload
      - --webhook-method=POST
    volumemounts:
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
        containerport: 8429
        protocol: TCP
    volumemounts:
      - name: config-out
        mountpath: /opt/vmauth
    livenessprobe:
      probehandler:
        httpget:
          path: /health
          port:
            intval: 8429
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
      timeoutseconds: 5
      periodseconds: 5
      successthreshold: 1
      failurethreshold: 10
    terminationmessagepolicy: FallbackToLogsOnError
  - name: config-reloader
    image: vmcustom:config-reloader-v0.35.0
    args:
      - --config-envsubst-file=/opt/vmauth/config.yaml
      - --config-secret-key=config.yaml.gz
      - --config-secret-name=default/vmauth-config-auth
      - --reload-url=http://127.0.0.1:8429/-/reload
      - --webhook-method=POST
    ports:
      - name: reloader-http
        containerport: 8435
        protocol: TCP
    volumemounts:
      - name: config-out
        mountpath: /opt/vmauth
    livenessprobe:
      probehandler:
        httpget:
          path: /health
          port:
            intval: 8435
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

	// with config-reloader
	f(&vmv1beta1.VMAuth{
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
		},
	}, `
volumes:
  - name: config-out
    volumesource:
      emptydir: {}
initcontainers:
  - name: config-init
    image: victoriametrics/operator:config-reloader-v0.66.1
    args:
      - --config-envsubst-file=/opt/vmauth/config.yaml
      - --config-secret-key=config.yaml.gz
      - --config-secret-name=default/vmauth-config-auth
      - --only-init-config
      - --reload-url=http://127.0.0.1:8429/-/reload
      - --webhook-method=POST
    volumemounts:
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
        containerport: 8429
        protocol: TCP
    volumemounts:
      - name: config-out
        mountpath: /opt/vmauth
    livenessprobe:
      probehandler:
        httpget:
          path: /health
          port:
            intval: 8429
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
      timeoutseconds: 5
      periodseconds: 5
      successthreshold: 1
      failurethreshold: 10
    terminationmessagepolicy: FallbackToLogsOnError
  - name: config-reloader
    image: victoriametrics/operator:config-reloader-v0.66.1
    args:
      - --config-envsubst-file=/opt/vmauth/config.yaml
      - --config-secret-key=config.yaml.gz
      - --config-secret-name=default/vmauth-config-auth
      - --reload-url=http://127.0.0.1:8429/-/reload
      - --webhook-method=POST
    ports:
      - name: reloader-http
        containerport: 8435
        protocol: TCP
    livenessprobe:
      probehandler:
        httpget:
          path: /health
          port:
            intval: 8435
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
    volumemounts:
      - name: config-out
        mountpath: /opt/vmauth
    terminationmessagepolicy: FallbackToLogsOnError
serviceaccountname: vmauth-auth

`)
}

func TestBuildIngressForAuthOk(t *testing.T) {
	f := func(cr *vmv1beta1.VMAuth, wantYaml string) {
		t.Helper()

		scheme := k8stools.GetTestClientWithObjects(nil).Scheme()
		build.AddDefaults(scheme)
		scheme.Default(cr)
		var wantSpec networkingv1.IngressSpec
		if err := yaml.Unmarshal([]byte(wantYaml), &wantSpec); err != nil {
			t.Fatalf("not expected wantYaml: %q: \n%q", wantYaml, err)
		}
		wantYAMLForCompare, err := yaml.Marshal(wantSpec)
		if err != nil {
			t.Fatalf("BUG: cannot parse as yaml: %q", err)
		}
		got := buildIngressConfig(cr)
		gotYAML, err := yaml.Marshal(got.Spec)
		if err != nil {
			t.Fatalf("cannot parse got as yaml: %q", err)
		}
		assert.Equal(t, string(wantYAMLForCompare), string(gotYAML))
	}

	// with default paths
	f(&vmv1beta1.VMAuth{
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
			Ingress: &vmv1beta1.EmbeddedIngress{
				Host: "example.com",
			},
		},
	}, `
rules:
- host: example.com
  ingressrulevalue:
    http:
      paths:
      - path: /
        pathtype: Prefix
        backend:
          service:
            name: vmauth-auth
            port:
              name: http
`)

	// with custom paths
	f(&vmv1beta1.VMAuth{
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
			Ingress: &vmv1beta1.EmbeddedIngress{
				Host: "example.com",
				Paths: []string{
					"/test",
				},
			},
		},
	}, `
rules:
- host: example.com
  ingressrulevalue:
    http:
      paths:
      - path: /test
        pathtype: Prefix
        backend:
          service:
            name: vmauth-auth
            port:
              name: http
`)
}
