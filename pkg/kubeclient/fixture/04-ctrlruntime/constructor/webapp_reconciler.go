package constructor

import (
	"context"
	"fmt"
	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/kubeclient/orkadapter"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	webappv1 "github.com/orkspace/orkestra-ctrlruntime-constructor/api/v1alpha1"
)

// WebAppReconciler is written in pure controller-runtime style.
// It holds a client.Client and knows nothing about Orkestra's internals.
type WebAppReconciler struct {
	client.Client
}

func (r *WebAppReconciler) Reconcile(ctx context.Context, req ctrl.Request) (reconcile.Result, error) {
	log := ctrl.LoggerFrom(ctx).WithValues("webapp", req.NamespacedName)
	log.Info("reconciling")

	webapp := &webappv1.WebApp{}
	if err := r.Get(ctx, req.NamespacedName, webapp); err != nil {
		if errors.IsNotFound(err) {
			log.Info("not found — deleted")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if err := r.reconcileDeployment(ctx, webapp); err != nil {
		log.Error(err, "reconcileDeployment failed")
		return ctrl.Result{}, err
	}

	if err := r.reconcileConfigMap(ctx, webapp); err != nil {
		log.Error(err, "reconcileConfigMap failed")
		return ctrl.Result{}, err
	}

	// List ConfigMaps by field index — served from cache when the watch informer
	// has synced; falls back to live API during the first reconcile cycle.
	cmList := &corev1.ConfigMapList{}
	if err := r.List(ctx, cmList, client.MatchingFields{"metadata.labels.app": webapp.Name}); err != nil {
		log.Error(err, "list ConfigMaps by index failed")
	} else {
		log.Info("configmaps found via index", "count", len(cmList.Items))
	}

	base := webapp.DeepCopyObject().(client.Object)
	webapp.Status.Phase = "Running"
	webapp.Status.Endpoint = fmt.Sprintf("%s.%s.svc.cluster.local", webapp.Name, webapp.Namespace)
	webapp.Status.Replicas = webapp.Spec.Replicas
	if err := r.Status().Patch(ctx, webapp, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("done", "phase", webapp.Status.Phase, "endpoint", webapp.Status.Endpoint, "replicas", webapp.Status.Replicas)
	return ctrl.Result{}, nil
}

func (r *WebAppReconciler) reconcileConfigMap(ctx context.Context, webapp *webappv1.WebApp) error {
	log := ctrl.LoggerFrom(ctx).WithValues("webapp", webapp.Name)
	desired := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      webapp.Name + "-config",
			Namespace: webapp.Namespace,
			Labels:    map[string]string{"app": webapp.Name},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(webapp, webappv1.GroupVersionKind),
			},
		},
		Data: map[string]string{
			"image": webapp.Spec.Image,
		},
	}
	existing := &corev1.ConfigMap{}
	err := r.Get(ctx, client.ObjectKey{Name: desired.Name, Namespace: desired.Namespace}, existing)
	if errors.IsNotFound(err) {
		log.Info("creating configmap")
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}
	if equality.Semantic.DeepEqual(existing.Data, desired.Data) {
		return nil
	}
	log.Info("patching configmap")
	patch := client.MergeFrom(existing.DeepCopy())
	existing.Data = desired.Data
	return r.Patch(ctx, existing, patch)
}

func (r *WebAppReconciler) reconcileDeployment(ctx context.Context, webapp *webappv1.WebApp) error {
	log := ctrl.LoggerFrom(ctx).WithValues("webapp", webapp.Name, "namespace", webapp.Namespace)

	replicas := webapp.Spec.Replicas
	if replicas == 0 {
		replicas = 1
	}

	desired := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      webapp.Name,
			Namespace: webapp.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(webapp, webappv1.GroupVersionKind),
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": webapp.Name},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": webapp.Name},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  webapp.Name,
							Image: webapp.Spec.Image,
							Ports: []corev1.ContainerPort{
								{ContainerPort: webapp.Spec.Port},
							},
						},
					},
				},
			},
		},
	}

	existing := &appsv1.Deployment{}
	err := r.Get(ctx, client.ObjectKey{Name: webapp.Name, Namespace: webapp.Namespace}, existing)
	if errors.IsNotFound(err) {
		log.Info("creating deployment", "image", webapp.Spec.Image, "replicas", replicas)
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	// Compare only the fields our reconciler controls. Comparing the whole Spec
	// fails because the API server adds defaults (Strategy, ProgressDeadlineSeconds,
	// etc.) that our desired struct omits, causing an endless patch loop.
	currentReplicas := int32(1)
	if existing.Spec.Replicas != nil {
		currentReplicas = *existing.Spec.Replicas
	}
	currentImage := ""
	if len(existing.Spec.Template.Spec.Containers) > 0 {
		currentImage = existing.Spec.Template.Spec.Containers[0].Image
	}
	if currentReplicas == replicas && currentImage == webapp.Spec.Image {
		return nil
	}
	log.Info("patching deployment", "image", webapp.Spec.Image, "replicas", replicas)
	patch := client.MergeFrom(existing.DeepCopy())
	existing.Spec.Replicas = desired.Spec.Replicas
	if len(existing.Spec.Template.Spec.Containers) > 0 {
		existing.Spec.Template.Spec.Containers[0].Image = webapp.Spec.Image
		existing.Spec.Template.Spec.Containers[0].Ports = desired.Spec.Template.Spec.Containers[0].Ports
	}
	return r.Patch(ctx, existing, patch)
}

// NewWebAppReconciler is the Orkestra constructor. It replaces SetupWithManager — no other
// changes to the reconciler are needed. ToClient returns the same client.Client
// your reconciler already uses; ReconcilerFrom adapts the signature.
// Generated by ork migrate
func NewWebAppReconciler(kube kubeclient.Interface) domain.Reconciler {
	return domain.ReconcilerFrom(&WebAppReconciler{
		Client: orkadapter.ToClient(kube),
	})
}
