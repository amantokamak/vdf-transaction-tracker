package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	trackingService "github.com/aman/vdf-tracker/services" // Update with your actual package path
	"github.com/joho/godotenv"
)

func main() {
	// Load environment variables from .env file
	err := godotenv.Load()
	if err != nil {
		log.Fatalf("Error loading .env file: %v", err)
	}

	// Initialize VDF tracker
	vdfEventTrackingService, err := trackingService.NewVDFTracker()
	if err != nil {
		log.Fatalf("Failed to initialize VDF tracker: %v", err)
	}

	// Start tracking VDF events
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := vdfEventTrackingService.TrackVDFEvents(ctx)
		if err != nil {
			log.Fatalf("Failed to track VDF events: %v", err)
		}
	}()

	// Handle OS signals for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Wait for termination signal
	<-sigCh
	log.Println("Received termination signal, stopping service...")

	// Cancel context to stop event tracking
	cancel()

	// Wait for all goroutines to finish
	wg.Wait()
	log.Println("Service stopped gracefully")
}
