package historyserver

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/url"
	"path"
	"strconv"
	"strings"

	authv1alpha1 "github.com/zncdatadev/operator-go/pkg/apis/authentication/v1alpha1"
	"github.com/zncdatadev/operator-go/pkg/builder"
	"github.com/zncdatadev/operator-go/pkg/constant"
	"github.com/zncdatadev/operator-go/pkg/vector"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	shsv1alpha1 "github.com/zncdatadev/spark-k8s-operator/api/v1alpha1"
	internalutil "github.com/zncdatadev/spark-k8s-operator/internal/util"
)

const (
	SparkConfigDefauleFileName = "spark-defaults.conf"
	SparkHistoryContainerName  = RoleName

	LogVolumeName    = "log-data"
	ConfigVolumeName = "config"

	MaxLogFileSize = "10Mi"
)

type StatefulSetBuilder struct {
	k8sClient     ctrlclient.Client
	name          string
	namespace     string
	image         string
	pullPolicy    corev1.PullPolicy
	ownerUID      types.UID
	clusterConfig *shsv1alpha1.ClusterConfigSpec
	ports         []corev1.ContainerPort
	replicas      int32
	labels        map[string]string
}

func NewStatefulSetBuilder(
	k8sClient ctrlclient.Client,
	name string,
	namespace string,
	image string,
	pullPolicy corev1.PullPolicy,
	ownerUID types.UID,
	clusterConfig *shsv1alpha1.ClusterConfigSpec,
	ports []corev1.ContainerPort,
	replicas int32,
	labels map[string]string,
) *StatefulSetBuilder {
	copiedLabels := make(map[string]string)
	for k, v := range labels {
		copiedLabels[k] = v
	}

	return &StatefulSetBuilder{
		k8sClient:     k8sClient,
		name:          name,
		namespace:     namespace,
		image:         image,
		pullPolicy:    pullPolicy,
		ownerUID:      ownerUID,
		clusterConfig: clusterConfig,
		ports:         ports,
		replicas:      replicas,
		labels:        copiedLabels,
	}
}

func (b *StatefulSetBuilder) getS3LogConfig(ctx context.Context) (*S3Logconfig, error) {
	if b.clusterConfig == nil || b.clusterConfig.LogFileDirectory == nil || b.clusterConfig.LogFileDirectory.S3 == nil {
		return nil, nil
	}

	return NewS3Logconfig(ctx, b.k8sClient, b.namespace, b.clusterConfig.LogFileDirectory.S3)
}

func (b *StatefulSetBuilder) getMainContainerCmdArgs(s3LogConfig *S3Logconfig) string {
	s3LogCmdArgs := ""
	if s3LogConfig != nil {
		s3LogCmdArgs = s3LogConfig.GetPartialCmdArgs()
	}

	return strings.TrimSpace(`
mkdir -p ` + constant.KubedoopConfigDir + `
cp ` + path.Join(constant.KubedoopConfigDirMount, "*") + ` ` + constant.KubedoopConfigDir + `
` + s3LogCmdArgs + `
echo ""
` + path.Join(constant.KubedoopRoot, "spark/sbin/start-history-server.sh") + ` --properties-file ` + path.Join(constant.KubedoopConfigDir, SparkConfigDefauleFileName) + `
`)
}

func (b *StatefulSetBuilder) getMainContainerEnvVars() []corev1.EnvVar {
	jvmOpts := []string{
		"-Dlog4j.configurationFile=" + path.Join(constant.KubedoopConfigDir, "log4j2.properties"),
		"-javaagent:" + path.Join(constant.KubedoopJmxDir, "jmx_prometheus_javaagent.jar=8090:"+path.Join(constant.KubedoopJmxDir, "config.yaml")),
	}

	return []corev1.EnvVar{
		{Name: "SPARK_NO_DAEMONIZE", Value: "true"},
		{Name: "SPARK_DAEMON_CLASSPATH", Value: "/kubedoop/spark/extra-jars/*"},
		{Name: "SPARK_HISTORY_OPTS", Value: strings.Join(jvmOpts, " ")},
	}
}

func (b *StatefulSetBuilder) getMainProbe() *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt(int(internalutil.HttpPort))},
		},
		InitialDelaySeconds: 10,
		TimeoutSeconds:      5,
		PeriodSeconds:       10,
		SuccessThreshold:    1,
	}
}

func (b *StatefulSetBuilder) getOidcContainer(ctx context.Context) (*corev1.Container, error) {
	authClass := &authv1alpha1.AuthenticationClass{}
	if err := b.k8sClient.Get(ctx, types.NamespacedName{Name: b.clusterConfig.Authentication.AuthenticationClass, Namespace: b.namespace}, authClass); err != nil {
		return nil, err
	}

	if authClass.Spec.AuthenticationProvider.OIDC == nil {
		return nil, nil
	}

	oidcProvider := authClass.Spec.AuthenticationProvider.OIDC
	scopes := []string{"openid", "email", "profile"}
	if b.clusterConfig.Authentication.Oidc.ExtraScopes != nil {
		scopes = append(scopes, b.clusterConfig.Authentication.Oidc.ExtraScopes...)
	}

	issuer := url.URL{Scheme: "http", Host: oidcProvider.Hostname, Path: oidcProvider.RootPath}
	if oidcProvider.Port != 0 && oidcProvider.Port != 80 {
		issuer.Host += ":" + strconv.Itoa(oidcProvider.Port)
	}

	providerHint := oidcProvider.ProviderHint
	if providerHint == "keycloak" {
		providerHint = "keycloak-oidc"
	}

	hash := sha256.Sum256([]byte(b.ownerUID))
	hashStr := hex.EncodeToString(hash[:])
	tokenBytes := []byte(hashStr[:16])
	cookieSecret := base64.StdEncoding.EncodeToString([]byte(base64.StdEncoding.EncodeToString(tokenBytes)))

	var sparkHistoryPort int32 = internalutil.HttpPort
	for _, p := range b.ports {
		if p.Name == internalutil.HttpPortName {
			sparkHistoryPort = p.ContainerPort
			break
		}
	}

	return &corev1.Container{
		Name:            "oidc",
		Image:           "quay.io/oauth2-proxy/oauth2-proxy:latest",
		ImagePullPolicy: corev1.PullIfNotPresent,
		Env: []corev1.EnvVar{
			{Name: "OAUTH2_PROXY_COOKIE_SECRET", Value: cookieSecret},
			{
				Name: "OAUTH2_PROXY_CLIENT_ID",
				ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: b.clusterConfig.Authentication.Oidc.ClientCredentialsSecret},
					Key:                  "CLIENT_ID",
				}},
			},
			{
				Name: "OAUTH2_PROXY_CLIENT_SECRET",
				ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: b.clusterConfig.Authentication.Oidc.ClientCredentialsSecret},
					Key:                  "CLIENT_SECRET",
				}},
			},
			{Name: "OAUTH2_PROXY_OIDC_ISSUER_URL", Value: issuer.String()},
			{Name: "OAUTH2_PROXY_SCOPE", Value: strings.Join(scopes, " ")},
			{Name: "OAUTH2_PROXY_PROVIDER", Value: providerHint},
			{Name: "OAUTH2_PROXY_UPSTREAMS", Value: "http://localhost:" + strconv.Itoa(int(sparkHistoryPort))},
			{Name: "OAUTH2_PROXY_HTTP_ADDRESS", Value: "0.0.0.0:4180"},
			{Name: "OAUTH2_PROXY_COOKIE_SECURE", Value: "false"},
			{Name: "OAUTH2_PROXY_WHITELIST_DOMAINS", Value: "*"},
			{Name: "OAUTH2_PROXY_CODE_CHALLENGE_METHOD", Value: "S256"},
			{Name: "OAUTH2_PROXY_EMAIL_DOMAINS", Value: "*"},
		},
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("600m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
		},
		Ports: OidcPorts,
	}, nil
}

func (b *StatefulSetBuilder) getVectorContainer() *corev1.Container {
	return &corev1.Container{
		Name:            "vector",
		Image:           b.image,
		ImagePullPolicy: b.pullPolicy,
		Command:         []string{"vector", "--config", path.Join(constant.KubedoopConfigDirMount, vector.VectorConfigFileName)},
		VolumeMounts: []corev1.VolumeMount{
			{Name: ConfigVolumeName, MountPath: constant.KubedoopConfigDirMount},
			{Name: LogVolumeName, MountPath: constant.KubedoopLogDir},
		},
	}
}

func (b *StatefulSetBuilder) Build(ctx context.Context) (*appsv1.StatefulSet, error) {
	probe := b.getMainProbe()

	runAsUser := int64(0)
	runAsNonRoot := false
	containerSecurityContext := &corev1.SecurityContext{
		RunAsUser:    &runAsUser,
		RunAsGroup:   &runAsUser,
		RunAsNonRoot: &runAsNonRoot,
	}
	podSecurityContext := &corev1.PodSecurityContext{RunAsUser: &runAsUser, RunAsGroup: &runAsUser}

	stsBuilder := builder.NewStatefulSetBuilder(b.name, b.namespace).
		WithLabels(b.labels).
		WithReplicas(b.replicas).
		WithImage(b.image, b.pullPolicy).
		WithPorts(b.ports).
		WithLivenessProbe(probe).
		WithReadinessProbe(probe).
		WithSecurityContext(containerSecurityContext, podSecurityContext)

	for _, env := range b.getMainContainerEnvVars() {
		stsBuilder.AddEnvVar(env.Name, env.Value)
	}

	stsBuilder.Command = []string{"/bin/bash", "-c"}

	s3LogConfig, err := b.getS3LogConfig(ctx)
	if err != nil {
		return nil, err
	}
	stsBuilder.Args = []string{b.getMainContainerCmdArgs(s3LogConfig)}

	stsBuilder.AddVolume(corev1.Volume{
		Name: ConfigVolumeName,
		VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: b.name},
		}},
	})
	stsBuilder.AddVolumeMount(corev1.VolumeMount{Name: ConfigVolumeName, MountPath: constant.KubedoopConfigDirMount})

	stsBuilder.AddVolume(corev1.Volume{
		Name: LogVolumeName,
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{
			SizeLimit: func() *resource.Quantity {
				q := resource.MustParse(MaxLogFileSize)
				return &q
			}(),
		}},
	})
	stsBuilder.AddVolumeMount(corev1.VolumeMount{Name: LogVolumeName, MountPath: constant.KubedoopLogDir})

	if s3LogConfig != nil {
		stsBuilder.AddVolume(*s3LogConfig.GetVolume())
		stsBuilder.AddVolumeMount(*s3LogConfig.GetVolumeMount())
	}

	sts := stsBuilder.Build()

	if b.clusterConfig != nil && b.clusterConfig.Authentication != nil && b.clusterConfig.Authentication.Oidc != nil {
		oidcContainer, err := b.getOidcContainer(ctx)
		if err != nil {
			return nil, err
		}
		if oidcContainer != nil {
			sts.Spec.Template.Spec.Containers = append(sts.Spec.Template.Spec.Containers, *oidcContainer)
		}
	}

	if b.clusterConfig != nil && b.clusterConfig.VectorAggregatorConfigMapName != "" {
		sts.Spec.Template.Spec.Containers = append(sts.Spec.Template.Spec.Containers, *b.getVectorContainer())
	}

	return sts, nil
}
