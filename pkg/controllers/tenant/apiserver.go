package tenant

import (
	"context"
	"fmt"

	"github.com/zoetrope/grakola/pkg/constants"

	grakolav1 "github.com/zoetrope/grakola/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	appsv1apply "k8s.io/client-go/applyconfigurations/apps/v1"
	corev1apply "k8s.io/client-go/applyconfigurations/core/v1"
	metav1apply "k8s.io/client-go/applyconfigurations/meta/v1"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func (r *TenantReconciler) reconcileAPIServer(ctx context.Context, tenant *grakolav1.Tenant) error {

	err := r.reconcileAPIServerStatefulSet(ctx, tenant)
	if err != nil {
		return err
	}
	err = r.reconcileAPIServerService(ctx, tenant)
	if err != nil {
		return err
	}
	return nil
}

func (r *TenantReconciler) reconcileAPIServerStatefulSet(ctx context.Context, tenant *grakolav1.Tenant) error {
	logger := log.FromContext(ctx)

	stsName := tenant.Name + "-apiserver"

	owner, err := ownerRef(tenant, r.Scheme)
	if err != nil {
		return err
	}

	dep := appsv1apply.StatefulSet(stsName, tenant.Namespace).
		WithLabels(labelSet("apiserver", tenant)).
		WithOwnerReferences(owner).
		WithSpec(appsv1apply.StatefulSetSpec().
			WithReplicas(1).
			WithServiceName(tenant.Name + "-apiserver").
			WithSelector(metav1apply.LabelSelector().WithMatchLabels(labelSet("apiserver", tenant))).
			WithUpdateStrategy(appsv1apply.StatefulSetUpdateStrategy().WithType(appsv1.OnDeleteStatefulSetStrategyType)).
			WithTemplate(corev1apply.PodTemplateSpec().
				WithLabels(labelSet("apiserver", tenant)).
				WithSpec(corev1apply.PodSpec().
					WithHostname("apiserver").
					WithSubdomain(tenant.Name+"apiserver").
					WithContainers(corev1apply.Container().
						WithName("apiserver").
						WithImage("k8s.gcr.io/kube-apiserver:v1.21.1"). //TODO
						WithImagePullPolicy(corev1.PullIfNotPresent).
						WithEnv(corev1apply.EnvVar().
							WithName("POD_NAMESPACE").
							WithValueFrom(corev1apply.EnvVarSource().
								WithFieldRef(corev1apply.ObjectFieldSelector().
									WithFieldPath("metadata.namespace"),
								),
							),
						).
						WithCommand("kube-apiserver").
						WithArgs(
							"--allow-privileged=true",
							"--anonymous-auth=true",
							"--api-audiences=api",
							"--apiserver-count=1",
							"--authorization-mode=Node,RBAC",
							"--bind-address=0.0.0.0",
							"--client-ca-file=/etc/kubernetes/pki/apiserver/ca/tls.crt",
							"--enable-admission-plugins=NamespaceLifecycle,NodeRestriction,LimitRanger,ServiceAccount,DefaultStorageClass,ResourceQuota",
							"--enable-aggregator-routing=true",
							"--enable-bootstrap-token-auth=true",
							"--endpoint-reconciler-type=master-count",
							"--etcd-cafile=/etc/kubernetes/pki/etcd/ca/tls.crt",
							"--etcd-certfile=/etc/kubernetes/pki/etcd/tls.crt",
							"--etcd-keyfile=/etc/kubernetes/pki/etcd/tls.key",
							fmt.Sprintf("--etcd-servers=https://%s-etcd-0.%s-etcd.$(POD_NAMESPACE):2379", tenant.Name, tenant.Name),
							"--runtime-config=api/all=true",
							"--service-account-issuer=api",
							"--service-account-key-file=/etc/kubernetes/pki/service-account/tls.key",
							"--service-account-signing-key-file=/etc/kubernetes/pki/service-account/tls.key",
							"--service-cluster-ip-range=10.32.0.0/16",
							"--service-node-port-range=30000-32767",
							"--tls-cert-file=/etc/kubernetes/pki/apiserver/tls.crt",
							"--tls-private-key-file=/etc/kubernetes/pki/apiserver/tls.key",
							"--v=2",
						).
						WithPorts(corev1apply.ContainerPort().
							WithName("api").
							WithProtocol(corev1.ProtocolTCP).
							WithContainerPort(6443),
						).
						WithLivenessProbe(corev1apply.Probe().
							WithTCPSocket(corev1apply.TCPSocketAction().
								WithPort(intstr.FromInt(6443)),
							).
							WithFailureThreshold(8).
							WithInitialDelaySeconds(15).
							WithPeriodSeconds(10).
							WithTimeoutSeconds(15),
						).
						WithReadinessProbe(corev1apply.Probe().
							WithHTTPGet(corev1apply.HTTPGetAction().
								WithPort(intstr.FromInt(6443)).
								WithPath("/healthz").
								WithScheme(corev1.URISchemeHTTPS),
							).
							WithFailureThreshold(8).
							WithInitialDelaySeconds(5).
							WithPeriodSeconds(2).
							WithTimeoutSeconds(30),
						).
						WithVolumeMounts(
							corev1apply.VolumeMount().
								WithName(tenant.Name+"-etcd-ca").
								WithMountPath("/etc/kubernetes/pki/etcd/ca").
								WithReadOnly(true),
							corev1apply.VolumeMount().
								WithName(tenant.Name+"-etcd-client").
								WithMountPath("/etc/kubernetes/pki/etcd").
								WithReadOnly(true),
							corev1apply.VolumeMount().
								WithName(tenant.Name+"-ca").
								WithMountPath("/etc/kubernetes/pki/apiserver/ca").
								WithReadOnly(true),
							corev1apply.VolumeMount().
								WithName(tenant.Name+"-apiserver-client").
								WithMountPath("/etc/kubernetes/pki/apiserver").
								WithReadOnly(true),
							corev1apply.VolumeMount().
								WithName(tenant.Name+"-sa").
								WithMountPath("/etc/kubernetes/pki/service-account").
								WithReadOnly(true),
						),
					).
					WithVolumes(
						corev1apply.Volume().
							WithName(tenant.Name+"-etcd-ca").
							WithSecret(corev1apply.SecretVolumeSource().
								WithSecretName(tenant.Name+"-etcd").
								WithDefaultMode(420),
							),
						corev1apply.Volume().
							WithName(tenant.Name+"-etcd-client").
							WithSecret(corev1apply.SecretVolumeSource().
								WithSecretName(tenant.Name+"-etcd-client").
								WithDefaultMode(420),
							),
						corev1apply.Volume().
							WithName(tenant.Name+"-ca").
							WithSecret(corev1apply.SecretVolumeSource().
								WithSecretName(tenant.Name+"-ca").
								WithDefaultMode(420),
							),
						corev1apply.Volume().
							WithName(tenant.Name+"-apiserver-client").
							WithSecret(corev1apply.SecretVolumeSource().
								WithSecretName(tenant.Name+"-apiserver-client").
								WithDefaultMode(420),
							),
						corev1apply.Volume().
							WithName(tenant.Name+"-sa").
							WithSecret(corev1apply.SecretVolumeSource().
								WithSecretName(tenant.Name+"-sa").
								WithDefaultMode(420),
							),
					),
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

	var current appsv1.StatefulSet
	err = r.Get(ctx, client.ObjectKey{Namespace: tenant.Namespace, Name: stsName}, &current)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	currApplyConfig, err := appsv1apply.ExtractStatefulSet(&current, constants.TenantControllerName)
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
		logger.Error(err, "unable to create or update StatefulSet")
		return err
	}
	logger.Info("reconcile StatefulSet successfully", "name", stsName)
	return nil
}

func (r *TenantReconciler) reconcileAPIServerService(ctx context.Context, tenant *grakolav1.Tenant) error {
	logger := log.FromContext(ctx)
	svcName := tenant.Name + "-apiserver"

	owner, err := ownerRef(tenant, r.Scheme)
	if err != nil {
		return err
	}

	svc := corev1apply.Service(svcName, tenant.Namespace).
		WithLabels(labelSet("apiserver", tenant)).
		WithOwnerReferences(owner).
		WithSpec(corev1apply.ServiceSpec().
			WithSelector(labelSet("apiserver", tenant)).
			WithType(corev1.ServiceTypeClusterIP).
			WithPorts(corev1apply.ServicePort().
				WithName("api").
				WithProtocol(corev1.ProtocolTCP).
				WithPort(6443).
				WithTargetPort(intstr.FromString("api")),
			),
		)

	obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(svc)
	if err != nil {
		return err
	}
	patch := &unstructured.Unstructured{
		Object: obj,
	}

	var current corev1.Service
	err = r.Get(ctx, client.ObjectKey{Namespace: tenant.Namespace, Name: svcName}, &current)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	currApplyConfig, err := corev1apply.ExtractService(&current, constants.TenantControllerName)
	if err != nil {
		return err
	}

	if equality.Semantic.DeepEqual(svc, currApplyConfig) {
		return nil
	}

	err = r.Patch(ctx, patch, client.Apply, &client.PatchOptions{
		FieldManager: constants.TenantControllerName,
		Force:        pointer.Bool(true),
	})
	if err != nil {
		logger.Error(err, "unable to create or update Service")
		return err
	}

	logger.Info("reconcile Service successfully", "name", tenant.Name)
	return nil
}

func ownerRef(tenant *grakolav1.Tenant, scheme *runtime.Scheme) (*metav1apply.OwnerReferenceApplyConfiguration, error) {
	gvk, err := apiutil.GVKForObject(tenant, scheme)
	if err != nil {
		return nil, err
	}
	ref := metav1apply.OwnerReference().
		WithAPIVersion(gvk.GroupVersion().String()).
		WithKind(gvk.Kind).
		WithName(tenant.Name).
		WithUID(tenant.GetUID()).
		WithBlockOwnerDeletion(true).
		WithController(true)
	return ref, nil
}

func labelSet(app string, tenant *grakolav1.Tenant) map[string]string {
	labels := map[string]string{
		constants.LabelAppName:      app,
		constants.LabelAppInstance:  tenant.Name,
		constants.LabelAppCreatedBy: constants.TenantControllerName,
	}
	return labels
}
