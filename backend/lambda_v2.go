package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/google/uuid"
)

// --------- shared config (TABLE_NAME & dynamo client) ----------

var (
	// fallback single-table model; we’ll handle DDB_STORIES_TABLE later if you want that layout
	tableName = func() string {
		if v := os.Getenv("TABLE_NAME"); v != "" {
			return v
		}
		return "strukturbild_data"
	}()

	svc      DynamoClient
	storySvc *StoryService
)

func init() {
	svc = initializeDynamoDB(context.TODO())
	log.Printf("✅ Using DynamoDB table: %s", tableName)
	// StoryService uses a single tableName too
	storySvc = NewStoryService(svc, tableName, corsHeaders)
}

// ---------- types used by /submit + /struktur/* ----------

type Node struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Detail   string `json:"detail,omitempty"`
	Type     string `json:"type,omitempty"`
	Time     string `json:"time,omitempty"`
	Color    string `json:"color,omitempty"`
	X        int    `json:"x"`
	Y        int    `json:"y"`
	PersonID string `json:"personId"`
}
type Edge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
	Type   string `json:"type,omitempty"`
}
type Strukturbild struct {
	ID       string `json:"id" dynamodbav:"id"`
	Nodes    []Node `json:"nodes"`
	Edges    []Edge `json:"edges"`
	PersonID string `json:"personId"`
}
type DBItem struct {
	ID        string `json:"id" dynamodbav:"id"`
	PersonID  string `json:"personId" dynamodbav:"personId"`
	Label     string `json:"label" dynamodbav:"label"`
	Detail    string `json:"detail,omitempty" dynamodbav:"detail,omitempty"`
	Type      string `json:"type,omitempty" dynamodbav:"type,omitempty"`
	Time      string `json:"time,omitempty" dynamodbav:"time,omitempty"`
	Color     string `json:"color,omitempty" dynamodbav:"color,omitempty"`
	IsNode    bool   `json:"isNode" dynamodbav:"isNode"`
	X         int    `json:"x,omitempty" dynamodbav:"x,omitempty"`
	Y         int    `json:"y,omitempty" dynamodbav:"y,omitempty"`
	From      string `json:"from,omitempty" dynamodbav:"from,omitempty"`
	To        string `json:"to,omitempty" dynamodbav:"to,omitempty"`
	Timestamp string `json:"timestamp" dynamodbav:"timestamp"`
}

// ---------- core: Lambda HTTP API v2 handler (Pattern A) ----------

func lambdaHandler(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	method := req.RequestContext.HTTP.Method
	path := req.RawPath
	if path == "" {
		path = req.RequestContext.HTTP.Path
	}
	npath := normalizePath(path)
	log.Printf("🪵 v2 method=%s raw=%s norm=%s", method, req.RawPath, npath)

	// CORS preflight
	if method == http.MethodOptions {
		return events.APIGatewayV2HTTPResponse{StatusCode: 200, Headers: corsHeaders(), Body: ""}, nil
	}

	// lightweight health
	if method == http.MethodGet && (npath == "/api/health" || npath == "/health") {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: 200,
			Headers:    corsHeaders(),
			Body:       `{"ok":"true"}`,
		}, nil
	}

	// -------- net-new graph endpoints in Lambda (no adapter) --------

	// POST /submit  (save a Strukturbild)
	if method == http.MethodPost && npath == "/submit" {
		var sb Strukturbild
		if err := json.Unmarshal([]byte(req.Body), &sb); err != nil {
			return badRequest("Invalid JSON")
		}
		if strings.TrimSpace(sb.PersonID) == "" {
			return badRequest("Missing personId")
		}
		if strings.TrimSpace(sb.ID) == "" {
			sb.ID = uuid.New().String()
		}

		var puts []DBItem
		now := time.Now().Format(time.RFC3339)

		for i := range sb.Nodes {
			if sb.Nodes[i].ID == "" {
				sb.Nodes[i].ID = uuid.New().String()
			}
			n := sb.Nodes[i]
			puts = append(puts, DBItem{
				ID:        n.ID,
				PersonID:  n.PersonID,
				Label:     n.Label,
				Detail:    n.Detail,
				Type:      n.Type,
				Time:      n.Time,
				Color:     n.Color,
				IsNode:    true,
				X:         n.X,
				Y:         n.Y,
				Timestamp: now,
			})
		}
		for _, e := range sb.Edges {
			puts = append(puts, DBItem{
				ID:        uuid.New().String(),
				PersonID:  sb.PersonID,
				Label:     e.Label,
				Detail:    e.Detail,
				Type:      e.Type,
				IsNode:    false,
				From:      e.From,
				To:        e.To,
				Timestamp: now,
			})
		}
		for _, item := range puts {
			av, err := attributevalue.MarshalMap(item)
			if err != nil {
				log.Printf("marshal error: %v", err)
				continue
			}
			_, err = svc.PutItem(ctx, &dynamodb.PutItemInput{
				TableName: aws.String(tableName),
				Item:      av,
			})
			if err != nil {
				log.Printf("put error: %v", err)
			}
		}
		return okJSON(map[string]string{"status": "ok", "personId": sb.PersonID})
	}

	// GET /struktur/{id}
	if method == http.MethodGet && strings.HasPrefix(npath, "/struktur/") {
		id := strings.TrimPrefix(npath, "/struktur/")
		if id == "" {
			return badRequest("Missing id")
		}
		qi := &dynamodb.QueryInput{
			TableName:              aws.String(tableName),
			KeyConditionExpression: awsString("personId = :pid"),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":pid": &types.AttributeValueMemberS{Value: id},
			},
		}
		res, err := svc.Query(ctx, qi)
		if err != nil {
			return serverError("Failed to fetch data")
		}
		if len(res.Items) == 0 {
			return notFound("Not found")
		}
		var nodes []Node
		var edges []Edge
		for _, m := range res.Items {
			var it DBItem
			if err := attributevalue.UnmarshalMap(m, &it); err != nil {
				continue
			}
			if it.IsNode {
				nodes = append(nodes, Node{
					ID:       it.ID,
					Label:    it.Label,
					Detail:   it.Detail,
					Type:     it.Type,
					Time:     it.Time,
					Color:    it.Color,
					X:        it.X,
					Y:        it.Y,
					PersonID: it.PersonID,
				})
			} else {
				edges = append(edges, Edge{
					From:   it.From,
					To:     it.To,
					Label:  it.Label,
					Detail: it.Detail,
					Type:   it.Type,
				})
			}
		}
		body, _ := json.Marshal(Strukturbild{PersonID: id, Nodes: nodes, Edges: edges})
		return events.APIGatewayV2HTTPResponse{
			StatusCode: 200,
			Headers:    jsonHeaders(),
			Body:       string(body),
		}, nil
	}

	// DELETE /struktur/{personId}/{nodeId}
	if method == http.MethodDelete && strings.HasPrefix(npath, "/struktur/") {
		parts := strings.Split(strings.TrimPrefix(npath, "/struktur/"), "/")
		if len(parts) == 2 {
			personID, nodeID := parts[0], parts[1]
			_, err := svc.DeleteItem(ctx, &dynamodb.DeleteItemInput{
				TableName: aws.String(tableName),
				Key: map[string]types.AttributeValue{
					"personId": &types.AttributeValueMemberS{Value: personID},
					"id":       &types.AttributeValueMemberS{Value: nodeID},
				},
			})
			if err != nil {
				return serverError("Failed to delete item")
			}
			return okText("deleted")
		}
		return badRequest("Invalid path for DELETE")
	}

	// -------- Story API under /api/* (v2 events, no adapter) --------

	if !strings.HasPrefix(npath, "/api/") {
		return notFound("Not Found")
	}
	if storySvc == nil {
		return serverError("Story service not initialised")
	}

	trimmed := strings.TrimPrefix(npath, "/api/")
	parts := strings.Split(trimmed, "/")

	switch {
	case method == http.MethodPost && trimmed == "stories":
		return storySvc.HandleCreateStory(ctx, req)

	case method == http.MethodPost && trimmed == "stories/import":
		return storySvc.HandleImportStory(ctx, req)

	case method == http.MethodPost && len(parts) == 3 && parts[0] == "stories" && parts[2] == "paragraphs":
		// inject storyId into path params (since route is /api/{proxy+})
		req.PathParameters = map[string]string{"storyId": parts[1]}
		return storySvc.HandleCreateParagraph(ctx, req)

	case method == http.MethodGet && len(parts) == 3 && parts[0] == "stories" && parts[2] == "full":
		req.PathParameters = map[string]string{"storyId": parts[1]}
		return storySvc.HandleGetFullStory(ctx, req)

	case method == http.MethodPatch && len(parts) == 2 && parts[0] == "paragraphs":
		req.PathParameters = map[string]string{"paragraphId": parts[1]}
		return storySvc.HandleUpdateParagraph(ctx, req)

	case method == http.MethodPost && len(parts) == 3 && parts[0] == "paragraphs" && parts[2] == "details":
		req.PathParameters = map[string]string{"paragraphId": parts[1]}
		return storySvc.HandleCreateDetail(ctx, req)

	default:
		return notFound("Not Found")
	}
}

// -------- helpers (v2 only; no v1→v2 shims) --------

func corsHeaders() map[string]string {
	return map[string]string{
		"Access-Control-Allow-Origin":      "*",
		"Access-Control-Allow-Headers":     "Content-Type, Authorization, X-Requested-With, X-Amz-Date, X-Api-Key, X-Amz-Security-Token",
		"Access-Control-Allow-Methods":     "OPTIONS,GET,POST,DELETE,PATCH",
		"Access-Control-Allow-Credentials": "true",
		"Access-Control-Max-Age":           "86400",
	}
}
func jsonHeaders() map[string]string {
	h := corsHeaders()
	h["Content-Type"] = "application/json"
	return h
}
func okJSON(v any) (events.APIGatewayV2HTTPResponse, error) {
	b, _ := json.Marshal(v)
	return events.APIGatewayV2HTTPResponse{StatusCode: 200, Headers: jsonHeaders(), Body: string(b)}, nil
}
func okText(msg string) (events.APIGatewayV2HTTPResponse, error) {
	return events.APIGatewayV2HTTPResponse{StatusCode: 200, Headers: corsHeaders(), Body: msg}, nil
}
func badRequest(msg string) (events.APIGatewayV2HTTPResponse, error) {
	return events.APIGatewayV2HTTPResponse{StatusCode: 400, Headers: corsHeaders(), Body: msg}, nil
}
func notFound(msg string) (events.APIGatewayV2HTTPResponse, error) {
	return events.APIGatewayV2HTTPResponse{StatusCode: 404, Headers: corsHeaders(), Body: msg}, nil
}
func serverError(msg string) (events.APIGatewayV2HTTPResponse, error) {
	return events.APIGatewayV2HTTPResponse{StatusCode: 500, Headers: corsHeaders(), Body: msg}, nil
}

func normalizePath(p string) string {
	if idx := strings.Index(p, "/struktur/"); idx >= 0 {
		return p[idx:]
	}
	if idx := strings.Index(p, "/submit"); idx >= 0 {
		return p[idx:]
	}
	if idx := strings.Index(p, "/api/"); idx >= 0 {
		return p[idx:]
	}
	return p
}

func initializeDynamoDB(ctx context.Context) *dynamodb.Client {
	var cfg aws.Config
	var err error

	if os.Getenv("LOCAL") == "true" {
		cfg, err = config.LoadDefaultConfig(ctx,
			config.WithRegion("us-east-1"),
			config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
				"fakeMyKeyId", "fakeSecretAccessKey", "fakeToken",
			)),
			config.WithEndpointResolver(aws.EndpointResolverFunc(
				func(service, region string) (aws.Endpoint, error) {
					if service == dynamodb.ServiceID {
						return aws.Endpoint{
							URL:           "http://localhost:8000",
							SigningRegion: "us-east-1",
						}, nil
					}
					return aws.Endpoint{}, &aws.EndpointNotFoundError{}
				},
			)),
		)
	} else {
		cfg, err = config.LoadDefaultConfig(ctx)
	}
	if err != nil {
		log.Fatalf("Failed to load AWS config: %v", err)
	}
	return dynamodb.NewFromConfig(cfg)
}

// Wire the Lambda runtime to our v2 handler.
func startLambdaV2() {
	lambda.Start(lambdaHandler)
}
