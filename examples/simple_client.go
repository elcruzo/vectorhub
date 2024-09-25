package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/elcruzo/vectorhub/pkg/client"
)

func main() {
	// This is a simple example showing how to use the VectorHub client
	fmt.Println("VectorHub Client Example")
	fmt.Println("========================")

	// Create a client instance
	clientConfig := &client.ClientConfig{
		Address:    "localhost:50051",
		Timeout:    30 * time.Second,
		MaxRetries: 3,
	}

	fmt.Printf("Connecting to VectorHub server at %s...\n", clientConfig.Address)

	vectorClient, err := client.NewClient(clientConfig)
	if err != nil {
		log.Printf("Failed to create client: %v", err)
		log.Println("Note: This example requires VectorHub server to be running")
		return
	}
	defer func() { _ = vectorClient.Close() }()

	ctx := context.Background()

	// Example operations (would work if server is running)
	fmt.Println("Client created successfully!")
	fmt.Println("To use this client:")
	fmt.Println("1. Start VectorHub server: ./bin/vectorhub")
	fmt.Println("2. Create an index with proper dimensions")
	fmt.Println("3. Insert vectors and perform searches")
	fmt.Println("")

	// Show example vector data
	exampleVector := make([]float32, 128)
	for i := range exampleVector {
		exampleVector[i] = float32(i) / 128.0
	}

	fmt.Printf("Example 128-dimensional vector: [%.3f, %.3f, ..., %.3f]\n",
		exampleVector[0], exampleVector[1], exampleVector[127])

	fmt.Println("Example metadata: map[string]string{\"category\": \"example\", \"source\": \"demo\"}")

	_, _ = ctx, exampleVector // Avoid unused variable warnings
}
