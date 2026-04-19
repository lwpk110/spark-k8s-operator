package historyserver

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/zncdatadev/operator-go/pkg/builder"
	"github.com/zncdatadev/operator-go/pkg/config"
	"github.com/zncdatadev/operator-go/pkg/constant"
	"github.com/zncdatadev/operator-go/pkg/vector"
	corev1 "k8s.io/api/core/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	sparkv1alpha1 "github.com/zncdatadev/spark-k8s-operator/api/v1alpha1"
)

type SparkConfigMapBuilder struct {
	k8sClient       ctrlclient.Client
	name            string
	namespace       string
	clusterName     string
	roleName        string
	roleGroupName   string
	clusterConfig   *sparkv1alpha1.ClusterConfigSpec
	roleGroupConfig *sparkv1alpha1.ConfigSpec
	cr              *sparkv1alpha1.SparkHistoryServer
	replicas        int32
	labels          map[string]string
}

func NewSparkConfigMapBuilder(
	k8sClient ctrlclient.Client,
	name string,
	namespace string,
	clusterName string,
	roleName string,
	roleGroupName string,
	clusterConfig *sparkv1alpha1.ClusterConfigSpec,
	roleGroupConfig *sparkv1alpha1.ConfigSpec,
	cr *sparkv1alpha1.SparkHistoryServer,
	replicas int32,
	labels map[string]string,
) *SparkConfigMapBuilder {
	copiedLabels := make(map[string]string)
	for k, v := range labels {
		copiedLabels[k] = v
	}

	return &SparkConfigMapBuilder{
		k8sClient:       k8sClient,
		name:            name,
		namespace:       namespace,
		clusterName:     clusterName,
		roleName:        roleName,
		roleGroupName:   roleGroupName,
		clusterConfig:   clusterConfig,
		roleGroupConfig: roleGroupConfig,
		cr:              cr,
		replicas:        replicas,
		labels:          copiedLabels,
	}
}

func (b *SparkConfigMapBuilder) getS3LogConfig(ctx context.Context) (*S3Logconfig, error) {
	if b.clusterConfig == nil || b.clusterConfig.LogFileDirectory == nil || b.clusterConfig.LogFileDirectory.S3 == nil {
		return nil, nil
	}
	return NewS3Logconfig(ctx, b.k8sClient, b.namespace, b.clusterConfig.LogFileDirectory.S3)
}

func (b *SparkConfigMapBuilder) Build(ctx context.Context) (*corev1.ConfigMap, error) {
	s3LogConfig, err := b.getS3LogConfig(ctx)
	if err != nil {
		return nil, err
	}

	cmBuilder := builder.NewConfigMapBuilder(b.name, b.namespace).WithLabels(b.labels)
	cmBuilder.AddData(SparkConfigDefauleFileName, b.getSparkDefaults(s3LogConfig))

	logProperties, err := b.getLog4j()
	if err != nil {
		return nil, err
	}
	cmBuilder.AddData("log4j2.properties", logProperties)

	vectorConfig, err := b.getVectorConfig(ctx)
	if err != nil {
		return nil, err
	}
	if vectorConfig != "" {
		cmBuilder.AddData(vector.VectorConfigFileName, vectorConfig)
	}

	return cmBuilder.Build(), nil
}

func (b *SparkConfigMapBuilder) getVectorConfig(ctx context.Context) (string, error) {
	if b.clusterConfig == nil || b.clusterConfig.VectorAggregatorConfigMapName == "" {
		return "", nil
	}

	aggregatorAddress, err := vector.DiscoverAggregatorAddress(ctx, b.k8sClient, b.namespace, b.clusterConfig.VectorAggregatorConfigMapName)
	if err != nil {
		return "", err
	}

	return vector.RenderVectorConfig(vector.VectorConfigData{
		LogDir:            constant.KubedoopLogDir,
		AggregatorAddress: aggregatorAddress,
		Namespace:         b.namespace,
		ClusterName:       b.clusterName,
		RoleName:          b.roleName,
		RoleGroupName:     b.roleGroupName,
	})
}

func toLogLevel(level string) config.LogLevel {
	switch strings.ToUpper(level) {
	case "TRACE":
		return config.LogLevelTrace
	case "DEBUG":
		return config.LogLevelDebug
	case "WARN":
		return config.LogLevelWarn
	case "ERROR":
		return config.LogLevelError
	case "FATAL":
		return config.LogLevelFatal
	default:
		return config.LogLevelInfo
	}
}

func (b *SparkConfigMapBuilder) getLog4j() (string, error) {
	configs := map[string]config.LoggerConfig{}

	if b.roleGroupConfig != nil && b.roleGroupConfig.RoleGroupConfigSpec != nil && b.roleGroupConfig.RoleGroupConfigSpec.Logging != nil {
		if containerLogging, ok := b.roleGroupConfig.RoleGroupConfigSpec.Logging.Containers[SparkHistoryContainerName]; ok {
			for loggerName, loggerSpec := range containerLogging.Loggers {
				if loggerSpec == nil {
					continue
				}
				configs[loggerName] = config.LoggerConfig{Name: loggerName, Level: toLogLevel(loggerSpec.Level)}
			}
			if containerLogging.Console != nil {
				configs["root"] = config.LoggerConfig{Name: "root", Level: toLogLevel(containerLogging.Console.Level)}
			}
		}
	}

	if _, ok := configs["root"]; !ok {
		configs["root"] = config.LoggerConfig{Name: "root", Level: config.LogLevelInfo}
	}

	return config.GenerateLog4j2(configs)
}

func (b *SparkConfigMapBuilder) isCleaner() (bool, error) {
	if b.cr == nil || b.cr.Spec.Node == nil {
		return false, nil
	}

	cleanerGroupCount := 0
	for roleGroupName, roleGroup := range b.cr.Spec.Node.RoleGroups {
		if roleGroup == nil || roleGroup.Config == nil || roleGroup.Config.Cleaner == nil {
			continue
		}
		if *roleGroup.Config.Cleaner {
			cleanerGroupCount++
			if roleGroup.Replicas != nil && *roleGroup.Replicas > 1 {
				return false, fmt.Errorf("role group %s has cleaner enabled but replicas > 1", roleGroupName)
			}
		}
	}

	if cleanerGroupCount > 1 {
		return false, fmt.Errorf("more than one role group has cleaner enabled")
	}

	if b.roleGroupConfig != nil && b.roleGroupConfig.Cleaner != nil {
		if *b.roleGroupConfig.Cleaner && b.replicas > 1 {
			return false, fmt.Errorf("role group %s has cleaner enabled but replicas > 1", b.roleGroupName)
		}
		return *b.roleGroupConfig.Cleaner, nil
	}

	if b.cr.Spec.Node.Config != nil && b.cr.Spec.Node.Config.Cleaner != nil {
		if *b.cr.Spec.Node.Config.Cleaner && len(b.cr.Spec.Node.RoleGroups) > 1 {
			return false, fmt.Errorf("role cleaner is enabled at role level but role has multiple groups")
		}
		return *b.cr.Spec.Node.Config.Cleaner, nil
	}

	return false, nil
}

func (b *SparkConfigMapBuilder) getSparkDefaults(s3LogConfig *S3Logconfig) string {
	cfg := map[string]string{}

	cleaner, err := b.isCleaner()
	if err == nil && cleaner {
		cfg["spark.history.fs.cleaner.enabled"] = "true"
	}

	if s3LogConfig != nil {
		maps.Copy(cfg, s3LogConfig.GetPartialProperties())
	}

	sorted := make([][]string, 0, len(cfg))
	for k, v := range cfg {
		sorted = append(sorted, []string{k, v})
	}
	slices.SortFunc(sorted, func(i, j []string) int {
		return strings.Compare(i[0], j[0])
	})

	var out strings.Builder
	for _, kv := range sorted {
		out.WriteString(kv[0] + "        " + kv[1] + "\n")
	}

	return out.String()
}
