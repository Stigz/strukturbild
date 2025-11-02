package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"context"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

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
	ID      string `json:"id"`
	Label   string `json:"label"`
	Detail  string `json:"detail,omitempty"`
	Type    string `json:"type,omitempty"`
	Time    string `json:"time,omitempty"`
	Color   string `json:"color,omitempty"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	StoryID string `json:"storyId"`
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
	Story            Story               `json:"story"`
	Paragraphs       []importParagraph   `json:"paragraphs"`
	ParagraphNodeMap map[string][]string `json:"paragraphNodeMap"`
	// paragraphNodeMapByIndex allows callers to map paragraphs to nodes by index when ids are unstable.
	ParagraphNodeMapByIndex map[string][]string `json:"paragraphNodeMapByIndex"`
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
	StoryID string `json:"storyId"`
	Nodes   []Node `json:"nodes"`
	Edges   []Edge `json:"edges"`
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

var (
	ddbClient   *dynamodb.Client
	ddbInitOnce sync.Once

	storiesTable = os.Getenv("DDB_STORIES_TABLE")
	graphsTable  = os.Getenv("DDB_GRAPHS_TABLE")
)

func ddbEnabled() bool { return storiesTable != "" || graphsTable != "" }

func getDDB(ctx context.Context) (*dynamodb.Client, error) {
	var initErr error
	ddbInitOnce.Do(func() {
		cfg, err := config.LoadDefaultConfig(ctx)
		if err != nil {
			initErr = err
			return
		}
		ddbClient = dynamodb.NewFromConfig(cfg)
	})
	return ddbClient, initErr
}

func ensureLoaded() error {
	if loadFixturesFunc == nil {
		return nil
	}
	fixtureOnce.Do(func() {
		err := loadFixturesFunc()
		// In Lambda/prod the test fixtures won't be packaged.
		// Treat missing-fixture errors as non-fatal so endpoints still work.
		if isFixtureMissing(err) {
			err = nil
		}
		fixtureErr = err
	})
	return fixtureErr
}

// isFixtureMissing reports whether the error came from trying to open a local dev fixture.
func isFixtureMissing(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "fixture") && strings.Contains(err.Error(), "not found")
}

func loadDefaultFixtures() error {
	if err := loadFixtureStory("import_rychenberg.json"); err != nil && !isFixtureMissing(err) {
		return err
	}
	if err := loadFixtureGraph("graph_rychenberg.json"); err != nil && !isFixtureMissing(err) {
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

func (s *inMemoryStore) setGraph(storyID string, nodes []Node, edges []Edge) {
	s.mu.Lock()
	defer s.mu.Unlock()
	nodeCopies := make([]Node, len(nodes))
	copy(nodeCopies, nodes)
	edgeCopies := make([]Edge, len(edges))
	copy(edgeCopies, edges)
	s.nodes[storyID] = nodeCopies
	s.edges[storyID] = edgeCopies
}

func (s *inMemoryStore) getGraph(storyID string) ([]Node, []Edge) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	nodes := append([]Node(nil), s.nodes[storyID]...)
	edges := append([]Edge(nil), s.edges[storyID]...)
	return nodes, edges
}

func (s *inMemoryStore) upsertGraph(storyID string, nodes []Node, edges []Edge) {
	s.mu.Lock()
	defer s.mu.Unlock()

	existingNodes := make(map[string]Node)
	for _, n := range s.nodes[storyID] {
		existingNodes[n.ID] = n
	}
	for _, node := range nodes {
		if strings.TrimSpace(node.ID) == "" {
			node.ID = uuid.New().String()
		}
		node.StoryID = storyID
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
	s.nodes[storyID] = upsertedNodes

	existingEdges := make(map[string]Edge)
	for _, e := range s.edges[storyID] {
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
	s.edges[storyID] = upsertedEdges
}

func (s *inMemoryStore) deleteNode(storyID, nodeID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	nodes := s.nodes[storyID]
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
	s.nodes[storyID] = filtered

	edges := s.edges[storyID]
	filteredEdges := make([]Edge, 0, len(edges))
	for _, edge := range edges {
		if edge.From == nodeID || edge.To == nodeID {
			continue
		}
		filteredEdges = append(filteredEdges, edge)
	}
	s.edges[storyID] = filteredEdges

	if nodeMap, ok := s.paragraphNodes[storyID]; ok {
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

// ===== DynamoDB helpers (optional persistence) =====
// Item shape in DDB (storiesTable):
//   PK: storyId (S)
//   schoolId, title, createdAt, updatedAt (S)
//   paragraphs: L of M
//   paragraphNodeMap: M (pid -> L of S)

func ddbPutStoryBundle(ctx context.Context, st Story, paras []Paragraph, nodeMap map[string][]string) error {
	if storiesTable == "" {
		return nil
	}
	cli, err := getDDB(ctx)
	if err != nil {
		return err
	}
	item := map[string]types.AttributeValue{
		"storyId":   &types.AttributeValueMemberS{Value: st.StoryID},
		"schoolId":  &types.AttributeValueMemberS{Value: st.SchoolID},
		"title":     &types.AttributeValueMemberS{Value: st.Title},
		"createdAt": &types.AttributeValueMemberS{Value: st.CreatedAt},
		"updatedAt": &types.AttributeValueMemberS{Value: st.UpdatedAt},
	}
	// paragraphs
	pList := make([]types.AttributeValue, 0, len(paras))
	for _, p := range paras {
		cits := make([]types.AttributeValue, 0, len(p.Citations))
		for _, c := range p.Citations {
			mins := make([]types.AttributeValue, 0, len(c.Minutes))
			for _, m := range c.Minutes {
				mins = append(mins, &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", m)})
			}
			cits = append(cits, &types.AttributeValueMemberM{Value: map[string]types.AttributeValue{
				"transcriptId": &types.AttributeValueMemberS{Value: c.TranscriptID},
				"minutes":      &types.AttributeValueMemberL{Value: mins},
			}})
		}
		pMap := map[string]types.AttributeValue{
			"paragraphId": &types.AttributeValueMemberS{Value: p.ParagraphID},
			"storyId":     &types.AttributeValueMemberS{Value: st.StoryID},
			"index":       &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", p.Index)},
			"title":       &types.AttributeValueMemberS{Value: p.Title},
			"bodyMd":      &types.AttributeValueMemberS{Value: p.BodyMd},
			"citations":   &types.AttributeValueMemberL{Value: cits},
			"createdAt":   &types.AttributeValueMemberS{Value: p.CreatedAt},
			"updatedAt":   &types.AttributeValueMemberS{Value: p.UpdatedAt},
		}
		pList = append(pList, &types.AttributeValueMemberM{Value: pMap})
	}
	item["paragraphs"] = &types.AttributeValueMemberL{Value: pList}

	// paragraphNodeMap
	m := map[string]types.AttributeValue{}
	for pid, ids := range nodeMap {
		lst := make([]types.AttributeValue, 0, len(ids))
		for _, id := range ids {
			lst = append(lst, &types.AttributeValueMemberS{Value: id})
		}
		m[pid] = &types.AttributeValueMemberL{Value: lst}
	}
	item["paragraphNodeMap"] = &types.AttributeValueMemberM{Value: m}

	_, err = cli.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &storiesTable,
		Item:      item,
	})
	return err
}

func ddbGetStoryBundle(ctx context.Context, storyID string) (Story, []Paragraph, map[string][]string, bool, error) {
	if storiesTable == "" {
		return Story{}, nil, nil, false, nil
	}
	cli, err := getDDB(ctx)
	if err != nil {
		return Story{}, nil, nil, false, err
	}
	out, err := cli.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: &storiesTable,
		Key: map[string]types.AttributeValue{
			"storyId": &types.AttributeValueMemberS{Value: storyID},
		},
	})
	if err != nil {
		return Story{}, nil, nil, false, err
	}
	if out.Item == nil {
		return Story{}, nil, nil, false, nil
	}
	st := Story{
		StoryID:   storyID,
		SchoolID:  getS(out.Item["schoolId"]),
		Title:     getS(out.Item["title"]),
		CreatedAt: getS(out.Item["createdAt"]),
		UpdatedAt: getS(out.Item["updatedAt"]),
	}
	paras := []Paragraph{}
	if lv, ok := out.Item["paragraphs"].(*types.AttributeValueMemberL); ok {
		for _, av := range lv.Value {
			if mm, ok := av.(*types.AttributeValueMemberM); ok {
				p := Paragraph{
					ParagraphID: getS(mm.Value["paragraphId"]),
					StoryID:     storyID,
					Index:       atoi(getN(mm.Value["index"])),
					Title:       getS(mm.Value["title"]),
					BodyMd:      getS(mm.Value["bodyMd"]),
					CreatedAt:   getS(mm.Value["createdAt"]),
					UpdatedAt:   getS(mm.Value["updatedAt"]),
				}
				// citations
				if cl, ok := mm.Value["citations"].(*types.AttributeValueMemberL); ok {
					for _, cav := range cl.Value {
						if cm, ok := cav.(*types.AttributeValueMemberM); ok {
							c := Citation{
								TranscriptID: getS(cm.Value["transcriptId"]),
							}
							if ml, ok := cm.Value["minutes"].(*types.AttributeValueMemberL); ok {
								for _, mv := range ml.Value {
									c.Minutes = append(c.Minutes, atoi(getN(mv)))
								}
							}
							p.Citations = append(p.Citations, c)
						}
					}
				}
				paras = append(paras, p)
			}
		}
	}
	// paragraphNodeMap
	nodeMap := map[string][]string{}
	if mm, ok := out.Item["paragraphNodeMap"].(*types.AttributeValueMemberM); ok {
		for pid, av := range mm.Value {
			if lv, ok := av.(*types.AttributeValueMemberL); ok {
				for _, v := range lv.Value {
					nodeMap[pid] = append(nodeMap[pid], getS(v))
				}
			}
		}
	}
	sort.Slice(paras, func(i, j int) bool { return paras[i].Index < paras[j].Index })
	return st, paras, nodeMap, true, nil
}

// Graphs as a single item per story (nodes + edges arrays)
func ddbPutGraph(ctx context.Context, storyID string, nodes []Node, edges []Edge) error {
	if graphsTable == "" {
		return nil
	}
	cli, err := getDDB(ctx)
	if err != nil {
		return err
	}
	nList := make([]types.AttributeValue, 0, len(nodes))
	for _, n := range nodes {
		nList = append(nList, &types.AttributeValueMemberM{Value: map[string]types.AttributeValue{
			"id":      &types.AttributeValueMemberS{Value: n.ID},
			"label":   &types.AttributeValueMemberS{Value: n.Label},
			"detail":  &types.AttributeValueMemberS{Value: n.Detail},
			"type":    &types.AttributeValueMemberS{Value: n.Type},
			"time":    &types.AttributeValueMemberS{Value: n.Time},
			"color":   &types.AttributeValueMemberS{Value: n.Color},
			"x":       &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", n.X)},
			"y":       &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", n.Y)},
			"storyId": &types.AttributeValueMemberS{Value: storyID},
		}})
	}
	eList := make([]types.AttributeValue, 0, len(edges))
	for _, e := range edges {
		eList = append(eList, &types.AttributeValueMemberM{Value: map[string]types.AttributeValue{
			"from":   &types.AttributeValueMemberS{Value: e.From},
			"to":     &types.AttributeValueMemberS{Value: e.To},
			"label":  &types.AttributeValueMemberS{Value: e.Label},
			"detail": &types.AttributeValueMemberS{Value: e.Detail},
			"type":   &types.AttributeValueMemberS{Value: e.Type},
		}})
	}
	item := map[string]types.AttributeValue{
		"storyId": &types.AttributeValueMemberS{Value: storyID},
		"nodes":   &types.AttributeValueMemberL{Value: nList},
		"edges":   &types.AttributeValueMemberL{Value: eList},
	}
	_, err = cli.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &graphsTable,
		Item:      item,
	})
	return err
}

func ddbGetGraph(ctx context.Context, storyID string) ([]Node, []Edge, bool, error) {
	if graphsTable == "" {
		return nil, nil, false, nil
	}
	cli, err := getDDB(ctx)
	if err != nil {
		return nil, nil, false, err
	}
	out, err := cli.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: &graphsTable,
		Key: map[string]types.AttributeValue{
			"storyId": &types.AttributeValueMemberS{Value: storyID},
		},
	})
	if err != nil {
		return nil, nil, false, err
	}
	if out.Item == nil {
		return nil, nil, false, nil
	}
	var nodes []Node
	if lv, ok := out.Item["nodes"].(*types.AttributeValueMemberL); ok {
		for _, av := range lv.Value {
			if mm, ok := av.(*types.AttributeValueMemberM); ok {
				nodes = append(nodes, Node{
					ID:      getS(mm.Value["id"]),
					Label:   getS(mm.Value["label"]),
					Detail:  getS(mm.Value["detail"]),
					Type:    getS(mm.Value["type"]),
					Time:    getS(mm.Value["time"]),
					Color:   getS(mm.Value["color"]),
					X:       atoi(getN(mm.Value["x"])),
					Y:       atoi(getN(mm.Value["y"])),
					StoryID: storyID,
				})
			}
		}
	}
	var edges []Edge
	if lv, ok := out.Item["edges"].(*types.AttributeValueMemberL); ok {
		for _, av := range lv.Value {
			if mm, ok := av.(*types.AttributeValueMemberM); ok {
				edges = append(edges, Edge{
					From:   getS(mm.Value["from"]),
					To:     getS(mm.Value["to"]),
					Label:  getS(mm.Value["label"]),
					Detail: getS(mm.Value["detail"]),
					Type:   getS(mm.Value["type"]),
				})
			}
		}
	}
	return nodes, edges, true, nil
}

func ddbListStories(ctx context.Context) ([]Story, error) {
	if storiesTable == "" {
		return nil, nil
	}
	cli, err := getDDB(ctx)
	if err != nil {
		return nil, err
	}
	projection := "storyId, schoolId, title, createdAt, updatedAt"
	stories := []Story{}
	var startKey map[string]types.AttributeValue
	for {
		out, err := cli.Scan(ctx, &dynamodb.ScanInput{
			TableName:            &storiesTable,
			ProjectionExpression: &projection,
			ExclusiveStartKey:    startKey,
		})
		if err != nil {
			return nil, err
		}
		for _, item := range out.Items {
			id := getS(item["storyId"])
			if id == "" {
				continue
			}
			stories = append(stories, Story{
				StoryID:   id,
				SchoolID:  getS(item["schoolId"]),
				Title:     getS(item["title"]),
				CreatedAt: getS(item["createdAt"]),
				UpdatedAt: getS(item["updatedAt"]),
			})
		}
		if len(out.LastEvaluatedKey) == 0 {
			break
		}
		startKey = out.LastEvaluatedKey
	}
	sort.Slice(stories, func(i, j int) bool {
		return strings.Compare(stories[i].StoryID, stories[j].StoryID) < 0
	})
	return stories, nil
}

// Utility getters from AttributeValue
func getS(av types.AttributeValue) string {
	if v, ok := av.(*types.AttributeValueMemberS); ok {
		return v.Value
	}
	return ""
}
func getN(av types.AttributeValue) string {
	if v, ok := av.(*types.AttributeValueMemberN); ok {
		return v.Value
	}
	return "0"
}
func atoi(s string) int {
	i, _ := strconv.Atoi(s)
	return i
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
	}
	sort.Slice(paragraphs, func(i, j int) bool {
		return paragraphs[i].Index < paragraphs[j].Index
	})

	// Use the explicit paragraphId -> []nodeId mapping only
	nodeMap := make(map[string][]string)
	for pid, nodes := range payload.ParagraphNodeMap {
		cleanID := strings.TrimSpace(pid)
		if cleanID == "" {
			continue
		}
		nodeMap[cleanID] = append([]string(nil), nodes...)
	}

	// Allow mapping by paragraph index (string form) for clients without stable paragraphIds yet.
	if len(payload.ParagraphNodeMapByIndex) > 0 {
		for _, para := range paragraphs {
			idxKey := strconv.Itoa(para.Index)
			nodes, ok := payload.ParagraphNodeMapByIndex[idxKey]
			if !ok {
				continue
			}
			nodeMap[para.ParagraphID] = append([]string(nil), nodes...)
		}
	}

	s.setStoryBundle(story, paragraphs, nodeMap)
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
		fixture.Nodes[i].StoryID = fixture.StoryID
	}
	dataStore.setGraph(fixture.StoryID, fixture.Nodes, fixture.Edges)
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

// Echo is a minimal POST endpoint for debugging API Gateway/Lambda wiring.
// It logs the raw body and echoes it back as application/json.
func Echo(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	_ = r.Body.Close()
	log.Printf("Echo: received %d bytes", len(b))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}

// ListStories responds with the list of stories available in the store.
func ListStories(w http.ResponseWriter, r *http.Request) {
	if err := ensureLoaded(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	stories := dataStore.listStories()
	if storiesTable != "" {
		existing := make(map[string]struct{}, len(stories))
		for _, st := range stories {
			existing[st.StoryID] = struct{}{}
		}
		if remote, err := ddbListStories(r.Context()); err != nil {
			log.Printf("ListStories: ddbListStories error: %v", err)
		} else {
			for _, st := range remote {
				if _, ok := existing[st.StoryID]; !ok {
					stories = append(stories, st)
				}
			}
			sort.Slice(stories, func(i, j int) bool {
				return strings.Compare(stories[i].StoryID, stories[j].StoryID) < 0
			})
		}
	}
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
	if !ok && storiesTable != "" {
		if st, paras, nmap, found, derr := ddbGetStoryBundle(r.Context(), storyID); derr == nil && found {
			// cache in memory
			dataStore.setStoryBundle(st, paras, nmap)
			story, paragraphs, nodeMap, ok = dataStore.getStoryFull(storyID)
		}
	}
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

// ImportStory ingests a story bundle into the in-memory store (MVP).
// It also logs the raw request body for easier debugging of 4xx/5xx issues.
func ImportStory(w http.ResponseWriter, r *http.Request) {
	if err := ensureLoaded(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Read and log the raw body (bounded) to help diagnose import problems.
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB guard
	if err != nil {
		log.Printf("ImportStory: read body error: %v", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read request body"})
		return
	}
	_ = r.Body.Close()
	log.Printf("ImportStory: raw body: %s", string(raw))

	var payload importPayload
	dec := json.NewDecoder(bytes.NewReader(raw))
	// We intentionally do NOT DisallowUnknownFields so extra keys like detailsByParagraph are ignored.
	if err := dec.Decode(&payload); err != nil {
		log.Printf("ImportStory: json decode error: %v", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	story, paragraphs, nodeMap, err := dataStore.importStory(payload)
	if err != nil {
		log.Printf("ImportStory: validation/store error: %v", err)
		// Treat bad input as 422 Unprocessable Entity for clarity
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	// Optional persistence
	if storiesTable != "" {
		if derr := ddbPutStoryBundle(r.Context(), story, paragraphs, nodeMap); derr != nil {
			log.Printf("ImportStory: ddbPutStoryBundle error: %v", derr)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"id":      story.StoryID,
		"message": "story imported (in-memory)",
	})
}

type strukturResponse struct {
	ID               string              `json:"id"`
	Nodes            []Node              `json:"nodes"`
	Edges            []Edge              `json:"edges"`
	StoryID          string              `json:"storyId"`
	Story            *Story              `json:"story,omitempty"`
	Paragraphs       []Paragraph         `json:"paragraphs,omitempty"`
	ParagraphNodeMap map[string][]string `json:"paragraphNodeMap,omitempty"`
}

// GetStrukturByStory returns the graph and narrative bundle for a story.
func GetStrukturByStory(w http.ResponseWriter, r *http.Request) {
	if err := ensureLoaded(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	storyID := mux.Vars(r)["storyId"]
	if strings.TrimSpace(storyID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing storyId"})
		return
	}
	story, paragraphs, nodeMap, ok := dataStore.getStoryFull(storyID)
	if !ok && storiesTable != "" {
		if st, paras, nmap, found, derr := ddbGetStoryBundle(r.Context(), storyID); derr == nil && found {
			dataStore.setStoryBundle(st, paras, nmap)
			story, paragraphs, nodeMap, ok = dataStore.getStoryFull(storyID)
		} else if derr != nil {
			log.Printf("GetStrukturByStory: ddbGetStoryBundle error: %v", derr)
		}
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "story not found"})
		return
	}
	nodes, edges := dataStore.getGraph(storyID)
	if len(nodes) == 0 && graphsTable != "" {
		if ns, es, found, derr := ddbGetGraph(r.Context(), storyID); derr == nil && found {
			dataStore.setGraph(storyID, ns, es)
			nodes, edges = dataStore.getGraph(storyID)
		}
	}
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
		StoryID:          storyID,
		Story:            &storyCopy,
		Paragraphs:       paragraphs,
		ParagraphNodeMap: nodeMap,
	}
	writeJSON(w, http.StatusOK, resp)
}

type submitPayload struct {
	StoryID string `json:"storyId"`
	Nodes   []Node `json:"nodes"`
	Edges   []Edge `json:"edges"`
}

// SubmitHandler upserts nodes and edges for a story.
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
	if strings.TrimSpace(payload.StoryID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "storyId is required"})
		return
	}
	dataStore.upsertGraph(payload.StoryID, payload.Nodes, payload.Edges)
	if graphsTable != "" {
		ns, es := dataStore.getGraph(payload.StoryID)
		if derr := ddbPutGraph(r.Context(), payload.StoryID, ns, es); derr != nil {
			log.Printf("SubmitHandler: ddbPutGraph error: %v", derr)
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DeleteNode removes a node, its edges and paragraph references.
func DeleteNode(w http.ResponseWriter, r *http.Request) {
	if err := ensureLoaded(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	vars := mux.Vars(r)
	storyID := vars["storyId"]
	nodeID := vars["nodeId"]
	if strings.TrimSpace(storyID) == "" || strings.TrimSpace(nodeID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing storyId or nodeId"})
		return
	}
	if !dataStore.deleteNode(storyID, nodeID) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "node not found"})
		return
	}
	if graphsTable != "" {
		ns, es := dataStore.getGraph(storyID)
		if derr := ddbPutGraph(r.Context(), storyID, ns, es); derr != nil {
			log.Printf("DeleteNode: ddbPutGraph error: %v", derr)
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
