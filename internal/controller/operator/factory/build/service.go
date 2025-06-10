package build

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// AdditionalServiceFromDefault builds service from given exist service and overrides params if needed
func AdditionalServiceFromDefault(defaultSvc *corev1.Service, svcSpec *vmv1beta1.AdditionalServiceSpec) *corev1.Service {
	if svcSpec == nil {
		return nil
	}
	result := defaultSvc.DeepCopy()
	result.Name = svcSpec.NameOrDefault(result.Name)
	// in case of labels, we must keep base labels to be able to discover this service later.
	result.Labels = labels.Merge(svcSpec.Labels, result.Labels)
	if result.Labels == nil {
		result.Labels = make(map[string]string)
	}
	result.Labels[vmv1beta1.AdditionalServiceLabel] = "managed"
	result.Annotations = labels.Merge(result.Annotations, svcSpec.Annotations)
	result.Spec = *svcSpec.Spec.DeepCopy()
	if result.Spec.Selector == nil {
		result.Spec.Selector = defaultSvc.Spec.Selector
	}
	// user may want to override port definition.
	if result.Spec.Ports == nil {
		result.Spec.Ports = defaultSvc.Spec.Ports
	}
	if result.Spec.Type == "" {
		result.Spec.Type = defaultSvc.Spec.Type
	}
	// note clusterIP not checked, its users responsibility.
	return result
}

// Service builds service for the given args and applies optional callback for it
func Service(cr builderOpts, defaultPort string, setOptions func(svc *corev1.Service)) *corev1.Service {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.GetNamespace(),
			Labels:          cr.AllLabels(),
			Annotations:     cr.AnnotationsFiltered(),
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{vmv1beta1.FinalizerName},
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: cr.SelectorLabels(),
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Protocol:   "TCP",
					Port:       intstr.Parse(defaultPort).IntVal,
					TargetPort: intstr.Parse(defaultPort),
				},
			},
		},
	}
	if setOptions != nil {
		setOptions(svc)
	}
	serviceOverrides := cr.GetAdditionalService()
	if serviceOverrides != nil && serviceOverrides.UseAsDefault {
		hasPortByName := func(name string) bool {
			for _, port := range serviceOverrides.Spec.Ports {
				if port.Name == name {
					return true
				}
			}
			return false
		}
		for _, defaultPort := range svc.Spec.Ports {
			if !hasPortByName(defaultPort.Name) {
				serviceOverrides.Spec.Ports = append(serviceOverrides.Spec.Ports, defaultPort)
			}
		}
		if serviceOverrides.Spec.Type == "" {
			serviceOverrides.Spec.Type = svc.Spec.Type
		}
		if serviceOverrides.Spec.ClusterIP == "" && serviceOverrides.Spec.Type == svc.Spec.Type {
			serviceOverrides.Spec.ClusterIP = svc.Spec.ClusterIP
		}

		serviceOverrides.Spec.Selector = svc.Spec.Selector
		if len(serviceOverrides.Labels) > 0 {
			svc.Labels = labels.Merge(serviceOverrides.Labels, svc.Labels)
		}
		if len(serviceOverrides.Annotations) > 0 {
			svc.Annotations = labels.Merge(serviceOverrides.Annotations, svc.Annotations)
		}

		svc.Spec = serviceOverrides.Spec
	}

	return svc
}

// AppendInsertPortsToService conditionally appends insert ports to the given service definition
func AppendInsertPortsToService(ip *vmv1beta1.InsertPorts, svc *corev1.Service) {
	if ip == nil || svc == nil {
		return
	}
	hasPortByName := func(name string) bool {
		for _, port := range svc.Spec.Ports {
			if port.Name == name {
				return true
			}
		}
		return false
	}
	// user want to use own definition for graphite
	if ip.GraphitePort != "" && !hasPortByName("graphite-tcp") && !hasPortByName("graphite-udp") {
		svc.Spec.Ports = append(svc.Spec.Ports,
			corev1.ServicePort{
				Name:       "graphite-tcp",
				Protocol:   "TCP",
				Port:       intstr.Parse(ip.GraphitePort).IntVal,
				TargetPort: intstr.Parse(ip.GraphitePort),
			},
			corev1.ServicePort{
				Name:       "graphite-udp",
				Protocol:   "UDP",
				Port:       intstr.Parse(ip.GraphitePort).IntVal,
				TargetPort: intstr.Parse(ip.GraphitePort),
			},
		)
	}
	if ip.InfluxPort != "" && !hasPortByName("influx-tcp") && !hasPortByName("influx-udp") {
		svc.Spec.Ports = append(svc.Spec.Ports,
			corev1.ServicePort{
				Name:       "influx-tcp",
				Protocol:   "TCP",
				Port:       intstr.Parse(ip.InfluxPort).IntVal,
				TargetPort: intstr.Parse(ip.InfluxPort),
			},
			corev1.ServicePort{
				Name:       "influx-udp",
				Protocol:   "UDP",
				Port:       intstr.Parse(ip.InfluxPort).IntVal,
				TargetPort: intstr.Parse(ip.InfluxPort),
			},
		)
	}
	if ip.OpenTSDBPort != "" && !hasPortByName("opentsdb-tcp") && !hasPortByName("opentsdb-udp") {
		svc.Spec.Ports = append(svc.Spec.Ports,
			corev1.ServicePort{
				Name:       "opentsdb-tcp",
				Protocol:   "TCP",
				Port:       intstr.Parse(ip.OpenTSDBPort).IntVal,
				TargetPort: intstr.Parse(ip.OpenTSDBPort),
			},
			corev1.ServicePort{
				Name:       "opentsdb-udp",
				Protocol:   "UDP",
				Port:       intstr.Parse(ip.OpenTSDBPort).IntVal,
				TargetPort: intstr.Parse(ip.OpenTSDBPort),
			},
		)
	}
	if ip.OpenTSDBHTTPPort != "" && !hasPortByName("opentsdb-http") {
		svc.Spec.Ports = append(svc.Spec.Ports,
			corev1.ServicePort{
				Name:       "opentsdb-http",
				Protocol:   "TCP",
				Port:       intstr.Parse(ip.OpenTSDBHTTPPort).IntVal,
				TargetPort: intstr.Parse(ip.OpenTSDBHTTPPort),
			})
	}
}
