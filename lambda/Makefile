.PHONY: build-create-file build-update-file deploy-create-file deploy-update-file update-create-file update-update-file build-all deploy-all update-all clean

BUCKET_NAME ?= $(AWS_BUCKET_NAME)
TABLE_NAME ?= $(AWS_TABLE_NAME)
ROLE_ARN ?= $(AWS_ROLE_ARN)
REGION ?= $(AWS_REGION)

build-create-file:
	GOOS=linux GOARCH=arm64 go build -o bin/create-file/bootstrap ./cmd/create-file/
	cd bin/create-file && zip ../../create-file.zip bootstrap

build-update-file:
	GOOS=linux GOARCH=arm64 go build -o bin/update-file/bootstrap ./cmd/update-file/
	cd bin/update-file && zip ../../update-file.zip bootstrap

build-all: build-create-file build-update-file

deploy-create-file: build-create-file
	aws lambda create-function \
		--function-name safe-upload-create-file \
		--runtime provided.al2023 \
		--handler bootstrap \
		--architectures arm64 \
		--role $(ROLE_ARN) \
		--zip-file fileb://create-file.zip \
		--environment '{"Variables":{"BUCKET_NAME":"$(BUCKET_NAME)","TABLE_NAME":"$(TABLE_NAME)"}}'

deploy-update-file: build-update-file
	aws lambda create-function \
		--function-name safe-upload-update-file \
		--runtime provided.al2023 \
		--handler bootstrap \
		--architectures arm64 \
		--role $(ROLE_ARN) \
		--zip-file fileb://update-file.zip \
		--environment '{"Variables":{"BUCKET_NAME":"$(BUCKET_NAME)","TABLE_NAME":"$(TABLE_NAME)"}}'

deploy-all: deploy-create-file deploy-update-file

update-create-file: build-create-file
	aws lambda update-function-code \
		--function-name safe-upload-create-file \
		--zip-file fileb://create-file.zip

update-update-file: build-update-file
	aws lambda update-function-code \
		--function-name safe-upload-update-file \
		--zip-file fileb://update-file.zip

update-all: update-create-file update-update-file

clean:
	rm -rf bin/ *.zip bootstrap
