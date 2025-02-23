package common

import (
	"context"
	opgostatus "github.com/zncdata-labs/operator-go/pkg/status"
	"github.com/zncdata-labs/spark-k8s-operator/internal/util"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"time"
)

type IReconciler interface {
	ReconcileResource(ctx context.Context, groupName string, do ResourceHandler) (ctrl.Result, error)
}

type ResourceBuilder interface {
	Build(ctx context.Context) (client.Object, error)
}

type ResourceHandler interface {
	DoReconcile(ctx context.Context, resource client.Object, instance ResourceHandler) (ctrl.Result, error)
}
type ConditionsGetter interface {
	GetConditions() *[]metav1.Condition
}

type WorkloadOverride interface {
	CommandOverride(resource client.Object)
	EnvOverride(resource client.Object)
	LogOverride(resource client.Object)
}

type ConfigurationOverride interface {
	ConfigurationOverride(resource client.Object)
}

type BaseResourceReconciler[T client.Object, G any] struct {
	Instance  T
	Scheme    *runtime.Scheme
	Client    client.Client
	GroupName string

	MergedLabels map[string]string
	MergedCfg    G
}

// NewBaseResourceReconciler new a BaseResourceReconciler
func NewBaseResourceReconciler[T client.Object, G any](
	scheme *runtime.Scheme,
	instance T,
	client client.Client,
	groupName string,
	mergedLabels map[string]string,
	mergedCfg G) *BaseResourceReconciler[T, G] {
	return &BaseResourceReconciler[T, G]{
		Instance:     instance,
		Scheme:       scheme,
		Client:       client,
		GroupName:    groupName,
		MergedLabels: mergedLabels,
		MergedCfg:    mergedCfg,
	}
}

func (b *BaseResourceReconciler[T, G]) ReconcileResource(
	ctx context.Context,
	groupName string,
	resInstance ResourceBuilder) (ctrl.Result, error) {
	// 1. mergelables
	// 2. build resource
	// 3. setControllerReference
	obj, err := resInstance.Build(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}
	//resInstance reconcile
	//return b.DoReconcile(ctx, resource)
	if handler, ok := resInstance.(ResourceHandler); ok {
		return handler.DoReconcile(ctx, obj, handler)
	} else {
		panic("resource is not ResourceHandler")
	}
}

func (b *BaseResourceReconciler[T, G]) Apply(ctx context.Context, dep client.Object) (ctrl.Result, error) {
	if dep == nil {
		return ctrl.Result{}, nil
	}
	if err := ctrl.SetControllerReference(b.Instance, dep, b.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	mutant, err := util.CreateOrUpdate(ctx, b.Client, dep)
	if err != nil {
		return ctrl.Result{}, err
	}

	if mutant {
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}
	return ctrl.Result{}, nil
}

// GeneralResourceStyleReconciler general style resource reconcile
// this reconciler is used to reconcile the general style resources
// such as configMap, secret, svc, etc.
type GeneralResourceStyleReconciler[T client.Object, G any] struct {
	BaseResourceReconciler[T, G]
}

func NewGeneraResourceStyleReconciler[T client.Object, G any](
	scheme *runtime.Scheme,
	instance T,
	client client.Client,
	groupName string,
	mergedLabels map[string]string,
	mergedCfg G,
) *GeneralResourceStyleReconciler[T, G] {
	return &GeneralResourceStyleReconciler[T, G]{
		BaseResourceReconciler: *NewBaseResourceReconciler[T, G](
			scheme,
			instance,
			client,
			groupName,
			mergedLabels,
			mergedCfg),
	}
}

func (s *GeneralResourceStyleReconciler[T, G]) DoReconcile(
	ctx context.Context,
	resource client.Object,
	_ ResourceHandler,
) (ctrl.Result, error) {
	return s.Apply(ctx, resource)
}

// ConfigurationStyleReconciler configuration style reconciler
// this reconciler is used to reconcile the configuration style resources
// such as configMap, secret, etc.
// it will do the following things:
// 1. apply the resource
// Additional:
// 1. configuration override support
type ConfigurationStyleReconciler[T client.Object, G any] struct {
	GeneralResourceStyleReconciler[T, G]
}

func NewConfigurationStyleReconciler[T client.Object, G any](
	scheme *runtime.Scheme,
	instance T,
	client client.Client,
	groupName string,
	mergedLabels map[string]string,
	mergedCfg G,
) *ConfigurationStyleReconciler[T, G] {
	return &ConfigurationStyleReconciler[T, G]{
		GeneralResourceStyleReconciler: *NewGeneraResourceStyleReconciler[T, G](
			scheme,
			instance,
			client,
			groupName,
			mergedLabels,
			mergedCfg),
	}
}

func (s *ConfigurationStyleReconciler[T, G]) DoReconcile(
	ctx context.Context,
	resource client.Object,
	instance ResourceHandler,
) (ctrl.Result, error) {
	if resource == nil {
		return ctrl.Result{}, nil
	}
	if override, ok := instance.(ConfigurationOverride); ok {
		override.ConfigurationOverride(resource)
	} else {
		panic("resource is not ConfigurationOverride")
	}
	return s.Apply(ctx, resource)
}

// DeploymentStyleReconciler deployment style reconciler
// this reconciler is used to reconcile the deployment style resources
// such as deployment, statefulSet, etc.
// it will do the following things:
// 1. apply the resource
// 2. check if the resource is satisfied
// 3. if not, return requeue
// 4. if satisfied, return nil
// Additional:
//
//	command and env override can support
type DeploymentStyleReconciler[T client.Object, G any] struct {
	BaseResourceReconciler[T, G]
	replicas int32
}

func NewDeploymentStyleReconciler[T client.Object, G any](
	scheme *runtime.Scheme,
	instance T,
	client client.Client,
	groupName string,
	mergedLabels map[string]string,
	mergedCfg G,
	replicas int32,
) *DeploymentStyleReconciler[T, G] {
	return &DeploymentStyleReconciler[T, G]{
		BaseResourceReconciler: *NewBaseResourceReconciler[T, G](
			scheme,
			instance,
			client,
			groupName,
			mergedLabels,
			mergedCfg),
		replicas: replicas,
	}
}

func (s *DeploymentStyleReconciler[T, G]) DoReconcile(
	ctx context.Context,
	resource client.Object,
	instance ResourceHandler,
) (ctrl.Result, error) {
	// apply resource
	// check if the resource is satisfied
	// if not, return requeue
	// if satisfied, return nil
	if override, ok := instance.(WorkloadOverride); ok {
		override.CommandOverride(resource)
		override.EnvOverride(resource)
		override.LogOverride(resource)
	} else {
		panic("resource is not WorkloadOverride")
	}

	if res, err := s.Apply(ctx, resource); err != nil {
		return ctrl.Result{}, err
	} else if res.RequeueAfter > 0 {
		return res, nil
	}

	// Check if the pods are satisfied
	satisfied, err := s.CheckPodsSatisfied(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	if satisfied {
		err = s.updateStatus(
			metav1.ConditionTrue,
			"DeploymentSatisfied",
			"Deployment is satisfied",
			instance,
		)
		if err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	err = s.updateStatus(
		metav1.ConditionFalse,
		"DeploymentNotSatisfied",
		"Deployment is not satisfied",
		instance,
	)
	if err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: time.Second * 10}, nil
}

func (s *DeploymentStyleReconciler[T, G]) CheckPodsSatisfied(ctx context.Context) (bool, error) {
	pods := corev1.PodList{}
	podListOptions := []client.ListOption{
		client.InNamespace(s.Instance.GetNamespace()),
		client.MatchingLabels(s.MergedLabels),
	}
	err := s.Client.List(ctx, &pods, podListOptions...)
	if err != nil {
		return false, err
	}

	return len(pods.Items) == int(s.replicas), nil
}

func (s *DeploymentStyleReconciler[T, G]) updateStatus(
	status metav1.ConditionStatus,
	reason string,
	message string,
	instance ResourceHandler) error {
	if conditionHandler, ok := instance.(ConditionsGetter); ok {
		apimeta.SetStatusCondition(conditionHandler.GetConditions(), metav1.Condition{
			Type:               opgostatus.ConditionTypeAvailable,
			Status:             status,
			Reason:             reason,
			Message:            message,
			LastTransitionTime: metav1.Now(),
			ObservedGeneration: s.Instance.GetGeneration(),
		})
		return s.Client.Status().Update(context.Background(), s.Instance)
	} else {
		panic("instance is not ConditionsGetter")
	}
}
