# Always run these targets (phony)
.PHONY: all build zip deploy frontend url test clean stop-local run-local fetch-local-data import import-dir help-import validate validate-dir help-validate validate-verbose help-fix fix-story validate-refs fix-types fix-types-dir
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
	cd backend && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(GO_BINARY) .

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

health: health-prod

health-dev:
	@echo "🔎 Checking DEV /api/health ..."
	$(call TF_SELECT_WORKSPACE,dev)
	@API_URL="$$(cd terraform && terraform output -raw api_url)"; \
	echo "GET $$API_URL/api/health"; \
	curl -sS "$$API_URL/api/health" | sed -e 's/^/  /'

health-prod:
	@echo "🔎 Checking PROD /api/health ..."
	$(call TF_SELECT_WORKSPACE,prod)
	@API_URL="$$(cd terraform && terraform output -raw api_url)"; \
	echo "GET $$API_URL/api/health"; \
	curl -sS "$$API_URL/api/health" | sed -e 's/^/  /'

logs-dev:
	@$(call TF_SELECT_WORKSPACE,dev)
	@aws logs tail /aws/lambda/$(LAMBDA_NAME)-dev --since 15m --follow

logs-prod:
	@$(call TF_SELECT_WORKSPACE,prod)
	@aws logs tail /aws/lambda/$(LAMBDA_NAME)-prod --since 15m --follow

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
#   make import STORY=MarcL FILE=marcl.batch.json
#   make import-dir STORY=MarcL DIR=imports/

help-import:
	@echo "make import STORY=<id> FILE=<file.json>    # POST one JSON to /submit"
	@echo "make import-dir STORY=<id> DIR=<dir>       # POST each *.json in dir"

import:
	@if [ -z "$(STORY)" ] || [ -z "$(FILE)" ]; then \
	  echo "Usage: make import STORY=<id> FILE=<file.json>"; exit 1; \
	fi
	@echo "→ Importing $(FILE) for $(STORY) -> $(API_URL)/submit"
	@curl -sS -X POST "$(API_URL)/submit" \
	  -H 'Content-Type: application/json' \
	  --data-binary @$(FILE) | sed -e 's/^/  /'

import-dir:
	@if [ -z "$(STORY)" ] || [ -z "$(DIR)" ]; then \
	  echo "Usage: make import-dir STORY=<id> DIR=<dir>"; exit 1; \
	fi
	@for f in $(DIR)/*.json; do \
	  echo "→ Importing $$f for $(STORY) -> $(API_URL)/submit"; \
	  curl -sS -X POST "$(API_URL)/submit" \
	    -H 'Content-Type: application/json' \
	    --data-binary @$$f | sed -e 's/^/  /'; \
	done

# --- Validate JSON before import ---
# Usage:
#   make validate STORY=MarcL FILE=Data/MarcL.json
#   make validate-dir STORY=MarcL DIR=Data

help-validate:
	@echo "make validate STORY=<id> FILE=<file.json>    # Check JSON shape & storyId & duplicate node ids"
	@echo "make validate-dir STORY=<id> DIR=<dir>       # Validate each *.json in dir"

validate:
	@if [ -z "$(STORY)" ] || [ -z "$(FILE)" ]; then \
	  echo "Usage: make validate STORY=<id> FILE=<file.json>"; exit 1; \
	fi
	@if [ ! -f "$(FILE)" ]; then \
	  echo "❌ File not found: $(FILE)"; exit 1; \
	fi
	@echo "🔎 Validating $(FILE) for STORY=$(STORY) ..."
	@# 1) Must be valid JSON
	@jq empty "$(FILE)" >/dev/null 2>&1 || { echo "❌ Not valid JSON: $(FILE)"; exit 1; }
	@# 2) Top-level storyId must match
	@env STORY="$(STORY)" jq -e '.storyId == env.STORY' "$(FILE)" >/dev/null || { echo "❌ Top-level .storyId does not match $(STORY)"; exit 1; }
	@# 3) All nodes[].storyId must match
	@env STORY="$(STORY)" jq -e '((.nodes // []) | map(select((.storyId // "") != env.STORY)) | length) == 0' "$(FILE)" >/dev/null || { echo "❌ Some nodes[].storyId differ from $(STORY)"; exit 1; }
	@# 4) All edges[].storyId must match
	@env STORY="$(STORY)" jq -e '((.edges // []) | map(select((.storyId // "") != env.STORY)) | length) == 0' "$(FILE)" >/dev/null || { echo "❌ Some edges[].storyId differ from $(STORY)"; exit 1; }
	@# 5) No duplicate node ids
	@jq -e '((.nodes // []) | map(.id) | length) == ((.nodes // []) | map(.id) | unique | length)' "$(FILE)" >/dev/null || { echo "❌ Duplicate node ids detected in .nodes[].id"; exit 1; }
	@echo "✅ Validation passed for $(FILE)"

validate-dir:
	@if [ -z "$(STORY)" ] || [ -z "$(DIR)" ]; then \
	  echo "Usage: make validate-dir STORY=<id> DIR=<dir>"; exit 1; \
	fi
	@for f in $(DIR)/*.json; do \
	  echo "---"; \
	  $(MAKE) --no-print-directory validate STORY=$(STORY) FILE=$$f || exit $$?; \
	done
	@echo "✅ All JSON files in $(DIR) passed validation"


# --- Verbose validator & fixer ---
# Usage:
#   make validate-verbose STORY=MarcL FILE=Data/MarcL.json
#   make fix-story STORY=MarcL FILE=Data/MarcL.json
#   make validate-refs FILE=Data/MarcL.json

help-fix:
	@echo "make fix-story STORY=<id> FILE=<file.json>   # Set top-level + all nodes/edges storyId to <id>"
	@echo "make validate-verbose STORY=<id> FILE=<file.json>  # Print offending nodes/edges for storyId + duplicates"
	@echo "make validate-refs FILE=<file.json>  # Ensure edges reference existing node ids and list offenders"

validate-verbose:
	@if [ -z "$(STORY)" ] || [ -z "$(FILE)" ]; then \
	  echo "Usage: make validate-verbose STORY=<id> FILE=<file.json>"; exit 1; \
	fi
	@echo "🔎 Verbose check for $(FILE) (STORY=$(STORY))"
	@echo "— Top-level storyId:" && jq -r '.storyId // "(missing)"' "$(FILE)"
	@echo "— Nodes with wrong/missing storyId:" && env STORY="$(STORY)" jq -r '(.nodes // []) | map(select((.storyId // "") != env.STORY))' "$(FILE)"
	@echo "— Edges with wrong/missing storyId:" && env STORY="$(STORY)" jq -r '(.edges // []) | map(select((.storyId // "") != env.STORY))' "$(FILE)"
	@echo "— Duplicate node ids:" && jq -r '((.nodes // []) | group_by(.id) | map(select(length>1) | {id: .[0].id, count: length}))' "$(FILE)"

fix-story:
	@if [ -z "$(STORY)" ] || [ -z "$(FILE)" ]; then \
	  echo "Usage: make fix-story STORY=<id> FILE=<file.json>"; exit 1; \
	fi
	@echo "🛠️  Setting storyId=$(STORY) on top-level, all nodes, and all edges in $(FILE) ..."
	@tmp="$(FILE).tmp"; \
	env STORY="$(STORY)" jq '.storyId=env.STORY | .nodes=((.nodes // []) | map(.storyId=env.STORY)) | .edges=((.edges // []) | map(.storyId=env.STORY))' "$(FILE)" > "$$tmp" && mv "$$tmp" "$(FILE)"
	@echo "✅ Fixed storyId in $(FILE)"

validate-refs:
	@if [ -z "$(FILE)" ]; then \
	  echo "Usage: make validate-refs FILE=<file.json>"; exit 1; \
	fi
	@echo "🔗 Checking edge references in $(FILE) ..."
	@TMP_JQ=$$(mktemp); \
	cat > $$TMP_JQ <<'JQ'; \
	(.nodes // [] | map(.id) | unique) as $ids
	| ((.edges // []) | all(
	    (type=="object")
	    and (.from|type=="string") and (.to|type=="string")
	    and ($ids | index(.from) != null)
	    and ($ids | index(.to) != null)
	  ))
	JQ
	@jq -e -f $$TMP_JQ "$(FILE)" >/dev/null || { \
	  echo "❌ Some edges reference missing node ids"; \
	  jq -r '(.nodes // [] | map(.id) | unique) as $$ids | (.edges // []) | map(select(($$ids | index(.from))==null or ($$ids | index(.to))==null))' "$(FILE)"; \
	  rm -f $$TMP_JQ; exit 1; }
	@rm -f $$TMP_JQ
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
# --- Data ops (wipe & import via API) ----------------------------------------
# These helpers let you wipe a story's graph and re-import both STORY and GRAPH
# JSON through the existing HTTP API (no direct DynamoDB access required).

help-data:
	@echo "make wipe-graph STORY=<id>                      # Delete ALL nodes for <id> via API"
	@echo "make import-story FILE=<story.json>              # POST story+paragraphs+paraNodeMap to /api/stories/import"
	@echo "make import-graph STORY=<id> FILE=<graph.json>  # POST nodes+edges to /submit (wrapper of 'make import')"
	@echo "make reset-story STORY=<id> STORY_FILE=<story.json> GRAPH_FILE=<graph.json>"
	@echo "                                               # wipe graph, then import story and graph"

# Delete all nodes for a storyId using the existing DELETE /struktur/<story>/<node> endpoint.
# (Edges connected to a node are expected to be removed server-side by your handler.)
wipe-graph:
	@if [ -z "$(STORY)" ]; then \
	  echo "Usage: make wipe-graph STORY=<id> [FILE=<graph.json>]"; exit 1; \
	fi
	@API_URL="$$(cd terraform && terraform output -raw api_url)"; \
	echo "🧨 Wiping graph for STORY=$(STORY) at $$API_URL ..."; \
	if [ -n "$(FILE)" ] && [ -f "$(FILE)" ]; then \
	  ids="$$(jq -r '(.nodes // []) | map(.id) | .[]' "$(FILE)")"; \
	else \
	  ids="$$(curl -s "$$API_URL/struktur/$(STORY)" | jq -r '.nodes[]?.id')"; \
	fi; \
	if [ -z "$$ids" ]; then \
	  echo "  (no nodes to delete)"; \
	else \
	  for id in $$ids; do \
	    echo "  - DELETE /struktur/$(STORY)/$$id"; \
	    curl -s -X DELETE "$$API_URL/struktur/$(STORY)/$$id" >/dev/null; \
	  done; \
	fi; \
	echo "✅ Done wipe-graph for $(STORY)"

# Import STORY (metadata + paragraphs + paragraphNodeMap) in the new MVP shape.
# Expects FILE to be your import JSON (e.g., testfiles/import_rychenberg.json).
import-story:
	@if [ -z "$(FILE)" ]; then \
	  echo "Usage: make import-story FILE=<story.json>"; exit 1; \
	fi
	@echo "📚 Importing STORY from $(FILE) -> $(API_URL)/api/stories/import"
	@curl -sS -X POST "$(API_URL)/api/stories/import" \
	  -H 'Content-Type: application/json' \
	  --data-binary @$(FILE) | sed -e 's/^/  /'
	@echo "✅ Story import complete."

# Import GRAPH for a storyId (wrapper around existing 'import' target).
# Validates refs first to catch missing node ids in edges.
import-graph:
	@if [ -z "$(STORY)" ] || [ -z "$(FILE)" ]; then \
	  echo "Usage: make import-graph STORY=<id> FILE=<graph.json>"; exit 1; \
	fi
	@$(MAKE) --no-print-directory validate-refs FILE=$(FILE)
	@echo "🗺️  Importing GRAPH from $(FILE) for STORY=$(STORY)"
	@$(MAKE) --no-print-directory import STORY=$(STORY) FILE=$(FILE)
	@echo "✅ Graph import complete."

# One-stop reset for a given story: wipe graph, import story, then import graph.
reset-story:
	@if [ -z "$(STORY)" ] || [ -z "$(STORY_FILE)" ] || [ -z "$(GRAPH_FILE)" ]; then \
	  echo "Usage: make reset-story STORY=<id> STORY_FILE=<story.json> GRAPH_FILE=<graph.json>"; exit 1; \
	fi
	@$(MAKE) --no-print-directory wipe-graph STORY=$(STORY)
	@$(MAKE) --no-print-directory import-story FILE=$(STORY_FILE)
	@$(MAKE) --no-print-directory import-graph STORY=$(STORY) FILE=$(GRAPH_FILE)
	@echo "✨ Finished reset-story for $(STORY)"
