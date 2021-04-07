package main

import (
	"flag"

	"github.com/sirupsen/logrus"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
	"k8s.io/sample-controller/pkg/signals"

	// Uncomment the following line to load the gcp plugin (only required to authenticate against GKE clusters).
	// _ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	"github.com/rahulchheda/retag-image/pkg/controller"
)

var (
	masterURL  string
	kubeconfig string
)

func init() {
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
}

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	// set up signals so we handle the first shutdown signal gracefully
	stopCh := signals.SetupSignalHandler()

	// build config from flags
	cfg, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
	if err != nil {
		klog.Fatalf("Error building kubeconfig: %s", err.Error())
	}

	// create new kubernetes client
	kClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		logrus.WithError(err).Fatal("could not generate dynamic client for config")
	}

	// create new dynamic client
	dClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		logrus.WithError(err).Fatal("could not generate dynamic client for config")
	}

	controller := controller.NewController(kClient, dClient)

	controller.Watch(stopCh)

	if err = controller.Run(2, stopCh); err != nil {
		klog.Fatalf("Error running controller: %s", err.Error())
	}
}
