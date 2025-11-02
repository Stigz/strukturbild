package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// Story represents the metadata for a story bundle.
type Story struct {
	StoryID   string `json:"storyId"`
	SchoolID  string `json:"schoolId"`
	Title     string `json:"title"`
	CreatedAt string `json:"createdAt,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

// Citation links a paragraph to a transcript segment.
type Citation struct {
	TranscriptID string `json:"transcriptId"`
	Minutes      []int  `json:"minutes"`
}

// Paragraph contains the narrative content for a story.
type Paragraph struct {
	ParagraphID string     `json:"paragraphId"`
	StoryID     string     `json:"storyId"`
	Index       int        `json:"index"`
	Title       string     `json:"title,omitempty"`
	BodyMd      string     `json:"bodyMd"`
	Citations   []Citation `json:"citations"`
	CreatedAt   string     `json:"createdAt,omitempty"`
	UpdatedAt   string     `json:"updatedAt,omitempty"`
}

// Node represents a node on the strukturbild graph.
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

// Edge connects two nodes on the strukturbild graph.
type Edge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
	Type   string `json:"type,omitempty"`
}

type importPayload struct {
	Story                 Story               `json:"story"`
	Paragraphs            []importParagraph   `json:"paragraphs"`
	ParagraphNodeMap      map[string][]string `json:"paragraphNodeMap"`
	ParagraphNodeMapIndex map[string][]string `json:"paragraphNodeMapByIndex"`
	Nodes                 []Node              `json:"nodes"`
	Edges                 []Edge              `json:"edges"`
}

type importParagraph struct {
	ParagraphID string     `json:"paragraphId,omitempty"`
	StoryID     string     `json:"storyId,omitempty"`
	Index       int        `json:"index"`
	Title       string     `json:"title,omitempty"`
	BodyMd      string     `json:"bodyMd"`
	Citations   []Citation `json:"citations"`
	CreatedAt   string     `json:"createdAt,omitempty"`
	UpdatedAt   string     `json:"updatedAt,omitempty"`
}

type graphFixture struct {
	PersonID string `json:"personId"`
	Nodes    []Node `json:"nodes"`
	Edges    []Edge `json:"edges"`
}

type inMemoryStore struct {
	mu             sync.RWMutex
	stories        map[string]Story
	paragraphs     map[string][]Paragraph
	paragraphNodes map[string]map[string][]string
	nodes          map[string][]Node
	edges          map[string][]Edge
}

func newInMemoryStore() *inMemoryStore {
	return &inMemoryStore{
		stories:        make(map[string]Story),
		paragraphs:     make(map[string][]Paragraph),
		paragraphNodes: make(map[string]map[string][]string),
		nodes:          make(map[string][]Node),
		edges:          make(map[string][]Edge),
	}
}

var (
	dataStore        = newInMemoryStore()
	fixtureOnce      sync.Once
	fixtureErr       error
	loadFixturesFunc = loadDefaultFixtures
)

func ensureLoaded() error {
	if loadFixturesFunc == nil {
		return nil
	}
	fixtureOnce.Do(func() {
		fixtureErr = loadFixturesFunc()
	})
	return fixtureErr
}

func loadDefaultFixtures() error {
	if err := loadFixtureStory("import_rychenberg.json"); err != nil {
		return err
	}
	if err := loadFixtureGraph("graph_rychenberg.json"); err != nil {
		return err
	}
	return nil
}

// setFixturesLoaderForTest allows tests to override the fixture loader.
func setFixturesLoaderForTest(loader func() error) {
	loadFixturesFunc = loader
	fixtureOnce = sync.Once{}
	fixtureErr = nil
}

// resetStoreForTest clears the in-memory store. Intended for tests only.
func resetStoreForTest() {
	dataStore = newInMemoryStore()
}

func (s *inMemoryStore) listStories() []Story {
	s.mu.RLock()
	defer s.mu.RUnlock()
	stories := make([]Story, 0, len(s.stories))
	for _, st := range s.stories {
		stories = append(stories, st)
	}
	sort.Slice(stories, func(i, j int) bool {
		return strings.Compare(stories[i].StoryID, stories[j].StoryID) < 0
	})
	return stories
}

func (s *inMemoryStore) getStoryFull(id string) (Story, []Paragraph, map[string][]string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	story, ok := s.stories[id]
	if !ok {
		return Story{}, nil, nil, false
	}
	paras := append([]Paragraph(nil), s.paragraphs[id]...)
	nodeMap := make(map[string][]string, len(s.paragraphNodes[id]))
	for pid, nodes := range s.paragraphNodes[id] {
		nodeMap[pid] = append([]string(nil), nodes...)
	}
	return story, paras, nodeMap, true
}

func (s *inMemoryStore) setStoryBundle(story Story, paragraphs []Paragraph, nodeMap map[string][]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stories[story.StoryID] = story
	s.paragraphs[story.StoryID] = append([]Paragraph(nil), paragraphs...)
	copied := make(map[string][]string, len(nodeMap))
	for pid, nodes := range nodeMap {
		copied[pid] = append([]string(nil), nodes...)
	}
	s.paragraphNodes[story.StoryID] = copied
}

func (s *inMemoryStore) setGraph(personID string, nodes []Node, edges []Edge) {
	s.mu.Lock()
	defer s.mu.Unlock()
	nodeCopies := make([]Node, len(nodes))
	copy(nodeCopies, nodes)
	edgeCopies := make([]Edge, len(edges))
	copy(edgeCopies, edges)
	s.nodes[personID] = nodeCopies
	s.edges[personID] = edgeCopies
}

func (s *inMemoryStore) getGraph(personID string) ([]Node, []Edge) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	nodes := append([]Node(nil), s.nodes[personID]...)
	edges := append([]Edge(nil), s.edges[personID]...)
	return nodes, edges
}

func (s *inMemoryStore) upsertGraph(personID string, nodes []Node, edges []Edge) {
	s.mu.Lock()
	defer s.mu.Unlock()

	existingNodes := make(map[string]Node)
	for _, n := range s.nodes[personID] {
		existingNodes[n.ID] = n
	}
	for _, node := range nodes {
		if strings.TrimSpace(node.ID) == "" {
			node.ID = uuid.New().String()
		}
		node.PersonID = personID
		existingNodes[node.ID] = node
	}
	nodeKeys := make([]string, 0, len(existingNodes))
	for id := range existingNodes {
		nodeKeys = append(nodeKeys, id)
	}
	sort.Strings(nodeKeys)
	upsertedNodes := make([]Node, 0, len(existingNodes))
	for _, id := range nodeKeys {
		upsertedNodes = append(upsertedNodes, existingNodes[id])
	}
	s.nodes[personID] = upsertedNodes

	existingEdges := make(map[string]Edge)
	for _, e := range s.edges[personID] {
		key := e.From + "|" + e.To
		existingEdges[key] = e
	}
	for _, edge := range edges {
		key := edge.From + "|" + edge.To
		existingEdges[key] = edge
	}
	edgeKeys := make([]string, 0, len(existingEdges))
	for key := range existingEdges {
		edgeKeys = append(edgeKeys, key)
	}
	sort.Strings(edgeKeys)
	upsertedEdges := make([]Edge, 0, len(existingEdges))
	for _, key := range edgeKeys {
		upsertedEdges = append(upsertedEdges, existingEdges[key])
	}
	s.edges[personID] = upsertedEdges
}

func (s *inMemoryStore) deleteNode(personID, nodeID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	nodes := s.nodes[personID]
	found := false
	filtered := make([]Node, 0, len(nodes))
	for _, node := range nodes {
		if node.ID == nodeID {
			found = true
			continue
		}
		filtered = append(filtered, node)
	}
	if !found {
		return false
	}
	s.nodes[personID] = filtered

	edges := s.edges[personID]
	filteredEdges := make([]Edge, 0, len(edges))
	for _, edge := range edges {
		if edge.From == nodeID || edge.To == nodeID {
			continue
		}
		filteredEdges = append(filteredEdges, edge)
	}
	s.edges[personID] = filteredEdges

	if nodeMap, ok := s.paragraphNodes[personID]; ok {
		for pid, nodeIDs := range nodeMap {
			nodeMap[pid] = removeNodeID(nodeIDs, nodeID)
		}
	}
	return true
}

func removeNodeID(nodeIDs []string, target string) []string {
	if len(nodeIDs) == 0 {
		return nodeIDs
	}
	filtered := make([]string, 0, len(nodeIDs))
	for _, id := range nodeIDs {
		if id != target {
			filtered = append(filtered, id)
		}
	}
	return filtered
}

func (s *inMemoryStore) importStory(payload importPayload) (Story, []Paragraph, map[string][]string, error) {
	if strings.TrimSpace(payload.Story.Title) == "" || strings.TrimSpace(payload.Story.SchoolID) == "" {
		return Story{}, nil, nil, errors.New("story title and schoolId are required")
	}
	if len(payload.Paragraphs) == 0 {
		return Story{}, nil, nil, errors.New("paragraphs are required")
	}

	storyID := strings.TrimSpace(payload.Story.StoryID)
	if storyID == "" {
		storyID = fmt.Sprintf("story-%s", uuid.New().String())
	}
	now := time.Now().UTC().Format(time.RFC3339)
	story := Story{
		StoryID:   storyID,
		SchoolID:  strings.TrimSpace(payload.Story.SchoolID),
		Title:     strings.TrimSpace(payload.Story.Title),
		CreatedAt: now,
		UpdatedAt: now,
	}

	paragraphs := make([]Paragraph, 0, len(payload.Paragraphs))
	indexToID := make(map[int]string)
	for _, para := range payload.Paragraphs {
		if para.Index < 1 {
			return Story{}, nil, nil, fmt.Errorf("paragraph index must be >= 1, got %d", para.Index)
		}
		paraID := strings.TrimSpace(para.ParagraphID)
		if paraID == "" {
			paraID = fmt.Sprintf("para-%s", uuid.New().String())
		}
		paragraph := Paragraph{
			ParagraphID: paraID,
			StoryID:     storyID,
			Index:       para.Index,
			Title:       strings.TrimSpace(para.Title),
			BodyMd:      para.BodyMd,
			Citations:   para.Citations,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		paragraphs = append(paragraphs, paragraph)
		indexToID[para.Index] = paraID
	}
	sort.Slice(paragraphs, func(i, j int) bool {
		return paragraphs[i].Index < paragraphs[j].Index
	})

	nodeMap := make(map[string][]string)
	for pid, nodes := range payload.ParagraphNodeMap {
		nodeMap[pid] = append([]string(nil), nodes...)
	}
	for indexStr, nodes := range payload.ParagraphNodeMapIndex {
		idx, err := strconv.Atoi(indexStr)
		if err != nil {
			return Story{}, nil, nil, fmt.Errorf("invalid paragraph index %q", indexStr)
		}
		paraID, ok := indexToID[idx]
		if !ok {
			return Story{}, nil, nil, fmt.Errorf("paragraph index %d has no matching paragraph", idx)
		}
		nodeMap[paraID] = append([]string(nil), nodes...)
	}

	for i := range payload.Nodes {
		if strings.TrimSpace(payload.Nodes[i].ID) == "" {
			payload.Nodes[i].ID = uuid.New().String()
		}
		payload.Nodes[i].PersonID = storyID
	}

	dataStore.setStoryBundle(story, paragraphs, nodeMap)
	if len(payload.Nodes) > 0 || len(payload.Edges) > 0 {
		dataStore.setGraph(storyID, payload.Nodes, payload.Edges)
	}
	return story, paragraphs, nodeMap, nil
}

func loadFixtureStory(name string) error {
	reader, err := openFixture(name)
	if err != nil {
		return err
	}
	defer reader.Close()
	var payload importPayload
	if err := json.NewDecoder(reader).Decode(&payload); err != nil {
		return err
	}
	_, _, _, err = dataStore.importStory(payload)
	return err
}

func loadFixtureGraph(name string) error {
	reader, err := openFixture(name)
	if err != nil {
		return err
	}
	defer reader.Close()
	var fixture graphFixture
	if err := json.NewDecoder(reader).Decode(&fixture); err != nil {
		return err
	}
	for i := range fixture.Nodes {
		if strings.TrimSpace(fixture.Nodes[i].ID) == "" {
			fixture.Nodes[i].ID = uuid.New().String()
		}
		fixture.Nodes[i].PersonID = fixture.PersonID
	}
	dataStore.setGraph(fixture.PersonID, fixture.Nodes, fixture.Edges)
	return nil
}

func openFixture(name string) (io.ReadCloser, error) {
	candidates := []string{
		name,
		filepath.Join("testfiles", name),
		filepath.Join("..", "testfiles", name),
		filepath.Join("..", "..", "testfiles", name),
	}
	for _, path := range candidates {
		if f, err := os.Open(path); err == nil {
			return f, nil
		}
	}
	return nil, fmt.Errorf("fixture %s not found", name)
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(payload)
}

// ListStories responds with the list of stories available in the store.
func ListStories(w http.ResponseWriter, r *http.Request) {
	if err := ensureLoaded(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	stories := dataStore.listStories()
	writeJSON(w, http.StatusOK, stories)
}

type storyFullResponse struct {
	Story            Story               `json:"story"`
	Paragraphs       []Paragraph         `json:"paragraphs"`
	ParagraphNodeMap map[string][]string `json:"paragraphNodeMap"`
}

// GetStoryFull returns the story with its paragraphs and paragraph-node mapping.
func GetStoryFull(w http.ResponseWriter, r *http.Request) {
	if err := ensureLoaded(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	storyID := mux.Vars(r)["id"]
	if strings.TrimSpace(storyID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing story id"})
		return
	}
	story, paragraphs, nodeMap, ok := dataStore.getStoryFull(storyID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "story not found"})
		return
	}
	if paragraphs == nil {
		paragraphs = []Paragraph{}
	}
	if nodeMap == nil {
		nodeMap = map[string][]string{}
	}
	resp := storyFullResponse{Story: story, Paragraphs: paragraphs, ParagraphNodeMap: nodeMap}
	writeJSON(w, http.StatusOK, resp)
}

// ImportStory ingests a story bundle into the in-memory store.
func ImportStory(w http.ResponseWriter, r *http.Request) {
	if err := ensureLoaded(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	var payload importPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	story, _, _, err := dataStore.importStory(payload)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": story.StoryID})
}

type strukturResponse struct {
	ID               string              `json:"id"`
	Nodes            []Node              `json:"nodes"`
	Edges            []Edge              `json:"edges"`
	PersonID         string              `json:"personId"`
	Story            *Story              `json:"story,omitempty"`
	Paragraphs       []Paragraph         `json:"paragraphs,omitempty"`
	ParagraphNodeMap map[string][]string `json:"paragraphNodeMap,omitempty"`
}

// GetStrukturByPerson returns the graph and narrative bundle for a person.
func GetStrukturByPerson(w http.ResponseWriter, r *http.Request) {
	if err := ensureLoaded(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	personID := mux.Vars(r)["personId"]
	if strings.TrimSpace(personID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing personId"})
		return
	}
	story, paragraphs, nodeMap, ok := dataStore.getStoryFull(personID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "story not found"})
		return
	}
	nodes, edges := dataStore.getGraph(personID)
	if paragraphs == nil {
		paragraphs = []Paragraph{}
	}
	if nodeMap == nil {
		nodeMap = map[string][]string{}
	}
	storyCopy := story
	resp := strukturResponse{
		ID:               "",
		Nodes:            nodes,
		Edges:            edges,
		PersonID:         personID,
		Story:            &storyCopy,
		Paragraphs:       paragraphs,
		ParagraphNodeMap: nodeMap,
	}
	writeJSON(w, http.StatusOK, resp)
}

type submitPayload struct {
	PersonID string `json:"personId"`
	Nodes    []Node `json:"nodes"`
	Edges    []Edge `json:"edges"`
}

// SubmitHandler upserts nodes and edges for a person.
func SubmitHandler(w http.ResponseWriter, r *http.Request) {
	if err := ensureLoaded(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	var payload submitPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if strings.TrimSpace(payload.PersonID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "personId is required"})
		return
	}
	dataStore.upsertGraph(payload.PersonID, payload.Nodes, payload.Edges)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DeleteNode removes a node, its edges and paragraph references.
func DeleteNode(w http.ResponseWriter, r *http.Request) {
	if err := ensureLoaded(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	vars := mux.Vars(r)
	personID := vars["personId"]
	nodeID := vars["nodeId"]
	if strings.TrimSpace(personID) == "" || strings.TrimSpace(nodeID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing personId or nodeId"})
		return
	}
	if !dataStore.deleteNode(personID, nodeID) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "node not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
