package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/google/uuid"
)

// DynamoClient defines the subset of DynamoDB operations used by the story service.
type DynamoClient interface {
	PutItem(context.Context, *dynamodb.PutItemInput, ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	Query(context.Context, *dynamodb.QueryInput, ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
	DeleteItem(context.Context, *dynamodb.DeleteItemInput, ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error)
	GetItem(context.Context, *dynamodb.GetItemInput, ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
}

// StoryService bundles the handlers for the Story API.
type StoryService struct {
	dynamo     DynamoClient
	tableName  string
	corsSource func() map[string]string
}

func NewStoryService(client DynamoClient, tableName string, cors func() map[string]string) *StoryService {
	return &StoryService{dynamo: client, tableName: tableName, corsSource: cors}
}

// Data model payloads --------------------------------------------------------

type Story struct {
	StoryID   string `json:"storyId"`
	SchoolID  string `json:"schoolId"`
	Title     string `json:"title"`
	CreatedAt string `json:"createdAt,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

type Citation struct {
	TranscriptID string `json:"transcriptId"`
	Minutes      []int  `json:"minutes"`
}

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

type Detail struct {
	DetailID     string `json:"detailId"`
	StoryID      string `json:"storyId"`
	ParagraphID  string `json:"paragraphId"`
	Kind         string `json:"kind"`
	TranscriptID string `json:"transcriptId"`
	StartMinute  int    `json:"startMinute"`
	EndMinute    int    `json:"endMinute"`
	Text         string `json:"text"`
}

type StoryFull struct {
	Story              Story               `json:"story"`
	Paragraphs         []Paragraph         `json:"paragraphs"`
	DetailsByParagraph map[string][]Detail `json:"detailsByParagraph"`
}

// Internal representations used for DynamoDB marshaling ----------------------

type storyRecord struct {
	PersonID string `dynamodbav:"personId"`
	ID       string `dynamodbav:"id"`
	Story
}

type paragraphRecord struct {
	PersonID    string     `dynamodbav:"personId"`
	ID          string     `dynamodbav:"id"`
	ParagraphID string     `dynamodbav:"paragraphId"`
	StoryID     string     `dynamodbav:"storyId"`
	Index       int        `dynamodbav:"index"`
	Title       string     `dynamodbav:"title,omitempty"`
	BodyMd      string     `dynamodbav:"bodyMd"`
	Citations   []Citation `dynamodbav:"citations"`
	CreatedAt   string     `dynamodbav:"createdAt"`
	UpdatedAt   string     `dynamodbav:"updatedAt"`
}

type detailRecord struct {
	PersonID     string `dynamodbav:"personId"`
	ID           string `dynamodbav:"id"`
	DetailID     string `dynamodbav:"detailId"`
	StoryID      string `dynamodbav:"storyId"`
	ParagraphID  string `dynamodbav:"paragraphId"`
	Kind         string `dynamodbav:"kind"`
	TranscriptID string `dynamodbav:"transcriptId"`
	StartMinute  int    `dynamodbav:"startMinute"`
	EndMinute    int    `dynamodbav:"endMinute"`
	Text         string `dynamodbav:"text"`
}

// Handler entrypoints --------------------------------------------------------

func (s *StoryService) HandleCreateStory(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	var payload struct {
		StoryID  string `json:"storyId"`
		SchoolID string `json:"schoolId"`
		Title    string `json:"title"`
	}
	if err := json.Unmarshal([]byte(req.Body), &payload); err != nil {
		return s.errorResponse(400, "Invalid JSON payload")
	}
	if strings.TrimSpace(payload.SchoolID) == "" || strings.TrimSpace(payload.Title) == "" {
		return s.errorResponse(400, "schoolId and title are required")
	}
	storyID := payload.StoryID
	if strings.TrimSpace(storyID) == "" {
		storyID = fmt.Sprintf("story-%s", uuid.New().String())
	}
	now := time.Now().UTC().Format(time.RFC3339)
	record := storyRecord{
		PersonID: fmt.Sprintf("STORY#%s", storyID),
		ID:       fmt.Sprintf("STORY#%s", storyID),
		Story: Story{
			StoryID:   storyID,
			SchoolID:  payload.SchoolID,
			Title:     payload.Title,
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	item, err := attributevalue.MarshalMap(record)
	if err != nil {
		return s.errorResponse(500, "Failed to marshal story")
	}
	_, err = s.dynamo.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &s.tableName,
		Item:      item,
	})
	if err != nil {
		return s.errorResponse(500, fmt.Sprintf("Failed to save story: %v", err))
	}
	return s.jsonResponse(200, map[string]string{"id": storyID})
}

func (s *StoryService) HandleCreateParagraph(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	storyID := req.PathParameters["storyId"]
	if storyID == "" {
		return s.errorResponse(400, "Missing storyId in path")
	}
	var payload struct {
		Index     int        `json:"index"`
		Title     string     `json:"title"`
		BodyMd    string     `json:"bodyMd"`
		Citations []Citation `json:"citations"`
	}
	if err := json.Unmarshal([]byte(req.Body), &payload); err != nil {
		return s.errorResponse(400, "Invalid JSON payload")
	}
	if payload.Index < 1 {
		return s.errorResponse(400, "index must be >= 1")
	}
	if err := validateCitations(payload.Citations); err != nil {
		return s.errorResponse(400, err.Error())
	}
	paragraphID := fmt.Sprintf("para-%s", uuid.New().String())
	now := time.Now().UTC().Format(time.RFC3339)
	record := paragraphRecord{
		PersonID:    fmt.Sprintf("STORY#%s", storyID),
		ID:          paragraphSortKey(payload.Index, paragraphID),
		ParagraphID: paragraphID,
		StoryID:     storyID,
		Index:       payload.Index,
		Title:       strings.TrimSpace(payload.Title),
		BodyMd:      payload.BodyMd,
		Citations:   payload.Citations,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	item, err := attributevalue.MarshalMap(record)
	if err != nil {
		return s.errorResponse(500, "Failed to marshal paragraph")
	}
	_, err = s.dynamo.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &s.tableName,
		Item:      item,
	})
	if err != nil {
		return s.errorResponse(500, fmt.Sprintf("Failed to save paragraph: %v", err))
	}
	return s.jsonResponse(200, map[string]string{"id": paragraphID})
}

func (s *StoryService) HandleUpdateParagraph(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	paragraphID := req.PathParameters["paragraphId"]
	if paragraphID == "" {
		return s.errorResponse(400, "Missing paragraphId in path")
	}
	var payload struct {
		StoryID   string      `json:"storyId"`
		Index     *int        `json:"index"`
		Title     *string     `json:"title"`
		BodyMd    *string     `json:"bodyMd"`
		Citations *[]Citation `json:"citations"`
	}
	if err := json.Unmarshal([]byte(req.Body), &payload); err != nil {
		return s.errorResponse(400, "Invalid JSON payload")
	}
	if strings.TrimSpace(payload.StoryID) == "" {
		return s.errorResponse(400, "storyId is required in body")
	}
	if payload.Index != nil && *payload.Index < 1 {
		return s.errorResponse(400, "index must be >= 1")
	}
	if payload.Citations != nil {
		if err := validateCitations(*payload.Citations); err != nil {
			return s.errorResponse(400, err.Error())
		}
	}
	existing, err := s.getParagraph(ctx, payload.StoryID, paragraphID)
	if err != nil {
		return s.errorResponse(404, err.Error())
	}
	if payload.Index != nil {
		existing.Index = *payload.Index
	}
	if payload.Title != nil {
		existing.Title = strings.TrimSpace(*payload.Title)
	}
	if payload.BodyMd != nil {
		existing.BodyMd = *payload.BodyMd
	}
	if payload.Citations != nil {
		existing.Citations = *payload.Citations
	}
	existing.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	newID := paragraphSortKey(existing.Index, existing.ParagraphID)
	newRecord := paragraphRecord{
		PersonID:    fmt.Sprintf("STORY#%s", existing.StoryID),
		ID:          newID,
		ParagraphID: existing.ParagraphID,
		StoryID:     existing.StoryID,
		Index:       existing.Index,
		Title:       existing.Title,
		BodyMd:      existing.BodyMd,
		Citations:   existing.Citations,
		CreatedAt:   existing.CreatedAt,
		UpdatedAt:   existing.UpdatedAt,
	}
	item, err := attributevalue.MarshalMap(newRecord)
	if err != nil {
		return s.errorResponse(500, "Failed to marshal paragraph")
	}
	_, err = s.dynamo.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &s.tableName,
		Item:      item,
	})
	if err != nil {
		return s.errorResponse(500, fmt.Sprintf("Failed to update paragraph: %v", err))
	}
	if newID != existing.ID {
		_, _ = s.dynamo.DeleteItem(ctx, &dynamodb.DeleteItemInput{
			TableName: &s.tableName,
			Key: map[string]types.AttributeValue{
				"personId": &types.AttributeValueMemberS{Value: fmt.Sprintf("STORY#%s", existing.StoryID)},
				"id":       &types.AttributeValueMemberS{Value: existing.ID},
			},
		})
	}
	return s.jsonResponse(200, map[string]string{"id": existing.ParagraphID})
}

func (s *StoryService) HandleCreateDetail(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	paragraphID := req.PathParameters["paragraphId"]
	if paragraphID == "" {
		return s.errorResponse(400, "Missing paragraphId in path")
	}
	var payload struct {
		StoryID      string `json:"storyId"`
		Kind         string `json:"kind"`
		TranscriptID string `json:"transcriptId"`
		StartMinute  int    `json:"startMinute"`
		EndMinute    int    `json:"endMinute"`
		Text         string `json:"text"`
	}
	if err := json.Unmarshal([]byte(req.Body), &payload); err != nil {
		return s.errorResponse(400, "Invalid JSON payload")
	}
	if strings.TrimSpace(payload.StoryID) == "" {
		return s.errorResponse(400, "storyId is required in body")
	}
	if strings.TrimSpace(payload.Kind) != "quote" {
		return s.errorResponse(400, "kind must be 'quote'")
	}
	if payload.StartMinute < 0 || payload.EndMinute < 0 {
		return s.errorResponse(400, "startMinute and endMinute must be >= 0")
	}
	detailID := fmt.Sprintf("det-%s", uuid.New().String())
	record := detailRecord{
		PersonID:     fmt.Sprintf("STORY#%s", payload.StoryID),
		ID:           fmt.Sprintf("DET#%s#%s", paragraphID, detailID),
		DetailID:     detailID,
		StoryID:      payload.StoryID,
		ParagraphID:  paragraphID,
		Kind:         payload.Kind,
		TranscriptID: payload.TranscriptID,
		StartMinute:  payload.StartMinute,
		EndMinute:    payload.EndMinute,
		Text:         payload.Text,
	}
	item, err := attributevalue.MarshalMap(record)
	if err != nil {
		return s.errorResponse(500, "Failed to marshal detail")
	}
	_, err = s.dynamo.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &s.tableName,
		Item:      item,
	})
	if err != nil {
		return s.errorResponse(500, fmt.Sprintf("Failed to save detail: %v", err))
	}
	return s.jsonResponse(200, map[string]string{"id": detailID})
}

func (s *StoryService) HandleGetFullStory(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	storyID := req.PathParameters["storyId"]
	if storyID == "" {
		return s.errorResponse(400, "Missing storyId in path")
	}
	story, paragraphs, details, err := s.fetchStoryBundle(ctx, storyID)
	if err != nil {
		return s.errorResponse(404, err.Error())
	}
	detailsByParagraph := map[string][]Detail{}
	for _, det := range details {
		detailsByParagraph[det.ParagraphID] = append(detailsByParagraph[det.ParagraphID], det)
	}
	sort.Slice(paragraphs, func(i, j int) bool {
		return paragraphs[i].Index < paragraphs[j].Index
	})
	payload := StoryFull{
		Story:              story,
		Paragraphs:         paragraphs,
		DetailsByParagraph: detailsByParagraph,
	}
	return s.jsonResponse(200, payload)
}

func (s *StoryService) HandleImportStory(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	var payload struct {
		Story      Story `json:"story"`
		Paragraphs []struct {
			Index     int        `json:"index"`
			Title     string     `json:"title"`
			BodyMd    string     `json:"bodyMd"`
			Citations []Citation `json:"citations"`
		} `json:"paragraphs"`
		Details []struct {
			ParagraphIndex int    `json:"paragraphIndex"`
			Kind           string `json:"kind"`
			TranscriptID   string `json:"transcriptId"`
			StartMinute    int    `json:"startMinute"`
			EndMinute      int    `json:"endMinute"`
			Text           string `json:"text"`
		} `json:"details"`
	}
	if err := json.Unmarshal([]byte(req.Body), &payload); err != nil {
		return s.errorResponse(400, "Invalid JSON payload")
	}
	if strings.TrimSpace(payload.Story.SchoolID) == "" || strings.TrimSpace(payload.Story.Title) == "" {
		return s.errorResponse(400, "story.schoolId and story.title are required")
	}
	storyID := strings.TrimSpace(payload.Story.StoryID)
	if storyID == "" {
		storyID = fmt.Sprintf("story-%s", uuid.New().String())
	}
	payload.Story.StoryID = storyID
	now := time.Now().UTC().Format(time.RFC3339)
	existingStory, existingParagraphs, existingDetails, _ := s.fetchStoryBundle(ctx, storyID)
	storyRecord := storyRecord{
		PersonID: fmt.Sprintf("STORY#%s", storyID),
		ID:       fmt.Sprintf("STORY#%s", storyID),
		Story: Story{
			StoryID:   storyID,
			SchoolID:  payload.Story.SchoolID,
			Title:     payload.Story.Title,
			CreatedAt: chooseNonEmpty(existingStory.CreatedAt, now),
			UpdatedAt: now,
		},
	}
	item, err := attributevalue.MarshalMap(storyRecord)
	if err != nil {
		return s.errorResponse(500, "Failed to marshal story")
	}
	_, err = s.dynamo.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &s.tableName,
		Item:      item,
	})
	if err != nil {
		return s.errorResponse(500, fmt.Sprintf("Failed to save story: %v", err))
	}
	// Remove existing paragraphs and details before recreating to avoid duplicates
	for _, detail := range existingDetails {
		_, _ = s.dynamo.DeleteItem(ctx, &dynamodb.DeleteItemInput{
			TableName: &s.tableName,
			Key: map[string]types.AttributeValue{
				"personId": &types.AttributeValueMemberS{Value: fmt.Sprintf("STORY#%s", storyID)},
				"id":       &types.AttributeValueMemberS{Value: fmt.Sprintf("DET#%s#%s", detail.ParagraphID, detail.DetailID)},
			},
		})
	}
	for _, paragraph := range existingParagraphs {
		_, _ = s.dynamo.DeleteItem(ctx, &dynamodb.DeleteItemInput{
			TableName: &s.tableName,
			Key: map[string]types.AttributeValue{
				"personId": &types.AttributeValueMemberS{Value: fmt.Sprintf("STORY#%s", storyID)},
				"id":       &types.AttributeValueMemberS{Value: paragraphSortKey(paragraph.Index, paragraph.ParagraphID)},
			},
		})
	}
	paragraphByIndex := map[int]paragraphRecord{}
	for _, p := range payload.Paragraphs {
		if p.Index < 1 {
			return s.errorResponse(400, "paragraph index must be >= 1")
		}
		if err := validateCitations(p.Citations); err != nil {
			return s.errorResponse(400, err.Error())
		}
		record := paragraphRecord{
			PersonID:    fmt.Sprintf("STORY#%s", storyID),
			ParagraphID: fmt.Sprintf("para-%s", uuid.New().String()),
			StoryID:     storyID,
			Index:       p.Index,
			Title:       strings.TrimSpace(p.Title),
			BodyMd:      p.BodyMd,
			Citations:   p.Citations,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		record.ID = paragraphSortKey(record.Index, record.ParagraphID)
		item, err := attributevalue.MarshalMap(record)
		if err != nil {
			return s.errorResponse(500, "Failed to marshal paragraph")
		}
		_, err = s.dynamo.PutItem(ctx, &dynamodb.PutItemInput{
			TableName: &s.tableName,
			Item:      item,
		})
		if err != nil {
			return s.errorResponse(500, fmt.Sprintf("Failed to save paragraph: %v", err))
		}
		paragraphByIndex[p.Index] = record
	}
	for _, det := range payload.Details {
		if det.Kind != "quote" {
			return s.errorResponse(400, "detail.kind must be 'quote'")
		}
		if det.ParagraphIndex < 1 {
			return s.errorResponse(400, "detail.paragraphIndex must be >= 1")
		}
		paraRecord, ok := paragraphByIndex[det.ParagraphIndex]
		if !ok {
			return s.errorResponse(400, fmt.Sprintf("No paragraph for index %d", det.ParagraphIndex))
		}
		if det.StartMinute < 0 || det.EndMinute < 0 {
			return s.errorResponse(400, "detail minutes must be >= 0")
		}
		detailID := fmt.Sprintf("det-%s", uuid.New().String())
		record := detailRecord{
			PersonID:     fmt.Sprintf("STORY#%s", storyID),
			ID:           fmt.Sprintf("DET#%s#%s", paraRecord.ParagraphID, detailID),
			DetailID:     detailID,
			StoryID:      storyID,
			ParagraphID:  paraRecord.ParagraphID,
			Kind:         det.Kind,
			TranscriptID: det.TranscriptID,
			StartMinute:  det.StartMinute,
			EndMinute:    det.EndMinute,
			Text:         det.Text,
		}
		item, err := attributevalue.MarshalMap(record)
		if err != nil {
			return s.errorResponse(500, "Failed to marshal detail")
		}
		_, err = s.dynamo.PutItem(ctx, &dynamodb.PutItemInput{
			TableName: &s.tableName,
			Item:      item,
		})
		if err != nil {
			return s.errorResponse(500, fmt.Sprintf("Failed to save detail: %v", err))
		}
	}
	return s.jsonResponse(200, map[string]string{"id": storyID})
}

// Helpers --------------------------------------------------------------------

func (s *StoryService) jsonResponse(status int, payload interface{}) (events.APIGatewayProxyResponse, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return events.APIGatewayProxyResponse{StatusCode: 500, Headers: s.corsSource(), Body: "Failed to encode response"}, nil
	}
	return events.APIGatewayProxyResponse{StatusCode: status, Headers: s.corsSource(), Body: string(body)}, nil
}

func (s *StoryService) errorResponse(status int, message string) (events.APIGatewayProxyResponse, error) {
	payload := map[string]string{"error": message}
	body, _ := json.Marshal(payload)
	return events.APIGatewayProxyResponse{StatusCode: status, Headers: s.corsSource(), Body: string(body)}, nil
}

func (s *StoryService) getParagraph(ctx context.Context, storyID, paragraphID string) (*paragraphRecord, error) {
	pk := fmt.Sprintf("STORY#%s", storyID)
	filter := "paragraphId = :paragraphId"
	result, err := s.dynamo.Query(ctx, &dynamodb.QueryInput{
		TableName:              &s.tableName,
		KeyConditionExpression: awsString("personId = :pid"),
		FilterExpression:       &filter,
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pid":         &types.AttributeValueMemberS{Value: pk},
			":paragraphId": &types.AttributeValueMemberS{Value: paragraphID},
		},
	})
	if err != nil {
		return nil, err
	}
	for _, item := range result.Items {
		var record paragraphRecord
		if err := attributevalue.UnmarshalMap(item, &record); err != nil {
			return nil, err
		}
		return &record, nil
	}
	return nil, errors.New("paragraph not found")
}

func (s *StoryService) fetchStoryBundle(ctx context.Context, storyID string) (Story, []Paragraph, []Detail, error) {
	pk := fmt.Sprintf("STORY#%s", storyID)
	result, err := s.dynamo.Query(ctx, &dynamodb.QueryInput{
		TableName:              &s.tableName,
		KeyConditionExpression: awsString("personId = :pid"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pid": &types.AttributeValueMemberS{Value: pk},
		},
	})
	if err != nil {
		return Story{}, nil, nil, err
	}
	var story Story
	var storyFound bool
	var paragraphs []Paragraph
	var details []Detail
	for _, item := range result.Items {
		if idAttr, ok := item["id"].(*types.AttributeValueMemberS); ok {
			switch {
			case strings.HasPrefix(idAttr.Value, "STORY#"):
				var rec storyRecord
				if err := attributevalue.UnmarshalMap(item, &rec); err == nil {
					story = rec.Story
					storyFound = true
				}
			case strings.HasPrefix(idAttr.Value, "PARA#"):
				var rec paragraphRecord
				if err := attributevalue.UnmarshalMap(item, &rec); err == nil {
					paragraphs = append(paragraphs, Paragraph{
						ParagraphID: rec.ParagraphID,
						StoryID:     rec.StoryID,
						Index:       rec.Index,
						Title:       rec.Title,
						BodyMd:      rec.BodyMd,
						Citations:   rec.Citations,
						CreatedAt:   rec.CreatedAt,
						UpdatedAt:   rec.UpdatedAt,
					})
				}
			case strings.HasPrefix(idAttr.Value, "DET#"):
				var rec detailRecord
				if err := attributevalue.UnmarshalMap(item, &rec); err == nil {
					details = append(details, Detail{
						DetailID:     rec.DetailID,
						StoryID:      rec.StoryID,
						ParagraphID:  rec.ParagraphID,
						Kind:         rec.Kind,
						TranscriptID: rec.TranscriptID,
						StartMinute:  rec.StartMinute,
						EndMinute:    rec.EndMinute,
						Text:         rec.Text,
					})
				}
			}
		}
	}
	if !storyFound {
		return Story{}, nil, nil, errors.New("story not found")
	}
	sort.Slice(paragraphs, func(i, j int) bool {
		return paragraphs[i].Index < paragraphs[j].Index
	})
	return story, paragraphs, details, nil
}

func paragraphSortKey(index int, paragraphID string) string {
	return fmt.Sprintf("PARA#%04d#%s", index, paragraphID)
}

func validateCitations(citations []Citation) error {
	for _, c := range citations {
		if strings.TrimSpace(c.TranscriptID) == "" {
			return errors.New("citations require transcriptId")
		}
		for _, m := range c.Minutes {
			if m < 0 {
				return errors.New("citation minutes must be >= 0")
			}
		}
	}
	return nil
}

func chooseNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func awsString(v string) *string {
	return &v
}
