package controllers

import (
	"context"
	"fmt"

	grakolav1 "github.com/zoetrope/grakola/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	appsv1apply "k8s.io/client-go/applyconfigurations/apps/v1"
	corev1apply "k8s.io/client-go/applyconfigurations/core/v1"
	metav1apply "k8s.io/client-go/applyconfigurations/meta/v1"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func (r *TenantReconciler) reconcileEtcd(ctx context.Context, tenant *grakolav1.Tenant) error {
	err := r.reconcileEtcdStatefulSet(ctx, tenant)
	if err != nil {
		return err
	}
	err = r.reconcileEtcdService(ctx, tenant)
	if err != nil {
		return err
	}
	return nil
}

func (r *TenantReconciler) reconcileEtcdStatefulSet(ctx context.Context, tenant *grakolav1.Tenant) error {
	logger := log.FromContext(ctx)

	stsName := tenant.Name + "-etcd"

	owner, err := ownerRef(tenant, r.Scheme)
	if err != nil {
		return err
	}

	dep := appsv1apply.StatefulSet(stsName, tenant.Namespace).
		WithLabels(labelSet("etcd", tenant)).
		WithOwnerReferences(owner).
		WithSpec(appsv1apply.StatefulSetSpec().
			WithReplicas(1).
			WithServiceName(tenant.Name + "-etcd").
			WithSelector(metav1apply.LabelSelector().WithMatchLabels(labelSet("etcd", tenant))).
			WithUpdateStrategy(appsv1apply.StatefulSetUpdateStrategy().WithType(appsv1.OnDeleteStatefulSetStrategyType)).
			WithTemplate(corev1apply.PodTemplateSpec().
				WithLabels(labelSet("etcd", tenant)).
				WithSpec(corev1apply.PodSpec().
					WithHostname("etcd").
					WithSubdomain(tenant.Name+"etcd").
					WithContainers(corev1apply.Container().
						WithName("etcd").
						WithImage("quay.io/cybozu/etcd:3.4.16.1"). //TODO
						WithImagePullPolicy(corev1.PullIfNotPresent).
						WithEnv(
							corev1apply.EnvVar().
								WithName("HOSTNAME").
								WithValueFrom(corev1apply.EnvVarSource().
									WithFieldRef(corev1apply.ObjectFieldSelector().
										WithFieldPath("metadata.name"),
									),
								),
							corev1apply.EnvVar().
								WithName("POD_NAMESPACE").
								WithValueFrom(corev1apply.EnvVarSource().
									WithFieldRef(corev1apply.ObjectFieldSelector().
										WithFieldPath("metadata.namespace"),
									),
								),
						).
						WithCommand("etcd").
						WithArgs(
							fmt.Sprintf("--advertise-client-urls=https://$(HOSTNAME).%s-etcd.$(POD_NAMESPACE):2379", tenant.Name),
							"--cert-file=/etc/kubernetes/pki/etcd/tls.crt",
							"--client-cert-auth",
							"--data-dir=/var/lib/etcd/data",
							fmt.Sprintf("--initial-advertise-peer-urls=https://$(HOSTNAME).%s-etcd.$(POD_NAMESPACE):2380", tenant.Name),
							"--initial-cluster-state=new",
							"--initial-cluster-token=vc-etcd",
							"--key-file=/etc/kubernetes/pki/etcd/tls.key",
							"--listen-client-urls=https://0.0.0.0:2379",
							"--listen-peer-urls=https://0.0.0.0:2380",
							"--name=$(HOSTNAME)",
							"--peer-cert-file=/etc/kubernetes/pki/etcd/tls.crt",
							"--peer-client-cert-auth",
							"--peer-key-file=/etc/kubernetes/pki/etcd/tls.key",
							"--peer-trusted-ca-file=/etc/kubernetes/pki/ca/tls.crt",
							"--trusted-ca-file=/etc/kubernetes/pki/ca/tls.crt",
						).
						WithPorts(
							corev1apply.ContainerPort().
								WithName("client").
								WithProtocol(corev1.ProtocolTCP).
								WithContainerPort(2379),
							corev1apply.ContainerPort().
								WithName("peer").
								WithProtocol(corev1.ProtocolTCP).
								WithContainerPort(2380),
						).
						WithLivenessProbe(corev1apply.Probe().
							WithExec(corev1apply.ExecAction().
								WithCommand(
									"sh",
									"-c",
									"etcdctl --endpoints=https://127.0.0.1:2379 --cacert=/etc/kubernetes/pki/ca/tls.crt --cert=/etc/kubernetes/pki/health/tls.crt --key=/etc/kubernetes/pki/health/tls.key endpoint health",
								)).
							WithFailureThreshold(8).
							WithInitialDelaySeconds(60).
							WithTimeoutSeconds(15),
						).
						WithReadinessProbe(corev1apply.Probe().
							WithExec(corev1apply.ExecAction().
								WithCommand(
									"sh",
									"-c",
									"etcdctl --endpoints=https://127.0.0.1:2379 --cacert=/etc/kubernetes/pki/ca/tls.crt --cert=/etc/kubernetes/pki/health/tls.crt --key=/etc/kubernetes/pki/health/tls.key endpoint health",
								)).
							WithFailureThreshold(8).
							WithInitialDelaySeconds(15).
							WithPeriodSeconds(2).
							WithTimeoutSeconds(15),
						).
						WithVolumeMounts(
							corev1apply.VolumeMount().
								WithName(tenant.Name+"-etcd-ca").
								WithMountPath("/etc/kubernetes/pki/ca").
								WithReadOnly(true),
							corev1apply.VolumeMount().
								WithName(tenant.Name+"-etcd-client").
								WithMountPath("/etc/kubernetes/pki/etcd").
								WithReadOnly(true),
							corev1apply.VolumeMount().
								WithName(tenant.Name+"-etcd-health-client").
								WithMountPath("/etc/kubernetes/pki/health").
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
							WithName(tenant.Name+"-etcd-health-client").
							WithSecret(corev1apply.SecretVolumeSource().
								WithSecretName(tenant.Name+"-etcd-health-client").
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

	var current appsv1.Deployment
	err = r.Get(ctx, client.ObjectKey{Namespace: tenant.Namespace, Name: stsName}, &current)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	currApplyConfig, err := appsv1apply.ExtractDeployment(&current, TenantControllerName)
	if err != nil {
		return err
	}

	if equality.Semantic.DeepEqual(dep, currApplyConfig) {
		return nil
	}

	err = r.Patch(ctx, patch, client.Apply, &client.PatchOptions{
		FieldManager: TenantControllerName,
		Force:        pointer.Bool(true),
	})

	if err != nil {
		logger.Error(err, "unable to create or update StatefulSet")
		return err
	}
	logger.Info("reconcile StatefulSet successfully", "name", stsName)
	return nil
}

func (r *TenantReconciler) reconcileEtcdService(ctx context.Context, tenant *grakolav1.Tenant) error {
	logger := log.FromContext(ctx)
	svcName := tenant.Name + "-etcd"

	owner, err := ownerRef(tenant, r.Scheme)
	if err != nil {
		return err
	}

	svc := corev1apply.Service(svcName, tenant.Namespace).
		WithLabels(labelSet("etcd", tenant)).
		WithOwnerReferences(owner).
		WithSpec(corev1apply.ServiceSpec().
			WithSelector(labelSet("etcd", tenant)).
			WithClusterIP("None").
			WithPublishNotReadyAddresses(true),
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

	currApplyConfig, err := corev1apply.ExtractService(&current, TenantControllerName)
	if err != nil {
		return err
	}

	if equality.Semantic.DeepEqual(svc, currApplyConfig) {
		return nil
	}

	err = r.Patch(ctx, patch, client.Apply, &client.PatchOptions{
		FieldManager: TenantControllerName,
		Force:        pointer.Bool(true),
	})
	if err != nil {
		logger.Error(err, "unable to create or update Service")
		return err
	}

	logger.Info("reconcile Service successfully", "name", tenant.Name)
	return nil
}
