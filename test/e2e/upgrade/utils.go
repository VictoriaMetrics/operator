package upgrade

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:staticcheck
	. "github.com/onsi/gomega"    //nolint:staticcheck
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/internal/manager"
)

const (
	operatorImageBase = "quay.io/victoriametrics/operator:"
)

// sanitizeDeploymentSpec clears fields that legitimately differ between operator versions
// such as config-reloader images.
func sanitizeDeploymentSpec(spec *appsv1.DeploymentSpec) *appsv1.DeploymentSpec {
	for i := range spec.Template.Spec.InitContainers {
		spec.Template.Spec.InitContainers[i].Image = ""
	}
	for i := range spec.Template.Spec.Containers {
		c := &spec.Template.Spec.Containers[i]
		if c.Name == "config-reloader" {
			c.Image = ""
		}
	}
	return spec
}

// operatorEnvVars builds the env var list for the operator pod,
// TODO[vrutkovs]: do we need to copy it?
func operatorEnvVars(watchNamespace string) []corev1.EnvVar {
	envs := map[string]string{
		"VM_CONTAINERREGISTRY":                           "quay.io",
		"VM_VMALERTMANAGER_ALERTMANAGERDEFAULTBASEIMAGE": "prometheus/alertmanager",
		"VM_ENABLEDPROMETHEUSCONVERTEROWNERREFERENCES":   "true",
		"VM_GATEWAY_API_ENABLED":                         "true",
		"VM_PODWAITREADYTIMEOUT":                         "20s",
		"VM_PODWAITREADYINTERVALCHECK":                   "1s",
		"VM_APPREADYTIMEOUT":                             "50s",
		"WATCH_NAMESPACE":                                watchNamespace,
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
	for k, v := range envs {
		result = append(result, corev1.EnvVar{Name: k, Value: v})
	}
	return result
}

func deployOldOperator(ctx context.Context, k8sClient client.Client, version, watchNamespace string) {
	GinkgoHelper()

	By("creating ServiceAccount for operator")
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-operator",
			Namespace: watchNamespace,
		},
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
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm-operator",
			Namespace: watchNamespace,
		},
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
					ServiceAccountName: "vm-operator",
					Containers: []corev1.Container{
						{
							Name:  "manager",
							Image: operatorImageBase + version,
							Args: []string{
								"--health-probe-bind-address=:8081",
							},
							Env: operatorEnvVars(watchNamespace),
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
	Expect(k8sClient.Create(ctx, dep)).ToNot(HaveOccurred())

	By("waiting for operator to be ready")
	Eventually(func() error {
		var d appsv1.Deployment
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: dep.Name, Namespace: dep.Namespace}, &d); err != nil {
			return err
		}
		if d.Status.ReadyReplicas < 1 {
			return fmt.Errorf("operator not ready yet, readyReplicas=%d", d.Status.ReadyReplicas)
		}
		return nil
	}, 120*time.Second, 3*time.Second).ShouldNot(HaveOccurred())
}

func removeOldOperator(ctx context.Context, k8sClient client.Client, watchNamespace string) {
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
		err := k8sClient.Get(ctx, types.NamespacedName{Name: dep.Name, Namespace: dep.Namespace}, &appsv1.Deployment{})
		return k8serrors.IsNotFound(err)
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

func startNewOperator(ctx context.Context) (context.CancelFunc, chan struct{}) {
	os.Args = append(os.Args[:1],
		"--metrics-bind-address", "0",
		"--pprof-addr", "0",
		"--health-probe-bind-address", "0",
		"--controller.maxConcurrentReconciles", "30",
	)
	managerDone := make(chan struct{})
	var managerCtx context.Context
	var cancelManager context.CancelFunc
	managerCtx, cancelManager = context.WithCancel(ctx)
	go func() {
		defer GinkgoRecover()
		err := manager.RunManager(managerCtx)
		close(managerDone)
		Expect(err).NotTo(HaveOccurred())
	}()

	return cancelManager, managerDone
}

func cleanupNamespace(ctx context.Context, k8sClient client.Client, watchNamespace string) {
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
	Eventually(func() bool {
		err := k8sClient.Get(ctx, types.NamespacedName{Name: watchNamespace}, &corev1.Namespace{})
		return k8serrors.IsNotFound(err)
	}, 120*time.Second, 2*time.Second).Should(BeTrue(), "timeout waiting for namespace to be deleted")
}
