package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"strings"
	"time"

	storyapi "strukturbild/api"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/google/uuid"
)

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

var svc storyapi.DynamoClient
var storySvc *storyapi.StoryService

// Resolve DynamoDB table from env (fallback to prod default)
var tableName = func() string {
	if v := os.Getenv("TABLE_NAME"); v != "" {
		return v
	}
	return "strukturbild_data"
}()

type Node struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
	Type   string `json:"type,omitempty"` // promoter|barrier|event|goal|actor|...
	Time   string `json:"time,omitempty"` // ISO date or relative (T0..Tn)
	Color  string `json:"color,omitempty"`
	X      int    `json:"x"` // X position for layout
	Y      int    `json:"y"` // Y position for layout
}

type Edge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
	Type   string `json:"type,omitempty"` // supports|blocks|causes|relates|...
}

type Strukturbild struct {
	ID                 string                       `json:"id" dynamodbav:"id"` // Add this line
	Nodes              []Node                       `json:"nodes"`
	Edges              []Edge                       `json:"edges"`
	StoryID            string                       `json:"storyId"`
	Story              *storyapi.Story              `json:"story,omitempty"`
	Paragraphs         []storyapi.Paragraph         `json:"paragraphs,omitempty"`
	DetailsByParagraph map[string][]storyapi.Detail `json:"detailsByParagraph,omitempty"`
}

type DBItem struct {
	ID        string `json:"id" dynamodbav:"id"`
	StoryID   string `json:"storyId" dynamodbav:"storyId"`
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

func getHandler(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	// Use path parameter if present (API Gateway mapping), otherwise try to extract from path
	id := ""
	if request.PathParameters != nil && request.PathParameters["id"] != "" {
		id = request.PathParameters["id"]
	} else {
		p := normalizePath(request.Path)
		if strings.HasPrefix(p, "/struktur/") {
			id = strings.TrimPrefix(p, "/struktur/")
		}
	}
	if id == "" {
		log.Printf("❌ Could not extract ID from request")
		return events.APIGatewayProxyResponse{
			StatusCode: 400,
			Headers:    corsHeaders(),
			Body:       "Missing ID",
		}, nil
	}

	// Use global svc directly

	// Scan for all items with storyId = id
	input := &dynamodb.QueryInput{
		TableName:              aws.String(tableName),
		KeyConditionExpression: aws.String("storyId = :sid"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":sid": &types.AttributeValueMemberS{Value: id},
		},
	}

	result, err := svc.Query(ctx, input)
	if err != nil {
		log.Printf("❌ Failed to query items: %v", err)
		return events.APIGatewayProxyResponse{
			StatusCode: 500,
			Headers:    corsHeaders(),
			Body:       "Failed to fetch data",
		}, nil
	}

	if len(result.Items) == 0 {
		return events.APIGatewayProxyResponse{
			StatusCode: 404,
			Headers:    corsHeaders(),
			Body:       "Not found",
		}, nil
	}

	var nodes []Node
	var edges []Edge
	for _, itemMap := range result.Items {
		var item DBItem
		err = attributevalue.UnmarshalMap(itemMap, &item)
		if err != nil {
			log.Printf("❌ Failed to unmarshal item: %v", err)
			continue
		}
		if item.IsNode {
			nodes = append(nodes, Node{
				ID:     item.ID,
				Label:  item.Label,
				Detail: item.Detail,
				Type:   item.Type,
				Time:   item.Time,
				Color:  item.Color,
				X:      item.X,
				Y:      item.Y,
			})
		} else {
			edges = append(edges, Edge{
				From:   item.From,
				To:     item.To,
				Label:  item.Label,
				Detail: item.Detail,
				Type:   item.Type,
			})
		}
	}

	sb := Strukturbild{
		ID:      "",
		Nodes:   nodes,
		Edges:   edges,
		StoryID: id,
	}

	if storySvc != nil {
		full, err := storySvc.GetFullStory(ctx, id)
		if err == nil {
			storyCopy := full.Story
			sb.Story = &storyCopy
			sb.Paragraphs = full.Paragraphs
			sb.DetailsByParagraph = full.DetailsByParagraph
		} else if !errors.Is(err, storyapi.ErrStoryNotFound) {
			log.Printf("❌ Failed to fetch story bundle for %s: %v", id, err)
		}
	}

	body, err := json.Marshal(sb)
	if err != nil {
		return events.APIGatewayProxyResponse{
			StatusCode: 500,
			Headers:    corsHeaders(),
			Body:       "Failed to encode response",
		}, nil
	}

	h := corsHeaders()
	h["Content-Type"] = "application/json"
	return events.APIGatewayProxyResponse{
		StatusCode: 200,
		Headers:    h,
		Body:       string(body),
	}, nil
}

func handler(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	var sb Strukturbild
	err := json.Unmarshal([]byte(request.Body), &sb)
	if err != nil {
		log.Printf("❌ Failed to decode JSON: %v", err)
		return events.APIGatewayProxyResponse{
			StatusCode: 400,
			Headers:    corsHeaders(),
			Body:       "Invalid JSON",
		}, nil
	}

	if sb.ID == "" {
		sb.ID = uuid.New().String()
	}

	if sb.StoryID == "" {
		log.Printf("❌ Missing storyId")
		return events.APIGatewayProxyResponse{
			StatusCode: 400,
			Headers:    corsHeaders(),
			Body:       "Missing storyId",
		}, nil
	}

	log.Printf("✅ Received strukturbild for story: %s with %d nodes", sb.StoryID, len(sb.Nodes))

	// Use global svc directly

	var dbItems []DBItem

	for i := range sb.Nodes {
		if sb.Nodes[i].ID == "" {
			sb.Nodes[i].ID = uuid.New().String()
		}
		node := sb.Nodes[i]
		dbItems = append(dbItems, DBItem{
			ID:        node.ID,
			StoryID:   sb.StoryID,
			Label:     node.Label,
			Detail:    node.Detail,
			Type:      node.Type,
			Time:      node.Time,
			Color:     node.Color,
			IsNode:    true,
			X:         node.X,
			Y:         node.Y,
			Timestamp: time.Now().Format(time.RFC3339),
		})
	}

	for _, edge := range sb.Edges {
		dbItems = append(dbItems, DBItem{
			ID:        uuid.New().String(),
			StoryID:   sb.StoryID,
			Label:     edge.Label,
			Detail:    edge.Detail,
			Type:      edge.Type,
			IsNode:    false,
			From:      edge.From,
			To:        edge.To,
			Timestamp: time.Now().Format(time.RFC3339),
		})
	}

	for _, item := range dbItems {
		av, err := attributevalue.MarshalMap(item)
		if err != nil {
			log.Printf("❌ Failed to marshal item: %v", err)
			continue
		}

		input := &dynamodb.PutItemInput{
			TableName: aws.String(tableName),
			Item:      av,
		}

		_, err = svc.PutItem(ctx, input)
		if err != nil {
			log.Printf("❌ Failed to put item in DynamoDB: %v", err)
		}
	}

	log.Printf("✅ Saved to DynamoDB successfully")

	return events.APIGatewayProxyResponse{
		StatusCode: 200,
		Headers:    corsHeaders(),
		Body:       "Strukturbild received successfully",
	}, nil
}

func initializeDynamoDB(ctx context.Context) *dynamodb.Client {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("Failed to load AWS config: %v", err)
	}
	log.Println("✅ DynamoDB client initialized.")
	return dynamodb.NewFromConfig(cfg)
}

func runLambda() {
	lambda.Start(lambdaHandler)
}

func lambdaHandler(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	method := req.HTTPMethod
	path := req.Path
	npath := normalizePath(path)
	log.Printf("🪵 Method: %s, Path: %s", method, path)

	if method == "OPTIONS" {
		return events.APIGatewayProxyResponse{
			StatusCode: 200,
			Headers:    corsHeaders(),
			Body:       "",
		}, nil
	}

	switch {
	case method == "POST" && npath == "/submit":
		return handler(ctx, req)
	case method == "GET" && strings.HasPrefix(npath, "/struktur/"):
		return getHandler(ctx, req)
	case method == "DELETE" && strings.HasPrefix(npath, "/struktur/"):
		parts := strings.Split(strings.TrimPrefix(npath, "/struktur/"), "/")
		if len(parts) == 2 {
			req.PathParameters = map[string]string{
				"storyId": parts[0],
				"nodeId":  parts[1],
			}
			return deleteHandler(ctx, req)
		}
		return events.APIGatewayProxyResponse{
			StatusCode: 400,
			Headers:    corsHeaders(),
			Body:       "Invalid path for DELETE",
		}, nil
	case strings.HasPrefix(npath, "/api/"):
		return handleStoryRoutes(ctx, req, method, npath)
	default:
		return events.APIGatewayProxyResponse{
			StatusCode: 404,
			Headers:    corsHeaders(),
			Body:       "Not Found",
		}, nil
	}
}

func deleteHandler(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	storyId := request.PathParameters["storyId"]
	nodeId := request.PathParameters["nodeId"]

	if storyId == "" || nodeId == "" {
		return events.APIGatewayProxyResponse{
			StatusCode: 400,
			Headers:    corsHeaders(),
			Body:       "Missing storyId or nodeId",
		}, nil
	}

	input := &dynamodb.DeleteItemInput{
		TableName: aws.String(tableName),
		Key: map[string]types.AttributeValue{
			"storyId": &types.AttributeValueMemberS{Value: storyId},
			"id":      &types.AttributeValueMemberS{Value: nodeId},
		},
	}

	_, err := svc.DeleteItem(ctx, input)
	if err != nil {
		log.Printf("❌ Failed to delete item: %v", err)
		return events.APIGatewayProxyResponse{
			StatusCode: 500,
			Headers:    corsHeaders(),
			Body:       "Failed to delete item",
		}, nil
	}

	log.Printf("✅ Deleted item with storyId: %s, nodeId: %s", storyId, nodeId)

	return events.APIGatewayProxyResponse{
		StatusCode: 200,
		Headers:    corsHeaders(),
		Body:       "Item deleted successfully",
	}, nil
}

func handleStoryRoutes(ctx context.Context, req events.APIGatewayProxyRequest, method, path string) (events.APIGatewayProxyResponse, error) {
	if storySvc == nil {
		return events.APIGatewayProxyResponse{StatusCode: 500, Headers: corsHeaders(), Body: "Story service not initialised"}, nil
	}
	normalized := normalizePath(path)
	trimmed := strings.TrimPrefix(normalized, "/api/")
	parts := strings.Split(trimmed, "/")
	switch {
	case method == "POST" && trimmed == "stories":
		return storySvc.HandleCreateStory(ctx, req)
	case method == "POST" && trimmed == "stories/import":
		return storySvc.HandleImportStory(ctx, req)
	case method == "PATCH" && len(parts) == 2 && parts[0] == "stories":
		storyID := parts[1]
		req.PathParameters = map[string]string{"storyId": storyID}
		return storySvc.HandleUpdateStory(ctx, req)
	case method == "GET" && trimmed == "stories":
		return storySvc.HandleListStories(ctx, req)
	case method == "POST" && len(parts) == 3 && parts[0] == "stories" && parts[2] == "paragraphs":
		storyID := parts[1]
		req.PathParameters = map[string]string{"storyId": storyID}
		return storySvc.HandleCreateParagraph(ctx, req)
	case method == "GET" && len(parts) == 3 && parts[0] == "stories" && parts[2] == "full":
		storyID := parts[1]
		req.PathParameters = map[string]string{"storyId": storyID}
		return storySvc.HandleGetFullStory(ctx, req)
	case method == "PATCH" && len(parts) == 2 && parts[0] == "paragraphs":
		paragraphID := parts[1]
		req.PathParameters = map[string]string{"paragraphId": paragraphID}
		return storySvc.HandleUpdateParagraph(ctx, req)
	case method == "POST" && len(parts) == 3 && parts[0] == "paragraphs" && parts[2] == "details":
		paragraphID := parts[1]
		req.PathParameters = map[string]string{"paragraphId": paragraphID}
		return storySvc.HandleCreateDetail(ctx, req)
	default:
		return events.APIGatewayProxyResponse{StatusCode: 404, Headers: corsHeaders(), Body: "Not Found"}, nil
	}
}

func corsHeaders() map[string]string {
	return map[string]string{
		"Access-Control-Allow-Origin":      "*",
		"Access-Control-Allow-Headers":     "Content-Type, Authorization, X-Requested-With, X-Amz-Date, X-Api-Key, X-Amz-Security-Token",
		"Access-Control-Allow-Methods":     "OPTIONS,GET,POST,DELETE,PATCH",
		"Access-Control-Allow-Credentials": "true",
		"Access-Control-Max-Age":           "86400",
	}
}

func main() {
	svc = initializeDynamoDB(context.TODO())
	log.Printf("✅ Using DynamoDB table: %s", tableName)
	storySvc = storyapi.NewStoryService(svc, tableName, corsHeaders)

	runLambda()
}
