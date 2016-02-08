package main

import (
	"os"
	"os/signal"
)

func main() {
	println("Hi! gominer loading!")
	m, err := NewMiner()
	if err != nil {
		println("Error initializing miner:", err)
		return
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		<-c
		println("Got Control+C, exiting...")
		m.Stop()
	}()

	m.Run()

	println("Bye!")
}
