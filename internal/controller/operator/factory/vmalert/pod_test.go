package vmalert

import (
	"path"
	"reflect"
	"testing"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

func TestBuildNotifiers(t *testing.T) {
	type args struct {
		cr          *vmv1beta1.VMAlert
		ntBasicAuth map[string]*authSecret
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{
			name: "ok build args",
			args: args{
				cr: &vmv1beta1.VMAlert{
					Spec: vmv1beta1.VMAlertSpec{Notifiers: []vmv1beta1.VMAlertNotifierSpec{
						{
							URL: "http://am-1",
						},
						{
							URL: "http://am-2",
							HTTPAuth: vmv1beta1.HTTPAuth{
								TLSConfig: &vmv1beta1.TLSConfig{
									CAFile:             "/tmp/ca.cert",
									InsecureSkipVerify: true,
									KeyFile:            "/tmp/key.pem",
									CertFile:           "/tmp/cert.pem",
								},
							},
						},
						{
							URL: "http://am-3",
						},
					}},
				},
			},
			want: []string{"-notifier.url=http://am-1,http://am-2,http://am-3", "-notifier.tlsKeyFile=,/tmp/key.pem,", "-notifier.tlsCertFile=,/tmp/cert.pem,", "-notifier.tlsCAFile=,/tmp/ca.cert,", "-notifier.tlsInsecureSkipVerify=false,true,false"},
		},
		{
			name: "ok build args with config",
			args: args{
				cr: &vmv1beta1.VMAlert{
					Spec: vmv1beta1.VMAlertSpec{
						NotifierConfigRef: &corev1.SecretKeySelector{
							Key: "cfg.yaml",
						},
					},
				},
			},
			want: []string{"-notifier.config=" + notifierConfigMountPath + "/cfg.yaml"},
		},
		{
			name: "with headers and oauth2",
			args: args{
				cr: &vmv1beta1.VMAlert{
					Spec: vmv1beta1.VMAlertSpec{
						Notifiers: []vmv1beta1.VMAlertNotifierSpec{
							{
								URL: "http://1",
								HTTPAuth: vmv1beta1.HTTPAuth{
									Headers: []string{"key=value", "key2=value2"},
									OAuth2: &vmv1beta1.OAuth2{
										Scopes:       []string{"1", "2"},
										TokenURL:     "http://some-url",
										ClientSecret: &corev1.SecretKeySelector{},
										ClientID:     vmv1beta1.SecretOrConfigMap{},
									},
								},
							},
							{
								URL: "http://2",
								HTTPAuth: vmv1beta1.HTTPAuth{
									Headers:    []string{"key3=value3", "key4=value4"},
									BearerAuth: &vmv1beta1.BearerAuth{},
								},
							},
						},
					},
				},
				ntBasicAuth: map[string]*authSecret{"notifier-0": {OAuthCreds: &k8stools.OAuthCreds{ClientSecret: "some-secret", ClientID: "some-id"}}, "notifier-1": {bearerValue: "some-v"}},
			},
			want: []string{"-notifier.url=http://1,http://2", "-notifier.headers=key=value^^key2=value2,key3=value3^^key4=value4", "-notifier.bearerTokenFile=,/etc/vmalert/remote_secrets/NOTIFIER-1_BEARERTOKEN", "-notifier.oauth2.clientSecretFile=/etc/vmalert/remote_secrets/NOTIFIER-0_OAUTH2SECRETKEY,", "-notifier.oauth2.clientID=some-id,", "-notifier.oauth2.scopes=1,2,", "-notifier.oauth2.tokenUrl=http://some-url,"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildNotifiersArgs(tt.args.cr, tt.args.ntBasicAuth); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("BuildNotifiersArgs() = \ngot \n%v, \nwant \n%v", got, tt.want)
			}
		})
	}
}

func Test_buildArgs(t *testing.T) {
	type args struct {
		cr                 *vmv1beta1.VMAlert
		ruleConfigMapNames []string
		remoteSecrets      map[string]*authSecret
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{
			name: "basic args",
			args: args{
				cr: &vmv1beta1.VMAlert{
					Spec: vmv1beta1.VMAlertSpec{
						Datasource: vmv1beta1.VMAlertDatasourceSpec{
							URL: "http://vmsingle-url",
						},
					},
				},
				ruleConfigMapNames: []string{"first-rule-cm.yaml"},
				remoteSecrets:      map[string]*authSecret{},
			},
			want: []string{"-datasource.url=http://vmsingle-url", "-httpListenAddr=:", "-notifier.url=", "-rule=\"/etc/vmalert/config/first-rule-cm.yaml/*.yaml\""},
		},
		{
			name: "with tls args",
			args: args{
				cr: &vmv1beta1.VMAlert{
					Spec: vmv1beta1.VMAlertSpec{
						Datasource: vmv1beta1.VMAlertDatasourceSpec{
							URL: "http://vmsingle-url",
							HTTPAuth: vmv1beta1.HTTPAuth{
								Headers: []string{"x-org-id:one", "x-org-tenant:5"},
								TLSConfig: &vmv1beta1.TLSConfig{
									InsecureSkipVerify: true,
									KeyFile:            "/path/to/key",
									CAFile:             "/path/to/sa",
								},
							},
						},
					},
				},
				ruleConfigMapNames: []string{"first-rule-cm.yaml"},
				remoteSecrets:      map[string]*authSecret{},
			},
			want: []string{"--datasource.headers=x-org-id:one^^x-org-tenant:5", "-datasource.tlsCAFile=/path/to/sa", "-datasource.tlsInsecureSkipVerify=true", "-datasource.tlsKeyFile=/path/to/key", "-datasource.url=http://vmsingle-url", "-httpListenAddr=:", "-notifier.url=", "-rule=\"/etc/vmalert/config/first-rule-cm.yaml/*.yaml\""},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildArgs(tt.args.cr, tt.args.ruleConfigMapNames, tt.args.remoteSecrets); !reflect.DeepEqual(got, tt.want) {
				assert.Equal(t, tt.want, got)
				t.Errorf("buildArgs() got = \n%v\n, want \n%v\n", got, tt.want)
			}
		})
	}
}

func TestBuildConfigReloaderContainer(t *testing.T) {
	f := func(cr *vmv1beta1.VMAlert, cmNames []string, expectedContainer corev1.Container) {
		t.Helper()
		var extraVolumes []corev1.VolumeMount
		for _, cm := range cr.Spec.ConfigMaps {
			extraVolumes = append(extraVolumes, corev1.VolumeMount{
				Name:      k8stools.SanitizeVolumeName("configmap-" + cm),
				ReadOnly:  true,
				MountPath: path.Join(vmv1beta1.ConfigMapsDir, cm),
			})
		}
		got := buildConfigReloaderContainer(cr, cmNames, extraVolumes)
		assert.Equal(t, expectedContainer, got)
	}

	// base case
	cr := &vmv1beta1.VMAlert{}
	cmNames := []string{"cm-0", "cm-1"}
	expected := corev1.Container{
		Name: "config-reloader",
		Args: []string{
			"-webhook-url=http://localhost:/-/reload",
			"-volume-dir=/etc/vmalert/config/cm-0",
			"-volume-dir=/etc/vmalert/config/cm-1",
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "cm-0",
				MountPath: "/etc/vmalert/config/cm-0",
			},
			{
				Name:      "cm-1",
				MountPath: "/etc/vmalert/config/cm-1",
			},
		},
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}
	f(cr, cmNames, expected)

	// vm config-reloader
	cr = &vmv1beta1.VMAlert{
		Spec: vmv1beta1.VMAlertSpec{
			CommonConfigReloaderParams: vmv1beta1.CommonConfigReloaderParams{
				UseVMConfigReloader: ptr.To(true),
			},
		},
	}
	cmNames = []string{"cm-0"}
	expected = corev1.Container{
		Name: "config-reloader",
		Args: []string{
			"--reload-url=http://localhost:/-/reload",
			"--watched-dir=/etc/vmalert/config/cm-0",
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "cm-0",
				MountPath: "/etc/vmalert/config/cm-0",
			},
		},
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		Ports: []corev1.ContainerPort{
			{
				Name:          "reloader-http",
				Protocol:      corev1.ProtocolTCP,
				ContainerPort: 8435,
			},
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/health",
					Port:   intstr.FromInt32(8435),
					Scheme: "HTTP",
				},
			},
			TimeoutSeconds:   1,
			PeriodSeconds:    10,
			SuccessThreshold: 1,
			FailureThreshold: 3,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/health",
					Port:   intstr.FromInt32(8435),
					Scheme: "HTTP",
				},
			},
			InitialDelaySeconds: 5,
			TimeoutSeconds:      1,
			PeriodSeconds:       10,
			SuccessThreshold:    1,
			FailureThreshold:    3,
		},
	}
	f(cr, cmNames, expected)

	// extra volumes
	cr = &vmv1beta1.VMAlert{
		Spec: vmv1beta1.VMAlertSpec{
			CommonConfigReloaderParams: vmv1beta1.CommonConfigReloaderParams{
				UseVMConfigReloader: ptr.To(true),
			},
			CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
				ConfigMaps: []string{"extra-template-1", "extra-template-2"},
			},
		},
	}
	cmNames = []string{"cm-0"}
	expected = corev1.Container{
		Name: "config-reloader",
		Args: []string{
			"--reload-url=http://localhost:/-/reload",
			"--watched-dir=/etc/vmalert/config/cm-0",
			"--watched-dir=/etc/vm/configs/extra-template-1",
			"--watched-dir=/etc/vm/configs/extra-template-2",
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "cm-0",
				MountPath: "/etc/vmalert/config/cm-0",
			},
			{
				Name:      "configmap-extra-template-1",
				ReadOnly:  true,
				MountPath: "/etc/vm/configs/extra-template-1",
			},
			{
				Name:      "configmap-extra-template-2",
				ReadOnly:  true,
				MountPath: "/etc/vm/configs/extra-template-2",
			},
		},
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		Ports: []corev1.ContainerPort{
			{
				Name:          "reloader-http",
				Protocol:      corev1.ProtocolTCP,
				ContainerPort: 8435,
			},
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/health",
					Port:   intstr.FromInt32(8435),
					Scheme: "HTTP",
				},
			},
			TimeoutSeconds:   1,
			PeriodSeconds:    10,
			SuccessThreshold: 1,
			FailureThreshold: 3,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/health",
					Port:   intstr.FromInt32(8435),
					Scheme: "HTTP",
				},
			},
			InitialDelaySeconds: 5,
			TimeoutSeconds:      1,
			PeriodSeconds:       10,
			SuccessThreshold:    1,
			FailureThreshold:    3,
		},
	}
	f(cr, cmNames, expected)
}
