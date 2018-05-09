package main

import (
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gopkg.in/alecthomas/kingpin.v2"
	"k8s.io/client-go/rest"

	log "github.com/sirupsen/logrus"
)

const (
	defaultInterval      = "1m"
	defaultPDBNameSuffix = "pdb-controller"
)

type config struct {
	Interval      time.Duration
	APIServer     *url.URL
	Debug         bool
	PDBNameSuffix string
}

func main() {
	config := config{}
	kingpin.Flag("interval", "Interval between creating PDBs.").Default(defaultInterval).DurationVar(&config.Interval)
	kingpin.Flag("apiserver", "API server url.").URLVar(&config.APIServer)
	kingpin.Flag("debug", "Enable debug logging.").BoolVar(&config.Debug)
	kingpin.Flag("pdb-name-suffix", "Specify default PDB name suffix.").Default(defaultPDBNameSuffix).StringVar(&config.PDBNameSuffix)
	kingpin.Parse()

	if config.Debug {
		log.SetLevel(log.DebugLevel)
	}

	var kubeConfig *rest.Config

	if config.APIServer != nil {
		kubeConfig = &rest.Config{
			Host: config.APIServer.String(),
		}
	}

	controller, err := NewPDBController(config.Interval, kubeConfig, config.PDBNameSuffix)
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
