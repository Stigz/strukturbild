package main

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/aws/aws-lambda-go/events"
)

func TestHandler(t *testing.T) {
	setupTestServices()
	testPayload := Strukturbild{
		ID:      "test123",
		StoryID: "testperson",
		Nodes: []Node{{
			ID:    "1",
			Label: "A",
			X:     0,
			Y:     0,
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
		ID:      testID,
		StoryID: testID,
		Nodes: []Node{{
			ID:    "1",
			Label: "Node1",
			X:     10,
			Y:     20,
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

	if returned.StoryID != testID {
		t.Errorf("Unexpected data: %+v", returned)
	}
}

func TestGetHandlerIncludesStoryBundle(t *testing.T) {
	setupTestServices()
	ctx := context.Background()
	storyID := "story-bundle-test"

	sbPayload := Strukturbild{
		StoryID: storyID,
		Nodes: []Node{{
			ID:    "node-1",
			Label: "Node",
			X:     0,
			Y:     0,
		}},
	}
	body, _ := json.Marshal(sbPayload)
	if _, err := handler(ctx, events.APIGatewayProxyRequest{HTTPMethod: "POST", Path: "/submit", Body: string(body)}); err != nil {
		t.Fatalf("failed to seed strukturbild: %v", err)
	}

	storyBody := fmt.Sprintf(`{"storyId":"%s","schoolId":"ry","title":"Title"}`, storyID)
	if _, err := storySvc.HandleCreateStory(ctx, events.APIGatewayProxyRequest{HTTPMethod: "POST", Body: storyBody}); err != nil {
		t.Fatalf("failed to create story: %v", err)
	}

	paraResp, err := storySvc.HandleCreateParagraph(ctx, events.APIGatewayProxyRequest{
		HTTPMethod:     "POST",
		PathParameters: map[string]string{"storyId": storyID},
		Body:           `{"index":1,"title":"Intro","bodyMd":"Text","citations":[]}`,
	})
	if err != nil {
		t.Fatalf("failed to create paragraph: %v", err)
	}
	var paraPayload map[string]string
	if err := json.Unmarshal([]byte(paraResp.Body), &paraPayload); err != nil {
		t.Fatalf("failed to parse paragraph response: %v", err)
	}
	paragraphID := paraPayload["id"]
	if paragraphID == "" {
		t.Fatalf("paragraph id missing in response: %+v", paraPayload)
	}

	detailBody := fmt.Sprintf(`{"storyId":"%s","kind":"quote","transcriptId":"ry","startMinute":0,"endMinute":1,"text":"Zitat"}`, storyID)
	if _, err := storySvc.HandleCreateDetail(ctx, events.APIGatewayProxyRequest{
		HTTPMethod:     "POST",
		PathParameters: map[string]string{"paragraphId": paragraphID},
		Body:           detailBody,
	}); err != nil {
		t.Fatalf("failed to create detail: %v", err)
	}

	resp, err := getHandler(ctx, events.APIGatewayProxyRequest{
		HTTPMethod:     "GET",
		Path:           "/struktur/" + storyID,
		PathParameters: map[string]string{"id": storyID},
	})
	if err != nil {
		t.Fatalf("getHandler returned error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("unexpected status: %d body=%s", resp.StatusCode, resp.Body)
	}

	var returned Strukturbild
	if err := json.Unmarshal([]byte(resp.Body), &returned); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if returned.Story == nil {
		t.Fatalf("expected story payload, got nil: %+v", returned)
	}
	if returned.Story.Title != "Title" {
		t.Errorf("unexpected story title: %s", returned.Story.Title)
	}
	if len(returned.Paragraphs) != 1 {
		t.Fatalf("expected 1 paragraph, got %d", len(returned.Paragraphs))
	}
	if returned.Paragraphs[0].ParagraphID != paragraphID {
		t.Errorf("unexpected paragraph id: %s", returned.Paragraphs[0].ParagraphID)
	}
	if len(returned.DetailsByParagraph[paragraphID]) != 1 {
		t.Fatalf("expected detail for paragraph, got %+v", returned.DetailsByParagraph)
	}
}
