# Always run these targets (phony)
.PHONY: all build zip deploy frontend url test clean local logs invoke
# Makefile for deploying Go Lambda with Terraform

LAMBDA_NAME=strukturbild-api
ZIP_NAME=bootstrap.zip
GO_BINARY=bootstrap
BUCKET=strukturbild-frontend-a9141bf9

all: build zip deploy frontend

build:
	@echo "🔧 Building Go binary for Lambda..."
	cd backend && GOOS=linux GOARCH=amd64 go build -o $(GO_BINARY) main.go

zip: build
	@echo "📦 Zipping binary..."
	cd backend && zip -q $(ZIP_NAME) $(GO_BINARY)
	mv backend/$(ZIP_NAME) terraform/

deploy: zip
	@echo "🚀 Deploying via Terraform..."
	cd terraform && terraform init -upgrade && terraform apply -auto-approve
	@echo "🌐 Deploying frontend..."
	$(MAKE) frontend

url:
	@echo "🌍 Your API endpoint:"
	cd terraform && terraform output api_url

test:
	@echo "🔬 Testing POST /submit..."
	curl -X POST $$(cd terraform && terraform output -raw api_url)/submit \
		-H "Content-Type: application/json" \
		-d '{"id":"test123","title":"Test","nodes":[{"id":"1","label":"Leadership","x":0,"y":0}],"edges":[]}'
	@echo "\n🔍 Testing GET /struktur/test123..."
	curl $$(cd terraform && terraform output -raw api_url)/struktur/test123

clean:
	@echo "🧹 Cleaning up..."
	rm -f backend/$(GO_BINARY)
	rm -f terraform/$(ZIP_NAME)

local:
	cd backend && go build -o bootstrap main.go && ./bootstrap

logs:
	aws logs tail /aws/lambda/$(LAMBDA_NAME) --follow --region us-east-1

invoke:
	aws lambda invoke --function-name $(LAMBDA_NAME) --payload '{}' response.json --region us-east-1 && cat response.json

.PHONY: frontend
frontend:
	@echo "🛠️  Injecting API URL into script.js..."
	cd frontend && sed -i.bak "s|__API_URL__|$$(cd ../terraform && terraform output -raw api_url)|g" script.js
	@echo "☁️  Syncing static files to S3..."
	aws s3 sync frontend/ s3://$(BUCKET) --delete