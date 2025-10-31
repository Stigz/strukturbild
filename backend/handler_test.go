package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/aws/aws-lambda-go/events"
)

func TestHandler(t *testing.T) {
	setupTestServices()
	testPayload := Strukturbild{
		ID:       "test123",
		PersonID: "testperson",
		Nodes: []Node{{
			ID:       "1",
			Label:    "A",
			X:        0,
			Y:        0,
			PersonID: "testperson",
		}},
		Edges: []Edge{{From: "1", To: "1", Label: "loop"}},
	}

	body, _ := json.Marshal(testPayload)
	request := events.APIGatewayProxyRequest{
		HTTPMethod: "POST",
		Path:       "/submit",
		Body:       string(body),
	}

	resp, err := handler(context.Background(), request)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("Handler failed: %v, response: %+v", err, resp)
	}
}

func TestGetHandler(t *testing.T) {
	setupTestServices()
	testID := "gettest123"
	testPayload := Strukturbild{
		ID:       testID,
		PersonID: testID,
		Nodes: []Node{{
			ID:       "1",
			Label:    "Node1",
			X:        10,
			Y:        20,
			PersonID: "testperson",
		}},
		Edges: []Edge{{From: "1", To: "1", Label: "self"}},
	}

	// First insert the item
	body, _ := json.Marshal(testPayload)
	insertRequest := events.APIGatewayProxyRequest{
		HTTPMethod: "POST",
		Path:       "/submit",
		Body:       string(body),
	}
	_, err := handler(context.Background(), insertRequest)
	if err != nil {
		t.Fatalf("Insert handler failed: %v", err)
	}

	// Then try to retrieve it
	getRequest := events.APIGatewayProxyRequest{
		HTTPMethod:     "GET",
		Path:           "/struktur/" + testID,
		PathParameters: map[string]string{"id": testID},
	}
	resp, err := getHandler(context.Background(), getRequest)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("GetHandler failed: %v, response: %+v", err, resp)
	}

	var returned Strukturbild
	if err := json.Unmarshal([]byte(resp.Body), &returned); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if returned.PersonID != testID {
		t.Errorf("Unexpected data: %+v", returned)
	}
}
