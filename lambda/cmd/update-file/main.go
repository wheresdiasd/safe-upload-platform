package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"safe-upload-platform-lambda/internal/clients"
	"safe-upload-platform-lambda/internal/models"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type GuardDutyScanEvent struct {
	Detail ScanDetail `json:"detail"`
}

type ScanDetail struct {
	S3ObjectDetails   S3Object          `json:"s3ObjectDetails"`
	ScanResultDetails ScanResultDetails `json:"scanResultDetails"`
}

type S3Object struct {
	BucketName string `json:"bucketName"`
	ObjectKey  string `json:"objectKey"`
}

type ScanResultDetails struct {
	ScanResult string `json:"scanResult"`
}

func handler(ctx context.Context, event GuardDutyScanEvent) error {
	objectKey := event.Detail.S3ObjectDetails.ObjectKey
	scanResult := event.Detail.ScanResultDetails.ScanResult

	if scanResult == "NO_THREATS_FOUND" {
		_, err := clients.DynamoClient.UpdateItem(ctx, &dynamodb.UpdateItemInput{
			TableName: aws.String(clients.TableName),
			Key: map[string]types.AttributeValue{
				"id": &types.AttributeValueMemberS{Value: fileIDFromKey(objectKey)},
			},
			UpdateExpression: aws.String("SET #status = :status REMOVE expires_at"),
			ExpressionAttributeNames: map[string]string{
				"#status": "status",
			},
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":status": &types.AttributeValueMemberS{Value: models.StatusClean},
			},
		})
		return err
	}

	_, err := clients.S3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(clients.BucketName),
		Key:    aws.String(objectKey),
	})

	if err != nil {
		return fmt.Errorf("failed to delete infected file: %w", err)
	}

	_, err = clients.DynamoClient.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(clients.TableName),
		Key: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: fileIDFromKey(objectKey)},
		},
		UpdateExpression: aws.String("SET #status = :status, expires_at = :expires"),
		ExpressionAttributeNames: map[string]string{
			"#status": "status",
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":status": &types.AttributeValueMemberS{Value: models.StatusDeleted},
			":expires": &types.AttributeValueMemberN{Value: fmt.Sprintf("%d",
				time.Now().Add(30*24*time.Hour).Unix())},
		},
	})
	return err
}

func fileIDFromKey(key string) string {
	parts := strings.Split(key, "/")
	if len(parts) >= 2 {
		return parts[1]
	}
	return key
}

func main() {
	clients.Init()
	lambda.Start(handler)
}
