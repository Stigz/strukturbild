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

func normalizePath(p string) string {
	if idx := strings.Index(p, "/struktur/"); idx >= 0 {
		return p[idx:]
	}
	if idx := strings.Index(p, "/submit"); idx >= 0 {
		return p[idx:]
	}
	return p
}

var svc *dynamodb.Client

type Node struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Detail   string `json:"detail,omitempty"`
	Type     string `json:"type,omitempty"` // promoter|barrier|event|goal|actor|...
	Time     string `json:"time,omitempty"` // ISO date or relative (T0..Tn)
	Color    string `json:"color,omitempty"`
	X        int    `json:"x"` // X position for layout
	Y        int    `json:"y"` // Y position for layout
	PersonID string `json:"personId"`
}

type Edge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
	Type   string `json:"type,omitempty"` // supports|blocks|causes|relates|...
}

type Strukturbild struct {
	ID       string `json:"id" dynamodbav:"id"` // Add this line
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

	// Scan for all items with personId = id
	input := &dynamodb.QueryInput{
		TableName:              aws.String("strukturbild_data"),
		KeyConditionExpression: aws.String("personId = :pid"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pid": &types.AttributeValueMemberS{Value: id},
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
				ID:       item.ID,
				Label:    item.Label,
				Detail:   item.Detail,
				Type:     item.Type,
				Time:     item.Time,
				Color:    item.Color,
				X:        item.X,
				Y:        item.Y,
				PersonID: item.PersonID,
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
		ID:       "",
		Nodes:    nodes,
		Edges:    edges,
		PersonID: id,
	}

	body, err := json.Marshal(sb)
	if err != nil {
		return events.APIGatewayProxyResponse{
			StatusCode: 500,
			Headers:    corsHeaders(),
			Body:       "Failed to encode response",
		}, nil
	}

	return events.APIGatewayProxyResponse{
		StatusCode: 200,
		Headers: map[string]string{
			"Content-Type":                     "application/json",
			"Access-Control-Allow-Origin":      "*",
			"Access-Control-Allow-Headers":     "Content-Type",
			"Access-Control-Allow-Methods":     "OPTIONS,GET,POST",
			"Access-Control-Allow-Credentials": "true",
			"Access-Control-Max-Age":           "86400",
		},
		Body: string(body),
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

	if sb.PersonID == "" {
		log.Printf("❌ Missing personId")
		return events.APIGatewayProxyResponse{
			StatusCode: 400,
			Headers:    corsHeaders(),
			Body:       "Missing personId",
		}, nil
	}

	log.Printf("✅ Received strukturbild for person: %s with %d nodes", sb.PersonID, len(sb.Nodes))

	// Use global svc directly

	var dbItems []DBItem

	for i := range sb.Nodes {
		if sb.Nodes[i].ID == "" {
			sb.Nodes[i].ID = uuid.New().String()
		}
		node := sb.Nodes[i]
		dbItems = append(dbItems, DBItem{
			ID:        node.ID,
			PersonID:  node.PersonID,
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
			PersonID:  sb.PersonID,
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
			TableName: aws.String("strukturbild_data"),
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

func handlerHTTP(w http.ResponseWriter, r *http.Request) {
	for k, v := range corsHeaders() {
		w.Header().Set(k, v)
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	var body map[string]interface{}
	json.NewDecoder(r.Body).Decode(&body)
	jsonBytes, _ := json.Marshal(body)

	resp, _ := handler(context.Background(), events.APIGatewayProxyRequest{
		HTTPMethod: "POST",
		Path:       "/submit",
		Body:       string(jsonBytes),
	})
	w.WriteHeader(resp.StatusCode)
	w.Write([]byte(resp.Body))
}

func getHandlerHTTP(w http.ResponseWriter, r *http.Request) {
	for k, v := range corsHeaders() {
		w.Header().Set(k, v)
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method == http.MethodDelete {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/struktur/"), "/")
		if len(parts) == 2 {
			resp, _ := deleteHandler(context.Background(), events.APIGatewayProxyRequest{
				HTTPMethod:     "DELETE",
				PathParameters: map[string]string{"personId": parts[0], "nodeId": parts[1]},
			})
			w.WriteHeader(resp.StatusCode)
			w.Write([]byte(resp.Body))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Invalid path for DELETE"))
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/struktur/")
	resp, _ := getHandler(context.Background(), events.APIGatewayProxyRequest{
		HTTPMethod:     "GET",
		Path:           "/struktur/" + id,
		PathParameters: map[string]string{"id": id},
	})
	w.WriteHeader(resp.StatusCode)
	w.Write([]byte(resp.Body))
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
	log.Println("✅ DynamoDB client initialized.")
	return dynamodb.NewFromConfig(cfg)
}

func runHTTPServer() {
	log.Println("✅ Running HTTP server on :3000 (LOCAL)")
	http.HandleFunc("/submit", handlerHTTP)
	http.HandleFunc("/struktur/", getHandlerHTTP)
	log.Fatal(http.ListenAndServe(":3000", nil))
}

func runLambda() {
	lambda.Start(lambdaHandler)
}

func lambdaHandler(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	method := req.HTTPMethod
	path := req.Path
	npath := normalizePath(path)
	log.Printf("🪵 Method: %s, Path: %s", method, path)

	switch {
	case method == "POST" && npath == "/submit":
		return handler(ctx, req)
	case method == "GET" && strings.HasPrefix(npath, "/struktur/"):
		return getHandler(ctx, req)
	case method == "DELETE" && strings.HasPrefix(npath, "/struktur/"):
		parts := strings.Split(strings.TrimPrefix(npath, "/struktur/"), "/")
		if len(parts) == 2 {
			req.PathParameters = map[string]string{
				"personId": parts[0],
				"nodeId":   parts[1],
			}
			return deleteHandler(ctx, req)
		}
		return events.APIGatewayProxyResponse{
			StatusCode: 400,
			Headers:    corsHeaders(),
			Body:       "Invalid path for DELETE",
		}, nil
	case method == "OPTIONS":
		return events.APIGatewayProxyResponse{
			StatusCode: 200,
			Headers:    corsHeaders(),
			Body:       "",
		}, nil
	default:
		return events.APIGatewayProxyResponse{
			StatusCode: 404,
			Headers:    corsHeaders(),
			Body:       "Not Found",
		}, nil
	}
}

func deleteHandler(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	personId := request.PathParameters["personId"]
	nodeId := request.PathParameters["nodeId"]

	if personId == "" || nodeId == "" {
		return events.APIGatewayProxyResponse{
			StatusCode: 400,
			Headers:    corsHeaders(),
			Body:       "Missing personId or nodeId",
		}, nil
	}

	input := &dynamodb.DeleteItemInput{
		TableName: aws.String("strukturbild_data"),
		Key: map[string]types.AttributeValue{
			"personId": &types.AttributeValueMemberS{Value: personId},
			"id":       &types.AttributeValueMemberS{Value: nodeId},
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

	log.Printf("✅ Deleted item with personId: %s, nodeId: %s", personId, nodeId)

	return events.APIGatewayProxyResponse{
		StatusCode: 200,
		Headers:    corsHeaders(),
		Body:       "Item deleted successfully",
	}, nil
}

func corsHeaders() map[string]string {
	return map[string]string{
		"Access-Control-Allow-Origin":      "*",
		"Access-Control-Allow-Headers":     "Content-Type, Authorization, X-Requested-With, X-Amz-Date, X-Api-Key, X-Amz-Security-Token",
		"Access-Control-Allow-Methods":     "OPTIONS,GET,POST,DELETE",
		"Access-Control-Allow-Credentials": "true",
		"Access-Control-Max-Age":           "86400",
	}
}

func main() {
	svc = initializeDynamoDB(context.TODO())

	if os.Getenv("LOCAL") == "true" {
		runHTTPServer()
	} else {
		runLambda()
	}
}
