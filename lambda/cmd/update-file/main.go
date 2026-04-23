package main

import (
	"context"
	"fmt"
	"log"
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
	VersionID  string `json:"versionId"`
}

type ScanResultDetails struct {
	ScanResultStatus string `json:"scanResultStatus"`
}

func handler(ctx context.Context, event GuardDutyScanEvent) error {
	objectKey := event.Detail.S3ObjectDetails.ObjectKey
	versionID := event.Detail.S3ObjectDetails.VersionID
	scanResult := event.Detail.ScanResultDetails.ScanResultStatus
	fileID := fileIDFromKey(objectKey)

	log.Printf("[update-file] Scan result received: objectKey=%s versionId=%s scanResult=%s fileID=%s", objectKey, versionID, scanResult, fileID)

	if scanResult == "NO_THREATS_FOUND" {
		log.Printf("[update-file] File is clean, updating status: fileID=%s", fileID)
		_, err := clients.DynamoClient.UpdateItem(ctx, &dynamodb.UpdateItemInput{
			TableName: aws.String(clients.TableName),
			Key: map[string]types.AttributeValue{
				"id": &types.AttributeValueMemberS{Value: fileID},
			},
			UpdateExpression: aws.String("SET #status = :status REMOVE expires_at"),
			ExpressionAttributeNames: map[string]string{
				"#status": "status",
			},
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":status": &types.AttributeValueMemberS{Value: models.StatusClean},
			},
		})
		if err != nil {
			log.Printf("[update-file] ERROR: failed to update clean status in DynamoDB: %v", err)
			return err
		}
		log.Printf("[update-file] SUCCESS: file marked as clean: fileID=%s", fileID)
		return nil
	}

	if scanResult != "THREATS_FOUND" {
		log.Printf("[update-file] Unexpected scan status, skipping: objectKey=%s scanResult=%s", objectKey, scanResult)
		return nil
	}

	if versionID == "" {
		log.Printf("[update-file] ERROR: missing versionId in event — refusing to delete (bucket has versioning enabled)")
		return fmt.Errorf("missing versionId in GuardDuty event for objectKey=%s", objectKey)
	}

	log.Printf("[update-file] Threat detected, deleting object version from S3: objectKey=%s versionId=%s", objectKey, versionID)

	_, err := clients.S3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket:    aws.String(clients.BucketName),
		Key:       aws.String(objectKey),
		VersionId: aws.String(versionID),
	})

	if err != nil {
		log.Printf("[update-file] ERROR: failed to delete infected file from S3: %v", err)
		return fmt.Errorf("failed to delete infected file: %w", err)
	}

	log.Printf("[update-file] Infected file deleted from S3, updating DynamoDB: fileID=%s", fileID)

	_, err = clients.DynamoClient.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(clients.TableName),
		Key: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: fileID},
		},
		UpdateExpression: aws.String("SET #status = :status, expires_at = :expires"),
		ExpressionAttributeNames: map[string]string{
			"#status": "status",
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":status":  &types.AttributeValueMemberS{Value: models.StatusDeleted},
			":expires": &types.AttributeValueMemberN{Value: fmt.Sprintf("%d",
				time.Now().Add(30*24*time.Hour).Unix())},
		},
	})
	if err != nil {
		log.Printf("[update-file] ERROR: failed to update deleted status in DynamoDB: %v", err)
		return err
	}

	log.Printf("[update-file] SUCCESS: infected file handled, record marked as deleted with 30-day TTL: fileID=%s", fileID)
	return nil
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
