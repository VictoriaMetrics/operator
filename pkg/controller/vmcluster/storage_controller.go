package vmcluster

import (
	"context"
	"fmt"
	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/pkg/apis/victoriametrics/v1beta1"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func (c *vmController) reconcileStorageLoop(ctx context.Context) (reconcile.Result, error) {
	if !c.cluster.Spec.VmStorage.Enabled {
		return reconcile.Result{}, nil
	}
	status, err := c.handleStorage(ctx, c.cluster)
	if err != nil {
		return reconcile.Result{}, err
	}
	if err := c.handleStorageService(ctx, c.cluster); err != nil {
		return reconcile.Result{}, err
	}
	if c.cluster.Status.VmStorage.Status != status {
		c.cluster.Status.VmStorage.Status = status
		log.Info("update storage status", "status", status)
		if err := c.client.Status().Update(ctx, c.cluster); err != nil {
			return reconcile.Result{}, err
		}
	}
	if status == victoriametricsv1beta1.VmStorageStatusExpanding {
		log.Info("cluster still expanding requeue request")
		return reconcile.Result{Requeue: true}, nil
	}
	return reconcile.Result{}, nil
}

func (c *vmController) handleStorage(ctx context.Context, cluster *victoriametricsv1beta1.VmCluster) (string, error) {
	sts := c.newStorageSts(cluster)
	if cluster.Status.VmStorage.Status == victoriametricsv1beta1.VmStorageStatusExpanding {
		log.Info("cluster in expanding state")
		wait, err := c.waitForExpanding(ctx, cluster, sts.Labels)
		if err != nil {
			return "", nil
		}
		log.Info("storage still expanding")
		if wait {
			return victoriametricsv1beta1.VmStorageStatusExpanding, nil
		}
	}
	status := victoriametricsv1beta1.VmStorageStatusOperational
	existingSts := &v1.StatefulSet{}
	if err := c.client.Get(ctx, types.NamespacedName{Namespace: sts.Namespace, Name: sts.Name}, existingSts); err != nil {
		if !errors.IsNotFound(err) {
			return status, err
		}
		log.Info("create new storage statefulset")
		return status, c.client.Create(ctx, sts)
	}
	if *sts.Spec.Replicas > *existingSts.Spec.Replicas {
		log.Info("need to wait for cluster expansion")
		status = victoriametricsv1beta1.VmStorageStatusExpanding
	}
	return status, c.updateStorage(ctx, sts, existingSts)
}

func (c *vmController) waitForExpanding(ctx context.Context, cluster *victoriametricsv1beta1.VmCluster, lbs map[string]string) (bool, error) {
	log.Info("check pods availability")
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(lbs)
	listOps := &client.ListOptions{Namespace: cluster.Namespace, LabelSelector: labelSelector}
	if err := c.client.List(ctx, podList, listOps); err != nil {
		return false, err
	}
	var availableCount int32
	for _, pod := range podList.Items {
		if pod.ObjectMeta.DeletionTimestamp != nil {
			continue
		}
		if pod.Status.Phase == corev1.PodRunning {
			availableCount++
		}
	}
	log.Info("pods available", "count", availableCount, "spec-count", cluster.Spec.VmStorage.ReplicaCount)
	return availableCount != cluster.Spec.VmStorage.ReplicaCount, nil
}

func (c *vmController) newStorageSts(cluster *victoriametricsv1beta1.VmCluster) *v1.StatefulSet {
	storageSpec := cluster.Spec.VmStorage
	commonLabels := cluster.StorageCommonLables()
	for k, v := range storageSpec.Labels {
		commonLabels[k] = v
	}
	readinessProbeHandler := corev1.Handler{
		HTTPGet: &corev1.HTTPGetAction{
			Port:   intstr.FromString("http"),
			Scheme: "HTTP",
			Path:   "/health",
		},
	}
	livenessProbeHandler := corev1.Handler{
		TCPSocket: &corev1.TCPSocketAction{
			Port: intstr.FromString("http"),
		},
	}
	args := []string{
		fmt.Sprintf("--retentionPeriod=%d", storageSpec.RetentionPeriod),
		fmt.Sprintf("--storageDataPath=%s", storageSpec.PersistentVolume.MountPath),
	}
	for arg, value := range storageSpec.ExtraArgs {
		args = append(args, fmt.Sprintf("--%s:%s", arg, value))
	}
	// fixme is it correct place?
	volume := corev1.VolumeMount{
		Name: victoriametricsv1beta1.StorageDefaultVolumeName,
	}
	var pvc []corev1.PersistentVolumeClaim
	if storageSpec.PersistentVolume.Enabled {
		pvc = append(pvc, corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:        victoriametricsv1beta1.StorageDefaultVolumeName,
				Annotations: storageSpec.PersistentVolume.Annotations,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: storageSpec.PersistentVolume.AccessModes,
				Selector:    nil,
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						// validation must be done before
						corev1.ResourceStorage: resource.MustParse(storageSpec.PersistentVolume.Size),
					},
				},
			},
		})
		volume.MountPath = storageSpec.PersistentVolume.MountPath
		volume.SubPath = storageSpec.PersistentVolume.SubPath
	}

	return &v1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            fmt.Sprintf("%s-%s", cluster.Name(), storageSpec.GetName()),
			Namespace:       cluster.Namespace,
			Labels:          commonLabels,
			Annotations:     storageSpec.Annotations,
			OwnerReferences: cluster.AsOwner(),
		},
		Spec: v1.StatefulSetSpec{
			Replicas: &storageSpec.ReplicaCount,
			Selector: &metav1.LabelSelector{MatchLabels: commonLabels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      commonLabels,
					Annotations: storageSpec.PodAnnotations,
				},
				Spec: corev1.PodSpec{
					Volumes: nil,
					Containers: []corev1.Container{
						{
							Name:            "vmstorage",
							Image:           fmt.Sprintf("%s:%s", storageSpec.Image.Repository, storageSpec.Image.Tag),
							ImagePullPolicy: storageSpec.Image.PullPolicy,
							Args:            args,
							Ports: []corev1.ContainerPort{
								{Name: "http", ContainerPort: 8482},
								{Name: "vminsert", ContainerPort: 8400},
								{Name: "vmselect", ContainerPort: 8401},
							},
							ReadinessProbe: &corev1.Probe{
								Handler:             readinessProbeHandler,
								InitialDelaySeconds: 5,
								TimeoutSeconds:      5,
								PeriodSeconds:       15,
							},
							LivenessProbe: &corev1.Probe{
								Handler:             livenessProbeHandler,
								InitialDelaySeconds: 5,
								TimeoutSeconds:      5,
								PeriodSeconds:       15,
							},
							Resources: storageSpec.Resources,
							// todo extraconfigmaps
							// todo extra volume mounts
							// todo extra secrets
							VolumeMounts: []corev1.VolumeMount{volume},
						},
					},
					PriorityClassName:             storageSpec.PriorityClassName,
					SchedulerName:                 storageSpec.SchedulerName,
					RestartPolicy:                 "",
					TerminationGracePeriodSeconds: &storageSpec.TerminationGracePeriodSeconds,
					SecurityContext:               &storageSpec.SecurityContext,
					Affinity:                      &storageSpec.Affinity,
					Tolerations:                   storageSpec.Tolerations,
					NodeSelector:                  storageSpec.NodeSelector,
					ServiceAccountName:            storageSpec.ServiceAccountName,
				},
			},
			VolumeClaimTemplates: pvc,
			PodManagementPolicy:  storageSpec.PodManagementPolicy,
		},
	}
}

func (c *vmController) updateStorage(ctx context.Context, want, have *v1.StatefulSet) error {
	// todo deep equal of two statefulsets to define whether update or not
	for k, v := range have.Labels {
		want.Labels[k] = v
	}
	for k, v := range have.Annotations {
		want.Annotations[k] = v
	}
	log.Info("update storage statefulset")
	return c.client.Update(ctx, want)
}

func (c *vmController) handleStorageService(ctx context.Context, cluster *victoriametricsv1beta1.VmCluster) error {
	svc := newStorageService(cluster)
	existingSvc := &corev1.Service{}
	if err := c.client.Get(ctx, types.NamespacedName{Namespace: svc.Namespace, Name: svc.Name}, existingSvc); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		log.Info("create new storage service")
		return c.client.Create(ctx, svc)
	}
	return c.updateStorageService(ctx, svc, existingSvc)
}

func newStorageService(cluster *victoriametricsv1beta1.VmCluster) *corev1.Service {
	storageSpec := cluster.Spec.VmStorage
	commonLabels := cluster.StorageCommonLables()
	for k, v := range storageSpec.Service.Labels {
		commonLabels[k] = v
	}
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            fmt.Sprintf("%s-%s", cluster.Name(), storageSpec.GetName()),
			Namespace:       cluster.Namespace,
			Labels:          commonLabels,
			Annotations:     storageSpec.Service.Annotations,
			OwnerReferences: cluster.AsOwner(),
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Protocol:   corev1.ProtocolTCP,
					Port:       storageSpec.Service.ServicePort,
					TargetPort: intstr.FromString("http"),
				},
				{
					Name:       "vmselect",
					Protocol:   corev1.ProtocolTCP,
					Port:       storageSpec.Service.VmselectPort,
					TargetPort: intstr.FromString("vmselect"),
				},
				{
					Name:       "vminsert",
					Protocol:   corev1.ProtocolTCP,
					Port:       storageSpec.Service.VminsertPort,
					TargetPort: intstr.FromString("vminsert"),
				},
			},
			Selector:  cluster.StorageCommonLables(),
			ClusterIP: "None",
		},
	}
}

func (c *vmController) updateStorageService(ctx context.Context, want, have *corev1.Service) error {
	// todo deep equal of two services to define whether update or not
	for k, v := range have.Labels {
		want.Labels[k] = v
	}
	for k, v := range have.Annotations {
		want.Annotations[k] = v
	}
	if have.ResourceVersion != "" {
		want.ResourceVersion = have.ResourceVersion
	}
	log.Info("update storage service")
	return c.client.Update(ctx, want)
}
