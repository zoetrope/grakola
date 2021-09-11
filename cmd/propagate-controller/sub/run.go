package sub

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"

	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/cybozu-go/well"
	"github.com/zoetrope/grakola/pkg/config"
	"github.com/zoetrope/grakola/pkg/controllers/propagate"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func subMain(ns string) error {
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&options.zapOpts)))
	logger := ctrl.Log.WithName("setup")

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return fmt.Errorf("unable to add client-go objects: %w", err)
	}

	cfgData, err := os.ReadFile(options.configFile)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", options.configFile, err)
	}
	cfg := &config.Config{}
	if err := cfg.Load(cfgData); err != nil {
		return fmt.Errorf("unable to load the configuration file: %w", err)
	}

	targets := make([]*unstructured.Unstructured, len(cfg.Targets))
	for i := range cfg.Targets {
		gvk := &cfg.Targets[i]
		targets[i] = &unstructured.Unstructured{}
		targets[i].SetGroupVersionKind(schema.GroupVersionKind{
			Group:   gvk.Group,
			Version: gvk.Version,
			Kind:    gvk.Kind,
		})
	}

	hostMgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Logger:                 log.FromContext(context.Background()).WithName("host-manager"),
		NewClient:              newCachingClient,
		MetricsBindAddress:     "0",
		HealthProbeBindAddress: "0",
		LeaderElection:         false,
		Namespace:              ns,
	})
	if err != nil {
		return fmt.Errorf("unable to start host manager: %w", err)
	}

	var tenantConfig clientcmd.ClientConfig
	out, err := ioutil.ReadFile(options.tenantKubeConfig)
	if err != nil {
		return err
	}
	tenantConfig, err = clientcmd.NewClientConfigFromBytes(out)
	if err != nil {
		return err
	}
	tenantClientConfig, err := tenantConfig.ClientConfig()

	tenantMgr, err := ctrl.NewManager(tenantClientConfig, ctrl.Options{
		Scheme:                 scheme,
		Logger:                 log.FromContext(context.Background()).WithName("tenant-manager"),
		NewClient:              newCachingClient,
		MetricsBindAddress:     "0",
		HealthProbeBindAddress: "0",
		LeaderElection:         false,
	})
	if err != nil {
		return fmt.Errorf("unable to start tenant manager: %w", err)
	}

	parser := propagate.NewParser()
	for _, res := range targets {
		if err = propagate.NewMaterializeReconciler(hostMgr.GetClient(), tenantMgr.GetClient(), ns, res, parser).
			SetupWithManager(tenantMgr); err != nil {
			return fmt.Errorf("unable to create Materialize controller: %w", err)
		}
		if err = propagate.NewVirtualizeReconciler(hostMgr.GetClient(), tenantMgr.GetClient(), ns, res, parser).
			SetupWithManager(hostMgr); err != nil {
			return fmt.Errorf("unable to create Virtualize controller: %w", err)
		}
		logger.Info("watching", "gvk", res.GroupVersionKind().String())
	}

	//+kubebuilder:scaffold:builder

	if err := hostMgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up health check: %w", err)
	}
	if err := hostMgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up ready check: %w", err)
	}
	if err := tenantMgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up health check: %w", err)
	}
	if err := tenantMgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up ready check: %w", err)
	}

	logger.Info("starting manager")
	well.Go(hostMgr.Start)
	well.Go(tenantMgr.Start)
	well.Stop()
	ctx := ctrl.SetupSignalHandler()
	hostMgr.GetCache().WaitForCacheSync(ctx)
	tenantMgr.GetCache().WaitForCacheSync(ctx)

	err = well.Wait()
	if err != nil && !well.IsSignaled(err) {
		return fmt.Errorf("problem running manager: %s", err)
	}
	return nil
}

func newCachingClient(cache cache.Cache, config *rest.Config, options client.Options, uncachedObjects ...client.Object) (client.Client, error) {
	c, err := client.New(config, options)
	if err != nil {
		return nil, err
	}

	return client.NewDelegatingClient(client.NewDelegatingClientInput{
		CacheReader:       cache,
		Client:            c,
		UncachedObjects:   uncachedObjects,
		CacheUnstructured: true,
	})
}
