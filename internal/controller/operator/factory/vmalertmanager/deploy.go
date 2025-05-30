package vmalertmanager

import (
	"context"
	"fmt"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

const templatesDir = "/etc/vm/templates"

var badConfigsTotal = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "operator_alertmanager_bad_objects_count",
	Help: "Number of child CRDs with bad or incomplete configurations",
	ConstLabels: prometheus.Labels{
		"crd": "vmalertmanager_config",
	},
})

func init() {
	metrics.Registry.MustRegister(badConfigsTotal)
}

// CreateOrUpdate creates alertmanager and and builds config for it
func CreateOrUpdate(ctx context.Context, cr *vmv1beta1.VMAlertmanager, rclient client.Client) error {
	var prevCR *vmv1beta1.VMAlertmanager
	if cr.ParsedLastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.ParsedLastAppliedSpec
	}
	if err := deletePrevStateResources(ctx, cr, rclient); err != nil {
		return fmt.Errorf("cannot delete objects from prev state: %w", err)
	}
	if cr.IsOwnsServiceAccount() {
		var prevSA *corev1.ServiceAccount
		if prevCR != nil {
			prevSA = build.ServiceAccount(prevCR)
		}
		if err := reconcile.ServiceAccount(ctx, rclient, build.ServiceAccount(cr), prevSA); err != nil {
			return fmt.Errorf("failed create service account: %w", err)
		}
		if ptr.Deref(cr.Spec.UseVMConfigReloader, false) {
			if err := createConfigSecretAccess(ctx, rclient, cr, prevCR); err != nil {
				return err
			}
		}
	}

	if err := CreateOrUpdateConfig(ctx, rclient, cr, nil); err != nil {
		return err
	}

	service, err := createOrUpdateService(ctx, rclient, cr, prevCR)
	if err != nil {
		return err
	}
	if !ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
		err := reconcile.VMServiceScrapeForCRD(ctx, rclient, build.VMServiceScrapeForAlertmanager(service, cr))
		if err != nil {
			return err
		}
	}

	if cr.Spec.PodDisruptionBudget != nil {
		var prevPDB *policyv1.PodDisruptionBudget
		if prevCR != nil && prevCR.Spec.PodDisruptionBudget != nil {
			prevPDB = build.PodDisruptionBudget(prevCR, prevCR.Spec.PodDisruptionBudget)
		}
		if err := reconcile.PDB(ctx, rclient, build.PodDisruptionBudget(cr, cr.Spec.PodDisruptionBudget), prevPDB); err != nil {
			return err
		}
	}
	var prevStatefulSet *appsv1.StatefulSet
	if prevCR != nil {
		var err error
		prevStatefulSet, err = newStatefulSet(prevCR)
		if err != nil {
			return fmt.Errorf("cannot generate prev alertmanager sts, name: %s,err: %w", cr.Name, err)
		}
	}
	newStatefulSet, err := newStatefulSet(cr)
	if err != nil {
		return fmt.Errorf("cannot generate alertmanager sts, name: %s,err: %w", cr.Name, err)
	}

	stsOpts := reconcile.StatefulSetOptions{
		HasClaim:       len(newStatefulSet.Spec.VolumeClaimTemplates) > 0,
		SelectorLabels: cr.SelectorLabels,
	}
	return reconcile.HandleStatefulSetUpdate(ctx, rclient, stsOpts, newStatefulSet, prevStatefulSet)
}

func newStatefulSet(cr *vmv1beta1.VMAlertmanager) (*appsv1.StatefulSet, error) {
	if cr.Spec.Retention == "" {
		cr.Spec.Retention = defaultRetention
	}

	spec, err := newPodSpec(cr)
	if err != nil {
		return nil, err
	}

	app := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Labels:          cr.AllLabels(),
			Annotations:     cr.AnnotationsFiltered(),
			Namespace:       cr.Namespace,
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{vmv1beta1.FinalizerName},
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: cr.PrefixedName(),
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: cr.Spec.RollingUpdateStrategy,
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(),
			},
			VolumeClaimTemplates: cr.Spec.ClaimTemplates,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      cr.PodLabels(),
					Annotations: cr.PodAnnotations(),
				},
				Spec: *spec,
			},
		},
	}

	build.StatefulSetAddCommonParams(app, ptr.Deref(cr.Spec.UseStrictSecurity, false), &cr.Spec.CommonApplicationDeploymentParams)
	cr.Spec.Storage.IntoStatefulSetVolume(cr.GetVolumeName(), &app.Spec)
	app.Spec.Template.Spec.Volumes = append(app.Spec.Template.Spec.Volumes, cr.Spec.Volumes...)

	return app, nil
}

func createOrUpdateService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAlertmanager) (*corev1.Service, error) {
	port, err := strconv.ParseInt(cr.Port(), 10, 32)
	if err != nil {
		return nil, fmt.Errorf("cannot reconcile additional service for vmalertmanager: failed to parse port: %w", err)
	}
	newService := build.Service(cr, cr.Spec.PortName, func(svc *corev1.Service) {
		svc.Spec.ClusterIP = "None"
		svc.Spec.Ports[0].Port = int32(port)
		svc.Spec.PublishNotReadyAddresses = true
		svc.Spec.Ports = append(svc.Spec.Ports,
			corev1.ServicePort{
				Name:       "tcp-mesh",
				Port:       9094,
				TargetPort: intstr.FromInt(9094),
				Protocol:   corev1.ProtocolTCP,
			},
			corev1.ServicePort{
				Name:       "udp-mesh",
				Port:       9094,
				TargetPort: intstr.FromInt(9094),
				Protocol:   corev1.ProtocolUDP,
			},
		)
	})
	var prevService, prevAdditionalService *corev1.Service
	if prevCR != nil {
		prevPort, err := strconv.ParseInt(prevCR.Port(), 10, 32)
		if err != nil {
			return nil, fmt.Errorf("cannot reconcile additional service for vmalertmanager: failed to parse port: %w", err)
		}
		prevService = build.Service(prevCR, prevCR.Spec.PortName, func(svc *corev1.Service) {
			svc.Spec.ClusterIP = "None"
			svc.Spec.Ports[0].Port = int32(prevPort)
			svc.Spec.PublishNotReadyAddresses = true
			svc.Spec.Ports = append(svc.Spec.Ports,
				corev1.ServicePort{
					Name:       "tcp-mesh",
					Port:       9094,
					TargetPort: intstr.FromInt(9094),
					Protocol:   corev1.ProtocolTCP,
				},
				corev1.ServicePort{
					Name:       "udp-mesh",
					Port:       9094,
					TargetPort: intstr.FromInt(9094),
					Protocol:   corev1.ProtocolUDP,
				},
			)
		})
		prevAdditionalService = build.AdditionalServiceFromDefault(prevService, cr.Spec.ServiceSpec)
	}

	if err := cr.Spec.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalService := build.AdditionalServiceFromDefault(newService, s)
		if additionalService.Name == newService.Name {
			return fmt.Errorf("vmalertmanager additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, newService.Name)
		}
		if err := reconcile.Service(ctx, rclient, additionalService, prevAdditionalService); err != nil {
			return fmt.Errorf("cannot reconcile additional service for vmalertmanager: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if err := reconcile.Service(ctx, rclient, newService, prevService); err != nil {
		return nil, fmt.Errorf("cannot reconcile service for vmalertmanager: %w", err)
	}
	return newService, nil
}

func deletePrevStateResources(ctx context.Context, cr *vmv1beta1.VMAlertmanager, rclient client.Client) error {
	if cr.ParsedLastAppliedSpec == nil {
		return nil
	}
	prevSvc, currSvc := cr.ParsedLastAppliedSpec.ServiceSpec, cr.Spec.ServiceSpec
	if err := reconcile.AdditionalServices(ctx, rclient, cr.PrefixedName(), cr.Namespace, prevSvc, currSvc); err != nil {
		return fmt.Errorf("cannot remove additional service: %w", err)
	}

	objMeta := metav1.ObjectMeta{Name: cr.PrefixedName(), Namespace: cr.Namespace}
	if cr.Spec.PodDisruptionBudget == nil && cr.ParsedLastAppliedSpec.PodDisruptionBudget != nil {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &policyv1.PodDisruptionBudget{ObjectMeta: objMeta}); err != nil {
			return fmt.Errorf("cannot delete PDB from prev state: %w", err)
		}
	}
	if ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) && !ptr.Deref(cr.ParsedLastAppliedSpec.DisableSelfServiceScrape, false) {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{ObjectMeta: objMeta}); err != nil {
			return fmt.Errorf("cannot remove serviceScrape: %w", err)
		}
	}

	return nil
}
