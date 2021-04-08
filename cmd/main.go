package main

import (
	"flag"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"

	// Uncomment the following line to load the gcp plugin (only required to authenticate against GKE clusters).
	// _ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	"github.com/rahulchheda/retag-image/pkg/controller"
	"github.com/rahulchheda/retag-image/pkg/signals"
)

var (
	masterURL  string
	kubeconfig string
)

func main() {
	// register the `kubeconfig` flag which is the used while running the binary for development purposes.
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.Parse()

	// set up signals so we handle the first shutdown signal gracefully
	stop := signals.SetupSignalHandler()

	// build config from flag
	cfg, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
	if err != nil {
		klog.Fatalf("Error building kubeconfig: %s", err.Error())
	}

	// create new kubernetes client
	kClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		klog.Fatalf("could not generate kubernetes client for config, due to error: %w", err)
	}

	// create new dynamic client
	dClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		klog.Fatalf("could not generate dynamic client for config, due to error: %w", err)
	}

	// create new controller.
	controller := controller.NewController(kClient, dClient)

	// start the watch process for the controller.
	controller.Watch(stop)

	// Run is a blocking operation waiting the `stop`
	// to propagate system interrupts as os.Interrupt, syscall.SIGTERM.
	if err = controller.Run(2, stop); err != nil {
		klog.Fatalf("Error running controller: %s", err.Error())
	}
}
