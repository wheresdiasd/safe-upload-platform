package main

import (
	"context"
	"log"

	"safe-upload-platform-lambda/internal/clients"

	"github.com/aws/aws-lambda-go/lambda"
	chiadapter "github.com/awslabs/aws-lambda-go-api-proxy/chi"
	chimux "github.com/go-chi/chi/v5"
)

func main() {
	cfg, err := clients.New(context.Background())
	if err != nil {
		log.Fatalf("init clients: %v", err)
	}
	srv := &Server{cfg: cfg}

	r := chimux.NewRouter()
	r.Post("/files", srv.CreateFile)
	r.Post("/files/{id}/complete-upload", srv.CompleteUpload)
	r.Get("/files/{id}", srv.DownloadFile)

	lambda.Start(chiadapter.New(r).ProxyWithContext)
}
