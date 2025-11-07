# Always run these targets (phony)
.PHONY: all build zip deploy frontend url test clean stop-local run-local fetch-local-data import import-dir help-import validate validate-dir help-validate validate-verbose help-fix fix-person validate-refs fix-types fix-types-dir health import-story get-story-full submit-graph testdata-init smoke cleanup-smoke clean-testfiles import-rychenberg submit-graph-rychenberg smoke-rychenberg smoke-rychenberg-dev data-pull data-push
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
	$(call TF_SELECT_WORKSPACE,$(ENV))
	@API_URL="$$(cd terraform && terraform output -raw api_url 2>/dev/null || echo http://localhost:3000)"; \
	echo "→ Importing $(FILE) for $(STORY) -> $$API_URL/submit"; \
	curl -sS -X POST "$$API_URL/submit" \
	  -H 'Content-Type: application/json' \
	  --data-binary @$(FILE) | sed -e 's/^/  /'

import-dir:
	@if [ -z "$(STORY)" ] || [ -z "$(DIR)" ]; then \
	  echo "Usage: make import-dir STORY=<id> DIR=<dir>"; exit 1; \
	fi
	$(call TF_SELECT_WORKSPACE,$(ENV))
	@API_URL="$$(cd terraform && terraform output -raw api_url 2>/dev/null || echo http://localhost:3000)"; \
	for f in $(DIR)/*.json; do \
	  echo "→ Importing $$f for $(STORY) -> $$API_URL/submit"; \
	  curl -sS -X POST "$$API_URL/submit" \
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
	@# 3) All nodes[].storyId (when present) must match
	@env STORY="$(STORY)" jq -e '((.nodes // []) | map(select((.storyId // env.STORY) != env.STORY)) | length) == 0' "$(FILE)" >/dev/null || { echo "❌ Some nodes[].storyId differ from $(STORY)"; exit 1; }
	@# 4) All edges[].storyId (when present) must match
	@env STORY="$(STORY)" jq -e '((.edges // []) | map(select((.storyId // env.STORY) != env.STORY)) | length) == 0' "$(FILE)" >/dev/null || { echo "❌ Some edges[].storyId differ from $(STORY)"; exit 1; }
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
	@echo "make fix-story STORY=<id> FILE=<file.json>   # Set top-level storyId and scrub mismatched node/edge storyIds"
	@echo "make validate-verbose STORY=<id> FILE=<file.json>  # Print offending nodes/edges for storyId + duplicates"
	@echo "make validate-refs FILE=<file.json>  # Ensure edges reference existing node ids and list offenders"

validate-verbose:
	@if [ -z "$(STORY)" ] || [ -z "$(FILE)" ]; then \
	  echo "Usage: make validate-verbose STORY=<id> FILE=<file.json>"; exit 1; \
	fi
	@echo "🔎 Verbose check for $(FILE) (STORY=$(STORY))"
	@echo "— Top-level storyId:" && jq -r '.storyId // "(missing)"' "$(FILE)"
	@echo "— Nodes with wrong storyId:" && env STORY="$(STORY)" jq -r '(.nodes // []) | map(select((.storyId // env.STORY) != env.STORY))' "$(FILE)"
	@echo "— Edges with wrong storyId:" && env STORY="$(STORY)" jq -r '(.edges // []) | map(select((.storyId // env.STORY) != env.STORY))' "$(FILE)"
	@echo "— Duplicate node ids:" && jq -r '((.nodes // []) | group_by(.id) | map(select(length>1) | {id: .[0].id, count: length}))' "$(FILE)"

fix-story:
	@if [ -z "$(STORY)" ] || [ -z "$(FILE)" ]; then \
          echo "Usage: make fix-story STORY=<id> FILE=<file.json>"; exit 1; \
        fi
	@echo "🛠️  Setting storyId=$(STORY) on top-level and cleaning node/edge storyIds in $(FILE) ..."
	@tmp="$(FILE).tmp"; \
        env STORY="$(STORY)" jq '.storyId=env.STORY | .nodes=((.nodes // []) | map(del(.storyId))) | .edges=((.edges // []) | map(del(.storyId)))' "$(FILE)" > "$$tmp" && mv "$$tmp" "$(FILE)"
	@echo "✅ Fixed storyId in $(FILE)"

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

# --- Health & smoke tests (dev/prod aware; set ENV=dev to target dev) ---
health:
	$(call TF_SELECT_WORKSPACE,$(ENV))
	@API_URL="$$(cd terraform && terraform output -raw api_url 2>/dev/null || echo http://localhost:3000)"; \
	echo "GET $$API_URL/api/stories"; \
	curl -sS -w "\nHTTP %{http_code}\n" "$$API_URL/api/stories" || true

import-story:
	@if [ -z "$(FILE)" ]; then \
	  echo "Usage: make import-story FILE=<file.json>"; exit 1; \
	fi
	$(call TF_SELECT_WORKSPACE,$(ENV))
	@API_URL="$$(cd terraform && terraform output -raw api_url 2>/dev/null || echo http://localhost:3000)"; \
	echo "📚 Importing STORY from $(FILE) -> $$API_URL/api/stories/import"; \
	curl -sS -X POST "$$API_URL/api/stories/import" -H 'Content-Type: application/json' --data-binary @$(FILE) | sed -e 's/^/  /'

get-story-full:
	@if [ -z "$(STORY)" ]; then \
	  echo "Usage: make get-story-full STORY=<storyId>"; exit 1; \
	fi
	$(call TF_SELECT_WORKSPACE,$(ENV))
	@API_URL="$$(cd terraform && terraform output -raw api_url 2>/dev/null || echo http://localhost:3000)"; \
	echo "GET $$API_URL/api/stories/$(STORY)/full"; \
	curl -sS "$$API_URL/api/stories/$(STORY)/full" | jq .

.PHONY: data-pull
data-pull:
	@if [ -z "$(STORY)" ]; then echo "Usage: make ENV=$(ENV) data-pull STORY=<storyId>"; exit 1; fi
	$(call TF_SELECT_WORKSPACE,$(ENV))
	@bash <<-'BASH'
	set -euo pipefail
	API_URL="$$(cd terraform && terraform output -raw api_url 2>/dev/null || echo http://localhost:3000)"
	STORY_ID="$(STORY)"
	
	mkdir -p Data
	JQ1=$$(mktemp)
	JQ2=$$(mktemp)
	
	cat > "$${JQ1}" <<'JQ'
	{
	  story: {
	    storyId: .story.storyId,
	    schoolId: .story.schoolId,
	    title: .story.title,
	    paragraphNodeMap: (.story.paragraphNodeMap // {})
	  },
	  paragraphs: ((.paragraphs // []) | map({
	    paragraphId, storyId, index,
	    title: (.title // null),
	    bodyMd,
	    citations
	  }))
	}
	JQ
	
	cat > "$${JQ2}" <<'JQ'
	{
	  storyId: env.STORY_ID,
	  nodes: ((.nodes // []) | map({id, label, type, color, x, y})),
	  edges: ((.edges // []) | map({from, to, label, type}))
	}
	JQ
	
	echo "GET $$API_URL/api/stories/$$STORY_ID/full  -> Data/story-$$STORY_ID.json (compact import shape)"
	curl -fsS "$$API_URL/api/stories/$$STORY_ID/full" | jq -c -f "$$JQ1" > "Data/story-$$STORY_ID.json"
	echo "GET $$API_URL/struktur/$$STORY_ID         -> Data/graph-$$STORY_ID.json (compact)"
	STORY_ID="$$STORY_ID" curl -fsS "$$API_URL/struktur/$$STORY_ID" | STORY_ID="$$STORY_ID" jq -c -f "$$JQ2" > "Data/graph-$$STORY_ID.json"
	rm -f "$$JQ1" "$$JQ2"
	echo "✅ Wrote Data/story-$$STORY_ID.json and Data/graph-$$STORY_ID.json"
	BASH

.PHONY: data-push
data-push:
	@if [ -z "$(STORY)" ]; then echo "Usage: make ENV=$(ENV) data-push STORY=<storyId>"; exit 1; fi
	@if [ ! -f "Data/story-$(STORY).json" ]; then echo "❌ Missing file Data/story-$(STORY).json"; exit 1; fi
	@if [ ! -f "Data/graph-$(STORY).json" ]; then echo "❌ Missing file Data/graph-$(STORY).json"; exit 1; fi
	$(call TF_SELECT_WORKSPACE,$(ENV))
	@API_URL="$$(cd terraform && terraform output -raw api_url 2>/dev/null || echo http://localhost:3000)"; \
	echo "POST $$API_URL/api/stories/import  <= Data/story-$(STORY).json"; \
	curl -fsS -X POST "$$API_URL/api/stories/import" -H 'Content-Type: application/json' --data-binary @"Data/story-$(STORY).json" | sed -e 's/^/  /'; \
	echo "POST $$API_URL/submit               <= Data/graph-$(STORY).json"; \
	curl -fsS -X POST "$$API_URL/submit" -H 'Content-Type: application/json' --data-binary @"Data/graph-$(STORY).json" | sed -e 's/^/  /'; \
	echo "✅ Pushed story+graph for $(STORY) to $(ENV)"

submit-graph:
	@if [ -z "$(FILE)" ]; then \
	  echo "Usage: make submit-graph FILE=<file.json>"; exit 1; \
	fi
	$(call TF_SELECT_WORKSPACE,$(ENV))
	@API_URL="$$(cd terraform && terraform output -raw api_url 2>/dev/null || echo http://localhost:3000)"; \
	echo "POST $$API_URL/submit  <= $(FILE)"; \
	curl -sS -X POST "$$API_URL/submit" -H 'Content-Type: application/json' --data-binary @$(FILE) | sed -e 's/^/  /'

# --- Rychenberg fixtures (uses files already in testfiles/) ---
.PHONY: import-rychenberg
import-rychenberg:
	@$(MAKE) --no-print-directory import-story FILE=testfiles/story-rychenberg.json ENV=$(ENV)

.PHONY: submit-graph-rychenberg
submit-graph-rychenberg:
	@$(MAKE) --no-print-directory submit-graph FILE=testfiles/graph-rychenberg.json ENV=$(ENV)

# Dev-only smoke that exercises the Rychenberg files
.PHONY: smoke-rychenberg
smoke-rychenberg:
	@if [ "$(ENV)" != "dev" ]; then echo "❌ smoke-rychenberg only allowed with ENV=dev. Run: make ENV=dev smoke-rychenberg"; exit 1; fi
	@$(MAKE) --no-print-directory import-rychenberg ENV=$(ENV)
	@$(MAKE) --no-print-directory get-story-full STORY=story-rychenberg ENV=$(ENV)
	@$(MAKE) --no-print-directory submit-graph-rychenberg ENV=$(ENV)
	@$(MAKE) --no-print-directory cleanup-smoke ENV=$(ENV) STORY=story-rychenberg
	@echo "✅ Rychenberg smoke test finished (ENV=$(ENV))"

.PHONY: smoke-rychenberg-dev
smoke-rychenberg-dev: ENV=dev
smoke-rychenberg-dev: smoke-rychenberg

testdata-init:
	@set -e
	@mkdir -p testfiles
	@echo "📝 Writing testfiles/story_import.json"
	@printf '%s\n' '{"story":{"storyId":"story-demo","schoolId":"Rychenberg","title":"Demo Story (Import)"},"paragraphs":[{"index":1,"title":"Ausloeser","bodyMd":"Erster Abschnitt - warum etwas ins Rollen kam.","citations":[{"transcriptId":"Rychenberg_Evelyne","minutes":[2,5]}]},{"index":2,"title":"Umsetzung","bodyMd":"Wie das Team vorgeht.","citations":[{"transcriptId":"Rychenberg_Maja","minutes":[10]}]}],"details":[{"paragraphIndex":1,"kind":"quote","transcriptId":"Rychenberg_Evelyne","startMinute":2,"endMinute":5,"text":"Das war der Knackpunkt."},{"paragraphIndex":2,"kind":"quote","transcriptId":"Rychenberg_Maja","startMinute":10,"endMinute":11,"text":"So haben wir es geloest."}]}' > testfiles/story_import.json
	@echo "📝 Writing testfiles/submit_graph.json"
	@printf '%s\n' '{"storyId":"story-demo","nodes":[{"id":"n1","label":"Schulentwicklungsziel X","type":"schulentwicklungsziel","detail":"Pilotprojekt","color":"#111827","x":120,"y":120,"isNode":true},{"id":"n2","label":"Promotor: SL","type":"promotor","color":"#22c55e","x":120,"y":280,"isNode":true}],"edges":[{"from":"n2","to":"n1","label":"unterstuetzt","type":"supports"}]}' > testfiles/submit_graph.json
smoke: testdata-init
	@if [ "$(ENV)" != "dev" ]; then echo "❌ smoke only allowed with ENV=dev. Run: make ENV=dev smoke"; exit 1; fi
	@$(MAKE) --no-print-directory import-story FILE=testfiles/story_import.json ENV=$(ENV)
	@$(MAKE) --no-print-directory get-story-full STORY=story-demo ENV=$(ENV)
	@$(MAKE) --no-print-directory submit-graph FILE=testfiles/submit_graph.json ENV=$(ENV)
	@$(MAKE) --no-print-directory cleanup-smoke ENV=$(ENV)
	@$(MAKE) --no-print-directory clean-testfiles
	@echo "✅ Smoke test finished (ENV=$(ENV))"

.PHONY: smoke-dev
smoke-dev: ENV=dev
smoke-dev: smoke

# Remove the graph nodes created by the smoke test and verify deletion
.PHONY: cleanup-smoke
cleanup-smoke:
	@if [ "$(ENV)" != "dev" ]; then echo "❌ cleanup-smoke only allowed with ENV=dev. Run: make ENV=dev cleanup-smoke"; exit 1; fi
	$(call TF_SELECT_WORKSPACE,$(ENV))
	@API_URL="$$(cd terraform && terraform output -raw api_url 2>/dev/null || echo http://localhost:3000)"; \
	STORY_ID="$${STORY:-story-demo}"; \
	echo "🧽 Deleting test nodes via API (storyId=$$STORY_ID): n1, n2"; \
	for node in n1 n2; do \
	  code=$$(curl -sS -o /dev/null -w "%{http_code}" -X DELETE "$$API_URL/struktur/$$STORY_ID/$$node"); \
	  echo "  DEL $$node -> $$code"; \
	done; \
	echo "🗑  Purging ALL DynamoDB items for storyId=$$STORY_ID (graph + story bundle) — HARD-CODED DEV TABLE"; \
	AWS_REGION="us-east-1"; \
	TABLE_NAME="strukturbild_data_dev"; \
	echo "   Using table=$$TABLE_NAME region=$$AWS_REGION"; \
	for PK in "$$STORY_ID" "STORY#$$STORY_ID"; do \
	  echo "→ Partition: $$PK"; \
          ids=$$(aws dynamodb query --region "$$AWS_REGION" --table-name "$$TABLE_NAME" --consistent-read \
                 --key-condition-expression "storyId = :pk" \
	         --expression-attribute-values "{\":pk\":{\"S\":\"$$PK\"}}" \
	         --projection-expression "#i" \
	         --expression-attribute-names "{\"#i\":\"id\"}" \
	       | jq -r ".Items[].id.S"); \
	  for ID in $$ids; do \
	    echo "   DEL $$PK / $$ID"; \
            aws dynamodb delete-item --region "$$AWS_REGION" --table-name "$$TABLE_NAME" \
              --key "{\"storyId\":{\"S\":\"$$PK\"},\"id\":{\"S\":\"$$ID\"}}"; \
	  done; \
	done; \
	echo "🔎 Verifying DynamoDB is empty for both partitions ..."; \
	for PK in "$$STORY_ID" "STORY#$$STORY_ID"; do \
          COUNT=$$(aws dynamodb query --region "$$AWS_REGION" --table-name "$$TABLE_NAME" --consistent-read \
            --key-condition-expression "storyId = :pk" \
	    --expression-attribute-values "{\":pk\":{\"S\":\"$$PK\"}}" \
	    --select "COUNT" --query "Count" --output text); \
	  echo "   Remaining in $$PK: $$COUNT"; \
	  if [ "$$COUNT" -ne 0 ]; then echo "❌ Not fully cleaned (partition $$PK)"; exit 1; fi; \
	done; \
	echo "✅ Full cleanup complete for $$STORY_ID"

# Remove generated local test files and verify
.PHONY: clean-testfiles
clean-testfiles:
	@rm -f testfiles/story_import.json testfiles/submit_graph.json
	@test ! -f testfiles/story_import.json -a ! -f testfiles/submit_graph.json || { echo "❌ testfiles not removed"; exit 1; }
	@rmdir testfiles >/dev/null 2>&1 || true
	@echo "🧹 Removed local test files"
