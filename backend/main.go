// This is a basic Go backend for serving Strukturbild data via a REST API.
// It integrates with AWS (S3 for storage) and can be deployed cheaply using AWS Lambda + API Gateway + Terraform.

package main

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/google/uuid"
)

type Node struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	X     int    `json:"x"`
	Y     int    `json:"y"`
}

type Edge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Label string `json:"label"`
}

type Strukturbild struct {
	ID    string `json:"id" dynamodbav:"id"` // Add this line
	Title string `json:"title"`
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}
type DBItem struct {
	ID        string `json:"id" dynamodbav:"id"`
	Title     string `json:"title"`
	Timestamp string `json:"timestamp"`
	Nodes     []Node `json:"nodes"`
	Edges     []Edge `json:"edges"`
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
			Headers: map[string]string{
				"Access-Control-Allow-Origin":      "*",
				"Access-Control-Allow-Headers":     "Content-Type",
				"Access-Control-Allow-Methods":     "OPTIONS,GET,POST",
				"Access-Control-Allow-Credentials": "true",
				"Access-Control-Max-Age":           "86400",
			},
			Body: "Missing ID",
		}, nil
	}

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion("us-east-1"))
	if err != nil {
		log.Printf("❌ Failed to load AWS config: %v", err)
		return events.APIGatewayProxyResponse{
			StatusCode: 500,
			Headers: map[string]string{
				"Access-Control-Allow-Origin":      "*",
				"Access-Control-Allow-Headers":     "Content-Type",
				"Access-Control-Allow-Methods":     "OPTIONS,GET,POST",
				"Access-Control-Allow-Credentials": "true",
				"Access-Control-Max-Age":           "86400",
			},
			Body: "Server config error",
		}, nil
	}
	svc := dynamodb.NewFromConfig(cfg)

	key, err := attributevalue.MarshalMap(map[string]string{"id": id})
	if err != nil {
		log.Printf("❌ Failed to marshal key: %v", err)
		return events.APIGatewayProxyResponse{
			StatusCode: 500,
			Headers: map[string]string{
				"Access-Control-Allow-Origin":      "*",
				"Access-Control-Allow-Headers":     "Content-Type",
				"Access-Control-Allow-Methods":     "OPTIONS,GET,POST",
				"Access-Control-Allow-Credentials": "true",
				"Access-Control-Max-Age":           "86400",
			},
			Body: "Failed to marshal key",
		}, nil
	}

	input := &dynamodb.GetItemInput{
		TableName: aws.String("strukturbild_data"),
		Key:       key,
	}

	result, err := svc.GetItem(ctx, input)
	if err != nil {
		log.Printf("❌ Failed to get item: %v", err)
		return events.APIGatewayProxyResponse{
			StatusCode: 500,
			Headers: map[string]string{
				"Access-Control-Allow-Origin":      "*",
				"Access-Control-Allow-Headers":     "Content-Type",
				"Access-Control-Allow-Methods":     "OPTIONS,GET,POST",
				"Access-Control-Allow-Credentials": "true",
				"Access-Control-Max-Age":           "86400",
			},
			Body: "Failed to fetch data",
		}, nil
	}

	if result.Item == nil {
		return events.APIGatewayProxyResponse{
			StatusCode: 404,
			Headers: map[string]string{
				"Access-Control-Allow-Origin":      "*",
				"Access-Control-Allow-Headers":     "Content-Type",
				"Access-Control-Allow-Methods":     "OPTIONS,GET,POST",
				"Access-Control-Allow-Credentials": "true",
				"Access-Control-Max-Age":           "86400",
			},
			Body: "Not found",
		}, nil
	}

	var item DBItem
	err = attributevalue.UnmarshalMap(result.Item, &item)
	if err != nil {
		log.Printf("❌ Failed to unmarshal item: %v", err)
		return events.APIGatewayProxyResponse{
			StatusCode: 500,
			Headers: map[string]string{
				"Access-Control-Allow-Origin":      "*",
				"Access-Control-Allow-Headers":     "Content-Type",
				"Access-Control-Allow-Methods":     "OPTIONS,GET,POST",
				"Access-Control-Allow-Credentials": "true",
				"Access-Control-Max-Age":           "86400",
			},
			Body: "Failed to decode data",
		}, nil
	}

	body, err := json.Marshal(item)
	if err != nil {
		return events.APIGatewayProxyResponse{
			StatusCode: 500,
			Headers: map[string]string{
				"Access-Control-Allow-Origin":      "*",
				"Access-Control-Allow-Headers":     "Content-Type",
				"Access-Control-Allow-Methods":     "OPTIONS,GET,POST",
				"Access-Control-Allow-Credentials": "true",
				"Access-Control-Max-Age":           "86400",
			},
			Body: "Failed to encode response",
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
			Headers: map[string]string{
				"Access-Control-Allow-Origin":      "*",
				"Access-Control-Allow-Headers":     "Content-Type",
				"Access-Control-Allow-Methods":     "OPTIONS,GET,POST",
				"Access-Control-Allow-Credentials": "true",
				"Access-Control-Max-Age":           "86400",
			},
			Body: "Invalid JSON",
		}, nil
	}

	if sb.ID == "" {
		sb.ID = uuid.New().String()
	}

	log.Printf("✅ Received strukturbild title: %s with %d nodes", sb.Title, len(sb.Nodes))

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion("us-east-1"))
	if err != nil {
		log.Printf("❌ Failed to load AWS config: %v", err)
		return events.APIGatewayProxyResponse{
			StatusCode: 500,
			Headers: map[string]string{
				"Access-Control-Allow-Origin":      "*",
				"Access-Control-Allow-Headers":     "Content-Type",
				"Access-Control-Allow-Methods":     "OPTIONS,GET,POST",
				"Access-Control-Allow-Credentials": "true",
				"Access-Control-Max-Age":           "86400",
			},
			Body: "Server config error",
		}, nil
	}
	svc := dynamodb.NewFromConfig(cfg)

	item := DBItem{
		ID:        sb.ID,
		Title:     sb.Title,
		Timestamp: time.Now().Format(time.RFC3339),
		Nodes:     sb.Nodes,
		Edges:     sb.Edges,
	}

	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		log.Printf("❌ Failed to marshal item: %v", err)
		return events.APIGatewayProxyResponse{
			StatusCode: 500,
			Headers: map[string]string{
				"Access-Control-Allow-Origin":      "*",
				"Access-Control-Allow-Headers":     "Content-Type",
				"Access-Control-Allow-Methods":     "OPTIONS,GET,POST",
				"Access-Control-Allow-Credentials": "true",
				"Access-Control-Max-Age":           "86400",
			},
			Body: "Server error",
		}, nil
	}

	input := &dynamodb.PutItemInput{
		TableName: aws.String("strukturbild_data"),
		Item:      av,
	}

	_, err = svc.PutItem(ctx, input)
	if err != nil {
		log.Printf("❌ Failed to put item in DynamoDB: %v", err)
		return events.APIGatewayProxyResponse{
			StatusCode: 500,
			Headers: map[string]string{
				"Access-Control-Allow-Origin":      "*",
				"Access-Control-Allow-Headers":     "Content-Type",
				"Access-Control-Allow-Methods":     "OPTIONS,GET,POST",
				"Access-Control-Allow-Credentials": "true",
				"Access-Control-Max-Age":           "86400",
			},
			Body: "Failed to save data",
		}, nil
	}

	log.Printf("✅ Saved to DynamoDB successfully")

	return events.APIGatewayProxyResponse{
		StatusCode: 200,
		Headers: map[string]string{
			"Access-Control-Allow-Origin":      "*",
			"Access-Control-Allow-Headers":     "Content-Type",
			"Access-Control-Allow-Methods":     "OPTIONS,GET,POST",
			"Access-Control-Allow-Credentials": "true",
			"Access-Control-Max-Age":           "86400",
		},
		Body: "Strukturbild received successfully",
	}, nil
}

func main() {
	lambda.Start(func(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
		method := req.HTTPMethod
		path := req.Path
		log.Printf("🪵 Method: %s, Path: %s", method, path)

		switch {
		case method == "POST" && path == "/submit":
			return handler(ctx, req)
		case method == "GET" && strings.HasPrefix(path, "/struktur/"):
			if req.PathParameters == nil || req.PathParameters["id"] == "" {
				log.Printf("❌ Missing ID in path parameters")
				return events.APIGatewayProxyResponse{
					StatusCode: 400,
					Headers: map[string]string{
						"Access-Control-Allow-Origin":      "*",
						"Access-Control-Allow-Headers":     "Content-Type",
						"Access-Control-Allow-Methods":     "OPTIONS,GET,POST",
						"Access-Control-Allow-Credentials": "true",
						"Access-Control-Max-Age":           "86400",
					},
					Body: "Missing ID",
				}, nil
			}
			return getHandler(ctx, req)
		case method == "OPTIONS":
			return events.APIGatewayProxyResponse{
				StatusCode: 200,
				Headers: map[string]string{
					"Access-Control-Allow-Origin":      "*",
					"Access-Control-Allow-Methods":     "OPTIONS,GET,POST",
					"Access-Control-Allow-Headers":     "Content-Type",
					"Access-Control-Allow-Credentials": "true",
					"Access-Control-Max-Age":           "86400",
				},
				Body: "",
			}, nil
		default:
			log.Printf("⚠️ Unexpected method/path: %s %s", method, path)
			return events.APIGatewayProxyResponse{
				StatusCode: 404,
				Headers: map[string]string{
					"Access-Control-Allow-Origin":      "*",
					"Access-Control-Allow-Headers":     "Content-Type",
					"Access-Control-Allow-Methods":     "OPTIONS,GET,POST",
					"Access-Control-Allow-Credentials": "true",
					"Access-Control-Max-Age":           "86400",
				},
				Body: "Not Found",
			}, nil
		}
	})
}
