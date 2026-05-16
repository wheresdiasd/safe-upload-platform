package clients

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// New loads AWS config from the ambient environment and returns a populated
// Config suitable for injection into a Server or Handler. Reads BUCKET_NAME
// and TABLE_NAME from env vars; returns an error rather than panicking on
// SDK init failure so main() can decide how to surface it.
func New(ctx context.Context) (*Config, error) {
	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}
	s3Client := s3.NewFromConfig(awsCfg)
	return &Config{
		S3:        s3Client,
		S3Presign: s3.NewPresignClient(s3Client),
		Dynamo:    dynamodb.NewFromConfig(awsCfg),
		Bucket:    os.Getenv("BUCKET_NAME"),
		Table:     os.Getenv("TABLE_NAME"),
	}, nil
}
