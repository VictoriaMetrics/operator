package upgrade

import (
	"context"
	"fmt"
	"maps"
	"os/exec"
	"time"

	"github.com/google/go-cmp/cmp" //nolint:staticcheck
	. "github.com/onsi/ginkgo/v2"  //nolint:staticcheck
	. "github.com/onsi/gomega"     //nolint:staticcheck
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

const (
	operatorImageBase = "victoriametrics/operator"
)

// operatorEnvVars builds the env var list for the operator pod,
// TODO[vrutkovs]: do we need to copy it?
func operatorEnvVars(watchNamespace string, extraEnvs map[string]string) []corev1.EnvVar {
	envs := map[string]string{
		"VM_VMALERTMANAGER_ALERTMANAGERDEFAULTBASEIMAGE": "prometheus/alertmanager",
		"VM_ENABLEDPROMETHEUSCONVERTEROWNERREFERENCES":   "true",
		"VM_GATEWAY_API_ENABLED":                         "true",
		"VM_PODWAITREADYTIMEOUT":                         "20s",
		"VM_PODWAITREADYINTERVALCHECK":                   "1s",
		"VM_APPREADYTIMEOUT":                             "50s",
		"VM_CONFIG_RELOADER_IMAGE":                       "quay.io/victoriametrics/operator:config-reloader-v0.68.3",
		"WATCH_NAMESPACE":                                watchNamespace,
		"VM_CONTAINERREGISTRY":                           "quay.io",
	}
	resourceEnvsPrefixes := []string{
		"VMBACKUP",
		"VMCLUSTERDEFAULT_VMSTORAGEDEFAULT",
		"VMCLUSTERDEFAULT_VMSELECTDEFAULT",
		"VMCLUSTERDEFAULT_VMINSERTDEFAULT",
		"VLCLUSTERDEFAULT_VLSTORAGEDEFAULT",
		"VLCLUSTERDEFAULT_VLSELECTDEFAULT",
		"VLCLUSTERDEFAULT_VLINSERTDEFAULT",
		"VTCLUSTERDEFAULT_STORAGE",
		"VTCLUSTERDEFAULT_SELECT",
		"VTCLUSTERDEFAULT_INSERT",
		"VMAGENTDEFAULT",
		"VMAUTHDEFAULT",
		"VMALERTDEFAULT",
		"VMSINGLEDEFAULT",
		"VLAGENTDEFAULT",
		"VLSINGLEDEFAULT",
		"VTSINGLEDEFAULT",
	}
	resources := map[string]string{
		"CPU": "10m",
		"MEM": "20Mi",
	}
	for _, prefix := range resourceEnvsPrefixes {
		for _, t := range []string{"LIMIT", "REQUEST"} {
			for rn, rv := range resources {
				envName := fmt.Sprintf("VM_%s_RESOURCE_%s_%s", prefix, t, rn)
				envs[envName] = rv
			}
		}
		envName := fmt.Sprintf("VM_%s_TERMINATION_GRACE_PERIOD_SECONDS", prefix)
		envs[envName] = "5"
	}
	var result []corev1.EnvVar
	maps.Copy(envs, extraEnvs)
	for k, v := range envs {
		result = append(result, corev1.EnvVar{Name: k, Value: v})
	}
	return result
}

func updateOperator(ctx context.Context, k8sClient client.Client, registry, version, watchNamespace string, envs map[string]string) {
	GinkgoHelper()

	nsn := types.NamespacedName{
		Name:      "vm-operator",
		Namespace: watchNamespace,
	}

	objMeta := metav1.ObjectMeta{
		Name:      "vm-operator",
		Namespace: watchNamespace,
	}

	if len(registry) == 0 {
		registry = "quay.io"
	}
	image := registry + "/" + operatorImageBase + ":" + version

	var dep appsv1.Deployment
	if err := k8sClient.Get(ctx, nsn, &dep); err == nil {
		dep.Spec.Template.Spec.Containers[0].Image = image
		Expect(k8sClient.Update(ctx, &dep)).ToNot(HaveOccurred())
	} else {
		By("creating ServiceAccount for operator")
		sa := &corev1.ServiceAccount{
			ObjectMeta: objMeta,
		}
		err := k8sClient.Create(ctx, sa)
		if err != nil && !k8serrors.IsAlreadyExists(err) {
			Expect(err).ToNot(HaveOccurred())
		}

		By("creating ClusterRoleBinding for operator")
		crb := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("upgrade-test-operator-%s", watchNamespace),
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      "vm-operator",
					Namespace: watchNamespace,
				},
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     "cluster-admin",
			},
		}
		err = k8sClient.Create(ctx, crb)
		if err != nil && !k8serrors.IsAlreadyExists(err) {
			Expect(err).ToNot(HaveOccurred())
		}

		By(fmt.Sprintf("deploying operator %s", version))
		dep = appsv1.Deployment{
			ObjectMeta: objMeta,
			Spec: appsv1.DeploymentSpec{
				Replicas: ptr.To[int32](1),
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "vm-operator"},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{"app": "vm-operator"},
					},
					Spec: corev1.PodSpec{
						ServiceAccountName:            "vm-operator",
						TerminationGracePeriodSeconds: ptr.To[int64](1),
						Containers: []corev1.Container{
							{
								Name:  "manager",
								Image: image,
								Args: []string{
									"--health-probe-bind-address=:8081",
								},
								Env: operatorEnvVars(watchNamespace, envs),
								ReadinessProbe: &corev1.Probe{
									ProbeHandler: corev1.ProbeHandler{
										HTTPGet: &corev1.HTTPGetAction{
											Path: "/ready",
											Port: intstr.FromInt(8081),
										},
									},
									InitialDelaySeconds: 5,
									PeriodSeconds:       5,
								},
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("50m"),
										corev1.ResourceMemory: resource.MustParse("64Mi"),
									},
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("200m"),
										corev1.ResourceMemory: resource.MustParse("256Mi"),
									},
								},
							},
						},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, &dep)).ToNot(HaveOccurred())
	}

	By("waiting for operator to be ready")
	Eventually(func() bool {
		if err := k8sClient.Get(ctx, nsn, &dep); err != nil {
			return false
		}
		replicas := ptr.Deref(dep.Spec.Replicas, 0)
		status := dep.Status
		if status.ObservedGeneration < dep.Generation {
			return false
		}
		if status.UpdatedReplicas != replicas || status.AvailableReplicas != replicas || status.ReadyReplicas != replicas {
			return false
		}
		return true
	}, 120*time.Second, 3*time.Second).Should(BeTrue())
}

func removeOperator(ctx context.Context, k8sClient client.Client, watchNamespace string) {
	GinkgoHelper()

	By("removing old operator deployment")
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-operator",
			Namespace: watchNamespace,
		},
	}
	Expect(k8sClient.Delete(ctx, dep)).ToNot(HaveOccurred())
	Eventually(func() bool {
		return k8serrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: dep.Name, Namespace: dep.Namespace}, &appsv1.Deployment{}))
	}, 30*time.Second, 2*time.Second).Should(BeTrue())

	// Delete RBAC resources
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("upgrade-test-operator-%s", watchNamespace)},
	}
	err := k8sClient.Delete(ctx, crb)
	if err != nil && !k8serrors.IsNotFound(err) {
		Expect(err).ToNot(HaveOccurred())
	}
}

func removeNamespace(ctx context.Context, k8sClient client.Client, watchNamespace string) {
	GinkgoHelper()

	// Clear finalizers from all namespaced objects using kubectl.
	exec.Command("sh", "-c", fmt.Sprintf(
		"kubectl get $(kubectl api-resources --namespaced=true --verbs=list -o name | tr \"\n\" \",\" | sed -e 's/,$//') -n %s -o name | xargs -I {} kubectl patch {} -n %s -p '{\"metadata\":{\"finalizers\":[]}}' --type=merge",
		watchNamespace, watchNamespace,
	)).Run()

	// Delete namespace
	nsObj := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: watchNamespace},
	}
	err := k8sClient.Delete(ctx, nsObj, &client.DeleteOptions{
		PropagationPolicy: ptr.To(metav1.DeletePropagationForeground),
	})
	if err != nil && !k8serrors.IsNotFound(err) {
		Expect(err).ToNot(HaveOccurred())
	}
}

func createRandomNamespace(ctx context.Context, k8sClient client.Client) string {
	GinkgoHelper()
	namespace := fmt.Sprintf("upgrade-%s", utilrand.String(5))

	Eventually(func() bool {
		return k8serrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: namespace}, &corev1.Namespace{}))
	}, 5*time.Minute, 5*time.Second).Should(BeTrue())

	err := k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}})
	Expect(err).ToNot(HaveOccurred())

	return namespace
}

func getSnapshots(ctx context.Context, k8sClient client.Client, objs ...client.Object) []*corev1.PodSpec {
	podSpecs := make([]*corev1.PodSpec, 0, len(objs))
	for _, o := range objs {
		nsn := types.NamespacedName{
			Name:      o.GetName(),
			Namespace: o.GetNamespace(),
		}
		Eventually(func() error {
			return k8sClient.Get(ctx, nsn, o)
		}, 5*time.Minute, 5*time.Second).ShouldNot(HaveOccurred())
		switch v := o.(type) {
		case *appsv1.Deployment:
			podSpecs = append(podSpecs, v.Spec.Template.Spec.DeepCopy())
		case *appsv1.StatefulSet:
			podSpecs = append(podSpecs, v.Spec.Template.Spec.DeepCopy())
		case *appsv1.DaemonSet:
			podSpecs = append(podSpecs, v.Spec.Template.Spec.DeepCopy())
		}
	}
	return podSpecs
}

func checkWorkloads(ctx context.Context, k8sClient client.Client, expectedPodSpecs []*corev1.PodSpec, apps []client.Object) string {
	for i := range apps {
		expectedPodSpec := expectedPodSpecs[i].DeepCopy()
		if diff := getRolloutDiff(ctx, k8sClient, expectedPodSpec, apps[i]); diff != "" {
			return fmt.Sprintf("%s/%s:\n%s", apps[i].GetNamespace(), apps[i].GetName(), diff)
		}
	}
	return ""
}

func getRolloutDiff(ctx context.Context, k8sClient client.Client, expectedPodSpec *corev1.PodSpec, o client.Object) string {
	nsn := types.NamespacedName{
		Name:      o.GetName(),
		Namespace: o.GetNamespace(),
	}
	if err := k8sClient.Get(ctx, nsn, o); err != nil {
		return err.Error()
	}
	var got *corev1.PodSpec
	switch v := o.(type) {
	case *appsv1.Deployment:
		got = v.Spec.Template.Spec.DeepCopy()
	case *appsv1.StatefulSet:
		got = v.Spec.Template.Spec.DeepCopy()
	case *appsv1.DaemonSet:
		got = v.Spec.Template.Spec.DeepCopy()
	}
	return cmp.Diff(expectedPodSpec, got)
}

func getApplications(objs ...client.Object) []client.Object {
	var apps []client.Object
	for _, o := range objs {
		switch v := o.(type) {
		case *vmv1beta1.VMAgent:
			objMeta := metav1.ObjectMeta{
				Name:      v.PrefixedName(),
				Namespace: v.Namespace,
			}
			if v.IsSharded() {
				prefix := objMeta.Name
				for i := range *v.Spec.ShardCount {
					objMeta.Name = fmt.Sprintf("%s-%d", prefix, i)
					switch {
					case v.Spec.StatefulMode:
						apps = append(apps, &appsv1.StatefulSet{ObjectMeta: objMeta})
					default:
						apps = append(apps, &appsv1.Deployment{ObjectMeta: objMeta})
					}
				}
			} else {
				switch {
				case v.Spec.StatefulMode:
					apps = append(apps, &appsv1.StatefulSet{ObjectMeta: objMeta})
				case v.Spec.DaemonSetMode:
					apps = append(apps, &appsv1.DaemonSet{ObjectMeta: objMeta})
				default:
					apps = append(apps, &appsv1.Deployment{ObjectMeta: objMeta})
				}
			}
		case *vmv1.VLAgent:
			objMeta := metav1.ObjectMeta{
				Name:      v.PrefixedName(),
				Namespace: v.Namespace,
			}
			switch {
			case v.Spec.K8sCollector.Enabled:
				apps = append(apps, &appsv1.DaemonSet{ObjectMeta: objMeta})
			default:
				apps = append(apps, &appsv1.StatefulSet{ObjectMeta: objMeta})
			}
		case *vmv1.VMAnomaly:
			objMeta := metav1.ObjectMeta{
				Name:      v.PrefixedName(),
				Namespace: v.Namespace,
			}
			if v.IsSharded() {
				prefix := objMeta.Name
				for i := range *v.Spec.ShardCount {
					objMeta.Name = fmt.Sprintf("%s-%d", prefix, i)
					apps = append(apps, &appsv1.StatefulSet{ObjectMeta: objMeta})
				}
			} else {
				apps = append(apps, &appsv1.StatefulSet{ObjectMeta: objMeta})
			}
		case *vmv1beta1.VMSingle:
			apps = append(apps, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
				Name:      v.PrefixedName(),
				Namespace: v.Namespace,
			}})
		case *vmv1.VLSingle:
			apps = append(apps, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
				Name:      v.PrefixedName(),
				Namespace: v.Namespace,
			}})
		case *vmv1.VTSingle:
			apps = append(apps, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
				Name:      v.PrefixedName(),
				Namespace: v.Namespace,
			}})
		case *vmv1beta1.VMAlert:
			apps = append(apps, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
				Name:      v.PrefixedName(),
				Namespace: v.Namespace,
			}})
		case *vmv1beta1.VMAuth:
			apps = append(apps, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
				Name:      v.PrefixedName(),
				Namespace: v.Namespace,
			}})
		case *vmv1beta1.VMAlertmanager:
			apps = append(apps, &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{
				Name:      v.PrefixedName(),
				Namespace: v.Namespace,
			}})
		case *vmv1beta1.VMCluster:
			apps = append(apps,
				&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{
					Name:      v.PrefixedName(vmv1beta1.ClusterComponentSelect),
					Namespace: v.Namespace,
				}},
				&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{
					Name:      v.PrefixedName(vmv1beta1.ClusterComponentStorage),
					Namespace: v.Namespace,
				}},
				&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
					Name:      v.PrefixedName(vmv1beta1.ClusterComponentInsert),
					Namespace: v.Namespace,
				}},
			)
			if v.Spec.RequestsLoadBalancer.Enabled {
				apps = append(apps, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
					Name:      v.PrefixedName(vmv1beta1.ClusterComponentBalancer),
					Namespace: v.Namespace,
				}})
			}
		case *vmv1.VLCluster:
			apps = append(apps,
				&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
					Name:      v.PrefixedName(vmv1beta1.ClusterComponentSelect),
					Namespace: v.Namespace,
				}},
				&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{
					Name:      v.PrefixedName(vmv1beta1.ClusterComponentStorage),
					Namespace: v.Namespace,
				}},
				&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
					Name:      v.PrefixedName(vmv1beta1.ClusterComponentInsert),
					Namespace: v.Namespace,
				}},
			)
			if v.Spec.RequestsLoadBalancer.Enabled {
				apps = append(apps, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
					Name:      v.PrefixedName(vmv1beta1.ClusterComponentBalancer),
					Namespace: v.Namespace,
				}})
			}
		case *vmv1.VTCluster:
			apps = append(apps,
				&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
					Name:      v.PrefixedName(vmv1beta1.ClusterComponentSelect),
					Namespace: v.Namespace,
				}},
				&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{
					Name:      v.PrefixedName(vmv1beta1.ClusterComponentStorage),
					Namespace: v.Namespace,
				}},
				&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
					Name:      v.PrefixedName(vmv1beta1.ClusterComponentInsert),
					Namespace: v.Namespace,
				}},
			)
			if v.Spec.RequestsLoadBalancer.Enabled {
				apps = append(apps, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
					Name:      v.PrefixedName(vmv1beta1.ClusterComponentBalancer),
					Namespace: v.Namespace,
				}})
			}
		case *vmv1alpha1.VMDistributed:
			if ptr.Deref(v.Spec.VMAuth.Enabled, true) {
				auth := &vmv1beta1.VMAuth{
					ObjectMeta: metav1.ObjectMeta{
						Name: v.VMAuthName(),
					},
				}
				apps = append(apps, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
					Name:      auth.PrefixedName(),
					Namespace: v.Namespace,
				}})
			}
			for _, z := range v.Spec.Zones {
				cluster := &vmv1beta1.VMCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: z.VMClusterName(v),
					},
				}
				apps = append(apps,
					&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{
						Name:      cluster.PrefixedName(vmv1beta1.ClusterComponentSelect),
						Namespace: v.Namespace,
					}},
					&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{
						Name:      cluster.PrefixedName(vmv1beta1.ClusterComponentStorage),
						Namespace: v.Namespace,
					}},
					&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
						Name:      cluster.PrefixedName(vmv1beta1.ClusterComponentInsert),
						Namespace: v.Namespace,
					}},
				)
				if z.VMCluster.Spec.RequestsLoadBalancer.Enabled {
					apps = append(apps, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
						Name:      cluster.PrefixedName(vmv1beta1.ClusterComponentBalancer),
						Namespace: v.Namespace,
					}})
				}
				agent := &vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name: z.VMAgentName(v),
					},
				}
				if z.VMAgent.Spec.StatefulMode {
					apps = append(apps, &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{
						Name:      agent.PrefixedName(),
						Namespace: v.Namespace,
					}})
				} else {
					apps = append(apps, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
						Name:      agent.PrefixedName(),
						Namespace: v.Namespace,
					}})
				}
			}
		}
	}
	return apps
}
