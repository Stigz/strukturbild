package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func setupFixtures(t *testing.T) {
	t.Helper()
	resetStoreForTest()
	setFixturesLoaderForTest(loadDefaultFixtures)
	if err := ensureLoaded(); err != nil {
		t.Fatalf("failed to load fixtures: %v", err)
	}
}

func setupEmptyStore(t *testing.T) {
	t.Helper()
	resetStoreForTest()
	setFixturesLoaderForTest(nil)
}

func TestGetStoryFullIncludesParagraphNodeMap(t *testing.T) {
	setupFixtures(t)

	router := newRouter()
	req := httptest.NewRequest(http.MethodGet, "/api/stories/story-rychenberg/full", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}

	var payload storyFullResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(payload.Paragraphs) == 0 {
		t.Fatalf("expected paragraphs in response")
	}
	if payload.ParagraphNodeMap == nil {
		t.Fatalf("expected paragraphNodeMap in response")
	}
	firstID := payload.Paragraphs[0].ParagraphID
	if _, ok := payload.ParagraphNodeMap[firstID]; !ok {
		t.Fatalf("expected paragraphNodeMap entry for %s", firstID)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(resp.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal raw response: %v", err)
	}
	if _, exists := raw["detailsByParagraph"]; exists {
		t.Fatalf("unexpected detailsByParagraph key in response")
	}
}

func TestGetStrukturIncludesParagraphNodeMap(t *testing.T) {
	setupFixtures(t)

	router := newRouter()
	req := httptest.NewRequest(http.MethodGet, "/struktur/story-rychenberg", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}

	var payload strukturResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if payload.Story == nil {
		t.Fatalf("expected story in struktur response")
	}
	if len(payload.Paragraphs) == 0 {
		t.Fatalf("expected paragraphs in struktur response")
	}
	if len(payload.Nodes) == 0 {
		t.Fatalf("expected nodes in struktur response")
	}
	if payload.ParagraphNodeMap == nil || len(payload.ParagraphNodeMap) == 0 {
		t.Fatalf("expected paragraphNodeMap entries")
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(resp.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal raw response: %v", err)
	}
	if _, exists := raw["detailsByParagraph"]; exists {
		t.Fatalf("unexpected detailsByParagraph key in struktur response")
	}
}

func TestImportStoryConvertsParagraphNodeMapByIndex(t *testing.T) {
	setupEmptyStore(t)
	router := newRouter()

	importPayload := map[string]interface{}{
		"story": map[string]interface{}{
			"storyId":  "story-rychenberg",
			"schoolId": "rychenberg",
			"title":    "Rychenberg – Soziokratie unter Prüfstein",
		},
		"paragraphs": []map[string]interface{}{
			{"index": 1, "bodyMd": "Para 1", "citations": []interface{}{}},
			{"index": 2, "bodyMd": "Para 2", "citations": []interface{}{}},
			{"index": 3, "bodyMd": "Para 3", "citations": []interface{}{}},
		},
		"paragraphNodeMapByIndex": map[string][]string{
			"1": []string{"n1", "n2"},
			"2": []string{"n3"},
			"3": []string{"n4", "n5"},
		},
	}

	body, err := json.Marshal(importPayload)
	if err != nil {
		t.Fatalf("marshal import payload: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/stories/import", bytes.NewReader(body))
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/stories/story-rychenberg/full", nil)
	getResp := httptest.NewRecorder()
	router.ServeHTTP(getResp, getReq)

	if getResp.Code != http.StatusOK {
		t.Fatalf("expected status 200 from full story, got %d", getResp.Code)
	}

	var full storyFullResponse
	if err := json.Unmarshal(getResp.Body.Bytes(), &full); err != nil {
		t.Fatalf("unmarshal full response: %v", err)
	}

	if len(full.Paragraphs) != 3 {
		t.Fatalf("expected 3 paragraphs, got %d", len(full.Paragraphs))
	}
	if len(full.ParagraphNodeMap) != 3 {
		t.Fatalf("expected 3 paragraphNodeMap entries, got %d", len(full.ParagraphNodeMap))
	}

	expected := map[int][]string{
		1: []string{"n1", "n2"},
		2: []string{"n3"},
		3: []string{"n4", "n5"},
	}
	for _, para := range full.Paragraphs {
		nodes, ok := full.ParagraphNodeMap[para.ParagraphID]
		if !ok {
			t.Fatalf("missing node map for paragraph %s", para.ParagraphID)
		}
		want := expected[para.Index]
		if len(nodes) != len(want) {
			t.Fatalf("expected %d nodes for paragraph %d, got %d", len(want), para.Index, len(nodes))
		}
		for i := range nodes {
			if nodes[i] != want[i] {
				t.Fatalf("unexpected node mapping for paragraph %d: %v", para.Index, nodes)
			}
		}
	}
}

func TestDeleteNodeRemovesGraphAndParagraphReferences(t *testing.T) {
	setupFixtures(t)
	router := newRouter()

	delReq := httptest.NewRequest(http.MethodDelete, "/struktur/story-rychenberg/n1", nil)
	delResp := httptest.NewRecorder()
	router.ServeHTTP(delResp, delReq)

	if delResp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", delResp.Code)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/struktur/story-rychenberg", nil)
	getResp := httptest.NewRecorder()
	router.ServeHTTP(getResp, getReq)

	if getResp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", getResp.Code)
	}

	var payload strukturResponse
	if err := json.Unmarshal(getResp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal struktur response: %v", err)
	}

	for _, node := range payload.Nodes {
		if node.ID == "n1" {
			t.Fatalf("expected node n1 to be deleted")
		}
	}
	for _, edge := range payload.Edges {
		if edge.From == "n1" || edge.To == "n1" {
			t.Fatalf("edges referencing n1 should be removed: %+v", edge)
		}
	}
	for pid, nodes := range payload.ParagraphNodeMap {
		for _, id := range nodes {
			if id == "n1" {
				t.Fatalf("paragraph %s still references deleted node", pid)
			}
		}
	}
}

func TestSubmitUpsertsNodePositions(t *testing.T) {
	setupFixtures(t)
	router := newRouter()

	submitPayload := submitPayload{
		PersonID: "story-rychenberg",
		Nodes: []Node{
			{ID: "n2", Label: "Soziokratie", X: 900, Y: 901},
			{ID: "n8", Label: "Neuer Knoten", X: 400, Y: 401},
		},
		Edges: []Edge{{From: "n2", To: "n8", Label: "verbindet"}},
	}
	body, err := json.Marshal(submitPayload)
	if err != nil {
		t.Fatalf("marshal submit payload: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/submit", bytes.NewReader(body))
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/struktur/story-rychenberg", nil)
	getResp := httptest.NewRecorder()
	router.ServeHTTP(getResp, getReq)

	if getResp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", getResp.Code)
	}

	var payload strukturResponse
	if err := json.Unmarshal(getResp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal struktur response: %v", err)
	}

	foundUpdated := false
	foundNew := false
	for _, node := range payload.Nodes {
		switch node.ID {
		case "n2":
			if node.X != 900 || node.Y != 901 {
				t.Fatalf("expected updated position for n2, got %+v", node)
			}
			foundUpdated = true
		case "n8":
			foundNew = true
			if node.PersonID != "story-rychenberg" {
				t.Fatalf("expected personId on new node, got %s", node.PersonID)
			}
		}
	}
	if !foundUpdated {
		t.Fatalf("expected to find updated node n2")
	}
	if !foundNew {
		t.Fatalf("expected to find new node n8")
	}

	foundEdge := false
	for _, edge := range payload.Edges {
		if edge.From == "n2" && edge.To == "n8" {
			foundEdge = true
			break
		}
	}
	if !foundEdge {
		t.Fatalf("expected to find upserted edge n2->n8")
	}
}
