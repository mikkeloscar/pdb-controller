package main

import (
	"context"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	kingpin "gopkg.in/alecthomas/kingpin.v2"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	log "github.com/sirupsen/logrus"
)

const (
	defaultInterval       = "1m"
	defaultPDBNameSuffix  = "pdb-controller"
	defaultNonReadyTTL    = "0s"
	defaultMaxUnavailable = "1"
)

type config struct {
	Interval           time.Duration
	APIServer          *url.URL
	Debug              bool
	PDBNameSuffix      string
	NonReadyTTL        time.Duration
	ParentResourceHash bool
	MaxUnavailable     string
}

func main() {
	config := config{}
	kingpin.Flag("interval", "Interval between creating PDBs.").Default(defaultInterval).DurationVar(&config.Interval)
	kingpin.Flag("apiserver", "API server url.").URLVar(&config.APIServer)
	kingpin.Flag("debug", "Enable debug logging.").BoolVar(&config.Debug)
	kingpin.Flag("pdb-name-suffix", "Specify default PDB name suffix.").Default(defaultPDBNameSuffix).StringVar(&config.PDBNameSuffix)
	kingpin.Flag("non-ready-ttl", "Set the ttl for when to remove the managed PDB if the deployment/statefulset is unhealthy (default: disabled).").Default(defaultNonReadyTTL).DurationVar(&config.NonReadyTTL)
	kingpin.Flag("use-parent-resource-hash", "Uses parent-resource-hash labels as selector for PDBs.").BoolVar(&config.ParentResourceHash)
	kingpin.Flag("max-unavailable", "The value of maxUnavailable that would be set in the generated PDBs.").Default(defaultMaxUnavailable).StringVar(&config.MaxUnavailable)
	kingpin.Parse()

	if config.Debug {
		log.SetLevel(log.DebugLevel)
	}

	var err error
	var kubeConfig *rest.Config

	if config.APIServer != nil {
		kubeConfig = &rest.Config{
			Host: config.APIServer.String(),
		}
	} else {
		kubeConfig, err = rest.InClusterConfig()
		if err != nil {
			log.Fatal(err)
		}
	}

	client, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		log.Fatal(err)
	}

	controller := NewPDBController(
		config.Interval,
		client,
		config.PDBNameSuffix,
		config.NonReadyTTL,
		config.ParentResourceHash,
		intstr.Parse(config.MaxUnavailable),
	)

	ctx, cancel := context.WithCancel(context.Background())
	go handleSigterm(cancel)

	controller.Run(ctx)
}

func handleSigterm(cancelFunc func()) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	<-signals
	log.Info("Received Term signal. Terminating...")
	cancelFunc()
}
