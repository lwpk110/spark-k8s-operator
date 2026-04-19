package util

const (
	HttpPortName = "http"
	GrpcPortName = "grpc"
	OidcPortName = "oidc"
	HttpPort     = 18080
	GrpcPort     = 15002
	OidcPort     = 4180
)

// GetMetricsPort returns the metrics port for a given role
func GetMetricsPort() int32 {
	return HttpPort
}

// GetMetricsServiceNameForGroup returns the metrics service name for a given cluster and role group.
func GetMetricsServiceNameForGroup(clusterName, roleGroupName string) string {
	return clusterName + "-" + roleGroupName + "-metrics"
}
