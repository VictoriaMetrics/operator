package vmauth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
		validate          func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAuth)
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
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		err := CreateOrUpdate(ctx, o.cr, fclient)
		if o.wantErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
		if o.validate != nil {
			o.validate(ctx, fclient, o.cr)
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
			Status: vmv1beta1.VMAuthStatus{
				LastAppliedSpec: &vmv1beta1.VMAuthSpec{
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

	// create VPA
	f(opts{
		cr: &vmv1beta1.VMAuth{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec: vmv1beta1.VMAuthSpec{
				VPA: &vmv1beta1.EmbeddedVPA{
					UpdatePolicy: &vpav1.PodUpdatePolicy{
						UpdateMode: ptr.To(vpav1.UpdateModeInitial),
					},
					ResourcePolicy: &vpav1.PodResourcePolicy{
						ContainerPolicies: []vpav1.ContainerResourcePolicy{
							{ContainerName: "vmauth"},
						},
					},
				},
			},
		},
		cfgMutator: func(c *config.BaseOperatorConf) {
			c.VPAAPIEnabled = true
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAuth) {
			var got vpav1.VerticalPodAutoscaler
			vpaName := cr.PrefixedName()
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: vpaName}, &got))
			expected := vpav1.VerticalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:            vpaName,
					Namespace:       cr.Namespace,
					Labels:          cr.FinalLabels(),
					ResourceVersion: "1",
					OwnerReferences: []metav1.OwnerReference{{Name: "test", Controller: ptr.To(true), BlockOwnerDeletion: ptr.To(true)}},
				},
				Spec: vpav1.VerticalPodAutoscalerSpec{
					TargetRef: &autoscalingv1.CrossVersionObjectReference{
						Name:       vpaName,
						Kind:       "Deployment",
						APIVersion: "apps/v1",
					},
					UpdatePolicy: &vpav1.PodUpdatePolicy{
						UpdateMode: ptr.To(vpav1.UpdateModeInitial),
					},
					ResourcePolicy: &vpav1.PodResourcePolicy{
						ContainerPolicies: []vpav1.ContainerResourcePolicy{
							{ContainerName: "vmauth"},
						},
					},
				},
			}
			assert.Equal(t, got, expected)
		},
	})

	// update VPA
	f(opts{
		predefinedObjects: []runtime.Object{
			&vpav1.VerticalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmauth-test",
					Namespace: "default",
				},
				Spec: vpav1.VerticalPodAutoscalerSpec{
					TargetRef: &autoscalingv1.CrossVersionObjectReference{
						Name:       "vmauth-test",
						Kind:       "Deployment",
						APIVersion: "apps/v1",
					},
					UpdatePolicy: &vpav1.PodUpdatePolicy{
						UpdateMode: ptr.To(vpav1.UpdateModeInitial),
					},
					ResourcePolicy: &vpav1.PodResourcePolicy{
						ContainerPolicies: []vpav1.ContainerResourcePolicy{
							{ContainerName: "vmauth"},
						},
					},
				},
			},
		},
		cr: &vmv1beta1.VMAuth{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec: vmv1beta1.VMAuthSpec{
				VPA: &vmv1beta1.EmbeddedVPA{
					UpdatePolicy: &vpav1.PodUpdatePolicy{
						UpdateMode: ptr.To(vpav1.UpdateModeRecreate),
					},
					ResourcePolicy: &vpav1.PodResourcePolicy{
						ContainerPolicies: []vpav1.ContainerResourcePolicy{
							{ContainerName: "vmauth"},
						},
					},
				},
			},
		},
		cfgMutator: func(c *config.BaseOperatorConf) {
			c.VPAAPIEnabled = true
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAuth) {
			var got vpav1.VerticalPodAutoscaler
			vpaName := cr.PrefixedName()
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: vpaName}, &got))
			expected := vpav1.VerticalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:            vpaName,
					Namespace:       cr.Namespace,
					Labels:          cr.FinalLabels(),
					ResourceVersion: "1000",
					OwnerReferences: []metav1.OwnerReference{{Name: "test", Controller: ptr.To(true), BlockOwnerDeletion: ptr.To(true)}},
				},
				Spec: vpav1.VerticalPodAutoscalerSpec{
					TargetRef: &autoscalingv1.CrossVersionObjectReference{
						Name:       vpaName,
						Kind:       "Deployment",
						APIVersion: "apps/v1",
					},
					UpdatePolicy: &vpav1.PodUpdatePolicy{
						UpdateMode: ptr.To(vpav1.UpdateModeRecreate),
					},
					ResourcePolicy: &vpav1.PodResourcePolicy{
						ContainerPolicies: []vpav1.ContainerResourcePolicy{
							{ContainerName: "vmauth"},
						},
					},
				},
			}
			assert.Equal(t, got, expected)
		},
	})

	// remove VPA
	f(opts{
		predefinedObjects: []runtime.Object{
			&vpav1.VerticalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "vmauth-test",
					Namespace:       "default",
					ResourceVersion: "1",
					OwnerReferences: []metav1.OwnerReference{{
						Name:               "test",
						Controller:         ptr.To(true),
						BlockOwnerDeletion: ptr.To(true)},
					},
					Labels: map[string]string{
						"app.kubernetes.io/instance":  "test",
						"app.kubernetes.io/component": "monitoring",
						"managed-by":                  "vm-operator",
						"app.kubernetes.io/name":      "vmauth",
					},
				},
				Spec: vpav1.VerticalPodAutoscalerSpec{
					TargetRef: &autoscalingv1.CrossVersionObjectReference{
						Name:       "vmauth-test",
						Kind:       "Deployment",
						APIVersion: "apps/v1",
					},
					UpdatePolicy: &vpav1.PodUpdatePolicy{
						UpdateMode: ptr.To(vpav1.UpdateModeInitial),
					},
					ResourcePolicy: &vpav1.PodResourcePolicy{
						ContainerPolicies: []vpav1.ContainerResourcePolicy{
							{ContainerName: "vmauth"},
						},
					},
				},
			},
		},
		cr: &vmv1beta1.VMAuth{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec:       vmv1beta1.VMAuthSpec{},
			Status: vmv1beta1.VMAuthStatus{
				LastAppliedSpec: &vmv1beta1.VMAuthSpec{},
			},
		},
		cfgMutator: func(c *config.BaseOperatorConf) {
			c.VPAAPIEnabled = true
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAuth) {
			var got vpav1.VerticalPodAutoscaler
			vpaName := cr.PrefixedName()
			err := rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: vpaName}, &got)
			assert.Error(t, err)
			assert.True(t, k8serrors.IsNotFound(err))
		},
	})
}

func TestMakeSpecForAuthOk(t *testing.T) {
	f := func(cr *vmv1beta1.VMAuth, wantYaml string) {
		t.Helper()

		scheme := k8stools.GetTestClientWithObjects(nil).Scheme()
		build.AddDefaults(scheme)
		scheme.Default(cr)
		var wantSpec corev1.PodSpec
		assert.NoError(t, yaml.Unmarshal([]byte(wantYaml), &wantSpec))
		wantYAMLForCompare, err := yaml.Marshal(wantSpec)
		assert.NoError(t, err)
		got, err := makeSpecForVMAuth(cr)
		assert.NoError(t, err)
		gotYAML, err := yaml.Marshal(got.Spec)
		assert.NoError(t, err)
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
      - -http.shutdownDelay=30s
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
      - -http.shutdownDelay=30s
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
		assert.NoError(t, yaml.Unmarshal([]byte(wantYaml), &wantSpec))
		wantYAMLForCompare, err := yaml.Marshal(wantSpec)
		assert.NoError(t, err)
		got := buildIngressConfig(cr)
		gotYAML, err := yaml.Marshal(got.Spec)
		assert.NoError(t, err)
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
