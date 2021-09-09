package controllers

import (
	"context"

	grakolav1 "github.com/zoetrope/grakola/api/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *TenantReconciler) createSelfSignedIssuer(ctx context.Context, tenant *grakolav1.Tenant) error {
	issuerName := tenant.Name + "-selfsigned-issuer"
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(certManagerGroupVersion.WithKind(IssuerKind))
	obj.SetName(issuerName)
	obj.SetNamespace(tenant.Namespace)
	obj.UnstructuredContent()["spec"] = map[string]interface{}{
		"selfSigned": map[string]interface{}{},
	}
	err := ctrl.SetControllerReference(tenant, obj, r.Scheme)
	if err != nil {
		return err
	}
	return r.Patch(ctx, obj, client.Apply, &client.PatchOptions{
		Force:        pointer.BoolPtr(true),
		FieldManager: TenantControllerName,
	})
}

func (r *TenantReconciler) createIssuer(ctx context.Context, tenant *grakolav1.Tenant, name string) error {
	issuerName := tenant.Name + "-" + name + "-issuer"
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(certManagerGroupVersion.WithKind(IssuerKind))
	obj.SetName(issuerName)
	obj.SetNamespace(tenant.Namespace)
	obj.UnstructuredContent()["spec"] = map[string]interface{}{
		"ca": map[string]interface{}{
			"secretName": tenant.Name + "-" + name,
		},
	}
	err := ctrl.SetControllerReference(tenant, obj, r.Scheme)
	if err != nil {
		return err
	}
	return r.Patch(ctx, obj, client.Apply, &client.PatchOptions{
		Force:        pointer.BoolPtr(true),
		FieldManager: TenantControllerName,
	})
}

var privateKey = map[string]interface{}{
	"algorithm": "RSA",
	"encoding":  "PKCS1",
	"size":      2048,
}

func caCert(name, issuer string) map[string]interface{} {
	return map[string]interface{}{
		"isCA":       true,
		"commonName": name,
		"secretName": name,
		"privateKey": privateKey,
		"issuerRef": map[string]interface{}{
			"kind": IssuerKind,
			"name": issuer,
		},
	}
}

func serverCert(name, issuer string, dnsNames, ipAddresses []string) map[string]interface{} {
	return map[string]interface{}{
		"isCA":        false,
		"commonName":  name,
		"secretName":  name,
		"duration":    "8760h",
		"renewBefore": "4380h",
		"usages": []string{
			"server auth",
			"client auth",
		},
		"dnsNames":    dnsNames,
		"ipAddresses": ipAddresses,
		"privateKey":  privateKey,
		"issuerRef": map[string]interface{}{
			"kind": IssuerKind,
			"name": issuer,
		},
	}
}

func clientCert(name, issuer, user, subject string) map[string]interface{} {
	return map[string]interface{}{
		"isCA":        false,
		"commonName":  user,
		"secretName":  name,
		"duration":    "8760h",
		"renewBefore": "4380h",
		"subject": map[string]interface{}{
			"organizations": []string{
				subject,
			},
		},
		"usages": []string{
			"client auth",
		},
		"privateKey": privateKey,
		"issuerRef": map[string]interface{}{
			"kind": IssuerKind,
			"name": issuer,
		},
	}
}

func (r *TenantReconciler) createCertificate(ctx context.Context, tenant *grakolav1.Tenant, name string, spec map[string]interface{}) error {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(certManagerGroupVersion.WithKind(CertificateKind))
	obj.SetName(name)
	obj.SetNamespace(tenant.Namespace)
	obj.UnstructuredContent()["spec"] = spec
	err := ctrl.SetControllerReference(tenant, obj, r.Scheme)
	if err != nil {
		return err
	}
	return r.Patch(ctx, obj, client.Apply, &client.PatchOptions{
		Force:        pointer.BoolPtr(true),
		FieldManager: TenantControllerName,
	})
}
