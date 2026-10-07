package v1beta1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestVMAlertmanagerValidate(t *testing.T) {
	type opts struct {
		cr      *VMAlertmanager
		wantErr bool
	}
	f := func(o opts) {
		t.Helper()
		if o.wantErr {
			assert.Error(t, o.cr.Validate())
		} else {
			assert.NoError(t, o.cr.Validate())
		}
	}

	// config file with bad syntax
	f(opts{
		cr: &VMAlertmanager{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-suite",
				Namespace: "test",
			},
			Spec: VMAlertmanagerSpec{
				ConfigRawYaml: `
global:
 resolve_timeout: 10m
 group_wait: 1s`,
			},
		},
		wantErr: true,
	})

	// config with correct syntax
	f(opts{
		cr: &VMAlertmanager{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-suite",
				Namespace: "test",
			},
			Spec: VMAlertmanagerSpec{
				ConfigRawYaml: `
global:
  resolve_timeout: 5m
route:
  group_wait: 10s
  group_interval: 2m
  group_by: ["alertgroup", "resource_id"]
  repeat_interval: 12h
  receiver: 'blackhole'
receivers:
  # by default route to dev/null
  - name: blackhole`,
			},
		},
	})

	// a non-headless default service is allowed for alertmanager, see https://github.com/VictoriaMetrics/operator/issues/2487#issuecomment-5807946714
	mkCR := func(svcSpec *AdditionalServiceSpec) *VMAlertmanager {
		return &VMAlertmanager{
			ObjectMeta: metav1.ObjectMeta{Name: "test-suite", Namespace: "test"},
			Spec:       VMAlertmanagerSpec{ServiceSpec: svcSpec},
		}
	}
	f(opts{
		cr: mkCR(&AdditionalServiceSpec{
			UseAsDefault: true,
			Spec:         corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP},
		}),
		wantErr: false,
	})
	f(opts{
		cr: mkCR(&AdditionalServiceSpec{
			UseAsDefault: true,
			Spec:         corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
		}),
		wantErr: false,
	})
	f(opts{
		cr: mkCR(&AdditionalServiceSpec{
			UseAsDefault: true,
			Spec:         corev1.ServiceSpec{ClusterIP: "1.1.1.1"},
		}),
		wantErr: false,
	})
	f(opts{
		cr: mkCR(&AdditionalServiceSpec{
			UseAsDefault: true,
			Spec: corev1.ServiceSpec{
				Type:      corev1.ServiceTypeClusterIP,
				ClusterIP: corev1.ClusterIPNone,
			},
		}),
		wantErr: false,
	})
	f(opts{
		cr: mkCR(&AdditionalServiceSpec{
			Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
		}),
		wantErr: false,
	})

	// eventRecorder: a spec with no outputs at all must be rejected, since Alertmanager
	// would silently render an empty event_recorder section
	f(opts{
		cr: &VMAlertmanager{
			ObjectMeta: metav1.ObjectMeta{Name: "test-suite", Namespace: "test"},
			Spec: VMAlertmanagerSpec{
				EventRecorder: &VMAlertmanagerEventRecorder{},
			},
		},
		wantErr: true,
	})

	// eventRecorder: stdoutOutput alone is a valid, complete configuration
	f(opts{
		cr: &VMAlertmanager{
			ObjectMeta: metav1.ObjectMeta{Name: "test-suite", Namespace: "test"},
			Spec: VMAlertmanagerSpec{
				EventRecorder: &VMAlertmanagerEventRecorder{StdoutOutput: true},
			},
		},
		wantErr: false,
	})

	// eventRecorder: an empty url string must be rejected the same as a nil url,
	// otherwise the config builder silently omits it and produces a destination-less webhook
	f(opts{
		cr: &VMAlertmanager{
			ObjectMeta: metav1.ObjectMeta{Name: "test-suite", Namespace: "test"},
			Spec: VMAlertmanagerSpec{
				EventRecorder: &VMAlertmanagerEventRecorder{
					WebhookOutputs: []VMAlertmanagerEventRecorderWebhookOutput{
						{URL: ptr.To("")},
					},
				},
			},
		},
		wantErr: true,
	})

	// eventRecorder: a non-empty url must pass
	f(opts{
		cr: &VMAlertmanager{
			ObjectMeta: metav1.ObjectMeta{Name: "test-suite", Namespace: "test"},
			Spec: VMAlertmanagerSpec{
				EventRecorder: &VMAlertmanagerEventRecorder{
					WebhookOutputs: []VMAlertmanagerEventRecorderWebhookOutput{
						{URL: ptr.To("https://example.com/hook")},
					},
				},
			},
		},
		wantErr: false,
	})

	// eventRecorder: a malformed literal url must be rejected, matching WebhookConfig.validate()
	f(opts{
		cr: &VMAlertmanager{
			ObjectMeta: metav1.ObjectMeta{Name: "test-suite", Namespace: "test"},
			Spec: VMAlertmanagerSpec{
				EventRecorder: &VMAlertmanagerEventRecorder{
					WebhookOutputs: []VMAlertmanagerEventRecorderWebhookOutput{
						{URL: ptr.To("://not-a-url")},
					},
				},
			},
		},
		wantErr: true,
	})

	// eventRecorder: a url that parses but isn't an absolute http(s) URL with a host
	// (relative path, or a non-HTTP scheme) must be rejected: this output sends HTTP POSTs
	for _, badURL := range []string{"/relative/path", "ftp://host/path", "https:no-host"} {
		f(opts{
			cr: &VMAlertmanager{
				ObjectMeta: metav1.ObjectMeta{Name: "test-suite", Namespace: "test"},
				Spec: VMAlertmanagerSpec{
					EventRecorder: &VMAlertmanagerEventRecorder{
						WebhookOutputs: []VMAlertmanagerEventRecorderWebhookOutput{
							{URL: ptr.To(badURL)},
						},
					},
				},
			},
			wantErr: true,
		})
	}

	// eventRecorder: incompatible httpConfig auth methods on a webhook output must be rejected
	f(opts{
		cr: &VMAlertmanager{
			ObjectMeta: metav1.ObjectMeta{Name: "test-suite", Namespace: "test"},
			Spec: VMAlertmanagerSpec{
				EventRecorder: &VMAlertmanagerEventRecorder{
					WebhookOutputs: []VMAlertmanagerEventRecorderWebhookOutput{
						{
							URL: ptr.To("https://example.com/hook"),
							HTTPConfig: &HTTPConfig{
								BasicAuth:         &BasicAuth{},
								BearerTokenSecret: &corev1.SecretKeySelector{},
							},
						},
					},
				},
			},
		},
		wantErr: true,
	})

	// eventRecorder: an empty literal url alongside a urlSecret must be rejected, since
	// config generation picks the non-nil (but empty) url first and emits no destination
	f(opts{
		cr: &VMAlertmanager{
			ObjectMeta: metav1.ObjectMeta{Name: "test-suite", Namespace: "test"},
			Spec: VMAlertmanagerSpec{
				EventRecorder: &VMAlertmanagerEventRecorder{
					WebhookOutputs: []VMAlertmanagerEventRecorderWebhookOutput{
						{
							URL: ptr.To(""),
							URLSecret: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "webhook-secrets"},
								Key:                  "url",
							},
						},
					},
				},
			},
		},
		wantErr: true,
	})

	// eventRecorder: malformed durations on a webhook output must be rejected
	for _, field := range []string{"timeout", "retryBackoff", "batchFlushInterval"} {
		wo := VMAlertmanagerEventRecorderWebhookOutput{URL: ptr.To("https://example.com/hook")}
		switch field {
		case "timeout":
			wo.Timeout = "10"
		case "retryBackoff":
			wo.RetryBackoff = "10"
		case "batchFlushInterval":
			wo.BatchFlushInterval = "10"
		}
		f(opts{
			cr: &VMAlertmanager{
				ObjectMeta: metav1.ObjectMeta{Name: "test-suite", Namespace: "test"},
				Spec: VMAlertmanagerSpec{
					EventRecorder: &VMAlertmanagerEventRecorder{
						WebhookOutputs: []VMAlertmanagerEventRecorderWebhookOutput{wo},
					},
				},
			},
			wantErr: true,
		})
	}

	// eventRecorder: an empty broker entry must be rejected, since it renders as an
	// unconnectable address in the generated config
	f(opts{
		cr: &VMAlertmanager{
			ObjectMeta: metav1.ObjectMeta{Name: "test-suite", Namespace: "test"},
			Spec: VMAlertmanagerSpec{
				EventRecorder: &VMAlertmanagerEventRecorder{
					KafkaOutputs: []VMAlertmanagerEventRecorderKafkaOutput{
						{Brokers: []string{"kafka:9092", ""}, Topic: "am-events"},
					},
				},
			},
		},
		wantErr: true,
	})

	// eventRecorder: a broker entry that isn't a valid host:port pair must be rejected,
	// since the Kafka producer cannot dial it
	for _, broker := range []string{"kafka", "kafka:", ":9092", "kafka:9092:extra"} {
		f(opts{
			cr: &VMAlertmanager{
				ObjectMeta: metav1.ObjectMeta{Name: "test-suite", Namespace: "test"},
				Spec: VMAlertmanagerSpec{
					EventRecorder: &VMAlertmanagerEventRecorder{
						KafkaOutputs: []VMAlertmanagerEventRecorderKafkaOutput{
							{Brokers: []string{broker}, Topic: "am-events"},
						},
					},
				},
			},
			wantErr: true,
		})
	}

	// eventRecorder: a kafka output TLS config with a cert but no key (or vice versa)
	// must be rejected, since the rendered TLS client cannot use an incomplete pair
	for _, tc := range []*TLSClientConfig{
		{Certs: Certs{CertFile: "/etc/tls/cert.pem"}},
		{Certs: Certs{KeyFile: "/etc/tls/key.pem"}},
	} {
		f(opts{
			cr: &VMAlertmanager{
				ObjectMeta: metav1.ObjectMeta{Name: "test-suite", Namespace: "test"},
				Spec: VMAlertmanagerSpec{
					EventRecorder: &VMAlertmanagerEventRecorder{
						KafkaOutputs: []VMAlertmanagerEventRecorderKafkaOutput{
							{Brokers: []string{"kafka:9092"}, Topic: "am-events", TLSConfig: tc},
						},
					},
				},
			},
			wantErr: true,
		})
	}

	// eventRecorder: a kafka output TLS config that sets both the file path and the
	// secretRef variant of the same field must be rejected, since config generation
	// silently prefers the secret and the file path is a dead, misleading value
	for _, tc := range []*TLSClientConfig{
		{CASecretRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "ca"}, Key: "key"}, CAFile: "/etc/tls/ca.pem"},
		{Certs: Certs{
			CertSecretRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "cert"}, Key: "key"},
			CertFile:      "/etc/tls/cert.pem",
			KeyFile:       "/etc/tls/key.pem",
		}},
		{Certs: Certs{
			KeySecretRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "key"}, Key: "key"},
			KeyFile:      "/etc/tls/key.pem",
			CertFile:     "/etc/tls/cert.pem",
		}},
	} {
		f(opts{
			cr: &VMAlertmanager{
				ObjectMeta: metav1.ObjectMeta{Name: "test-suite", Namespace: "test"},
				Spec: VMAlertmanagerSpec{
					EventRecorder: &VMAlertmanagerEventRecorder{
						KafkaOutputs: []VMAlertmanagerEventRecorderKafkaOutput{
							{Brokers: []string{"kafka:9092"}, Topic: "am-events", TLSConfig: tc},
						},
					},
				},
			},
			wantErr: true,
		})
	}
}

func TestVMAlertmanager_PrefixedName(t *testing.T) {
	f := func(name string, omit bool, want string) {
		t.Helper()
		cr := &VMAlertmanager{Spec: VMAlertmanagerSpec{UseLegacyNaming: omit}}
		cr.Name = name
		assert.Equal(t, want, cr.PrefixedName())
	}

	f("myapp", false, "vmalertmanager-myapp")
	f("myapp", true, "myapp")
}

// TestVMAlertmanager_IsUnmanaged is the VMAlertmanager counterpart of TestVMAlert_IsUnmanaged.
func TestVMAlertmanager_IsUnmanaged(t *testing.T) {
	f := func(cr VMAlertmanager, want bool) {
		t.Helper()
		assert.Equal(t, want, cr.IsUnmanaged())
	}

	f(VMAlertmanager{Spec: VMAlertmanagerSpec{SelectAllByDefault: true}}, false)
	f(VMAlertmanager{}, true)
	f(VMAlertmanager{
		Status: VMAlertmanagerStatus{ParsingSpecError: `json: unknown field "foo"`},
		Spec:   VMAlertmanagerSpec{SelectAllByDefault: true},
	}, false)
	f(VMAlertmanager{
		Status: VMAlertmanagerStatus{ParsingSpecError: "some other unrelated parse failure"},
		Spec:   VMAlertmanagerSpec{SelectAllByDefault: true},
	}, true)
}
