package propagate

import (
	"context"

	"github.com/zoetrope/grakola/pkg/constants"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/structured-merge-diff/v4/typed"
)

func NewVirtualizeReconciler(hostClient, tenantClient client.Client, targetNamespace string, res *unstructured.Unstructured, parser *typed.Parser) *VirtualizeReconciler {
	if res.GetKind() == "" {
		panic("no group version kind")
	}
	return &VirtualizeReconciler{
		hostClient:      hostClient,
		tenantClient:    tenantClient,
		targetNamespace: targetNamespace,
		res:             res.DeepCopy(),
		parser:          parser,
	}
}

type VirtualizeReconciler struct {
	hostClient      client.Client
	tenantClient    client.Client
	targetNamespace string
	res             *unstructured.Unstructured
	parser          *typed.Parser
}

func (r *VirtualizeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("virtualize-reconciler")
	res := r.res.DeepCopy()
	err := r.hostClient.Get(ctx, req.NamespacedName, res)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	//TODO: deletionTimestamp

	fields, err := extractFields(ctx, r.parser, res, func(mf metav1.ManagedFieldsEntry) bool {
		return mf.Manager != constants.PropagateControllerName
	})
	logger.Info("extractFields", "fields", fields)
	patch := &unstructured.Unstructured{
		Object: fields,
	}
	patch.SetNamespace(r.targetNamespace)
	patch.SetName(req.Name)
	patch.SetGroupVersionKind(res.GroupVersionKind())
	//err = r.tenantClient.Patch(ctx, patch, client.Apply, &client.PatchOptions{
	//	FieldManager: constants.PropagateControllerName,
	//	Force:        pointer.Bool(true),
	//})
	//if err != nil {
	//	return ctrl.Result{}, err
	//}
	logger.Info("update status", "object", patch.Object["status"])
	err = r.tenantClient.Status().Patch(ctx, patch, client.Apply, &client.PatchOptions{
		FieldManager: constants.PropagateControllerName,
		Force:        pointer.Bool(true),
	})
	if err != nil {
		return ctrl.Result{}, err
	}
	logger.Info("virtualize successfully")

	return ctrl.Result{}, nil
}

func (r *VirtualizeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	pred := func(obj client.Object) bool {
		ann := obj.GetAnnotations()
		owner, ok := ann["materialized-by"]
		if !ok {
			return false
		}
		return owner == constants.PropagateControllerName
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(r.res).
		WithEventFilter(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool { return pred(e.Object) },
			UpdateFunc: func(e event.UpdateEvent) bool { return pred(e.ObjectOld) || pred(e.ObjectNew) },
			DeleteFunc: func(e event.DeleteEvent) bool { return pred(e.Object) },
		}).
		Complete(r)
}
