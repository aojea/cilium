package pip

import (
	"context"

	v1 "gke-internal.googlesource.com/anthos-networking/apis/v2/persistent-ip/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// IPRouteReconciler reconciles IPRoute objects.
type IPRouteReconciler struct {
	client.Client
}

func (r *IPRouteReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, rerr error) {
	// TODO(b/254671349) - Implement the reconciler
	return ctrl.Result{}, nil
}

// SetupWithManager configures this controller in the manager.
func (r *IPRouteReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.IPRoute{}).
		WithEventFilter(predicate.Funcs{
			CreateFunc: func(ce event.CreateEvent) bool {
				// we are only interested in IPRoutes that are updated with
				// Accepted conditions by the main IPRoute controller.
				// Hence we will not reconcile on Create event.
				return false
			},
		}).
		Complete(r)
}
