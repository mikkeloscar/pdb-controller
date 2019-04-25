package main

import (
	"os"
	"os/signal"
	"syscall"
	"time"

	"gopkg.in/alecthomas/kingpin.v2"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	log "github.com/sirupsen/logrus"
)

const (
	defaultInterval      = "1m"
	defaultPDBNameSuffix = "pdb-controller"
	defaultNonReadyTTL   = "0s"
)

type config struct {
	Interval      time.Duration
	Debug         bool
	PDBNameSuffix string
	NonReadyTTL   time.Duration
	Kubeconfig    string
}

func init() {
	log.SetFormatter(&log.TextFormatter{
		TimestampFormat: "2006-01-02 15:04:05",
		FullTimestamp:   true,
	})
}

func main() {
	config := config{}
	kingpin.Flag("interval", "Interval between creating PDBs.").Default(defaultInterval).DurationVar(&config.Interval)
	kingpin.Flag("kubeconfig", "Path to kubeconfig file; if not specified, use in-cluster config").ExistingFileVar(&config.Kubeconfig)
	kingpin.Flag("debug", "Enable debug logging.").BoolVar(&config.Debug)
	kingpin.Flag("pdb-name-suffix", "Specify default PDB name suffix.").Default(defaultPDBNameSuffix).StringVar(&config.PDBNameSuffix)
	kingpin.Flag("non-ready-ttl", "Set the ttl for when to remove the managed PDB if the deployment/statefulset is unhealthy (default: disabled).").Default(defaultNonReadyTTL).DurationVar(&config.NonReadyTTL)
	kingpin.Parse()

	if config.Debug {
		log.SetLevel(log.DebugLevel)
	}

	var err error
	var kubernetesClientConfig *rest.Config

	if config.Kubeconfig != "" {
		log.Infof("Using current context from kubeconfig file: %v", config.Kubeconfig)
		kubernetesClientConfig, err = clientcmd.BuildConfigFromFlags("", config.Kubeconfig)
		if err != nil {
			log.Fatal(err)
		}
	} else {
		kubernetesClientConfig, err = rest.InClusterConfig()
		if err != nil {
			log.Fatal(err)
		}
	}

	client, err := kubernetes.NewForConfig(kubernetesClientConfig)
	if err != nil {
		log.Fatal(err)
	}

	controller, err := NewPDBController(config.Interval, client, config.PDBNameSuffix, config.NonReadyTTL)
	if err != nil {
		log.Fatal(err)
	}

	stopChan := make(chan struct{}, 1)
	go handleSigterm(stopChan)

	controller.Run(stopChan)
}

func handleSigterm(stopChan chan struct{}) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	<-signals
	log.Info("Received Term signal. Terminating...")
	close(stopChan)
}
