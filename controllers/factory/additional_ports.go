package factory

import (
	"fmt"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func buildAdditionalContainerPorts(ports []corev1.ContainerPort, ip *victoriametricsv1beta1.InsertPorts) []corev1.ContainerPort {
	if ip == nil {
		return ports
	}
	if ip.GraphitePort != "" {
		ports = append(ports,
			corev1.ContainerPort{
				Name:          "graphite-tcp",
				Protocol:      "TCP",
				ContainerPort: intstr.Parse(ip.GraphitePort).IntVal,
			},
			corev1.ContainerPort{
				Name:          "graphite-udp",
				Protocol:      "UDP",
				ContainerPort: intstr.Parse(ip.GraphitePort).IntVal,
			},
		)
	}
	if ip.InfluxPort != "" {
		ports = append(ports,
			corev1.ContainerPort{
				Name:          "influx-tcp",
				Protocol:      "TCP",
				ContainerPort: intstr.Parse(ip.InfluxPort).IntVal,
			},
			corev1.ContainerPort{
				Name:          "influx-udp",
				Protocol:      "UDP",
				ContainerPort: intstr.Parse(ip.InfluxPort).IntVal,
			},
		)
	}
	if ip.OpenTSDBPort != "" {
		ports = append(ports,
			corev1.ContainerPort{
				Name:          "opentsdb-tcp",
				Protocol:      "TCP",
				ContainerPort: intstr.Parse(ip.OpenTSDBPort).IntVal,
			},
			corev1.ContainerPort{
				Name:          "opentsdb-udp",
				Protocol:      "UDP",
				ContainerPort: intstr.Parse(ip.OpenTSDBPort).IntVal,
			},
		)
	}
	if ip.OpenTSDBHTTPPort != "" {
		ports = append(ports,
			corev1.ContainerPort{
				Name:          "opentsdb-http",
				Protocol:      "TCP",
				ContainerPort: intstr.Parse(ip.OpenTSDBHTTPPort).IntVal,
			},
		)
	}
	return ports
}

func buildArgsForAdditionalPorts(args []string, ip *victoriametricsv1beta1.InsertPorts) []string {
	if ip == nil {
		return args
	}
	if ip.GraphitePort != "" {
		args = append(args, fmt.Sprintf("--graphiteListenAddr=:%s", ip.GraphitePort))
	}
	if ip.InfluxPort != "" {
		args = append(args, fmt.Sprintf("--influxListenAddr=:%s", ip.InfluxPort))
	}
	if ip.OpenTSDBPort != "" {
		args = append(args, fmt.Sprintf("--opentsdbListenAddr=:%s", ip.OpenTSDBPort))
	}
	if ip.OpenTSDBHTTPPort != "" {
		args = append(args, fmt.Sprintf("--opentsdbHTTPListenAddr=:%s", ip.OpenTSDBHTTPPort))
	}
	return args
}

func appendInsertPortsToService(ip *victoriametricsv1beta1.InsertPorts, svc *corev1.Service) {
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
