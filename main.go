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

var (
	config struct {
		Interval time.Duration
	}
)

func init() {
	kingpin.Flag("interval", "Interval between creating PDBs.").
		Default(defaultInterval).DurationVar(&config.Interval)
}

func main() {
	kingpin.Parse()

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
