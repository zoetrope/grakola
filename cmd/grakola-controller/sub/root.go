package sub

import (
	"flag"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/zoetrope/grakola"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var options struct {
	metricsAddr      string
	probeAddr        string
	leaderElectionID string
	zapOpts          zap.Options
}

var rootCmd = &cobra.Command{
	Use:     "grakola-controller",
	Version: grakola.Version,
	Short:   "grakola controller",
	Long:    `grakola controller`,

	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true
		return subMain()
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
	fs.StringVar(&options.metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to")
	fs.StringVar(&options.probeAddr, "health-probe-addr", ":8081", "Listen address for health probes")
	fs.StringVar(&options.leaderElectionID, "leader-election-id", "grakola", "ID for leader election by controller-runtime")

	goflags := flag.NewFlagSet("klog", flag.ExitOnError)
	klog.InitFlags(goflags)
	options.zapOpts.BindFlags(goflags)

	fs.AddGoFlagSet(goflags)
}
