package build

import (
	"path"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

type testBuildProbeCR struct {
	ep              *vmv1beta1.EmbeddedProbes
	probePath       func() string
	port            string
	scheme          string
	needAddLiveness bool
}

func (t testBuildProbeCR) Probe() *vmv1beta1.EmbeddedProbes {
	return t.ep
}

func (t testBuildProbeCR) ProbePath() string {
	return t.probePath()
}

func (t testBuildProbeCR) ProbeScheme() string {
	return t.scheme
}

func (t testBuildProbeCR) ProbePort() string {
	return t.port
}

func (t testBuildProbeCR) ProbeNeedLiveness() bool {
	return t.needAddLiveness
}

func Test_buildProbe(t *testing.T) {
	type opts struct {
		container corev1.Container
		cr        testBuildProbeCR
		validate  func(corev1.Container)
	}
	f := func(o opts) {
		t.Helper()
		got := Probe(o.container, o.cr)
		o.validate(got)
	}

	// build default probe with empty ep
	f(opts{
		cr: testBuildProbeCR{
			probePath: func() string {
				return "/health"
			},
			port:            "8051",
			needAddLiveness: true,
			scheme:          "HTTP",
		},
		container: corev1.Container{},
		validate: func(container corev1.Container) {
			assert.NotNil(t, container.LivenessProbe)
			assert.NotNil(t, container.ReadinessProbe)
			assert.Equal(t, corev1.URIScheme("HTTP"), container.ReadinessProbe.HTTPGet.Scheme)
		},
	})

	// build default probe with empty ep using HTTPS
	f(opts{
		cr: testBuildProbeCR{
			probePath: func() string {
				return "/health"
			},
			port:            "8051",
			needAddLiveness: true,
			scheme:          "HTTPS",
		},
		container: corev1.Container{},
		validate: func(container corev1.Container) {
			assert.NotNil(t, container.LivenessProbe)
			assert.Equal(t, corev1.URIScheme("HTTPS"), container.LivenessProbe.HTTPGet.Scheme)
			assert.NotNil(t, container.ReadinessProbe)
			assert.Equal(t, corev1.URIScheme("HTTPS"), container.ReadinessProbe.HTTPGet.Scheme)
		},
	})

	// build default probe with ep
	f(opts{
		cr: testBuildProbeCR{
			probePath: func() string {
				return "/health"
			},
			port:            "8051",
			needAddLiveness: true,
			ep: &vmv1beta1.EmbeddedProbes{
				ReadinessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						Exec: &corev1.ExecAction{
							Command: []string{"echo", "1"},
						},
					},
				},
				StartupProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Host: "some",
						},
					},
				},
				LivenessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/live1",
						},
					},
					TimeoutSeconds:      15,
					InitialDelaySeconds: 20,
				},
			},
		},
		container: corev1.Container{},
		validate: func(container corev1.Container) {
			assert.NotNil(t, container.LivenessProbe)
			assert.Equal(t, "/live1", container.LivenessProbe.HTTPGet.Path)
			assert.Equal(t, int32(20), container.LivenessProbe.InitialDelaySeconds)
			assert.Equal(t, int32(15), container.LivenessProbe.TimeoutSeconds)
			assert.NotNil(t, container.ReadinessProbe)
			assert.Len(t, container.ReadinessProbe.Exec.Command, 2)
			assert.NotNil(t, container.StartupProbe)
			assert.Equal(t, "some", container.StartupProbe.HTTPGet.Host)
		},
	})
}

func Test_addExtraArgsOverrideDefaults(t *testing.T) {
	type opts struct {
		args      []string
		extraArgs map[string]string
		dashes    string
		want      []string
	}
	f := func(o opts) {
		t.Helper()
		assert.Equalf(
			t,
			o.want,
			AddExtraArgsOverrideDefaults(o.args, o.extraArgs, o.dashes),
			"addExtraArgsOverrideDefaults(%v, %v)", o.args, o.extraArgs)
	}

	// no changes
	f(opts{
		args:   []string{"-http.ListenAddr=:8081"},
		dashes: "-",
		want:   []string{"-http.ListenAddr=:8081"},
	})

	// override default
	f(opts{
		args:      []string{"-http.ListenAddr=:8081"},
		extraArgs: map[string]string{"http.ListenAddr": "127.0.0.1:8085"},
		dashes:    "-",
		want:      []string{"-http.ListenAddr=127.0.0.1:8085"},
	})

	// override default, add to the end
	f(opts{
		args:      []string{"-http.ListenAddr=:8081", "-promscrape.config=/opt/vmagent.yml"},
		extraArgs: map[string]string{"http.ListenAddr": "127.0.0.1:8085"},
		dashes:    "-",
		want:      []string{"-promscrape.config=/opt/vmagent.yml", "-http.ListenAddr=127.0.0.1:8085"},
	})

	// two dashes, extend
	f(opts{
		args:      []string{"--web.timeout=0"},
		extraArgs: map[string]string{"log.level": "debug"},
		dashes:    "--",
		want:      []string{"--web.timeout=0", "--log.level=debug"},
	})

	// two dashes, override default
	f(opts{
		args:      []string{"--log.level=info"},
		extraArgs: map[string]string{"log.level": "debug"},
		dashes:    "--",
		want:      []string{"--log.level=debug"},
	})

	// two dashes, alertmanager migration
	f(opts{
		args:      []string{"--log.level=info"},
		extraArgs: map[string]string{"-web.externalURL": "http://domain.example"},
		dashes:    "--",
		want:      []string{"--log.level=info", "--web.externalURL=http://domain.example"},
	})
}

func TestAddHTTPShutdownDelayArg(t *testing.T) {
	type opts struct {
		extraArgs                     map[string]string
		terminationGracePeriodSeconds *int64
		isNewResource                 bool
		wantArgs                      []string
	}
	f := func(opts opts) {
		t.Helper()
		assert.Equal(
			t,
			AddHTTPShutdownDelayArg(nil, opts.extraArgs, opts.terminationGracePeriodSeconds, opts.isNewResource),
			opts.wantArgs,
		)
	}
	// new resource, no explicit settings
	f(opts{
		extraArgs:                     nil,
		terminationGracePeriodSeconds: nil,
		isNewResource:                 true,
		wantArgs:                      []string{"-http.shutdownDelay=30s"},
	})
	f(opts{
		extraArgs:                     nil,
		terminationGracePeriodSeconds: nil,
		isNewResource:                 false,
		wantArgs:                      nil,
	})
	//if extraArg already exists, no more args should be added, it will be added later in the process of adding extra args
	f(opts{
		extraArgs:                     map[string]string{"http.shutdownDelay": "5s"},
		terminationGracePeriodSeconds: nil,
		isNewResource:                 true,
		wantArgs:                      nil,
	})
	// new resource with explicit terminationGracePeriodSeconds
	f(opts{
		extraArgs:                     nil,
		terminationGracePeriodSeconds: ptr.To[int64](60),
		isNewResource:                 true,
		wantArgs:                      []string{"-http.shutdownDelay=60s"},
	})
	// existing resource with explicit terminationGracePeriodSeconds
	f(opts{
		extraArgs:                     nil,
		terminationGracePeriodSeconds: ptr.To[int64](120),
		isNewResource:                 false,
		wantArgs:                      nil,
	})
	f(opts{
		extraArgs:                     map[string]string{"http.shutdownDelay": "20s"},
		terminationGracePeriodSeconds: ptr.To[int64](120),
		isNewResource:                 true,
		wantArgs:                      nil,
	})
}

func TestFormatContainerImage(t *testing.T) {
	f := func(globalRepo, image, wantImage string) {
		t.Helper()
		gotImage := formatContainerImage(globalRepo, image)
		assert.Equal(t, wantImage, gotImage)
	}
	f("", "victoria-metrics/storage", "victoria-metrics/storage")
	f("docker.io", "victoria-metrics/storage", "docker.io/victoria-metrics/storage")
	// strip quay and replace with global repo
	f("docker.io", "quay.io/prometheus-operator/prometheus-config-reloader:v0.82.1", "quay.io/prometheus-operator/prometheus-config-reloader:v0.82.1")
	f("private.github.io", "victoria-metrics/storage", "private.github.io/victoria-metrics/storage")
	// for private repo
	f("private.github.io", "quay.io/prometheus-operator/prometheus-config-reloader:v0.82.1", "quay.io/prometheus-operator/prometheus-config-reloader:v0.82.1")
	// edge case
	f("private.github.io", "quay.io/victoria-metrics/storage", "quay.io/victoria-metrics/storage")
	// replace registry
	f("private.github.io", "my-private.registry/victoria-metrics/storage", "my-private.registry/victoria-metrics/storage")
	f("docker.proxy.org/public-repo-proxy-docker-hub", "docker.proxy.org/public-repo-proxy-docker-hub/victoriametrics/vmauth", "docker.proxy.org/public-repo-proxy-docker-hub/victoriametrics/vmauth")
	f("docker.io", "alpine", "docker.io/library/alpine")
}

func TestAddSyslogArgsTo(t *testing.T) {
	f := func(syslogSpec *vmv1.SyslogServerSpec, wantArgs []string) {
		t.Helper()
		args := AddSyslogArgsTo(nil, syslogSpec, "/etc/vm/tls-server-secrets")
		sort.Strings(args)
		sort.Strings(wantArgs)
		assert.Equal(t, wantArgs, args)
	}
	f(nil, nil)
	// multiple tcp listeners
	spec := vmv1.SyslogServerSpec{
		TCPListeners: []*vmv1.SyslogTCPListener{
			{
				ListenPort: 3001,
			},
			{
				ListenPort:   3002,
				StreamFields: vmv1.FieldsListString(`["msg_1","msg_2"]`),
			},
		},
	}
	expected := []string{
		"-syslog.listenAddr.tcp=:3001,:3002",
		`-syslog.streamFields.tcp='','["msg_1","msg_2"]'`,
	}
	f(&spec, expected)

	// multiple udp listeners
	spec = vmv1.SyslogServerSpec{
		UDPListeners: []*vmv1.SyslogUDPListener{
			{
				ListenPort:   3001,
				IgnoreFields: vmv1.FieldsListString(`["ignore_1"]`),
			},
			{
				ListenPort:   3002,
				StreamFields: vmv1.FieldsListString(`["msg_1","msg_2"]`),
			},
			{
				ListenPort: 3005,
			},
		},
	}
	expected = []string{
		"-syslog.listenAddr.udp=:3001,:3002,:3005",
		`-syslog.streamFields.udp='','["msg_1","msg_2"]',''`,
		`-syslog.ignoreFields.udp='["ignore_1"]','',''`,
	}
	f(&spec, expected)

	// mixed udp and tcp
	// multiple udp listeners
	spec = vmv1.SyslogServerSpec{
		TCPListeners: []*vmv1.SyslogTCPListener{
			{
				ListenPort: 3001,
			},
			{
				ListenPort:   3002,
				StreamFields: vmv1.FieldsListString(`["msg_1","msg_2"]`),
			},
		},
		UDPListeners: []*vmv1.SyslogUDPListener{
			{
				ListenPort:   3001,
				IgnoreFields: vmv1.FieldsListString(`["ignore_1"]`),
			},
			{
				ListenPort:   3002,
				StreamFields: vmv1.FieldsListString(`["msg_1","msg_2"]`),
			},
			{
				ListenPort: 3005,
			},
		},
	}
	expected = []string{
		"-syslog.listenAddr.tcp=:3001,:3002",
		`-syslog.streamFields.tcp='','["msg_1","msg_2"]'`,
		"-syslog.listenAddr.udp=:3001,:3002,:3005",
		`-syslog.streamFields.udp='','["msg_1","msg_2"]',''`,
		`-syslog.ignoreFields.udp='["ignore_1"]','',''`,
	}
	f(&spec, expected)

	// with tls
	spec = vmv1.SyslogServerSpec{
		TCPListeners: []*vmv1.SyslogTCPListener{
			{
				ListenPort: 3001,
				TenantID:   "10:25",
			},
			{
				ListenPort:   3002,
				StreamFields: vmv1.FieldsListString(`["msg_1","msg_2"]`),
				TLSConfig: &vmv1.TLSServerConfig{
					CertSecret: &corev1.SecretKeySelector{
						Key: "CERT",
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "tls",
						},
					},
					KeyFile: "/etc/vm/secrets/tls/key",
				},
			},
		},
		UDPListeners: []*vmv1.SyslogUDPListener{
			{
				ListenPort:     3001,
				CompressMethod: "zstd",
			},
		},
	}
	expected = []string{
		"-syslog.listenAddr.tcp=:3001,:3002",
		`-syslog.streamFields.tcp='','["msg_1","msg_2"]'`,
		`-syslog.tenantID.tcp=10:25,`,
		"-syslog.tls=,true",
		"-syslog.tlsCertFile=,/etc/vm/tls-server-secrets/tls/CERT",
		"-syslog.tlsKeyFile=,/etc/vm/secrets/tls/key",
		"-syslog.listenAddr.udp=:3001",
		"-syslog.compressMethod.udp=zstd",
	}
	f(&spec, expected)
}

func TestStorageVolumeMountsTo(t *testing.T) {
	type opts struct {
		pvcSrc          *corev1.PersistentVolumeClaimVolumeSource
		storagePath     string
		volumes         []corev1.Volume
		expectedVolumes []corev1.Volume
		mounts          []corev1.VolumeMount
		expectedMounts  []corev1.VolumeMount
		wantErr         bool
	}
	f := func(o opts) {
		t.Helper()
		gotVolumes, gotMounts, err := StorageVolumeMountsTo(o.volumes, o.mounts, o.pvcSrc, o.storagePath, DataVolumeName)
		assert.Equal(t, o.expectedMounts, gotMounts)
		assert.Equal(t, o.expectedVolumes, gotVolumes)
		if o.wantErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
	}

	// no PVC spec and no volumes and mounts
	f(opts{
		storagePath: "/test",
		expectedVolumes: []corev1.Volume{{
			Name: DataVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		}},
		expectedMounts: []corev1.VolumeMount{{
			Name:      DataVolumeName,
			MountPath: "/test",
		}},
	})

	// with PVC spec and no volumes and mounts
	f(opts{
		storagePath: "/test",
		pvcSrc: &corev1.PersistentVolumeClaimVolumeSource{
			ClaimName: "test-claim",
		},
		expectedVolumes: []corev1.Volume{{
			Name: DataVolumeName,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: "test-claim",
				},
			},
		}},
		expectedMounts: []corev1.VolumeMount{{
			Name:      DataVolumeName,
			MountPath: "/test",
		}},
	})

	// with PVC spec and matching data volume
	f(opts{
		volumes: []corev1.Volume{{
			Name: DataVolumeName,
			VolumeSource: corev1.VolumeSource{
				AWSElasticBlockStore: &corev1.AWSElasticBlockStoreVolumeSource{
					VolumeID: "aws-volume",
				},
			},
		}},
		storagePath: "/test",
		pvcSrc: &corev1.PersistentVolumeClaimVolumeSource{
			ClaimName: "test-claim",
		},
		wantErr: true,
	})

	// with PVC spec and not matching data volume
	f(opts{
		volumes: []corev1.Volume{{
			Name: "extra",
			VolumeSource: corev1.VolumeSource{
				AWSElasticBlockStore: &corev1.AWSElasticBlockStoreVolumeSource{
					VolumeID: "aws-volume",
				},
			},
		}},
		storagePath: "/test",
		pvcSrc: &corev1.PersistentVolumeClaimVolumeSource{
			ClaimName: "test-claim",
		},
		expectedVolumes: []corev1.Volume{
			{
				Name: DataVolumeName,
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: "test-claim",
					},
				},
			},
			{
				Name: "extra",
				VolumeSource: corev1.VolumeSource{
					AWSElasticBlockStore: &corev1.AWSElasticBlockStoreVolumeSource{
						VolumeID: "aws-volume",
					},
				},
			},
		},
		expectedMounts: []corev1.VolumeMount{{
			Name:      DataVolumeName,
			MountPath: "/test",
		}},
	})

	// with PVC spec and existing data volume mount
	f(opts{
		volumes: []corev1.Volume{{
			Name: "extra",
			VolumeSource: corev1.VolumeSource{
				AWSElasticBlockStore: &corev1.AWSElasticBlockStoreVolumeSource{
					VolumeID: "aws-volume",
				},
			},
		}},
		mounts: []corev1.VolumeMount{{
			Name:      DataVolumeName,
			MountPath: "/other-path",
		}},
		wantErr:     true,
		storagePath: "/test",
		pvcSrc: &corev1.PersistentVolumeClaimVolumeSource{
			ClaimName: "test-claim",
		},
	})

	// with PVC spec and intersecting data volume mount
	f(opts{
		volumes: []corev1.Volume{{
			Name: "extra",
			VolumeSource: corev1.VolumeSource{
				AWSElasticBlockStore: &corev1.AWSElasticBlockStoreVolumeSource{
					VolumeID: "aws-volume",
				},
			},
		}},
		mounts: []corev1.VolumeMount{{
			Name:      DataVolumeName,
			MountPath: "/test",
		}},
		storagePath: "/test/data",
		pvcSrc: &corev1.PersistentVolumeClaimVolumeSource{
			ClaimName: "test-claim",
		},
		expectedVolumes: []corev1.Volume{
			{
				Name: DataVolumeName,
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: "test-claim",
					},
				},
			},
			{
				Name: "extra",
				VolumeSource: corev1.VolumeSource{
					AWSElasticBlockStore: &corev1.AWSElasticBlockStoreVolumeSource{
						VolumeID: "aws-volume",
					},
				},
			},
		},
		expectedMounts: []corev1.VolumeMount{{
			Name:      DataVolumeName,
			MountPath: "/test",
		}},
	})

	// with PVC spec and intersecting volume mount and absent volume
	f(opts{
		volumes: []corev1.Volume{{
			Name: "test",
			VolumeSource: corev1.VolumeSource{
				AWSElasticBlockStore: &corev1.AWSElasticBlockStoreVolumeSource{
					VolumeID: "aws-volume",
				},
			},
		}},
		mounts: []corev1.VolumeMount{{
			Name:      "test",
			MountPath: "/test",
		}},
		storagePath: "/test/data",
		pvcSrc: &corev1.PersistentVolumeClaimVolumeSource{
			ClaimName: "test-claim",
		},
		wantErr: true,
	})
}

func TestBuildConfigReloaderContainer(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMAlert
		cmNames           []string
		expectedContainer corev1.Container
		cfgMutator        func(*config.BaseOperatorConf)
	}
	f := func(o opts) {
		t.Helper()
		cfg := config.MustGetBaseConfig()
		if o.cfgMutator != nil {
			defaultCfg := *cfg
			o.cfgMutator(cfg)
			defer func() {
				*config.MustGetBaseConfig() = defaultCfg
			}()
		}
		var extraMounts []corev1.VolumeMount
		for _, cm := range o.cr.Spec.ConfigMaps {
			extraMounts = append(extraMounts, corev1.VolumeMount{
				Name:      k8stools.SanitizeVolumeName("configmap-" + cm),
				ReadOnly:  true,
				MountPath: path.Join(vmv1beta1.ConfigMapsDir, cm),
			})
		}
		for _, cm := range o.cmNames {
			extraMounts = append(extraMounts, corev1.VolumeMount{
				Name:      cm,
				MountPath: path.Join("/cm-dir", cm),
			})
		}
		got := ConfigReloaderContainer(false, o.cr, extraMounts, nil)
		assert.Equal(t, o.expectedContainer, got)
	}

	// base case
	f(opts{
		cr: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "base",
			},
			Spec: vmv1beta1.VMAlertSpec{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ExtraArgs: map[string]string{
						"reloadAuthKey": "test",
					},
				},
			},
		},
		cmNames: []string{"cm-0", "cm-1"},
		cfgMutator: func(c *config.BaseOperatorConf) {
			c.EnableTCP6 = true
		},
		expectedContainer: corev1.Container{
			Name: "config-reloader",
			Args: []string{
				"--enableTCP6",
				"--reload-url=http://localhost:/-/reload?authKey=test",
				"--watched-dir=/cm-dir/cm-0",
				"--watched-dir=/cm-dir/cm-1",
				"--webhook-method=POST",
			},
			Ports: []corev1.ContainerPort{{
				Name:          "reloader-http",
				ContainerPort: 8435,
				Protocol:      "TCP",
			}},
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
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      "cm-0",
					MountPath: "/cm-dir/cm-0",
				},
				{
					Name:      "cm-1",
					MountPath: "/cm-dir/cm-1",
				},
			},
			TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		},
	})

	// vm config-reloader
	f(opts{
		cr: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "base",
			},
		},
		cmNames: []string{"cm-0"},
		expectedContainer: corev1.Container{
			Name: "config-reloader",
			Args: []string{
				"--reload-url=http://127.0.0.1:/-/reload",
				"--watched-dir=/cm-dir/cm-0",
				"--webhook-method=POST",
			},
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      "cm-0",
					MountPath: "/cm-dir/cm-0",
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
		},
	})

	// extra volumes
	f(opts{
		cr: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "base",
			},
			Spec: vmv1beta1.VMAlertSpec{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ConfigMaps: []string{"extra-template-1", "extra-template-2"},
				},
			},
		},
		cmNames: []string{"cm-0"},
		expectedContainer: corev1.Container{
			Name: "config-reloader",
			Args: []string{
				"--reload-url=http://127.0.0.1:/-/reload",
				"--watched-dir=/cm-dir/cm-0",
				"--watched-dir=/etc/vm/configs/extra-template-1",
				"--watched-dir=/etc/vm/configs/extra-template-2",
				"--webhook-method=POST",
			},
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      "cm-0",
					MountPath: "/cm-dir/cm-0",
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
		},
	})
}
