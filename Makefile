# Always run these targets (phony)
.PHONY: all build zip deploy frontend url test clean stop-local run-local fetch-local-data
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

stop-local:
	@echo "Stopping DynamoDB Local and frontend server..."
	-kill `cat dynamodb.pid` || true
	-kill `cat frontend.pid` || true
	rm -f dynamodb.pid frontend.pid

run-local: stop-local
	@echo "Killing anything on ports 8000, 8080..."
	-lsof -ti :8000 | xargs kill -9 || true
	-lsof -ti :8080 | xargs kill -9 || true
	@echo "Starting DynamoDB Local with sharedDb..."
	java -Djava.library.path=./dynamodb-local/DynamoDBLocal_lib \
	     -jar ./dynamodb-local/DynamoDBLocal.jar \
	     -inMemory -sharedDb -port 8000 \
	     & echo $$! > dynamodb.pid
	sleep 5
	@echo "Waiting for DynamoDB Local to be ready..."
	@i=0; until aws dynamodb list-tables --endpoint-url http://localhost:8000 --no-cli-pager | grep strukturbild_data || [ $$i -ge 20 ]; do sleep 1; i=$$(($$i+1)); done
	@echo "Creating table if not exists..."
	aws dynamodb create-table --no-cli-pager \
		--table-name strukturbild_data \
		--attribute-definitions AttributeName=personId,AttributeType=S AttributeName=id,AttributeType=S \
		--key-schema AttributeName=personId,KeyType=HASH AttributeName=id,KeyType=RANGE \
		--billing-mode PAY_PER_REQUEST \
		--endpoint-url http://localhost:8000 || true
	@echo "Waiting for table to be ACTIVE..."
	@i=0; until [ "$$(aws dynamodb describe-table --table-name strukturbild_data --endpoint-url http://localhost:8000 --no-cli-pager --query 'Table.TableStatus' --output text)" = "ACTIVE" ] || [ $$i -ge 20 ]; do sleep 1; i=$$(($$i+1)); done
	@echo "Confirmed table strukturbild_data is active."
	@echo "Inserting example data..."
	aws dynamodb put-item --no-cli-pager \
		--table-name strukturbild_data \
		--item '{"personId": {"S": "alice"}, "id": {"S": "node1"}, "label": {"S": "Start"}, "isNode": {"BOOL": true}, "x": {"N": "10"}, "y": {"N": "20"}, "timestamp": {"S": "2025-01-01T00:00:00Z"}}' \
		--endpoint-url http://localhost:8000
	@echo "Injecting LOCAL API URL into config.js..."
	cd frontend && echo "window.STRUKTURBILD_API_URL = 'http://localhost:3000';" > config.local.js
	cp frontend/config.local.js frontend/config.js
	@echo "Checking and killing existing frontend on port 8080..."
	-lsof -ti :8080 | xargs kill -9 || true
	@echo "Starting local frontend at http://localhost:8080 ..."
	cd frontend && python3 -m http.server 8080 > /dev/null 2>&1 & echo $$! > ../frontend.pid
	@echo "Starting local Go API on http://localhost:3000 ..."
	LOCAL=true go run ./backend


fetch-local-data:
	aws dynamodb scan --table-name strukturbild_data --endpoint-url http://localhost:8000 --output json --no-cli-pager > local-data.json

frontend:
	@echo "🛠️  Injecting API URL into config.js..."
	cd frontend && echo "window.STRUKTURBILD_API_URL = '$(shell cd terraform && terraform output -raw api_url)';" > config.prod.js
	cp frontend/config.prod.js frontend/config.js
	@echo "☁️  Syncing static files to S3..."
	aws s3 sync frontend/ s3://$(BUCKET) --delete