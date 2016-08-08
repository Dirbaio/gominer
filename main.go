package main

import (
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"runtime/pprof"
	"time"
)

var (
	cfg *config
)

func gominerMain() error {
	// Load configuration and parse command line.  This function also
	// initializes logging and configures it accordingly.
	tcfg, _, err := loadConfig()
	if err != nil {
		return err
	}
	cfg = tcfg
	defer backendLog.Flush()

	// Show version at startup.
	mainLog.Infof("Version %s", version())

	// Enable http profiling server if requested.
	if cfg.Profile != "" {
		go func() {
			listenAddr := net.JoinHostPort("", cfg.Profile)
			mainLog.Infof("Creating profiling server "+
				"listening on %s", listenAddr)
			profileRedirect := http.RedirectHandler("/debug/pprof",
				http.StatusSeeOther)
			http.Handle("/", profileRedirect)
			err := http.ListenAndServe(listenAddr, nil)
			if err != nil {
				mainLog.Errorf("Unable to create profiler: %v", err)
				backendLog.Flush()
				os.Exit(1)
			}
		}()
	}

	// Write cpu profile if requested.
	if cfg.CPUProfile != "" {
		f, err := os.Create(cfg.CPUProfile)
		if err != nil {
			mainLog.Errorf("Unable to create cpu profile: %v", err.Error())
			return err
		}
		pprof.StartCPUProfile(f)
		defer f.Close()
		defer pprof.StopCPUProfile()
	}

	// Write mem profile if requested.
	if cfg.MemProfile != "" {
		f, err := os.Create(cfg.MemProfile)
		if err != nil {
			mainLog.Errorf("Unable to create cpu profile: %v", err)
			return err
		}
		timer := time.NewTimer(time.Minute * 20) // 20 minutes
		go func() {
			<-timer.C
			pprof.WriteHeapProfile(f)
			f.Close()
		}()
	}

	m, err := NewMiner()
	if err != nil {
		mainLog.Criticalf("Error initializing miner: %v", err)
		return err
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		<-c
		mainLog.Warn("Got Control+C, exiting...")
		m.Stop()
	}()

	m.Run()

	return nil
}

func main() {
	// Use all processor cores.
	runtime.GOMAXPROCS(runtime.NumCPU())

	// Work around defer not working after os.Exit()
	if err := gominerMain(); err != nil {
		os.Exit(1)
	}
}
