package historyserver

import (
	"context"
	"fmt"

	"github.com/zncdatadev/operator-go/pkg/constant"
	"github.com/zncdatadev/operator-go/pkg/reconciler"
	corev1 "k8s.io/api/core/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	sparkv1alpha1 "github.com/zncdatadev/spark-k8s-operator/api/v1alpha1"
	"github.com/zncdatadev/spark-k8s-operator/internal/util/version"
)

const (
	RoleName = "node"
)

type SparkHistoryServerRoleGroupHandler struct{}

func buildResourceLabels(buildCtx *reconciler.RoleGroupBuildContext) map[string]string {
	labels := map[string]string{}
	for k, v := range buildCtx.ClusterLabels {
		labels[k] = v
	}

	labels[constant.LabelKubernetesName] = sparkv1alpha1.DefaultProductName
	labels[constant.LabelKubernetesInstance] = buildCtx.ClusterName
	labels[constant.LabelKubernetesComponent] = buildCtx.RoleName
	labels[constant.LabelKubernetesRoleGroup] = buildCtx.RoleGroupName
	labels[constant.LabelKubernetesManagedBy] = "spark-k8s-operator"

	return labels
}

func computeImageString(spec *sparkv1alpha1.ImageSpec) (string, corev1.PullPolicy) {
	if spec == nil {
		return sparkv1alpha1.DefaultRepository + "/" + sparkv1alpha1.DefaultProductName + ":" + version.BuildVersion + "-" + sparkv1alpha1.DefaultProductVersion, corev1.PullIfNotPresent
	}

	pullPolicy := spec.PullPolicy
	if pullPolicy == "" {
		pullPolicy = corev1.PullIfNotPresent
	}

	if spec.Custom != "" {
		return spec.Custom, pullPolicy
	}

	repo := spec.Repo
	if repo == "" {
		repo = sparkv1alpha1.DefaultRepository
	}

	kubedoopVersion := spec.KubedoopVersion
	if kubedoopVersion == "" {
		kubedoopVersion = version.BuildVersion
	}

	productVersion := spec.ProductVersion
	if productVersion == "" {
		productVersion = sparkv1alpha1.DefaultProductVersion
	}

	return repo + "/" + sparkv1alpha1.DefaultProductName + ":" + kubedoopVersion + "-" + productVersion, pullPolicy
}

func (h *SparkHistoryServerRoleGroupHandler) BuildResources(
	ctx context.Context,
	k8sClient ctrlclient.Client,
	cr *sparkv1alpha1.SparkHistoryServer,
	buildCtx *reconciler.RoleGroupBuildContext,
) (*reconciler.RoleGroupResources, error) {
	if cr.Spec.Node == nil {
		return nil, fmt.Errorf("node role spec is required")
	}

	roleGroupSpec, ok := cr.Spec.Node.RoleGroups[buildCtx.RoleGroupName]
	if !ok || roleGroupSpec == nil {
		return nil, fmt.Errorf("role group %s not found", buildCtx.RoleGroupName)
	}

	replicas := int32(1)
	if buildCtx.RoleGroupSpec.Replicas != nil {
		replicas = *buildCtx.RoleGroupSpec.Replicas
	}

	labels := buildResourceLabels(buildCtx)
	image, pullPolicy := computeImageString(cr.Spec.Image)

	cmBuilder := NewSparkConfigMapBuilder(
		k8sClient,
		buildCtx.ResourceName,
		buildCtx.ClusterNamespace,
		buildCtx.ClusterName,
		buildCtx.RoleName,
		buildCtx.RoleGroupName,
		cr.Spec.ClusterConfig,
		roleGroupSpec.Config,
		cr,
		replicas,
		labels,
	)
	cm, err := cmBuilder.Build(ctx)
	if err != nil {
		return nil, err
	}

	stsBuilder := NewStatefulSetBuilder(
		k8sClient,
		buildCtx.ResourceName,
		buildCtx.ClusterNamespace,
		image,
		pullPolicy,
		cr.GetUID(),
		cr.Spec.ClusterConfig,
		SparkHistoryPorts,
		replicas,
		labels,
	)
	sts, err := stsBuilder.Build(ctx)
	if err != nil {
		return nil, err
	}

	allPorts := append([]corev1.ContainerPort{}, SparkHistoryPorts...)
	if cr.Spec.ClusterConfig != nil && cr.Spec.ClusterConfig.Authentication != nil && cr.Spec.ClusterConfig.Authentication.Oidc != nil {
		allPorts = append(allPorts, OidcPorts...)
	}

	headlessService := NewRoleGroupHeadlessService(buildCtx.ResourceName, buildCtx.ClusterNamespace, labels, allPorts)
	service := NewRoleGroupClientService(buildCtx.ResourceName, buildCtx.ClusterNamespace, labels, allPorts)
	metricsService := NewRoleGroupService(buildCtx.ClusterName, buildCtx.RoleGroupName, buildCtx.ClusterNamespace, labels)

	return &reconciler.RoleGroupResources{
		ConfigMap:       cm,
		StatefulSet:     sts,
		HeadlessService: headlessService,
		Service:         service,
		MetricsService:  metricsService,
	}, nil
}
