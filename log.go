package main

import (
	"os"
	"fmt"
	"path/filepath"
	"github.com/jrick/logrotate/rotator"
	"github.com/btcsuite/btclog"
)

type logWriter struct{}

func (logWriter) Write(p []byte) (n int, err error) {
	os.Stdout.Write(p)
	logRotator.Write(p)
	return len(p), nil
}

var (
	backendLogger = btclog.NewBackend(logWriter{})
	log btclog.Logger =backendLogger.Logger("HSRSTATIC")
	logRotator *rotator.Rotator
)

func init() {
	logFile := "C:\\Users\\Administrator\\Desktop\\project\\src\\hsrstatistics\\log.txt"
	logDir, _ := filepath.Split(logFile)
	err := os.MkdirAll(logDir, 0700)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create log directory: %v\n", err)
		os.Exit(1)
	}
	r, err := rotator.New(logFile, 10*1024, false, 3)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create file rotator: %v\n", err)
		os.Exit(1)
	}

	logRotator = r
}