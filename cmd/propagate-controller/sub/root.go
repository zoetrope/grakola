package sub

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/zoetrope/grakola"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const defaultConfigPath = "/etc/propagate-controller/config.yaml"
const defaultTenantKubeConfigPath = "/etc/kubernetes/kubeconfig"

var options struct {
	configFile       string
	tenantKubeConfig string
	metricsAddr      string
	probeAddr        string
	zapOpts          zap.Options
}

var rootCmd = &cobra.Command{
	Use:     "propagate-controller",
	Version: grakola.Version,
	Short:   "propagate controller",
	Long:    `propagate controller`,

	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true
		ns := os.Getenv("POD_NAMESPACE")
		if ns == "" {
			return errors.New("no environment variable POD_NAMESPACE")
		}
		return subMain(ns)
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	fs := rootCmd.Flags()
	fs.StringVar(&options.configFile, "config-file", defaultConfigPath, "Configuration file path")
	fs.StringVar(&options.tenantKubeConfig, "tenant-kubeconfig", defaultTenantKubeConfigPath, "kubeconfig file path for tenant apiserver")
	fs.StringVar(&options.metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to")
	fs.StringVar(&options.probeAddr, "health-probe-addr", ":8081", "Listen address for health probes")

	goflags := flag.NewFlagSet("klog", flag.ExitOnError)
	klog.InitFlags(goflags)
	options.zapOpts.BindFlags(goflags)

	fs.AddGoFlagSet(goflags)
}
