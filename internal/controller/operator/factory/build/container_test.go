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
	f := func(cr testBuildProbeCR, validate func(corev1.Container) error) {
		t.Helper()
		var c corev1.Container
		got := Probe(c, cr)
		if err := validate(got); err != nil {
			t.Errorf("buildProbe() unexpected error: %v", err)
		}
	}

	// build default probe with empty ep
	f(testBuildProbeCR{
		probePath: func() string {
			return "/health"
		},
		port:            "8051",
		needAddLiveness: true,
		scheme:          "HTTP",
	}, func(container corev1.Container) error {
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
	})

	// build default probe with empty ep using HTTPS
	f(testBuildProbeCR{
		probePath: func() string {
			return "/health"
		},
		port:            "8051",
		needAddLiveness: true,
		scheme:          "HTTPS",
	}, func(container corev1.Container) error {
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
	})

	// build default probe with ep
	f(testBuildProbeCR{
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
	}, func(container corev1.Container) error {
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
	})
}

func Test_addExtraArgsOverrideDefaults(t *testing.T) {
	f := func(args []string, extraArgs map[string]string, dashes string, want []string) {
		t.Helper()
		assert.Equalf(
			t,
			want,
			AddExtraArgsOverrideDefaults(args, extraArgs, dashes),
			"addExtraArgsOverrideDefaults(%v, %v)", args, extraArgs)
	}

	// no changes",
	f([]string{
		"-http.ListenAddr=:8081",
	}, nil, "-", []string{
		"-http.ListenAddr=:8081",
	})

	// override default
	f([]string{
		"-http.ListenAddr=:8081",
	}, map[string]string{
		"http.ListenAddr": "127.0.0.1:8085",
	}, "-", []string{
		"-http.ListenAddr=127.0.0.1:8085",
	})

	// override default, add to the end
	f([]string{
		"-http.ListenAddr=:8081",
		"-promscrape.config=/opt/vmagent.yml",
	}, map[string]string{
		"http.ListenAddr": "127.0.0.1:8085",
	}, "-", []string{
		"-promscrape.config=/opt/vmagent.yml",
		"-http.ListenAddr=127.0.0.1:8085",
	})

	// two dashes, extend
	f([]string{
		"--web.timeout=0",
	}, map[string]string{
		"log.level": "debug",
	}, "--", []string{
		"--web.timeout=0", "--log.level=debug",
	})

	// two dashes, override default
	f([]string{
		"--log.level=info",
	}, map[string]string{
		"log.level": "debug",
	}, "--", []string{
		"--log.level=debug",
	})

	// two dashes, alertmanager migration
	f([]string{
		"--log.level=info",
	}, map[string]string{
		"-web.externalURL": "http://domain.example",
	}, "--", []string{
		"--log.level=info",
		"--web.externalURL=http://domain.example",
	})
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
