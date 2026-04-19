/*
Copyright 2023 zncdatadev.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package historyserver

import (
	"context"

	"github.com/zncdatadev/operator-go/pkg/reconciler"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	sparkv1alpha1 "github.com/zncdatadev/spark-k8s-operator/api/v1alpha1"
)

var (
	logger = ctrl.Log.WithName("controller")
)

// SparkHistoryServerReconciler reconciles a SparkHistoryServer object.
type SparkHistoryServerReconciler struct {
	ctrlclient.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=spark.kubedoop.dev,resources=sparkhistoryservers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=spark.kubedoop.dev,resources=sparkhistoryservers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=spark.kubedoop.dev,resources=sparkhistoryservers/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=authentication.kubedoop.dev,resources=authenticationclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups=s3.kubedoop.dev,resources=s3connections,verbs=get;list;watch
// +kubebuilder:rbac:groups=s3.kubedoop.dev,resources=s3buckets,verbs=get;list;watch
// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=get;list;watch;create;update;patch;delete
func (r *SparkHistoryServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger.Info("Reconciling SparkHistory")

	gr, err := reconciler.NewGenericReconciler(&reconciler.GenericReconcilerConfig[*sparkv1alpha1.SparkHistoryServer]{
		Client:           r.Client,
		Scheme:           r.Scheme,
		Recorder:         r.Recorder,
		RoleGroupHandler: &SparkHistoryServerRoleGroupHandler{},
		Prototype:        &sparkv1alpha1.SparkHistoryServer{},
	})
	if err != nil {
		return ctrl.Result{}, err
	}

	return gr.Reconcile(ctx, req)
}

// SetupWithManager sets up the controller with the Manager.
func (r *SparkHistoryServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Recorder = mgr.GetEventRecorderFor("sparkhistoryserver-controller")
	return ctrl.NewControllerManagedBy(mgr).
		For(&sparkv1alpha1.SparkHistoryServer{}).
		Complete(r)
}
