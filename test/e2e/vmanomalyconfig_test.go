package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
)

//nolint:dupl,lll
var _ = Describe("test VMAnomalyConfig Controller", Serial, Label("vm", "anomaly", "enterprise"), func() {
	ctx := context.Background()
	namespace := fmt.Sprintf("default-%d", GinkgoParallelProcess())
	anomalyConfigDatasourceURL := fmt.Sprintf("http://vmsingle-anomalyconfig.%s.svc:8428", namespace)
	anomalyConfigSingle := vmv1beta1.VMSingle{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "anomalyconfig",
			Namespace: namespace,
		},
	}

	BeforeEach(func() {
		if LICENSE_KEY == "" {
			Skip("ignoring VMAnomalyConfig tests, license was not found")
		}
		CreateLicenseSecret(ctx, k8sClient, namespace)

		singleNsn := types.NamespacedName{Name: anomalyConfigSingle.Name, Namespace: namespace}
		expectStatusAfterAction(ctx, &vmv1beta1.VMSingleList{}, singleNsn, eventualDeploymentAppReadyTimeout, func() {
			Expect(k8sClient.Create(ctx, anomalyConfigSingle.DeepCopy())).ToNot(HaveOccurred())
		}, vmv1beta1.UpdateStatusOperational)
	})

	AfterEach(func() {
		DeleteLicenseSecret(ctx, k8sClient, namespace)
		Expect(k8sClient.Delete(ctx, &anomalyConfigSingle)).ToNot(HaveOccurred())
		waitResourceDeleted(ctx, types.NamespacedName{Name: anomalyConfigSingle.Name, Namespace: namespace}, &vmv1beta1.VMSingleList{})
	})

	// baseVMAnomaly returns a VMAnomaly with configSelector set, ready for config-injection tests.
	baseVMAnomaly := func(name string, configSelectorLabels map[string]string) *vmv1.VMAnomaly {
		return &vmv1.VMAnomaly{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      name,
			},
			Spec: vmv1.VMAnomalySpec{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To[int32](1),
				},
				License: &vmv1beta1.License{
					KeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "license"},
						Key:                  "key",
					},
				},
				ConfigRawYaml: anomalyConfig,
				Reader: &vmv1.VMAnomalyReadersSpec{
					DatasourceURL:  anomalyConfigDatasourceURL,
					QueryRangePath: "/api/v1/query_range",
					SamplingPeriod: "10s",
				},
				Writer: &vmv1.VMAnomalyWritersSpec{
					DatasourceURL: anomalyConfigDatasourceURL,
				},
				ConfigSelector: metav1.SetAsLabelSelector(configSelectorLabels),
			},
		}
	}

	Context("e2e vmanomalyconfig", func() {
		nsn := types.NamespacedName{Namespace: namespace}

		AfterEach(func() {
			if nsn.Name == "" {
				return
			}
			Expect(finalize.SafeDelete(ctx, k8sClient, &vmv1.VMAnomaly{
				ObjectMeta: metav1.ObjectMeta{Name: nsn.Name, Namespace: nsn.Namespace},
			})).ToNot(HaveOccurred())
			waitResourceDeleted(ctx, nsn, &vmv1.VMAnomalyList{})
			nsn.Name = ""
		})

		It("should merge multiple VMAnomalyConfigs into single secret", func() {
			nsn.Name = "cfg-multi"
			cr := baseVMAnomaly(nsn.Name, map[string]string{"cfg-test": "multi"})
			cfg1 := &vmv1.VMAnomalyConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cfg-multi-first",
					Namespace: namespace,
					Labels:    map[string]string{"cfg-test": "multi"},
				},
				Spec: runtime.RawExtension{Raw: []byte(`{
  "models": {
    "multi-model-first": {
      "class": "zscore",
      "queries": ["multi-query-first"],
      "schedulers": ["multi-scheduler"],
      "z_threshold": 2.5
    }
  },
  "schedulers": {
    "multi-scheduler": {
      "class": "periodic",
      "infer_every": "1m",
      "fit_every": "2m",
      "fit_window": "1h"
    }
  },
  "queries": {
    "multi-query-first": {
      "expr": "vm_multi_metric_first"
    }
  }
}`)},
			}
			cfg2 := &vmv1.VMAnomalyConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cfg-multi-second",
					Namespace: namespace,
					Labels:    map[string]string{"cfg-test": "multi"},
				},
				Spec: runtime.RawExtension{Raw: []byte(`{
  "models": {
    "multi-model-second": {
      "class": "zscore",
      "queries": ["multi-query-second"],
      "schedulers": ["multi-scheduler"],
      "z_threshold": 3.0
    }
  },
  "schedulers": {
    "multi-scheduler": {
      "class": "periodic",
      "infer_every": "1m",
      "fit_every": "2m",
      "fit_window": "1h"
    }
  },
  "queries": {
    "multi-query-second": {
      "expr": "vm_multi_metric_second"
    }
  }
}`)},
			}
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cfg1)).ToNot(HaveOccurred())
				Expect(finalize.SafeDelete(ctx, k8sClient, cfg2)).ToNot(HaveOccurred())
			})

			expectStatusAfterAction(ctx, &vmv1.VMAnomalyList{}, nsn, anomalyExpandTimeout, func() {
				Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
			}, vmv1beta1.UpdateStatusOperational)

			Expect(k8sClient.Create(ctx, cfg1)).ToNot(HaveOccurred())
			Expect(k8sClient.Create(ctx, cfg2)).ToNot(HaveOccurred())

			// Both configs must appear in the merged secret with namespace-prefixed model names
			Eventually(func() string {
				var secret corev1.Secret
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.PrefixedName(), Namespace: namespace}, &secret)).ToNot(HaveOccurred())
				return string(secret.Data["vmanomaly.env.yaml"])
			}, anomalyReadyTimeout).Should(And(
				ContainSubstring("vm_multi_metric_first"),
				ContainSubstring("vm_multi_metric_second"),
				ContainSubstring(fmt.Sprintf("%s-cfg-multi-first-multi-model-first", namespace)),
				ContainSubstring(fmt.Sprintf("%s-cfg-multi-second-multi-model-second", namespace)),
			))
		})

		It("should select all VMAnomalyConfigs when selectAllByDefault is true and no selector set", func() {
			nsn.Name = "cfg-select-all"
			cr := &vmv1.VMAnomaly{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1.VMAnomalySpec{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To[int32](1),
					},
					License: &vmv1beta1.License{
						KeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "license"},
							Key:                  "key",
						},
					},
					ConfigRawYaml:      anomalyConfig,
					SelectAllByDefault: true,
					Reader: &vmv1.VMAnomalyReadersSpec{
						DatasourceURL:  anomalyConfigDatasourceURL,
						QueryRangePath: "/api/v1/query_range",
						SamplingPeriod: "10s",
					},
					Writer: &vmv1.VMAnomalyWritersSpec{
						DatasourceURL: anomalyConfigDatasourceURL,
					},
				},
			}
			cfg1 := &vmv1.VMAnomalyConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cfg-select-all-first",
					Namespace: namespace,
					Labels:    map[string]string{"cfg-any": "yes"},
				},
				Spec: runtime.RawExtension{Raw: []byte(`{
  "queries": {
    "selectall-query-first": {
      "expr": "vm_selectall_metric_first"
    }
  }
}`)},
			}
			cfg2 := &vmv1.VMAnomalyConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cfg-select-all-second",
					Namespace: namespace,
					Labels:    map[string]string{"cfg-any": "also-yes"},
				},
				Spec: runtime.RawExtension{Raw: []byte(`{
  "queries": {
    "selectall-query-second": {
      "expr": "vm_selectall_metric_second"
    }
  }
}`)},
			}
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cfg1)).ToNot(HaveOccurred())
				Expect(finalize.SafeDelete(ctx, k8sClient, cfg2)).ToNot(HaveOccurred())
			})

			expectStatusAfterAction(ctx, &vmv1.VMAnomalyList{}, nsn, anomalyExpandTimeout, func() {
				Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
			}, vmv1beta1.UpdateStatusOperational)

			Expect(k8sClient.Create(ctx, cfg1)).ToNot(HaveOccurred())
			Expect(k8sClient.Create(ctx, cfg2)).ToNot(HaveOccurred())

			Eventually(func() string {
				var secret corev1.Secret
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.PrefixedName(), Namespace: namespace}, &secret)).ToNot(HaveOccurred())
				return string(secret.Data["vmanomaly.env.yaml"])
			}, anomalyReadyTimeout).Should(And(
				ContainSubstring("vm_selectall_metric_first"),
				ContainSubstring("vm_selectall_metric_second"),
			))
		})
	})
})
