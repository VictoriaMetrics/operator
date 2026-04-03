package upgrade

import (
	"fmt"
	"os"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/test/e2e/suite"
)

var (
	vmanomaly = &vmv1.VMAnomaly{
		Spec: vmv1.VMAnomalySpec{
			Reader: &vmv1.VMAnomalyReadersSpec{
				DatasourceURL:  "http://vmsingle-anomaly.svc:8428",
				SamplingPeriod: "1m",
			},
			Writer: &vmv1.VMAnomalyWritersSpec{
				DatasourceURL: "http://vmsingle-anomaly.svc:8428",
			},
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				ReplicaCount: ptr.To[int32](1),
				Image: vmv1beta1.Image{
					Repository: "quay.io/victoriametrics/vmanomaly",
					Tag:        "v1.29.0",
				},
				TerminationGracePeriodSeconds: ptr.To(int64(1)),
			},
			License: &vmv1beta1.License{
				KeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "license",
					},
					Key: "key",
				},
			},
			ConfigRawYaml: "preset: ui",
		},
	}
	vmagent = &vmv1beta1.VMAgent{
		Spec: vmv1beta1.VMAgentSpec{
			RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
				{URL: "http://localhost:8428/api/v1/write"},
			},
			CommonConfigReloaderParams: vmv1beta1.CommonConfigReloaderParams{
				ConfigReloaderImage: "quay.io/victoriametrics/operator:config-reloader-v0.65.0",
			},
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				ReplicaCount: ptr.To[int32](1),
				Image: vmv1beta1.Image{
					Repository: "quay.io/victoriametrics/vmagent",
					Tag:        "v1.136.0",
				},
				TerminationGracePeriodSeconds: ptr.To(int64(1)),
			},
		},
	}
	vlagent = &vmv1.VLAgent{
		Spec: vmv1.VLAgentSpec{
			RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
				{
					URL: "http://127.0.0.1:9428/insert/loki/api/v1/push",
				},
			},
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				ReplicaCount: ptr.To[int32](1),
				Image: vmv1beta1.Image{
					Repository: "quay.io/victoriametrics/vlagent",
					Tag:        "v1.48.0",
				},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("20m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("20m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
				},
				TerminationGracePeriodSeconds: ptr.To(int64(1)),
			},
		},
	}
	vmauth = &vmv1beta1.VMAuth{
		Spec: vmv1beta1.VMAuthSpec{
			CommonConfigReloaderParams: vmv1beta1.CommonConfigReloaderParams{
				ConfigReloaderImage: "quay.io/victoriametrics/operator:config-reloader-v0.65.0",
			},
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				ReplicaCount: ptr.To[int32](1),
				Image: vmv1beta1.Image{
					Repository: "quay.io/victoriametrics/vmauth",
					Tag:        "v1.136.0",
				},
				TerminationGracePeriodSeconds: ptr.To(int64(1)),
			},
			UnauthorizedAccessConfig: []vmv1beta1.UnauthorizedAccessConfigURLMap{
				{
					SrcPaths:  []string{"/api/v1/query"},
					URLPrefix: vmv1beta1.StringOrArray{"http://localhost:8428"},
				},
			},
		},
	}
	//nolint:dupl
	vtcluster = &vmv1.VTCluster{
		Spec: vmv1.VTClusterSpec{
			RequestsLoadBalancer: vmv1beta1.VMAuthLoadBalancer{
				Spec: vmv1beta1.VMAuthLoadBalancerSpec{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To[int32](1),
						Image: vmv1beta1.Image{
							Repository: "quay.io/victoriametrics/vmauth",
							Tag:        "v1.136.0",
						},
						TerminationGracePeriodSeconds: ptr.To(int64(1)),
					},
				},
			},
			Select: &vmv1.VTSelect{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To[int32](1),
					Image: vmv1beta1.Image{
						Repository: "quay.io/victoriametrics/victoria-traces",
						Tag:        "v0.4.0",
					},
					TerminationGracePeriodSeconds: ptr.To(int64(1)),
				},
			},
			Insert: &vmv1.VTInsert{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To[int32](1),
					Image: vmv1beta1.Image{
						Repository: "quay.io/victoriametrics/victoria-traces",
						Tag:        "v0.4.0",
					},
					TerminationGracePeriodSeconds: ptr.To(int64(1)),
				},
			},
			Storage: &vmv1.VTStorage{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To[int32](1),
					Image: vmv1beta1.Image{
						Repository: "quay.io/victoriametrics/victoria-traces",
						Tag:        "v0.4.0",
					},
					TerminationGracePeriodSeconds: ptr.To(int64(1)),
				},
			},
		},
	}
	vtsingle = &vmv1.VTSingle{
		Spec: vmv1.VTSingleSpec{
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				ReplicaCount: ptr.To[int32](1),
				Image: vmv1beta1.Image{
					Repository: "quay.io/victoriametrics/victoria-traces",
					Tag:        "v0.4.0",
				},
				TerminationGracePeriodSeconds: ptr.To(int64(1)),
			},
		},
	}
	vmsingle = &vmv1beta1.VMSingle{
		Spec: vmv1beta1.VMSingleSpec{
			CommonConfigReloaderParams: vmv1beta1.CommonConfigReloaderParams{
				ConfigReloaderImage: "quay.io/victoriametrics/operator:config-reloader-v0.65.0",
			},
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				ReplicaCount: ptr.To[int32](1),
				Image: vmv1beta1.Image{
					Repository: "quay.io/victoriametrics/victoria-metrics",
					Tag:        "v1.136.0",
				},
				TerminationGracePeriodSeconds: ptr.To(int64(1)),
			},
		},
	}
	vmalert = &vmv1beta1.VMAlert{
		Spec: vmv1beta1.VMAlertSpec{
			CommonConfigReloaderParams: vmv1beta1.CommonConfigReloaderParams{
				ConfigReloaderImage: "quay.io/victoriametrics/operator:config-reloader-v0.65.0",
			},
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				ReplicaCount: ptr.To[int32](1),
				Image: vmv1beta1.Image{
					Repository: "quay.io/victoriametrics/vmalert",
					Tag:        "v1.136.0",
				},
				TerminationGracePeriodSeconds: ptr.To(int64(1)),
			},
			Datasource: vmv1beta1.VMAlertDatasourceSpec{
				URL: "http://localhost:8428",
			},
			Notifier: &vmv1beta1.VMAlertNotifierSpec{
				URL: "http://localhost:9093",
			},
			EvaluationInterval: "15s",
		},
	}
	//nolint:dupl
	vlcluster = &vmv1.VLCluster{
		Spec: vmv1.VLClusterSpec{
			RequestsLoadBalancer: vmv1beta1.VMAuthLoadBalancer{
				Spec: vmv1beta1.VMAuthLoadBalancerSpec{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To[int32](1),
						Image: vmv1beta1.Image{
							Repository: "quay.io/victoriametrics/vmauth",
							Tag:        "v1.136.0",
						},
						TerminationGracePeriodSeconds: ptr.To(int64(1)),
					},
				},
			},
			VLSelect: &vmv1.VLSelect{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To[int32](1),
					Image: vmv1beta1.Image{
						Repository: "quay.io/victoriametrics/victoria-logs",
						Tag:        "v1.44.0",
					},
					TerminationGracePeriodSeconds: ptr.To(int64(1)),
				},
			},
			VLInsert: &vmv1.VLInsert{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To[int32](1),
					Image: vmv1beta1.Image{
						Repository: "quay.io/victoriametrics/victoria-logs",
						Tag:        "v1.44.0",
					},
					TerminationGracePeriodSeconds: ptr.To(int64(1)),
				},
			},
			VLStorage: &vmv1.VLStorage{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To[int32](1),
					Image: vmv1beta1.Image{
						Repository: "quay.io/victoriametrics/victoria-logs",
						Tag:        "v1.44.0",
					},
					TerminationGracePeriodSeconds: ptr.To(int64(1)),
				},
			},
		},
	}
	vmcluster = &vmv1beta1.VMCluster{
		Spec: vmv1beta1.VMClusterSpec{
			RetentionPeriod: "1",
			RequestsLoadBalancer: vmv1beta1.VMAuthLoadBalancer{
				Spec: vmv1beta1.VMAuthLoadBalancerSpec{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To[int32](1),
						Image: vmv1beta1.Image{
							Repository: "quay.io/victoriametrics/vmauth",
							Tag:        "v1.136.0",
						},
						TerminationGracePeriodSeconds: ptr.To(int64(1)),
					},
				},
			},
			VMSelect: &vmv1beta1.VMSelect{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To[int32](1),
					Image: vmv1beta1.Image{
						Repository: "quay.io/victoriametrics/vmselect",
						Tag:        "v1.136.0-cluster",
					},
					TerminationGracePeriodSeconds: ptr.To(int64(1)),
				},
			},
			VMInsert: &vmv1beta1.VMInsert{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To[int32](1),
					Image: vmv1beta1.Image{
						Repository: "quay.io/victoriametrics/vminsert",
						Tag:        "v1.136.0-cluster",
					},
					TerminationGracePeriodSeconds: ptr.To(int64(1)),
				},
			},
			VMStorage: &vmv1beta1.VMStorage{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To[int32](1),
					Image: vmv1beta1.Image{
						Repository: "quay.io/victoriametrics/vmstorage",
						Tag:        "v1.136.0-cluster",
					},
					TerminationGracePeriodSeconds: ptr.To(int64(1)),
				},
			},
		},
	}
	vmalertmanager = &vmv1beta1.VMAlertmanager{
		Spec: vmv1beta1.VMAlertmanagerSpec{
			CommonConfigReloaderParams: vmv1beta1.CommonConfigReloaderParams{
				ConfigReloaderImage: "quay.io/victoriametrics/operator:config-reloader-v0.65.0",
			},
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				ReplicaCount: ptr.To[int32](1),
				Image: vmv1beta1.Image{
					Repository: "quay.io/prometheus/alertmanager",
					Tag:        "v0.27.0",
				},
				TerminationGracePeriodSeconds: ptr.To(int64(1)),
			},
		},
	}
	vlsingle = &vmv1.VLSingle{
		Spec: vmv1.VLSingleSpec{
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				ReplicaCount: ptr.To[int32](1),
				Image: vmv1beta1.Image{
					Repository: "quay.io/victoriametrics/victoria-logs",
					Tag:        "v1.44.0",
				},
				TerminationGracePeriodSeconds: ptr.To(int64(1)),
			},
		},
	}
	vmdistributed = &vmv1alpha1.VMDistributed{
		Spec: vmv1alpha1.VMDistributedSpec{
			VMAuth: vmv1alpha1.VMDistributedAuth{
				Spec: vmv1beta1.VMAuthSpec{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To[int32](1),
						Image: vmv1beta1.Image{
							Repository: "quay.io/victoriametrics/vmauth",
							Tag:        "v1.136.0",
						},
						TerminationGracePeriodSeconds: ptr.To(int64(1)),
					},
				},
			},
			ZoneCommon: vmv1alpha1.VMDistributedZoneCommon{
				ReadyTimeout: &metav1.Duration{Duration: 2 * time.Minute},
				UpdatePause:  &metav1.Duration{Duration: 1 * time.Second},
				VMCluster: vmv1alpha1.VMDistributedZoneCluster{
					Spec: vmv1beta1.VMClusterSpec{
						RetentionPeriod: "1",
						VMSelect: &vmv1beta1.VMSelect{
							CommonAppsParams: vmv1beta1.CommonAppsParams{
								ReplicaCount: ptr.To[int32](1),
								Image: vmv1beta1.Image{
									Repository: "quay.io/victoriametrics/vmselect",
									Tag:        "v1.136.0-cluster",
								},
								TerminationGracePeriodSeconds: ptr.To(int64(1)),
							},
						},
						VMInsert: &vmv1beta1.VMInsert{
							CommonAppsParams: vmv1beta1.CommonAppsParams{
								ReplicaCount: ptr.To[int32](1),
								Image: vmv1beta1.Image{
									Repository: "quay.io/victoriametrics/vminsert",
									Tag:        "v1.136.0-cluster",
								},
								TerminationGracePeriodSeconds: ptr.To(int64(1)),
							},
						},
						VMStorage: &vmv1beta1.VMStorage{
							CommonAppsParams: vmv1beta1.CommonAppsParams{
								ReplicaCount: ptr.To[int32](1),
								Image: vmv1beta1.Image{
									Repository: "quay.io/victoriametrics/vmstorage",
									Tag:        "v1.136.0-cluster",
								},
								TerminationGracePeriodSeconds: ptr.To(int64(1)),
							},
						},
					},
				},
				VMAgent: vmv1alpha1.VMDistributedZoneAgent{
					Spec: vmv1alpha1.VMDistributedZoneAgentSpec{
						CommonAppsParams: vmv1beta1.CommonAppsParams{
							ReplicaCount: ptr.To[int32](1),
							Image: vmv1beta1.Image{
								Repository: "quay.io/victoriametrics/vmagent",
								Tag:        "v1.136.0",
							},
							TerminationGracePeriodSeconds: ptr.To(int64(1)),
						},
					},
				},
			},
			Zones: []vmv1alpha1.VMDistributedZone{
				{Name: "a"},
			},
		},
	}
)

type object[T any] interface {
	DeepCopy() T
}

func with[T object[T]](cr T, opts ...func(T)) T {
	obj := cr.DeepCopy()
	for _, o := range opts {
		o(obj)
	}
	return obj
}

var (
	waitTimeout    = 5 * time.Minute
	currentVersion = os.Getenv("OPERATOR_TAG")
	licenseKey     = os.Getenv("LICENSE_KEY")
)

type entry struct {
	name     string
	genDeps  func(string) []client.Object
	crs      []client.Object
	versions []string
	envs     map[string]string
}

func entries(es []entry) []TableEntry {
	var result []TableEntry
	for _, e := range es {
		for _, v := range e.versions {
			var objs []client.Object
			for _, cr := range e.crs {
				objs = append(objs, cr.DeepCopyObject().(client.Object))
			}
			result = append(result, Entry(fmt.Sprintf("from %s: %s", v, e.name), v, e.genDeps, objs, e.envs))
		}
	}
	return result
}

func ensureNoPodRollout(version string, genDeps func(string) []client.Object, objs []client.Object, envs map[string]string) {
	namespace := createRandomNamespace(ctx, k8sClient)
	updateOperator(ctx, k8sClient, "quay.io", version, namespace, envs)
	DeferCleanup(func() {
		defer GinkgoRecover()
		removeOperator(ctx, k8sClient, namespace)
		removeNamespace(ctx, k8sClient, namespace)
	})

	if genDeps != nil {
		for _, d := range genDeps(namespace) {
			Expect(k8sClient.Create(ctx, d)).ToNot(HaveOccurred())
			DeferCleanup(func() {
				defer GinkgoRecover()
				Expect(k8sClient.Delete(ctx, d)).ToNot(HaveOccurred())
			})
		}
	}

	if licenseKey != "" {
		Expect(k8sClient.Create(ctx,
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "license",
					Namespace: namespace,
				},
				StringData: map[string]string{
					"key": licenseKey,
				},
			},
		)).ToNot(HaveOccurred())
	}

	names := make([]string, len(objs))
	displayNames := make([]string, len(objs))
	nsns := make([]types.NamespacedName, len(objs))

	for i := range names {
		names[i] = utilrand.String(10)
		displayNames[i] = fmt.Sprintf("%T=%s/%s", objs[i], namespace, names[i])
		nsns[i] = types.NamespacedName{
			Name:      names[i],
			Namespace: namespace,
		}
	}

	for i, o := range objs {
		o.SetName(names[i])
		o.SetNamespace(namespace)
		By(fmt.Sprintf("creating %s", displayNames[i]))
		Expect(k8sClient.Create(ctx, o)).ToNot(HaveOccurred())
		DeferCleanup(func() {
			defer GinkgoRecover()
			nsn := types.NamespacedName{
				Name:      names[i],
				Namespace: namespace,
			}
			Expect(k8sClient.Delete(ctx, o)).ToNot(HaveOccurred())
			Eventually(func() bool {
				return k8serrors.IsNotFound(k8sClient.Get(ctx, nsn, o))
			}, 30*time.Second, 2*time.Second).Should(BeTrue())
		})
	}

	var wg sync.WaitGroup
	for i, o := range objs {
		By(fmt.Sprintf("waiting for %s to become operational", displayNames[i]))
		wg.Go(func() {
			Eventually(func() error {
				return suite.ExpectObjectStatus(ctx, k8sClient, o, nsns[i], vmv1beta1.UpdateStatusOperational)
			}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())
		})
	}
	wg.Wait()

	By("snapshotting child workload specs")
	apps := getApplications(objs...)
	podSpecs := getSnapshots(ctx, k8sClient, apps...)

	updateOperator(ctx, k8sClient, "docker.io", currentVersion, namespace, envs)

	for i, o := range objs {
		By(fmt.Sprintf("waiting for latest operator to reconcile %s", displayNames[i]))
		wg.Go(func() {
			Eventually(func() error {
				return suite.ExpectObjectStatus(ctx, k8sClient, o, nsns[i], vmv1beta1.UpdateStatusOperational)
			}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())
		})
	}
	wg.Wait()

	By("verifying workload spec remains stable over time")
	Consistently(func() string {
		return checkWorkloads(ctx, k8sClient, podSpecs, apps)
	}, 5*time.Second, 1*time.Second).Should(BeEmpty())
}

var _ = Describe("operator upgrade", Label("upgrade"), func() {
	DescribeTable("should not rollout changes", ensureNoPodRollout, entries([]entry{
		{
			name: "VMAgent/VLAgent",
			genDeps: func(ns string) []client.Object {
				return []client.Object{
					&corev1.ServiceAccount{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "vlagent-collector",
							Namespace: ns,
						},
					},
					&rbacv1.ClusterRoleBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: fmt.Sprintf("vlagent-collector-%s", ns),
						},
						Subjects: []rbacv1.Subject{{
							Kind:      rbacv1.ServiceAccountKind,
							Name:      "vlagent-collector",
							Namespace: ns,
						}},
						RoleRef: rbacv1.RoleRef{
							APIGroup: rbacv1.GroupName,
							Name:     fmt.Sprintf("vlagent-collector-%s", ns),
							Kind:     "ClusterRole",
						},
					},
					&rbacv1.ClusterRole{
						ObjectMeta: metav1.ObjectMeta{
							Name: fmt.Sprintf("vlagent-collector-%s", ns),
						},
						Rules: []rbacv1.PolicyRule{{
							APIGroups: []string{""},
							Verbs: []string{
								"get",
								"list",
								"watch",
							},
							Resources: []string{
								"pods",
								"namespaces",
								"nodes",
							},
						}},
					},
				}
			},
			crs: []client.Object{
				with(vmagent),
				with(vmagent, func(cr *vmv1beta1.VMAgent) {
					cr.Spec.DaemonSetMode = true
				}),
				with(vmagent, func(cr *vmv1beta1.VMAgent) {
					cr.Spec.StatefulMode = true
				}),
				with(vlagent),
				with(vlagent, func(cr *vmv1.VLAgent) {
					cr.Spec.K8sCollector.Enabled = true
					cr.Spec.ServiceAccountName = "vlagent-collector"
				}),
			},
			versions: []string{"v0.68.0", "v0.68.1", "v0.68.2", "v0.68.3"},
		}, {
			name: "VMAlert/VMAuth/VMAlertmanager/VMAnomaly",
			genDeps: func(ns string) []client.Object {
				return []client.Object{
					with(vmsingle, func(cr *vmv1beta1.VMSingle) {
						cr.Name = "anomaly"
						cr.Namespace = ns
					}),
				}
			},
			crs: []client.Object{
				with(vmalert),
				with(vmauth),
				with(vmalertmanager),
				with(vmanomaly),
			},
			versions: []string{"v0.68.0", "v0.68.1", "v0.68.2", "v0.68.3"},
		}, {
			name: "VM/VL/VTSingle",
			crs: []client.Object{
				with(vmsingle),
				with(vtsingle),
				with(vlsingle),
			},
			versions: []string{"v0.65.0", "v0.67.0", "v0.68.0", "v0.68.1", "v0.68.2", "v0.68.3"},
		}, {
			name: "VLCluster",
			crs: []client.Object{
				with(vlcluster),
				with(vlcluster, func(cr *vmv1.VLCluster) {
					cr.Spec.RequestsLoadBalancer.Enabled = true
				}),
			},
			versions: []string{"v0.65.0", "v0.67.0", "v0.68.0", "v0.68.1", "v0.68.2", "v0.68.3"},
		}, {
			name: "VTCluster",
			crs: []client.Object{
				with(vtcluster),
				with(vtcluster, func(cr *vmv1.VTCluster) {
					cr.Spec.RequestsLoadBalancer.Enabled = true
				}),
			},
			versions: []string{"v0.65.0", "v0.67.0", "v0.68.0", "v0.68.1", "v0.68.2", "v0.68.3"},
		}, {
			name: "VMCluster",
			crs: []client.Object{
				with(vmcluster),
			},
			versions: []string{"v0.65.0", "v0.67.0", "v0.68.0", "v0.68.1", "v0.68.2", "v0.68.3"},
		}, {
			name: "VMCluster with VMBackup",
			crs: []client.Object{
				with(vmcluster, func(cr *vmv1beta1.VMCluster) {
					cr.Spec.RequestsLoadBalancer.Enabled = true
					cr.Spec.VMStorage.Image.Tag = "v1.136.0-enterprise-cluster"
					cr.Spec.VMSelect.Image.Tag = "v1.136.0-enterprise-cluster"
					cr.Spec.VMInsert.Image.Tag = "v1.136.0-enterprise-cluster"
					cr.Spec.RequestsLoadBalancer.Spec.Image.Tag = "v1.136.0-enterprise"
					cr.Spec.VMStorage.VMBackup = &vmv1beta1.VMBackup{
						Destination:                 "fs:///tmp",
						DestinationDisableSuffixAdd: true,
						Image: vmv1beta1.Image{
							Tag: "v1.136.0-enterprise",
						},
					}
					cr.Spec.License = &vmv1beta1.License{
						KeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "license",
							},
							Key: "key",
						},
					}
				}),
			},
			versions: []string{"v0.65.0", "v0.68.3"},
			envs: map[string]string{
				"VM_LOOPBACK": "localhost",
			},
		}, {
			name: "VMDistributed",
			crs: []client.Object{
				with(vmdistributed),
			},
			versions: []string{"v0.68.0", "v0.68.1", "v0.68.2", "v0.68.3"},
		},
	}))
})
