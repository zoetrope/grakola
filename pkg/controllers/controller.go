package controllers

import (
	grakolav1 "github.com/zoetrope/grakola/api/v1"
	"github.com/zoetrope/grakola/pkg/constants"
	"k8s.io/apimachinery/pkg/runtime"
	metav1apply "k8s.io/client-go/applyconfigurations/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

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
