# Always run these targets (phony)
.PHONY: all build zip deploy frontend url test clean stop-local run-local fetch-local-data import import-dir help-import validate validate-dir help-validate validate-verbose help-fix fix-person validate-refs fix-types fix-types-dir
# --- Environment / Workspaces (dev/prod split) ---
ENV ?= prod
BUCKET_DEV  = strukturbild-frontend-dev-a9141bf9
BUCKET_PROD = strukturbild-frontend-a9141bf9

# Helper to ensure Terraform workspace exists/selected
TF_SELECT_WORKSPACE = cd terraform && (terraform workspace select $(1) >/dev/null 2>&1 || terraform workspace new $(1))
# Makefile for deploying Go Lambda with Terraform

LAMBDA_NAME=strukturbild-api
ZIP_NAME=bootstrap.zip
GO_BINARY=bootstrap
API_URL := $(shell cd terraform && terraform output -raw api_url 2>/dev/null || echo http://localhost:3000)

all: build zip deploy frontend

build:
	@echo "🔧 Building Go binary for Lambda..."
	cd backend && GOOS=linux GOARCH=amd64 go build -o $(GO_BINARY) main.go

zip: build
	@echo "📦 Zipping binary..."
	cd backend && zip -q $(ZIP_NAME) $(GO_BINARY)
	mv backend/$(ZIP_NAME) terraform/

deploy: deploy-prod

# --- Dev/Prod aware deploy targets (non-breaking additions) ---

deploy-dev: zip
	@echo "🚀 Deploying DEV via Terraform..."
	$(call TF_SELECT_WORKSPACE,dev)
	cd terraform && TF_VAR_env=dev terraform init -upgrade && TF_VAR_env=dev terraform apply -auto-approve
	@$(MAKE) frontend-dev

deploy-prod: zip
	@echo "🚀 Deploying PROD via Terraform..."
	$(call TF_SELECT_WORKSPACE,prod)
	cd terraform && TF_VAR_env=prod terraform init -upgrade && TF_VAR_env=prod terraform apply -auto-approve
	@$(MAKE) frontend-prod

url: url-prod

url-dev:
	@echo "🌍 Your DEV API endpoint:"
	$(call TF_SELECT_WORKSPACE,dev)
	cd terraform && terraform output api_url

url-prod:
	@echo "🌍 Your PROD API endpoint:"
	$(call TF_SELECT_WORKSPACE,prod)
	cd terraform && terraform output api_url

test:
	$(call TF_SELECT_WORKSPACE,prod)
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

frontend: frontend-prod

frontend-dev:
	@echo "🛠️  Injecting DEV API URL into config.js..."
	$(call TF_SELECT_WORKSPACE,dev)
	@API_URL="$$(cd terraform && terraform output -raw api_url)"; \
	cd frontend && echo "window.STRUKTURBILD_API_URL = '$$API_URL';" > config.dev.js; \
	cp config.dev.js config.js
	@echo "☁️  Syncing static files to DEV S3 (with --delete)..."
	aws s3 sync frontend/ s3://$(BUCKET_DEV) --delete
	@echo "🧼 Forcing fresh index.html headers (DEV)..."
	aws s3 cp frontend/index.html s3://$(BUCKET_DEV)/index.html \
	  --cache-control "no-cache, no-store, must-revalidate" \
	  --expires 0 --metadata-directive REPLACE

frontend-prod:
	@echo "🛠️  Injecting PROD API URL into config.js..."
	$(call TF_SELECT_WORKSPACE,prod)
	@API_URL="$$(cd terraform && terraform output -raw api_url)"; \
	cd frontend && echo "window.STRUKTURBILD_API_URL = '$$API_URL';" > config.prod.js; \
	cp config.prod.js config.js
	@echo "☁️  Syncing static files to PROD S3 (with --delete)..."
	aws s3 sync frontend/ s3://$(BUCKET_PROD) --delete
	@echo "🧼 Forcing fresh index.html headers (PROD)..."
	aws s3 cp frontend/index.html s3://$(BUCKET_PROD)/index.html \
	  --cache-control "no-cache, no-store, must-revalidate" \
	  --expires 0 --metadata-directive REPLACE

# --- Batch import JSON to API ---
# Usage:
#   make import PERSON=MarcL FILE=marcl.batch.json
#   make import-dir PERSON=MarcL DIR=imports/

help-import:
	@echo "make import PERSON=<id> FILE=<file.json>    # POST one JSON to /submit"
	@echo "make import-dir PERSON=<id> DIR=<dir>       # POST each *.json in dir"

import:
	@if [ -z "$(PERSON)" ] || [ -z "$(FILE)" ]; then \
	  echo "Usage: make import PERSON=<id> FILE=<file.json>"; exit 1; \
	fi
	@echo "→ Importing $(FILE) for $(PERSON) -> $(API_URL)/submit"
	@curl -sS -X POST "$(API_URL)/submit" \
	  -H 'Content-Type: application/json' \
	  --data-binary @$(FILE) | sed -e 's/^/  /'

import-dir:
	@if [ -z "$(PERSON)" ] || [ -z "$(DIR)" ]; then \
	  echo "Usage: make import-dir PERSON=<id> DIR=<dir>"; exit 1; \
	fi
	@for f in $(DIR)/*.json; do \
	  echo "→ Importing $$f for $(PERSON) -> $(API_URL)/submit"; \
	  curl -sS -X POST "$(API_URL)/submit" \
	    -H 'Content-Type: application/json' \
	    --data-binary @$$f | sed -e 's/^/  /'; \
	done

# --- Validate JSON before import ---
# Usage:
#   make validate PERSON=MarcL FILE=Data/MarcL.json
#   make validate-dir PERSON=MarcL DIR=Data

help-validate:
	@echo "make validate PERSON=<id> FILE=<file.json>    # Check JSON shape & personId & duplicate node ids"
	@echo "make validate-dir PERSON=<id> DIR=<dir>       # Validate each *.json in dir"

validate:
	@if [ -z "$(PERSON)" ] || [ -z "$(FILE)" ]; then \
	  echo "Usage: make validate PERSON=<id> FILE=<file.json>"; exit 1; \
	fi
	@if [ ! -f "$(FILE)" ]; then \
	  echo "❌ File not found: $(FILE)"; exit 1; \
	fi
	@echo "🔎 Validating $(FILE) for PERSON=$(PERSON) ..."
	@# 1) Must be valid JSON
	@jq empty "$(FILE)" >/dev/null 2>&1 || { echo "❌ Not valid JSON: $(FILE)"; exit 1; }
	@# 2) Top-level personId must match
	@env PERSON="$(PERSON)" jq -e '.personId == env.PERSON' "$(FILE)" >/dev/null || { echo "❌ Top-level .personId does not match $(PERSON)"; exit 1; }
	@# 3) All nodes[].personId must match
	@env PERSON="$(PERSON)" jq -e '((.nodes // []) | map(select((.personId // "") != env.PERSON)) | length) == 0' "$(FILE)" >/dev/null || { echo "❌ Some nodes[].personId differ from $(PERSON)"; exit 1; }
	@# 4) All edges[].personId must match
	@env PERSON="$(PERSON)" jq -e '((.edges // []) | map(select((.personId // "") != env.PERSON)) | length) == 0' "$(FILE)" >/dev/null || { echo "❌ Some edges[].personId differ from $(PERSON)"; exit 1; }
	@# 5) No duplicate node ids
	@jq -e '((.nodes // []) | map(.id) | length) == ((.nodes // []) | map(.id) | unique | length)' "$(FILE)" >/dev/null || { echo "❌ Duplicate node ids detected in .nodes[].id"; exit 1; }
	@echo "✅ Validation passed for $(FILE)"

validate-dir:
	@if [ -z "$(PERSON)" ] || [ -z "$(DIR)" ]; then \
	  echo "Usage: make validate-dir PERSON=<id> DIR=<dir>"; exit 1; \
	fi
	@for f in $(DIR)/*.json; do \
	  echo "---"; \
	  $(MAKE) --no-print-directory validate PERSON=$(PERSON) FILE=$$f || exit $$?; \
	done
	@echo "✅ All JSON files in $(DIR) passed validation"


# --- Verbose validator & fixer ---
# Usage:
#   make validate-verbose PERSON=MarcL FILE=Data/MarcL.json
#   make fix-person PERSON=MarcL FILE=Data/MarcL.json
#   make validate-refs FILE=Data/MarcL.json

help-fix:
	@echo "make fix-person PERSON=<id> FILE=<file.json>   # Set top-level + all nodes/edges personId to <id>"
	@echo "make validate-verbose PERSON=<id> FILE=<file.json>  # Print offending nodes/edges for personId + duplicates"
	@echo "make validate-refs FILE=<file.json>  # Ensure edges reference existing node ids and list offenders"

validate-verbose:
	@if [ -z "$(PERSON)" ] || [ -z "$(FILE)" ]; then \
	  echo "Usage: make validate-verbose PERSON=<id> FILE=<file.json>"; exit 1; \
	fi
	@echo "🔎 Verbose check for $(FILE) (PERSON=$(PERSON))"
	@echo "— Top-level personId:" && jq -r '.personId // "(missing)"' "$(FILE)"
	@echo "— Nodes with wrong/missing personId:" && env PERSON="$(PERSON)" jq -r '(.nodes // []) | map(select((.personId // "") != env.PERSON))' "$(FILE)"
	@echo "— Edges with wrong/missing personId:" && env PERSON="$(PERSON)" jq -r '(.edges // []) | map(select((.personId // "") != env.PERSON))' "$(FILE)"
	@echo "— Duplicate node ids:" && jq -r '((.nodes // []) | group_by(.id) | map(select(length>1) | {id: .[0].id, count: length}))' "$(FILE)"

fix-person:
	@if [ -z "$(PERSON)" ] || [ -z "$(FILE)" ]; then \
	  echo "Usage: make fix-person PERSON=<id> FILE=<file.json>"; exit 1; \
	fi
	@echo "🛠️  Setting personId=$(PERSON) on top-level, all nodes, and all edges in $(FILE) ..."
	@tmp="$(FILE).tmp"; \
	env PERSON="$(PERSON)" jq '.personId=env.PERSON | .nodes=((.nodes // []) | map(.personId=env.PERSON)) | .edges=((.edges // []) | map(.personId=env.PERSON))' "$(FILE)" > "$$tmp" && mv "$$tmp" "$(FILE)"
	@echo "✅ Fixed personId in $(FILE)"

validate-refs:
	@if [ -z "$(FILE)" ]; then \
	  echo "Usage: make validate-refs FILE=<file.json>"; exit 1; \
	fi
	@echo "🔗 Checking edge references in $(FILE) ..."
	@jq -e '(.nodes // [] | map(.id)) as $ids | ((.edges // []) | map(select(($ids | index(.from))!=null and ($ids | index(.to))!=null)) | length) == ((.edges // []) | length)' "$(FILE)" >/dev/null \
	  || { echo "❌ Some edges reference missing node ids"; \
	       echo "Offenders:"; \
	       jq -r '(.nodes // [] | map(.id)) as $ids | (.edges // []) | map(select(($ids | index(.from))==null or ($ids | index(.to))==null))' "$(FILE)"; exit 1; }
	@echo "✅ All edges reference existing node ids"


# --- Migrate legacy types in JSON files ---
# Maps: entwicklungsinhalt, schulentwicklungsziele -> schulentwicklungsziel
# Usage:
#   make fix-types FILE=Data/MarcL.json
#   make fix-types-dir DIR=Data
#
# Notes:
# - Only rewrites .type in nodes[] and edges[]
# - Safe to re-run (idempotent)

fix-types:
	@if [ -z "$(FILE)" ]; then \
	  echo "Usage: make fix-types FILE=<file.json>"; exit 1; \
	fi
	@echo "🛠️  Rewriting types in $(FILE): entwicklungsinhalt*, schulentwicklungsziele -> schulentwicklungsziel"
	@tmp="$(FILE).tmp"; \
	jq '(.nodes // []) |= map(.type = ( ( .type // "" ) \
	      | ascii_downcase \
	      | if . == "entwicklungsinhalt" or . == "schulentwicklungsziele" then "schulentwicklungsziel" else . end )) \
	    | (.edges // []) |= map(.type = ( ( .type // "" ) \
	      | ascii_downcase \
	      | if . == "entwicklungsinhalt" or . == "schulentwicklungsziele" then "schulentwicklungsziel" else . end ))' \
	  "$(FILE)" > "$$tmp" && mv "$$tmp" "$(FILE)"
	@echo "✅ Types migrated in $(FILE)"

fix-types-dir:
	@if [ -z "$(DIR)" ]; then \
	  echo "Usage: make fix-types-dir DIR=<dir>"; exit 1; \
	fi
	@for f in $(DIR)/*.json; do \
	  $(MAKE) --no-print-directory fix-types FILE=$$f || exit $$?; \
	done
	@echo "✅ All JSON files in $(DIR) migrated"
# --- Convenience aliases for frontend deploy ---
deploy-frontend-dev: frontend-dev
deploy-frontend-prod: frontend-prod