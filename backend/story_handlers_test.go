package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	storyapi "strukturbild/api"
)

func TestStoryCreateParagraphAndFetch(t *testing.T) {
	setupTestServices()
	ctx := context.Background()

	storyReq := events.APIGatewayProxyRequest{Body: `{"schoolId":"rychenberg","title":"Story Title"}`}
	resp, err := storySvc.HandleCreateStory(ctx, storyReq)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("create story failed: %v status=%d", err, resp.StatusCode)
	}
	var storyRes map[string]string
	if err := json.Unmarshal([]byte(resp.Body), &storyRes); err != nil {
		t.Fatalf("unmarshal story response: %v", err)
	}
	storyID := storyRes["id"]
	if storyID == "" {
		t.Fatalf("expected story id")
	}

	paraReq1 := events.APIGatewayProxyRequest{Body: `{"index":1,"bodyMd":"Paragraph 1","citations":[]}`,
		PathParameters: map[string]string{"storyId": storyID}}
	resp, err = storySvc.HandleCreateParagraph(ctx, paraReq1)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("create paragraph 1 failed: %v status=%d", err, resp.StatusCode)
	}

	paraReq2 := events.APIGatewayProxyRequest{Body: `{"index":2,"bodyMd":"Paragraph 2","citations":[]}`,
		PathParameters: map[string]string{"storyId": storyID}}
	resp, err = storySvc.HandleCreateParagraph(ctx, paraReq2)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("create paragraph 2 failed: %v status=%d", err, resp.StatusCode)
	}

	getReq := events.APIGatewayProxyRequest{PathParameters: map[string]string{"storyId": storyID}}
	resp, err = storySvc.HandleGetFullStory(ctx, getReq)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("get full story failed: %v status=%d", err, resp.StatusCode)
	}
	var full storyapi.StoryFull
	if err := json.Unmarshal([]byte(resp.Body), &full); err != nil {
		t.Fatalf("unmarshal story full: %v", err)
	}
	if len(full.Paragraphs) != 2 {
		t.Fatalf("expected 2 paragraphs, got %d", len(full.Paragraphs))
	}
	if full.Paragraphs[0].Index != 1 || full.Paragraphs[1].Index != 2 {
		t.Fatalf("paragraphs not ordered: %+v", full.Paragraphs)
	}
}

func TestUpdateParagraphReorder(t *testing.T) {
	setupTestServices()
	ctx := context.Background()

	storyReq := events.APIGatewayProxyRequest{Body: `{"schoolId":"rychenberg","title":"Story Title"}`}
	resp, _ := storySvc.HandleCreateStory(ctx, storyReq)
	var storyRes map[string]string
	if err := json.Unmarshal([]byte(resp.Body), &storyRes); err != nil {
		t.Fatalf("unmarshal story response: %v", err)
	}
	storyID := storyRes["id"]

	// create two paragraphs
	storySvc.HandleCreateParagraph(ctx, events.APIGatewayProxyRequest{Body: `{"index":1,"bodyMd":"First","citations":[]}`,
		PathParameters: map[string]string{"storyId": storyID}})
	storySvc.HandleCreateParagraph(ctx, events.APIGatewayProxyRequest{Body: `{"index":2,"bodyMd":"Second","citations":[]}`,
		PathParameters: map[string]string{"storyId": storyID}})

	fullResp, _ := storySvc.HandleGetFullStory(ctx, events.APIGatewayProxyRequest{PathParameters: map[string]string{"storyId": storyID}})
	var full storyapi.StoryFull
	if err := json.Unmarshal([]byte(fullResp.Body), &full); err != nil {
		t.Fatalf("unmarshal full story: %v", err)
	}
	if len(full.Paragraphs) != 2 {
		t.Fatalf("expected 2 paragraphs")
	}
	target := full.Paragraphs[0]

	patchPayload := map[string]interface{}{
		"storyId": storyID,
		"index":   2,
		"bodyMd":  "Updated",
	}
	body, _ := json.Marshal(patchPayload)
	patchReq := events.APIGatewayProxyRequest{
		Body:           string(body),
		PathParameters: map[string]string{"paragraphId": target.ParagraphID},
	}
	resp, err := storySvc.HandleUpdateParagraph(ctx, patchReq)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("patch failed: %v status=%d", err, resp.StatusCode)
	}

	// swap second paragraph to index 1 to simulate reorder conflict resolution
	other := full.Paragraphs[1]
	patchPayload = map[string]interface{}{
		"storyId": storyID,
		"index":   1,
	}
	body, _ = json.Marshal(patchPayload)
	_, err = storySvc.HandleUpdateParagraph(ctx, events.APIGatewayProxyRequest{
		Body:           string(body),
		PathParameters: map[string]string{"paragraphId": other.ParagraphID},
	})
	if err != nil {
		t.Fatalf("second patch failed: %v", err)
	}

	fullResp, _ = storySvc.HandleGetFullStory(ctx, events.APIGatewayProxyRequest{PathParameters: map[string]string{"storyId": storyID}})
	if err := json.Unmarshal([]byte(fullResp.Body), &full); err != nil {
		t.Fatalf("unmarshal full story: %v", err)
	}
	if len(full.Paragraphs) != 2 {
		t.Fatalf("expected 2 paragraphs")
	}
	if full.Paragraphs[0].ParagraphID != other.ParagraphID || full.Paragraphs[1].ParagraphID != target.ParagraphID {
		t.Fatalf("paragraph order incorrect: %+v", full.Paragraphs)
	}
	if full.Paragraphs[1].BodyMd != "Updated" {
		t.Fatalf("body not updated: %+v", full.Paragraphs[1])
	}
}

func TestImportStory(t *testing.T) {
	setupTestServices()
	ctx := context.Background()
	importJSON := `{
  "story": { "storyId": "story-rychenberg", "schoolId": "rychenberg", "title": "Rychenberg – Soziokratie unter Prüfstein" },
  "paragraphs": [
    { "index": 1, "bodyMd": "Am Rychenberg ...", "citations": [{ "transcriptId":"rychenberg_clean", "minutes":[0,1] }] },
    { "index": 2, "bodyMd": "Soziokratie ...", "citations": [{ "transcriptId":"rychenberg_clean", "minutes":[2,3,4] }] },
    { "index": 3, "bodyMd": "Mit dem Wegfall ...", "citations": [] }
  ],
  "details": [
    { "paragraphIndex": 2, "kind": "quote", "transcriptId": "rychenberg_clean", "startMinute": 2, "endMinute": 4, "text": "Quote" }
  ]
}`
	resp, err := storySvc.HandleImportStory(ctx, events.APIGatewayProxyRequest{Body: importJSON})
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("import failed: %v status=%d", err, resp.StatusCode)
	}
	var result map[string]string
	if err := json.Unmarshal([]byte(resp.Body), &result); err != nil {
		t.Fatalf("unmarshal import response: %v", err)
	}
	if result["id"] != "story-rychenberg" {
		t.Fatalf("unexpected story id: %v", result)
	}

	fullResp, _ := storySvc.HandleGetFullStory(ctx, events.APIGatewayProxyRequest{PathParameters: map[string]string{"storyId": "story-rychenberg"}})
	if fullResp.StatusCode != 200 {
		t.Fatalf("fetch imported story failed: status=%d", fullResp.StatusCode)
	}
	var full storyapi.StoryFull
	if err := json.Unmarshal([]byte(fullResp.Body), &full); err != nil {
		t.Fatalf("unmarshal full story: %v", err)
	}
	if len(full.Paragraphs) != 3 {
		t.Fatalf("expected 3 paragraphs")
	}
	if len(full.DetailsByParagraph) != 1 {
		t.Fatalf("expected 1 paragraph with details, got %d", len(full.DetailsByParagraph))
	}
	found := false
	for pid, details := range full.DetailsByParagraph {
		if len(details) == 1 && details[0].ParagraphID == pid {
			found = true
		}
	}
	if !found {
		t.Fatalf("detail mapping incorrect: %+v", full.DetailsByParagraph)
	}
}

func TestListStories(t *testing.T) {
	setupTestServices()
	ctx := context.Background()

	stories := []struct {
		id    string
		title string
	}{
		{"story-rychenberg", "Rychenberg"},
		{"story-sonnhalde", "Sonnhalde"},
	}

	for _, s := range stories {
		body := map[string]string{
			"storyId":  s.id,
			"schoolId": "school-" + s.id,
			"title":    s.title,
		}
		payload, _ := json.Marshal(body)
		resp, err := storySvc.HandleCreateStory(ctx, events.APIGatewayProxyRequest{Body: string(payload)})
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("create story failed for %s: %v status=%d", s.id, err, resp.StatusCode)
		}
	}

	resp, err := storySvc.HandleListStories(ctx, events.APIGatewayProxyRequest{})
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("list stories failed: %v status=%d", err, resp.StatusCode)
	}

	var payload struct {
		Stories []storyapi.Story `json:"stories"`
	}
	if err := json.Unmarshal([]byte(resp.Body), &payload); err != nil {
		t.Fatalf("unmarshal list response: %v", err)
	}
	if len(payload.Stories) != len(stories) {
		t.Fatalf("expected %d stories, got %d", len(stories), len(payload.Stories))
	}
	ids := make(map[string]bool)
	for _, s := range payload.Stories {
		ids[s.StoryID] = true
	}
	for _, expected := range stories {
		if !ids[expected.id] {
			t.Fatalf("missing story %s in response", expected.id)
		}
	}
}
