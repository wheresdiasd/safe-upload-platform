package clients

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"os"
)

var (
	S3Client        *s3.Client
	S3PresignClient *s3.PresignClient
	DynamoClient    *dynamodb.Client
	BucketName      string
	TableName       string
)

func Init() {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		panic(fmt.Sprintf("Unable to load AWS config: %v", err))
	}
	S3Client = s3.NewFromConfig(cfg)
	S3PresignClient = s3.NewPresignClient(S3Client)
	DynamoClient = dynamodb.NewFromConfig(cfg)
	BucketName = os.Getenv("BUCKET_NAME")
	TableName = os.Getenv("TABLE_NAME")
}
