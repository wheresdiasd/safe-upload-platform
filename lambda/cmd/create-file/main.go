package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"safe-upload-platform-lambda/internal/clients"
	"safe-upload-platform-lambda/internal/models"

	"time"

	"github.com/aws/aws-lambda-go/lambda"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	chiadapter "github.com/awslabs/aws-lambda-go-api-proxy/chi"
	chimux "github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const chunkSize int64 = 5 * 1024 * 1024 // 5 MB

type CreateFileRequest struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type PartUrl struct {
	PartNumber int    `json:"part_number"`
	URL        string `json:"url"`
}

type CreateFileResponse struct {
	ID        string    `json:"id"`
	UploadID  string    `json:"upload_id"`
	ChunkSize int64     `json:"chunk_size"`
	Parts     []PartUrl `json:"parts"`
	ExpiresAt int64     `json:"expires_at"`
}

func CreateFile(w http.ResponseWriter, r *http.Request) {
	log.Printf("[create-file] Request received: method=%s path=%s", r.Method, r.URL.Path)

	var req CreateFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[create-file] ERROR: invalid request body: %v", err)
		http.Error(w, `{"error": "invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.Name == "" || req.Size <= 0 {
		log.Printf("[create-file] ERROR: validation failed name=%q size=%d", req.Name, req.Size)
		http.Error(w, `{"error": "name and size are required"}`, http.StatusBadRequest)
		return
	}

	log.Printf("[create-file] Processing file: name=%s size=%d", req.Name, req.Size)

	uploadedBy := r.Header.Get("x-api-key")
	fileID := uuid.New().String()
	s3Key := fmt.Sprintf("uploads/%s/%s", fileID, req.Name)
	expiresAt := time.Now().Add(1 * time.Hour).Unix()

	log.Printf("[create-file] Initiating multipart upload: fileID=%s s3Key=%s", fileID, s3Key)

	multipartOutput, err := clients.S3Client.CreateMultipartUpload(r.Context(), &s3.CreateMultipartUploadInput{
		Bucket: aws.String(clients.BucketName),
		Key:    aws.String(s3Key),
	})

	if err != nil {
		log.Printf("[create-file] ERROR: failed to initiate multipart upload: %v", err)
		http.Error(w, `{"error": "failed to initiate multi-part upload" }`, http.StatusInternalServerError)
		return
	}

	log.Printf("[create-file] Multipart upload initiated: uploadID=%s", *multipartOutput.UploadId)

	numberOfParts := int(math.Ceil(float64(req.Size) / float64(chunkSize)))
	parts := make([]PartUrl, numberOfParts)

	for i := 0; i < numberOfParts; i++ {
		partNumber := i + 1

		presignedResult, err := clients.S3PresignClient.PresignUploadPart(r.Context(), &s3.UploadPartInput{
			Bucket:     aws.String(clients.BucketName),
			Key:        aws.String(s3Key),
			UploadId:   multipartOutput.UploadId,
			PartNumber: aws.Int32(int32(partNumber)),
		}, s3.WithPresignExpires(15*time.Minute))

		if err != nil {
			log.Printf("[create-file] ERROR: failed to presign part %d: %v", partNumber, err)
			http.Error(w, `{"error": "failed to generate part URL"}`, http.StatusInternalServerError)
			return
		}

		parts[i] = PartUrl{
			PartNumber: partNumber,
			URL:        presignedResult.URL,
		}
	}

	log.Printf("[create-file] Generated %d presigned part URLs", numberOfParts)

	record := models.FileRecord{
		ID:         fileID,
		Name:       req.Name,
		Size:       req.Size,
		UploadedBy: uploadedBy,
		Status:     models.StatusPendingUpload,
		UploadID:   *multipartOutput.UploadId,
		S3Key:      s3Key,
		ExpiresAt:  expiresAt,
	}

	item, err := attributevalue.MarshalMap(record)

	if err != nil {
		log.Printf("[create-file] ERROR: failed to marshal file record: %v", err)
		http.Error(w, `{"error": "failed to parse file record"}`, http.StatusInternalServerError)
		return
	}

	_, err = clients.DynamoClient.PutItem(r.Context(), &dynamodb.PutItemInput{
		TableName: aws.String(clients.TableName),
		Item:      item,
	})

	if err != nil {
		log.Printf("[create-file] ERROR: failed to save to DynamoDB: %v", err)
		http.Error(w, `{"error":"failed to save file record"}`, http.StatusInternalServerError)
		return
	}

	log.Printf("[create-file] SUCCESS: fileID=%s parts=%d uploadID=%s", fileID, numberOfParts, *multipartOutput.UploadId)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(CreateFileResponse{
		ID:        fileID,
		UploadID:  *multipartOutput.UploadId,
		ChunkSize: chunkSize,
		Parts:     parts,
		ExpiresAt: expiresAt,
	})
}

func main() {
	clients.Init()

	r := chimux.NewRouter()
	r.Post("/files", CreateFile)

	chiLambda := chiadapter.New(r)
	lambda.Start(chiLambda.ProxyWithContext)
}
