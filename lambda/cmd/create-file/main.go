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
	dynamotypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	chiadapter "github.com/awslabs/aws-lambda-go-api-proxy/chi"
	"github.com/awslabs/aws-lambda-go-api-proxy/core"
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

type UploadedPart struct {
	PartNumber int32  `json:"part_number"`
	ETag       string `json:"etag"`
}

type CompleteMultipartUploadRequest struct {
	Parts []UploadedPart `json:"parts"`
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

	apiCtx, ok := core.GetAPIGatewayContextFromContext(r.Context())
	if !ok || apiCtx.Identity.APIKeyID == "" {
		log.Printf("[create-file] ERROR: missing api key id in request context")
		http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
		return
	}
	uploadedBy := apiCtx.Identity.APIKeyID

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

func CompleteUpload(w http.ResponseWriter, r *http.Request) {
	fileID := chimux.URLParam(r, "id")
	log.Printf("[complete-upload] Request received: fileID=%s", fileID)

	if fileID == "" {
		log.Printf("[complete-upload] ERROR: missing fileID in path")
		http.Error(w, `{"error": "file id is required"}`, http.StatusBadRequest)
		return
	}

	var req CompleteMultipartUploadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[complete-upload] ERROR: invalid request body: %v", err)
		http.Error(w, `{"error": "invalid request body"}`, http.StatusBadRequest)
		return
	}

	if len(req.Parts) == 0 {
		log.Printf("[complete-upload] ERROR: no parts provided")
		http.Error(w, `{"error": "parts are required"}`, http.StatusBadRequest)
		return
	}

	getResult, err := clients.DynamoClient.GetItem(r.Context(), &dynamodb.GetItemInput{
		TableName: aws.String(clients.TableName),
		Key: map[string]dynamotypes.AttributeValue{
			"id": &dynamotypes.AttributeValueMemberS{Value: fileID},
		},
	})
	if err != nil {
		log.Printf("[complete-upload] ERROR: failed to load file record: %v", err)
		http.Error(w, `{"error": "failed to load file record"}`, http.StatusInternalServerError)
		return
	}

	if getResult.Item == nil {
		log.Printf("[complete-upload] ERROR: file record not found: fileID=%s", fileID)
		http.Error(w, `{"error": "file not found"}`, http.StatusNotFound)
		return
	}

	var record models.FileRecord
	if err := attributevalue.UnmarshalMap(getResult.Item, &record); err != nil {
		log.Printf("[complete-upload] ERROR: failed to unmarshal file record: %v", err)
		http.Error(w, `{"error": "failed to parse file record"}`, http.StatusInternalServerError)
		return
	}

	if record.Status != models.StatusPendingUpload {
		log.Printf("[complete-upload] ERROR: invalid status transition: fileID=%s status=%s", fileID, record.Status)
		http.Error(w, `{"error": "file is not pending upload"}`, http.StatusConflict)
		return
	}

	completedParts := make([]s3types.CompletedPart, len(req.Parts))
	for i, p := range req.Parts {
		completedParts[i] = s3types.CompletedPart{
			PartNumber: aws.Int32(p.PartNumber),
			ETag:       aws.String(p.ETag),
		}
	}

	log.Printf("[complete-upload] Completing multipart upload: fileID=%s uploadID=%s parts=%d", fileID, record.UploadID, len(completedParts))

	_, err = clients.S3Client.CompleteMultipartUpload(r.Context(), &s3.CompleteMultipartUploadInput{
		Bucket:          aws.String(clients.BucketName),
		Key:             aws.String(record.S3Key),
		UploadId:        aws.String(record.UploadID),
		MultipartUpload: &s3types.CompletedMultipartUpload{Parts: completedParts},
	})
	if err != nil {
		log.Printf("[complete-upload] ERROR: failed to complete multipart upload: %v", err)
		http.Error(w, `{"error": "failed to complete upload"}`, http.StatusInternalServerError)
		return
	}

	_, err = clients.DynamoClient.UpdateItem(r.Context(), &dynamodb.UpdateItemInput{
		TableName: aws.String(clients.TableName),
		Key: map[string]dynamotypes.AttributeValue{
			"id": &dynamotypes.AttributeValueMemberS{Value: fileID},
		},
		UpdateExpression: aws.String("SET #status = :status REMOVE expires_at"),
		ExpressionAttributeNames: map[string]string{
			"#status": "status",
		},
		ExpressionAttributeValues: map[string]dynamotypes.AttributeValue{
			":status": &dynamotypes.AttributeValueMemberS{Value: models.StatusPendingScan},
		},
	})
	if err != nil {
		log.Printf("[complete-upload] ERROR: failed to update status in DynamoDB: %v", err)
		http.Error(w, `{"error": "failed to update file status"}`, http.StatusInternalServerError)
		return
	}

	log.Printf("[complete-upload] SUCCESS: fileID=%s status=pending_scan", fileID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"id":     fileID,
		"status": models.StatusPendingScan,
	})
}

type DownloadFileResponse struct {
	ID        string `json:"id"`
	URL       string `json:"url"`
	ExpiresAt int64  `json:"expires_at"`
}

func DownloadFile(w http.ResponseWriter, r *http.Request) {
	fileID := chimux.URLParam(r, "id")
	log.Printf("[download-file] Request received: fileID=%s", fileID)

	if fileID == "" {
		log.Printf("[download-file] ERROR: missing fileID in path")
		http.Error(w, `{"error": "file id is required"}`, http.StatusBadRequest)
		return
	}

	getResult, err := clients.DynamoClient.GetItem(r.Context(), &dynamodb.GetItemInput{
		TableName: aws.String(clients.TableName),
		Key: map[string]dynamotypes.AttributeValue{
			"id": &dynamotypes.AttributeValueMemberS{Value: fileID},
		},
	})
	if err != nil {
		log.Printf("[download-file] ERROR: failed to load file record: %v", err)
		http.Error(w, `{"error": "failed to load file record"}`, http.StatusInternalServerError)
		return
	}

	if getResult.Item == nil {
		log.Printf("[download-file] ERROR: file record not found: fileID=%s", fileID)
		http.Error(w, `{"error": "file not found"}`, http.StatusNotFound)
		return
	}

	var record models.FileRecord
	if err := attributevalue.UnmarshalMap(getResult.Item, &record); err != nil {
		log.Printf("[download-file] ERROR: failed to unmarshal file record: %v", err)
		http.Error(w, `{"error": "failed to parse file record"}`, http.StatusInternalServerError)
		return
	}

	if record.Status != models.StatusClean {
		log.Printf("[download-file] ERROR: file not available for download: fileID=%s status=%s", fileID, record.Status)
		http.Error(w, `{"error": "file is not available for download"}`, http.StatusConflict)
		return
	}

	expiresIn := 15 * time.Minute
	presigned, err := clients.S3PresignClient.PresignGetObject(r.Context(), &s3.GetObjectInput{
		Bucket: aws.String(clients.BucketName),
		Key:    aws.String(record.S3Key),
	}, s3.WithPresignExpires(expiresIn))
	if err != nil {
		log.Printf("[download-file] ERROR: failed to presign download URL: %v", err)
		http.Error(w, `{"error": "failed to generate download URL"}`, http.StatusInternalServerError)
		return
	}

	log.Printf("[download-file] SUCCESS: fileID=%s s3Key=%s", fileID, record.S3Key)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(DownloadFileResponse{
		ID:        fileID,
		URL:       presigned.URL,
		ExpiresAt: time.Now().Add(expiresIn).Unix(),
	})
}

func main() {
	clients.Init()

	r := chimux.NewRouter()
	r.Post("/files", CreateFile)
	r.Post("/files/{id}/complete-upload", CompleteUpload)
	r.Get("/files/{id}", DownloadFile)

	chiLambda := chiadapter.New(r)
	lambda.Start(chiLambda.ProxyWithContext)
}
