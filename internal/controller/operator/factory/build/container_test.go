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
	type args struct {
		container corev1.Container
		cr        testBuildProbeCR
	}
	tests := []struct {
		name     string
		args     args
		validate func(corev1.Container) error
	}{
		{
			name: "build default probe with empty ep",
			args: args{
				cr: testBuildProbeCR{
					probePath: func() string {
						return "/health"
					},
					port:            "8051",
					needAddLiveness: true,
					scheme:          "HTTP",
				},
				container: corev1.Container{},
			},
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
		},
		{
			name: "build default probe with empty ep using HTTPS",
			args: args{
				cr: testBuildProbeCR{
					probePath: func() string {
						return "/health"
					},
					port:            "8051",
					needAddLiveness: true,
					scheme:          "HTTPS",
				},
				container: corev1.Container{},
			},
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
		},
		{
			name: "build default probe with ep",
			args: args{
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
			},
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
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Probe(tt.args.container, tt.args.cr)
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
				AddExtraArgsOverrideDefaults(tt.args.args, tt.args.extraArgs, tt.args.dashes),
				"addExtraArgsOverrideDefaults(%v, %v)", tt.args.args, tt.args.extraArgs)
		})
	}
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
