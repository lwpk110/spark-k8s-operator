package historyserver

import (
"strconv"

"github.com/zncdatadev/operator-go/pkg/builder"
corev1 "k8s.io/api/core/v1"

"github.com/zncdatadev/spark-k8s-operator/internal/util"
)

// NewRoleGroupService creates a metrics service using the new fluent builder API.
func NewRoleGroupService(
clusterName string,
roleGroupName string,
namespace string,
labels map[string]string,
) *corev1.Service {
	metricsPort := util.GetMetricsPort()
	serviceName := clusterName + "-" + roleGroupName + "-metrics"
	scheme := "http"

	svcLabels := make(map[string]string)
	for k, v := range labels {
		svcLabels[k] = v
	}
	svcLabels["prometheus.io/scrape"] = "true"

	annotations := map[string]string{
		"prometheus.io/scrape": "true",
		"prometheus.io/path":   "/prom",
		"prometheus.io/port":   strconv.Itoa(int(metricsPort)),
		"prometheus.io/scheme": scheme,
	}

	return builder.NewServiceBuilder(serviceName, namespace).
		WithLabels(svcLabels).
		WithAnnotations(annotations).
		WithSelector(labels).
		WithServiceType(builder.ServiceTypeHeadless).
		AddPortSimple(util.HttpPortName, metricsPort, corev1.ProtocolTCP).
		Build()
}

// NewRoleGroupHeadlessService creates a headless service for StatefulSet network identity.
func NewRoleGroupHeadlessService(
name string,
namespace string,
labels map[string]string,
ports []corev1.ContainerPort,
) *corev1.Service {
	svcBuilder := builder.NewServiceBuilder(name+"-headless", namespace).
		WithLabels(labels).
		WithSelector(labels).
		WithServiceType(builder.ServiceTypeHeadless)

	for _, p := range ports {
		svcBuilder.AddPortSimple(p.Name, p.ContainerPort, p.Protocol)
	}

	return svcBuilder.Build()
}

// NewRoleGroupClientService creates a ClusterIP service for client access.
func NewRoleGroupClientService(
name string,
namespace string,
labels map[string]string,
ports []corev1.ContainerPort,
) *corev1.Service {
	svcBuilder := builder.NewServiceBuilder(name, namespace).
		WithLabels(labels).
		WithSelector(labels)

	for _, p := range ports {
		svcBuilder.AddPortSimple(p.Name, p.ContainerPort, p.Protocol)
	}

	return svcBuilder.Build()
}
