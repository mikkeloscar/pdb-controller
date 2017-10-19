package main

import (
	"os"
	"os/signal"
	"syscall"
	"time"

	"gopkg.in/alecthomas/kingpin.v2"

	log "github.com/sirupsen/logrus"
)

const (
	defaultInterval = "1m"
)

type config struct {
	Interval time.Duration
	Debug    bool
}

func main() {
	config := config{}
	kingpin.Flag("interval", "Interval between creating PDBs.").Default(defaultInterval).DurationVar(&config.Interval)
	kingpin.Flag("debug", "Enable debug logging.").BoolVar(&config.Debug)
	kingpin.Parse()

	if config.Debug {
		log.SetLevel(log.DebugLevel)
	}

	controller, err := NewPDBController(config.Interval)
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
