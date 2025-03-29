/*
Copyright 2025.

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

package controller

import (
	"context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"time"

	"github.com/liguojun1/i-operator/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ApplicationReconciler reconciles a Application object
type ApplicationReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

const (
	Finalizer = "liguojun/finalizer"
)

// +kubebuilder:rbac:groups=core.crd.test.com,resources=applications,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.crd.test.com,resources=applications/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.crd.test.com,resources=applications/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Application object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.18.4/pkg/reconcile
func (r *ApplicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	log := logger.WithValues("application", req.NamespacedName)
	log.Info("start reconcile")
	var app v1.Application
	if err := r.Get(ctx, req.NamespacedName, &app); err != nil {
		log.Error(err, "unable to fetch application")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if app.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(&app, Finalizer) {
			controllerutil.AddFinalizer(&app, Finalizer)
			if err := r.Update(ctx, &app); err != nil {
				return ctrl.Result{}, err
			}
			r.Recorder.Event(&app, corev1.EventTypeNormal, "Success", "Application finalized")
		}
	} else {
		err := r.RemoveExternalResource()
		if err != nil {
			return ctrl.Result{}, err
		}
		if controllerutil.ContainsFinalizer(&app, Finalizer) {
			controllerutil.RemoveFinalizer(&app, Finalizer)
			if err := r.Update(ctx, &app); err != nil {
				return ctrl.Result{}, err
			}
			r.Recorder.Event(&app, corev1.EventTypeNormal, "Success", "Application finalized remove ")
		}
		return ctrl.Result{}, nil
	}
	log.Info("run reconcile")
	err := r.syncApp(ctx, req, &app)
	if err != nil {
		return ctrl.Result{}, err
	}
	dp := &appsv1.Deployment{}
	objKey := client.ObjectKey{Name: fullName(app.Name), Namespace: app.Namespace}
	err = r.Get(ctx, objKey, dp)
	if err != nil {
		return ctrl.Result{}, err
	}
	copyApp := app.DeepCopy()
	copyApp.Status.Ready = dp.Status.ReadyReplicas >= 1
	if !reflect.DeepEqual(copyApp, app) {
		err = r.Status().Update(ctx, copyApp)
		if err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ApplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.Application{}).
		Complete(r)
}

func (r *ApplicationReconciler) RemoveExternalResource() error {
	return nil
}

func (r *ApplicationReconciler) syncApp(ctx context.Context, req ctrl.Request, app *v1.Application) error {
	if app.Spec.Enabled {
		return r.syncEnabled(ctx, req, app)
	}
	return r.syncDisabled(ctx, req, app)
}

func (r *ApplicationReconciler) syncEnabled(ctx context.Context, req ctrl.Request, app *v1.Application) error {
	var dp appsv1.Deployment
	objKey := client.ObjectKey{Name: fullName(req.Name), Namespace: req.Namespace}
	if err := r.Get(ctx, objKey, &dp); err != nil {
		if client.IgnoreNotFound(err) == nil {
			dp = generalDeployment(app)
			if err := r.Create(ctx, &dp); err != nil {
				return err
			}
			log.Log.Info("create deployment", "name", objKey.Name, "namespace", objKey.Namespace)
		}
	}
	return nil
}

func (r *ApplicationReconciler) syncDisabled(ctx context.Context, req ctrl.Request, app *v1.Application) error {
	var dp appsv1.Deployment
	objKey := client.ObjectKey{Name: fullName(req.Name), Namespace: req.Namespace}
	if err := r.Get(ctx, objKey, &dp); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return nil
		}
		return err
	}
	err := r.Delete(ctx, &dp)
	return err
}

func fullName(name string) string {
	return "app-" + name
}

func generalDeployment(app *v1.Application) appsv1.Deployment {
	dp := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fullName(app.Name),
			Namespace: app.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": fullName(app.Name),
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": fullName(app.Name),
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  fullName(app.Name),
							Image: app.Spec.Image,
						},
					},
				},
			},
		},
	}
	return dp
}
