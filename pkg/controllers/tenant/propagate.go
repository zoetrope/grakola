package tenant

import (
	"context"

	"github.com/zoetrope/grakola/pkg/constants"

	grakolav1 "github.com/zoetrope/grakola/api/v1"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	appsv1apply "k8s.io/client-go/applyconfigurations/apps/v1"
	corev1apply "k8s.io/client-go/applyconfigurations/core/v1"
	metav1apply "k8s.io/client-go/applyconfigurations/meta/v1"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func (r *TenantReconciler) reconcilePropagateController(ctx context.Context, tenant *grakolav1.Tenant) error {
	err := r.reconcilePropagateControllerConfig(ctx, tenant)
	if err != nil {
		return err
	}
	err = r.reconcilePropagateControllerServiceAccount(ctx, tenant)
	if err != nil {
		return err
	}
	err = r.reconcilePropagateControllerRole(ctx, tenant)
	if err != nil {
		return err
	}
	err = r.reconcilePropagateControllerRoleBinding(ctx, tenant)
	if err != nil {
		return err
	}
	err = r.reconcilePropagateControllerDeployment(ctx, tenant)
	if err != nil {
		return err
	}

	return nil
}

func (r *TenantReconciler) reconcilePropagateControllerDeployment(ctx context.Context, tenant *grakolav1.Tenant) error {
	logger := log.FromContext(ctx)

	depName := tenant.Name + "-propagate"

	owner, err := ownerRef(tenant, r.Scheme)
	if err != nil {
		return err
	}

	dep := appsv1apply.Deployment(depName, tenant.Namespace).
		WithLabels(labelSet("propagate", tenant)).
		WithOwnerReferences(owner).
		WithSpec(appsv1apply.DeploymentSpec().
			WithReplicas(1).
			WithSelector(metav1apply.LabelSelector().WithMatchLabels(labelSet("propagate", tenant))).
			WithTemplate(corev1apply.PodTemplateSpec().
				WithLabels(labelSet("propagate", tenant)).
				WithSpec(corev1apply.PodSpec().
					WithContainers(corev1apply.Container().
						WithName("propagate").
						WithImage("propagate-controller:latest"). //TODO
						WithImagePullPolicy(corev1.PullIfNotPresent).
						WithEnv(corev1apply.EnvVar().
							WithName("POD_NAMESPACE").
							WithValueFrom(corev1apply.EnvVarSource().
								WithFieldRef(corev1apply.ObjectFieldSelector().
									WithFieldPath("metadata.namespace"),
								),
							),
						).
						WithCommand("/propagate-controller").
						WithArgs("--zap-devel=true").
						WithVolumeMounts(
							corev1apply.VolumeMount().
								WithName("kubeconfig").
								WithMountPath("/etc/kubernetes").
								WithReadOnly(true),
							corev1apply.VolumeMount().
								WithName("config").
								WithMountPath("/etc/propagate-controller").
								WithReadOnly(true),
						),
					).
					WithVolumes(
						corev1apply.Volume().
							WithName("kubeconfig").
							WithSecret(corev1apply.SecretVolumeSource().
								WithSecretName(tenant.Name+"-kubeconfig").
								WithDefaultMode(420),
							),
						corev1apply.Volume().
							WithName("config").
							WithConfigMap(corev1apply.ConfigMapVolumeSource().
								WithName(tenant.Name+"-propagate-config").
								WithDefaultMode(420),
							),
					).
					WithServiceAccountName(tenant.Name + "-propagate"),
				),
			),
		)

	obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(dep)
	if err != nil {
		return err
	}
	patch := &unstructured.Unstructured{
		Object: obj,
	}

	var current appsv1.Deployment
	err = r.Get(ctx, client.ObjectKey{Namespace: tenant.Namespace, Name: depName}, &current)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	currApplyConfig, err := appsv1apply.ExtractDeployment(&current, constants.TenantControllerName)
	if err != nil {
		return err
	}

	if equality.Semantic.DeepEqual(dep, currApplyConfig) {
		return nil
	}

	err = r.Patch(ctx, patch, client.Apply, &client.PatchOptions{
		FieldManager: constants.TenantControllerName,
		Force:        pointer.Bool(true),
	})

	if err != nil {
		logger.Error(err, "unable to create or update Deployment")
		return err
	}
	logger.Info("reconcile Deployment successfully", "name", depName)
	return nil
}

func (r *TenantReconciler) reconcilePropagateControllerConfig(ctx context.Context, tenant *grakolav1.Tenant) error {
	logger := log.FromContext(ctx)
	cfgName := tenant.Name + "-propagate-config"

	owner, err := ownerRef(tenant, r.Scheme)
	if err != nil {
		return err
	}

	data, err := yaml.Marshal(struct {
		Targets []metav1.GroupVersionKind `json:"targets,omitempty"`
	}{
		Targets: tenant.Spec.Targets,
	})
	if err != nil {
		return err
	}

	dep := corev1apply.ConfigMap(cfgName, tenant.Namespace).
		WithLabels(labelSet("propagate", tenant)).
		WithOwnerReferences(owner).
		WithData(map[string]string{
			"config.yaml": string(data),
		})

	obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(dep)
	if err != nil {
		return err
	}
	patch := &unstructured.Unstructured{
		Object: obj,
	}

	var current corev1.ConfigMap
	err = r.Get(ctx, client.ObjectKey{Namespace: tenant.Namespace, Name: cfgName}, &current)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	currApplyConfig, err := corev1apply.ExtractConfigMap(&current, constants.TenantControllerName)
	if err != nil {
		return err
	}

	if equality.Semantic.DeepEqual(dep, currApplyConfig) {
		return nil
	}

	err = r.Patch(ctx, patch, client.Apply, &client.PatchOptions{
		FieldManager: constants.TenantControllerName,
		Force:        pointer.Bool(true),
	})

	if err != nil {
		logger.Error(err, "unable to create or update Configmap")
		return err
	}
	logger.Info("reconcile Configmap successfully", "name", cfgName)
	return nil
}

func (r *TenantReconciler) reconcilePropagateControllerRole(ctx context.Context, tenant *grakolav1.Tenant) error {
	logger := log.FromContext(ctx)
	roleName := tenant.Name + "-propagate"

	role := &rbacv1.Role{}
	role.SetNamespace(tenant.Namespace)
	role.SetName(roleName)

	op, err := ctrl.CreateOrUpdate(ctx, r.Client, role, func() error {
		role.Rules = []rbacv1.PolicyRule{
			{
				Verbs:     []string{"*"},
				APIGroups: []string{"*"},
				Resources: []string{"*"},
			},
		}
		return ctrl.SetControllerReference(tenant, role, r.Scheme)
	})

	if err != nil {
		return err
	}
	if op != controllerutil.OperationResultNone {
		logger.Info("reconcile RoleBinding successfully", "op", op)
	}
	return nil
}

func (r *TenantReconciler) reconcilePropagateControllerRoleBinding(ctx context.Context, tenant *grakolav1.Tenant) error {
	logger := log.FromContext(ctx)
	bindingName := tenant.Name + "-propagate"

	binding := &rbacv1.RoleBinding{}
	binding.SetNamespace(tenant.Namespace)
	binding.SetName(bindingName)

	op, err := ctrl.CreateOrUpdate(ctx, r.Client, binding, func() error {
		binding.RoleRef = rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     tenant.Name + "-propagate",
		}
		binding.Subjects = []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      tenant.Name + "-propagate",
				Namespace: tenant.Namespace,
			},
		}
		return ctrl.SetControllerReference(tenant, binding, r.Scheme)
	})

	if err != nil {
		return err
	}
	if op != controllerutil.OperationResultNone {
		logger.Info("reconcile RoleBinding successfully", "op", op)
	}
	return nil
}

func (r *TenantReconciler) reconcilePropagateControllerServiceAccount(ctx context.Context, tenant *grakolav1.Tenant) error {
	logger := log.FromContext(ctx)
	saName := tenant.Name + "-propagate"

	sa := &corev1.ServiceAccount{}
	sa.SetNamespace(tenant.Namespace)
	sa.SetName(saName)

	op, err := ctrl.CreateOrUpdate(ctx, r.Client, sa, func() error {
		return ctrl.SetControllerReference(tenant, sa, r.Scheme)
	})

	if err != nil {
		return err
	}
	if op != controllerutil.OperationResultNone {
		logger.Info("reconcile ServiceAccount successfully", "op", op)
	}
	return nil
}
