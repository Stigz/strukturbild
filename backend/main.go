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

var svc *dynamodb.Client

type Node struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	X        int    `json:"x"` // X position for layout
	Y        int    `json:"y"` // Y position for layout
	PersonID string `json:"personId"`
}

type Edge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Label string `json:"label"`
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
	} else if strings.HasPrefix(request.Path, "/struktur/") {
		id = strings.TrimPrefix(request.Path, "/struktur/")
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
				X:        item.X,
				Y:        item.Y,
				PersonID: item.PersonID,
			})
		} else {
			edges = append(edges, Edge{
				From:  item.From,
				To:    item.To,
				Label: item.Label,
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
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "OPTIONS,GET,POST")
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
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "OPTIONS,GET,POST")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
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
	log.Printf("🪵 Method: %s, Path: %s", method, path)

	switch {
	case method == "POST" && path == "/submit":
		return handler(ctx, req)
	case method == "GET" && strings.HasPrefix(path, "/struktur/"):
		return getHandler(ctx, req)
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

func corsHeaders() map[string]string {
	return map[string]string{
		"Access-Control-Allow-Origin":      "*",
		"Access-Control-Allow-Headers":     "Content-Type",
		"Access-Control-Allow-Methods":     "OPTIONS,GET,POST",
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
