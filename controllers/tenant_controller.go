/*
Copyright 2021.

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

package controllers

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	_ "embed"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"text/template"

	grakolav1 "github.com/zoetrope/grakola/api/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

//go:embed kubeconfig.tmpl
var kubeconfigTmpl string

// TenantReconciler reconciles a Tenant object
type TenantReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=grakola.zoetrope.github.io,resources=tenants,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=grakola.zoetrope.github.io,resources=tenants/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=grakola.zoetrope.github.io,resources=tenants/finalizers,verbs=update
//TODO: add certificate, issuer, secret, statefulset, service

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Tenant object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *TenantReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	// your logic here
	tenant := &grakolav1.Tenant{}
	err := r.Get(ctx, req.NamespacedName, tenant)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if tenant.DeletionTimestamp != nil {
		return ctrl.Result{}, nil
	}
	err = r.reconcileSecret(ctx, tenant)
	if err != nil {
		return ctrl.Result{}, err
	}
	err = r.reconcileServiceAccountKey(ctx, tenant)
	if err != nil {
		return ctrl.Result{}, err
	}
	err = r.reconcileKubeConfig(ctx, tenant)
	if err != nil {
		return ctrl.Result{}, err
	}
	err = r.reconcileEtcd(ctx, tenant)
	if err != nil {
		return ctrl.Result{}, err
	}
	err = r.reconcileAPIServer(ctx, tenant)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *TenantReconciler) reconcileSecret(ctx context.Context, tenant *grakolav1.Tenant) error {
	logger := log.FromContext(ctx)
	logger.Info("creating selfsigned issuer")
	err := r.createSelfSignedIssuer(ctx, tenant)
	if err != nil {
		return err
	}
	logger.Info("creating etcd ca")
	err = r.createCertificate(ctx, tenant, tenant.Name+"-etcd", caCert(
		tenant.Name+"-etcd", tenant.Name+"-selfsigned-issuer"))
	if err != nil {
		return err
	}
	logger.Info("creating etcd issuer")
	err = r.createIssuer(ctx, tenant, "etcd")
	if err != nil {
		return err
	}
	logger.Info("creating etcd client certificate")
	err = r.createCertificate(ctx, tenant, tenant.Name+"-etcd-client", serverCert(
		tenant.Name+"-etcd-client",
		tenant.Name+"-etcd-issuer",
		[]string{
			tenant.Name + "-etcd",
			tenant.Name + "-etcd-0",
			tenant.Name + "-etcd-0." + tenant.Name + "-etcd." + tenant.Namespace,
		},
		[]string{
			"127.0.0.1",
		},
	))
	if err != nil {
		return err
	}
	logger.Info("creating etcd health-client certificate")
	err = r.createCertificate(ctx, tenant, tenant.Name+"-etcd-health-client", clientCert(
		tenant.Name+"-etcd-health-client",
		tenant.Name+"-etcd-issuer",
		"kube-etcd-healthcheck-client",
		"system:masters",
	))
	if err != nil {
		return err
	}

	logger.Info("creating apiserver ca")
	err = r.createCertificate(ctx, tenant, tenant.Name+"-ca", caCert(
		tenant.Name+"-ca", tenant.Name+"-selfsigned-issuer"))
	if err != nil {
		return err
	}
	logger.Info("creating apiserver issuer")
	err = r.createIssuer(ctx, tenant, "ca")
	if err != nil {
		return err
	}
	logger.Info("creating apiserver client certificate")
	err = r.createCertificate(ctx, tenant, tenant.Name+"-apiserver-client", serverCert(
		tenant.Name+"-apiserver-client",
		tenant.Name+"-ca-issuer",
		[]string{
			"kubernetes",
			"kubernetes.default",
			"kubernetes.default.svc",
			"kubernetes.default.svc.cluster.local",
			tenant.Name,
			tenant.Name + "-apiserver",
			tenant.Name + "-apiserver." + tenant.Namespace,
			tenant.Name + "-apiserver." + tenant.Namespace + ".svc",
		},
		[]string{
			//TODO: ClusterIP
			"127.0.0.1",
			"0.0.0.0",
		},
	))
	if err != nil {
		return err
	}
	logger.Info("creating apiserver admin certificate")
	err = r.createCertificate(ctx, tenant, tenant.Name+"-apiserver-admin", clientCert(
		tenant.Name+"-apiserver-admin",
		tenant.Name+"-ca-issuer",
		"admin",
		"system:masters",
	))
	if err != nil {
		return err
	}

	return nil
}

func (r *TenantReconciler) reconcileServiceAccountKey(ctx context.Context, tenant *grakolav1.Tenant) error {
	var s corev1.Secret
	err := r.Get(ctx, client.ObjectKey{
		Name:      tenant.Name + "-sa",
		Namespace: tenant.Namespace,
	}, &s)
	if !apierrors.IsNotFound(err) {
		return err
	}

	keyPair, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	pubKey, err := encodePublicKey(&keyPair.PublicKey)
	if err != nil {
		return err
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tenant.Name + "-sa",
			Namespace: tenant.Namespace,
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       pubKey,
			corev1.TLSPrivateKeyKey: encodePrivateKey(keyPair),
		},
	}
	err = r.Create(ctx, secret)

	return err
}

func (r *TenantReconciler) reconcileKubeConfig(ctx context.Context, tenant *grakolav1.Tenant) error {
	rootKey, err := r.getCrtKeyPair(ctx, tenant.Namespace, tenant.Name+"-ca")
	if err != nil {
		return err
	}

	clientPair, err := r.getCrtKeyPair(ctx, tenant.Namespace, tenant.Name+"-apiserver-admin")
	if err != nil {
		return err
	}

	adminKbCfg, err := generateKubeconfig(tenant.Name, tenant.Name+"-apiserver", rootKey.Crt, clientPair, "admin")
	if err != nil {
		return err
	}

	adminSecret := &corev1.Secret{}
	adminSecret.SetNamespace(tenant.Namespace)
	adminSecret.SetName(tenant.Name + "-kubeconfig")

	_, err = ctrl.CreateOrUpdate(ctx, r.Client, adminSecret, func() error {
		adminSecret.Type = corev1.SecretTypeOpaque
		if adminSecret.Data == nil {
			adminSecret.Data = map[string][]byte{}
		}
		adminSecret.Data["kubeconfig"] = []byte(adminKbCfg)
		return ctrl.SetControllerReference(tenant, adminSecret, r.Scheme)
	})

	return err
}

func (r *TenantReconciler) getCrtKeyPair(ctx context.Context, ns, name string) (*CrtKeyPair, error) {
	rootCA := &corev1.Secret{}
	err := r.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, rootCA)
	if err != nil {
		return nil, err
	}
	tlsKey, ok := rootCA.Data[corev1.TLSPrivateKeyKey]
	if !ok {
		return nil, errors.New("tls.key not found")
	}
	keyBlock, _ := pem.Decode(tlsKey)
	key, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, err
	}

	tlsCrt, ok := rootCA.Data[corev1.TLSCertKey]
	if !ok {
		return nil, errors.New("tls.crt not found")
	}
	crtBlock, _ := pem.Decode(tlsCrt)
	cert, err := x509.ParseCertificates(crtBlock.Bytes)
	if err != nil {
		return nil, err
	}
	return &CrtKeyPair{
		Crt: cert[0],
		Key: key,
	}, nil
}

type CrtKeyPair struct {
	Crt *x509.Certificate
	Key *rsa.PrivateKey
}

func generateKubeconfig(clusterName string, server string, apiserverCA *x509.Certificate, caPair *CrtKeyPair, username string) (string, error) {
	data := map[string]string{
		"ca":       base64.StdEncoding.EncodeToString(encodeCert(apiserverCA)),
		"cert":     base64.StdEncoding.EncodeToString(encodeCert(caPair.Crt)),
		"cluster":  clusterName,
		"key":      base64.StdEncoding.EncodeToString(encodePrivateKey(caPair.Key)),
		"server":   fmt.Sprintf("https://%s:6443", server),
		"username": username,
	}

	t, err := template.New("kubeconfig").Parse(kubeconfigTmpl)
	if err != nil {
		return "", err
	}
	buf := bytes.NewBuffer([]byte{})
	if err := t.Execute(buf, data); nil != err {
		return "", err
	}

	return buf.String(), nil
}

func encodePublicKey(key crypto.PublicKey) ([]byte, error) {
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return []byte{}, err
	}
	return pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: der,
	}), nil
}

func encodePrivateKey(key *rsa.PrivateKey) []byte {
	return pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
}

func encodeCert(cert *x509.Certificate) []byte {
	return pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	})
}

// SetupWithManager sets up the controller with the Manager.
func (r *TenantReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&grakolav1.Tenant{}).
		Complete(r)
	//TODO: watch issuers and certificates
}
