package propagate

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/zoetrope/grakola/pkg/constants"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/structured-merge-diff/v4/typed"
)

func NewMaterializeReconciler(hostClient, tenantClient client.Client, targetNamespace string, res *unstructured.Unstructured, parser *typed.Parser) *MaterializeReconciler {
	if res.GetKind() == "" {
		panic("no group version kind")
	}
	return &MaterializeReconciler{
		hostClient:      hostClient,
		tenantClient:    tenantClient,
		targetNamespace: targetNamespace,
		res:             res.DeepCopy(),
		parser:          parser,
	}
}

type MaterializeReconciler struct {
	hostClient      client.Client
	tenantClient    client.Client
	targetNamespace string
	res             *unstructured.Unstructured
	parser          *typed.Parser
}

func (r *MaterializeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("materialize-reconciler")
	res := r.res.DeepCopy()
	err := r.tenantClient.Get(ctx, req.NamespacedName, res)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	//TODO: deletionTimestamp

	fields, err := extractFields(ctx, r.parser, res, func(mf metav1.ManagedFieldsEntry) bool { return true })
	patch := &unstructured.Unstructured{
		Object: fields,
	}
	patch.SetNamespace(r.targetNamespace)
	patch.SetName(req.Name)
	patch.SetGroupVersionKind(res.GroupVersionKind())
	ann := patch.GetAnnotations()
	ann["materialized-by"] = constants.PropagateControllerName //TODO: ownerRef?
	patch.SetAnnotations(ann)
	err = r.hostClient.Patch(ctx, patch, client.Apply, &client.PatchOptions{
		FieldManager: constants.PropagateControllerName,
		Force:        pointer.Bool(true),
	})
	if err != nil {
		return ctrl.Result{}, err
	}
	logger.Info("materialize successfully")

	return ctrl.Result{}, nil
}

func (r *MaterializeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(r.res).
		Complete(r)
}
