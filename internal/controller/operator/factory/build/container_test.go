package build

import (
	"fmt"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
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
		validate  func(corev1.Container) error
	}
	f := func(opts opts) {
		t.Helper()
		got := Probe(opts.container, opts.cr)
		if err := opts.validate(got); err != nil {
			t.Errorf("buildProbe() unexpected error: %v", err)
		}
	}

	// build default probe with empty ep
	o := opts{
		cr: testBuildProbeCR{
			probePath: func() string {
				return "/health"
			},
			port:            "8051",
			needAddLiveness: true,
			scheme:          "HTTP",
		},
		container: corev1.Container{},
		validate: func(container corev1.Container) error {
			if container.LivenessProbe == nil {
				return fmt.Errorf("want liveness to be not nil")
			}
			if container.ReadinessProbe == nil {
				return fmt.Errorf("want readinessProbe to be not nil")
			}
			if container.ReadinessProbe.HTTPGet.Scheme != "HTTP" {
				return fmt.Errorf("expect scheme to be HTTP got: %s", container.ReadinessProbe.HTTPGet.Scheme)
			}
			return nil
		},
	}
	f(o)

	// build default probe with empty ep using HTTPS
	o = opts{
		cr: testBuildProbeCR{
			probePath: func() string {
				return "/health"
			},
			port:            "8051",
			needAddLiveness: true,
			scheme:          "HTTPS",
		},
		container: corev1.Container{},
		validate: func(container corev1.Container) error {
			if container.LivenessProbe == nil {
				return fmt.Errorf("want liveness to be not nil")
			}
			if container.ReadinessProbe == nil {
				return fmt.Errorf("want readinessProbe to be not nil")
			}
			if container.LivenessProbe.HTTPGet.Scheme != "HTTPS" {
				return fmt.Errorf("expect scheme to be HTTPS got: %s", container.LivenessProbe.HTTPGet.Scheme)
			}
			if container.ReadinessProbe.HTTPGet.Scheme != "HTTPS" {
				return fmt.Errorf("expect scheme to be HTTPS got: %s", container.ReadinessProbe.HTTPGet.Scheme)
			}
			return nil
		},
	}
	f(o)

	// build default probe with ep
	o = opts{
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
		validate: func(container corev1.Container) error {
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
	}
	f(o)
}

func Test_addExtraArgsOverrideDefaults(t *testing.T) {
	type opts struct {
		args      []string
		extraArgs map[string]string
		dashes    string
		want      []string
	}
	f := func(opts opts) {
		t.Helper()
		assert.Equalf(t, opts.want,
			AddExtraArgsOverrideDefaults(opts.args, opts.extraArgs, opts.dashes),
			"addExtraArgsOverrideDefaults(%v, %v)", opts.args, opts.extraArgs)
	}

	// no changes
	o := opts{
		args:   []string{"-http.ListenAddr=:8081"},
		dashes: "-",
		want:   []string{"-http.ListenAddr=:8081"},
	}
	f(o)

	// override default
	o = opts{
		args:      []string{"-http.ListenAddr=:8081"},
		extraArgs: map[string]string{"http.ListenAddr": "127.0.0.1:8085"},
		dashes:    "-",
		want:      []string{"-http.ListenAddr=127.0.0.1:8085"},
	}
	f(o)

	// override default, add to the end
	o = opts{
		args:      []string{"-http.ListenAddr=:8081", "-promscrape.config=/opt/vmagent.yml"},
		extraArgs: map[string]string{"http.ListenAddr": "127.0.0.1:8085"},
		dashes:    "-",
		want:      []string{"-promscrape.config=/opt/vmagent.yml", "-http.ListenAddr=127.0.0.1:8085"},
	}
	f(o)

	// two dashes, extend
	o = opts{
		args:      []string{"--web.timeout=0"},
		extraArgs: map[string]string{"log.level": "debug"},
		dashes:    "--",
		want:      []string{"--web.timeout=0", "--log.level=debug"},
	}
	f(o)

	// two dashes, override default
	o = opts{
		args:      []string{"--log.level=info"},
		extraArgs: map[string]string{"log.level": "debug"},
		dashes:    "--",
		want:      []string{"--log.level=debug"},
	}
	f(o)

	// two dashes, alertmanager migration
	o = opts{
		args:      []string{"--log.level=info"},
		extraArgs: map[string]string{"-web.externalURL": "http://domain.example"},
		dashes:    "--",
		want:      []string{"--log.level=info", "--web.externalURL=http://domain.example"},
	}
	f(o)
}

func TestFormatContainerImage(t *testing.T) {
	f := func(globalRepo, image, wantImage string) {
		t.Helper()
		gotImage := formatContainerImage(globalRepo, image)
		if gotImage != wantImage {
			t.Errorf("unexpected container image, got: \n%s\nwant: \n%s", gotImage, wantImage)
		}
	}
	f("", "victoria-metrics/storage", "victoria-metrics/storage")
	f("docker.io", "victoria-metrics/storage", "docker.io/victoria-metrics/storage")
	// strip quay and replace with global repo
	f("docker.io", "quay.io/prometheus-operator/prometheus-config-reloader:v0.82.1", "docker.io/prometheus-operator/prometheus-config-reloader:v0.82.1")
	f("private.github.io", "victoria-metrics/storage", "private.github.io/victoria-metrics/storage")
	// for private repo
	f("private.github.io", "quay.io/prometheus-operator/prometheus-config-reloader:v0.82.1", "private.github.io/prometheus-operator/prometheus-config-reloader:v0.82.1")
	// edge case
	f("private.github.io", "quay.io/victoria-metrics/storage", "private.github.io/victoria-metrics/storage")
	// correct behaviour, user must fix image naming
	f("private.github.io", "my-private.registry/victoria-metrics/storage", "private.github.io/my-private.registry/victoria-metrics/storage")
}

func TestAddSyslogArgsTo(t *testing.T) {
	type opts struct {
		spec     *vmv1.SyslogServerSpec
		expected []string
	}
	f := func(opts opts) {
		t.Helper()
		args := AddSyslogArgsTo(nil, opts.spec, "/etc/vm/tls-server-secrets")
		sort.Strings(args)
		sort.Strings(opts.expected)
		assert.Equal(t, opts.expected, args)
	}

	// empty
	o := opts{}
	f(o)

	// multiple tcp listeners
	o = opts{
		spec: &vmv1.SyslogServerSpec{
			TCPListeners: []*vmv1.SyslogTCPListener{
				{
					ListenPort: 3001,
				},
				{
					ListenPort:   3002,
					StreamFields: vmv1.FieldsListString(`["msg_1","msg_2"]`),
				},
			},
		},
		expected: []string{
			"-syslog.listenAddr.tcp=:3001,:3002",
			`-syslog.streamFields.tcp='','["msg_1","msg_2"]'`,
		},
	}
	f(o)

	// multiple udp listeners
	o = opts{
		spec: &vmv1.SyslogServerSpec{
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
		},
		expected: []string{
			"-syslog.listenAddr.udp=:3001,:3002,:3005",
			`-syslog.streamFields.udp='','["msg_1","msg_2"]',''`,
			`-syslog.ignoreFields.udp='["ignore_1"]','',''`,
		},
	}
	f(o)

	// mixed udp and tcp
	// multiple udp listeners
	o = opts{
		spec: &vmv1.SyslogServerSpec{
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
		},
		expected: []string{
			"-syslog.listenAddr.tcp=:3001,:3002",
			`-syslog.streamFields.tcp='','["msg_1","msg_2"]'`,
			"-syslog.listenAddr.udp=:3001,:3002,:3005",
			`-syslog.streamFields.udp='','["msg_1","msg_2"]',''`,
			`-syslog.ignoreFields.udp='["ignore_1"]','',''`,
		},
	}
	f(o)

	// with tls
	o = opts{
		spec: &vmv1.SyslogServerSpec{
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
		},
		expected: []string{
			"-syslog.listenAddr.tcp=:3001,:3002",
			`-syslog.streamFields.tcp='','["msg_1","msg_2"]'`,
			`-syslog.tenantID.tcp=10:25,`,
			"-syslog.tls=,true",
			"-syslog.tlsCertFile=,/etc/vm/tls-server-secrets/tls/CERT",
			"-syslog.tlsKeyFile=,/etc/vm/secrets/tls/key",
			"-syslog.listenAddr.udp=:3001",
			"-syslog.compressMethod.udp=zstd",
		},
	}
	f(o)
}

func TestStorageVolumeMountsTo(t *testing.T) {
	type opts struct {
		pvcSrc          *corev1.PersistentVolumeClaimVolumeSource
		volumeName      string
		storagePath     string
		volumes         []corev1.Volume
		expectedVolumes []corev1.Volume
		mounts          []corev1.VolumeMount
		expectedMounts  []corev1.VolumeMount
	}
	f := func(opts opts) {
		t.Helper()
		gotVolumes, gotMounts := StorageVolumeMountsTo(opts.volumes, opts.mounts, opts.pvcSrc, opts.volumeName, opts.storagePath)
		assert.Equal(t, opts.expectedMounts, gotMounts)
		assert.Equal(t, opts.expectedVolumes, gotVolumes)
	}

	// no PVC spec and no volumes and mounts
	o := opts{
		volumeName:  "test",
		storagePath: "/test",
		expectedVolumes: []corev1.Volume{{
			Name: "test",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		}},
		expectedMounts: []corev1.VolumeMount{{
			Name:      "test",
			MountPath: "/test",
		}},
	}
	f(o)

	// with PVC spec and no volumes and mounts
	o = opts{
		volumeName:  "test",
		storagePath: "/test",
		pvcSrc: &corev1.PersistentVolumeClaimVolumeSource{
			ClaimName: "test-claim",
		},
		expectedVolumes: []corev1.Volume{{
			Name: "test",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: "test-claim",
				},
			},
		}},
		expectedMounts: []corev1.VolumeMount{{
			Name:      "test",
			MountPath: "/test",
		}},
	}
	f(o)

	// with PVC spec and matching data volume
	o = opts{
		volumes: []corev1.Volume{{
			Name: "test",
			VolumeSource: corev1.VolumeSource{
				AWSElasticBlockStore: &corev1.AWSElasticBlockStoreVolumeSource{
					VolumeID: "aws-volume",
				},
			},
		}},
		volumeName:  "test",
		storagePath: "/test",
		pvcSrc: &corev1.PersistentVolumeClaimVolumeSource{
			ClaimName: "test-claim",
		},
		expectedVolumes: []corev1.Volume{
			{
				Name: "test",
				VolumeSource: corev1.VolumeSource{
					AWSElasticBlockStore: &corev1.AWSElasticBlockStoreVolumeSource{
						VolumeID: "aws-volume",
					},
				},
			},
			{
				Name: "test",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: "test-claim",
					},
				},
			},
		},
		expectedMounts: []corev1.VolumeMount{{
			Name:      "test",
			MountPath: "/test",
		}},
	}
	f(o)

	// with PVC spec and not matching data volume
	o = opts{
		volumes: []corev1.Volume{{
			Name: "extra",
			VolumeSource: corev1.VolumeSource{
				AWSElasticBlockStore: &corev1.AWSElasticBlockStoreVolumeSource{
					VolumeID: "aws-volume",
				},
			},
		}},
		volumeName:  "test",
		storagePath: "/test",
		pvcSrc: &corev1.PersistentVolumeClaimVolumeSource{
			ClaimName: "test-claim",
		},
		expectedVolumes: []corev1.Volume{
			{
				Name: "extra",
				VolumeSource: corev1.VolumeSource{
					AWSElasticBlockStore: &corev1.AWSElasticBlockStoreVolumeSource{
						VolumeID: "aws-volume",
					},
				},
			},
			{
				Name: "test",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: "test-claim",
					},
				},
			},
		},
		expectedMounts: []corev1.VolumeMount{{
			Name:      "test",
			MountPath: "/test",
		}},
	}
	f(o)

	// with PVC spec and existing data volume mount
	o = opts{
		volumes: []corev1.Volume{{
			Name: "extra",
			VolumeSource: corev1.VolumeSource{
				AWSElasticBlockStore: &corev1.AWSElasticBlockStoreVolumeSource{
					VolumeID: "aws-volume",
				},
			},
		}},
		mounts: []corev1.VolumeMount{{
			Name:      "test",
			MountPath: "/other-path",
		}},
		volumeName:  "test",
		storagePath: "/test",
		pvcSrc: &corev1.PersistentVolumeClaimVolumeSource{
			ClaimName: "test-claim",
		},
		expectedVolumes: []corev1.Volume{
			{
				Name: "extra",
				VolumeSource: corev1.VolumeSource{
					AWSElasticBlockStore: &corev1.AWSElasticBlockStoreVolumeSource{
						VolumeID: "aws-volume",
					},
				},
			},
			{
				Name: "test",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: "test-claim",
					},
				},
			},
		},
		expectedMounts: []corev1.VolumeMount{{
			Name:      "test",
			MountPath: "/other-path",
		}},
	}
	f(o)
}
