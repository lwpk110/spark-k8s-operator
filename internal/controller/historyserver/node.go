package historyserver

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/zncdatadev/spark-k8s-operator/internal/util"
)

var (
	SparkHistoryPorts = []corev1.ContainerPort{
		{
			Name:          util.HttpPortName,
			ContainerPort: util.HttpPort,
		},
		//  TODO: Add GRPC port
	}
	OidcPorts = []corev1.ContainerPort{
		{
			Name:          util.OidcPortName,
			ContainerPort: util.OidcPort,
		},
	}
)
