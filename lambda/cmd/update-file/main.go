package main

import (
	"context"
	"log"

	"safe-upload-platform-lambda/internal/clients"

	"github.com/aws/aws-lambda-go/lambda"
)

func main() {
	cfg, err := clients.New(context.Background())
	if err != nil {
		log.Fatalf("init clients: %v", err)
	}
	h := &Handler{cfg: cfg}
	lambda.Start(h.Handle)
}
