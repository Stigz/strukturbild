package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	storyapi "strukturbild/api"
)

type memoryDynamo struct {
	mu    sync.Mutex
	items map[string]map[string]map[string]types.AttributeValue
}

func newMemoryDynamo() *memoryDynamo {
	return &memoryDynamo{items: make(map[string]map[string]map[string]types.AttributeValue)}
}

func cloneAttrMap(src map[string]types.AttributeValue) map[string]types.AttributeValue {
	cloned := make(map[string]types.AttributeValue, len(src))
	for k, v := range src {
		cloned[k] = cloneAttr(v)
	}
	return cloned
}

func cloneAttr(attr types.AttributeValue) types.AttributeValue {
	switch v := attr.(type) {
	case *types.AttributeValueMemberS:
		return &types.AttributeValueMemberS{Value: v.Value}
	case *types.AttributeValueMemberN:
		return &types.AttributeValueMemberN{Value: v.Value}
	case *types.AttributeValueMemberBOOL:
		return &types.AttributeValueMemberBOOL{Value: v.Value}
	case *types.AttributeValueMemberL:
		out := make([]types.AttributeValue, len(v.Value))
		for i, child := range v.Value {
			out[i] = cloneAttr(child)
		}
		return &types.AttributeValueMemberL{Value: out}
	case *types.AttributeValueMemberM:
		return &types.AttributeValueMemberM{Value: cloneAttrMap(v.Value)}
	default:
		return attr
	}
}

func (m *memoryDynamo) PutItem(ctx context.Context, input *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	pk := getStringAttr(input.Item["storyId"])
	sk := getStringAttr(input.Item["id"])
	if pk == "" || sk == "" {
		return nil, fmt.Errorf("missing keys")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	bucket, ok := m.items[pk]
	if !ok {
		bucket = make(map[string]map[string]types.AttributeValue)
		m.items[pk] = bucket
	}
	bucket[sk] = cloneAttrMap(input.Item)
	return &dynamodb.PutItemOutput{}, nil
}

func (m *memoryDynamo) Query(ctx context.Context, input *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	pk := getStringAttr(input.ExpressionAttributeValues[":sid"])
	m.mu.Lock()
	bucket := m.items[pk]
	m.mu.Unlock()
	if bucket == nil {
		return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{}}, nil
	}
	items := make([]map[string]types.AttributeValue, 0, len(bucket))
	for _, item := range bucket {
		if matchesFilter(item, input.FilterExpression, input.ExpressionAttributeValues) {
			items = append(items, cloneAttrMap(item))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return getStringAttr(items[i]["id"]) < getStringAttr(items[j]["id"])
	})
	return &dynamodb.QueryOutput{Items: items}, nil
}

func (m *memoryDynamo) DeleteItem(ctx context.Context, input *dynamodb.DeleteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	pk := getStringAttr(input.Key["storyId"])
	sk := getStringAttr(input.Key["id"])
	m.mu.Lock()
	defer m.mu.Unlock()
	if bucket, ok := m.items[pk]; ok {
		delete(bucket, sk)
		if len(bucket) == 0 {
			delete(m.items, pk)
		}
	}
	return &dynamodb.DeleteItemOutput{}, nil
}

func (m *memoryDynamo) GetItem(ctx context.Context, input *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	pk := getStringAttr(input.Key["storyId"])
	sk := getStringAttr(input.Key["id"])
	m.mu.Lock()
	defer m.mu.Unlock()
	if bucket, ok := m.items[pk]; ok {
		if item, ok := bucket[sk]; ok {
			return &dynamodb.GetItemOutput{Item: cloneAttrMap(item)}, nil
		}
	}
	return &dynamodb.GetItemOutput{}, nil
}

func (m *memoryDynamo) Scan(ctx context.Context, input *dynamodb.ScanInput, optFns ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var items []map[string]types.AttributeValue
	for _, bucket := range m.items {
		for _, item := range bucket {
			if matchesFilter(item, input.FilterExpression, input.ExpressionAttributeValues) {
				items = append(items, cloneAttrMap(item))
			}
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return getStringAttr(items[i]["id"]) < getStringAttr(items[j]["id"])
	})
	return &dynamodb.ScanOutput{Items: items}, nil
}

func matchesFilter(item map[string]types.AttributeValue, filter *string, expr map[string]types.AttributeValue) bool {
	if filter == nil || *filter == "" {
		return true
	}
	trimmed := strings.TrimSpace(*filter)
	switch {
	case trimmed == "paragraphId = :paragraphId":
		want := getStringAttr(expr[":paragraphId"])
		return getStringAttr(item["paragraphId"]) == want
	case strings.HasPrefix(trimmed, "begins_with(") && strings.HasSuffix(trimmed, ")"):
		inner := strings.TrimSuffix(strings.TrimPrefix(trimmed, "begins_with("), ")")
		parts := strings.Split(inner, ",")
		if len(parts) != 2 {
			return true
		}
		field := strings.TrimSpace(parts[0])
		token := strings.TrimSpace(parts[1])
		attr := item[field]
		prefix := getStringAttr(expr[token])
		if v, ok := attr.(*types.AttributeValueMemberS); ok {
			return strings.HasPrefix(v.Value, prefix)
		}
		return false
	default:
		return true
	}
}

func getStringAttr(attr types.AttributeValue) string {
	if v, ok := attr.(*types.AttributeValueMemberS); ok {
		return v.Value
	}
	return ""
}

func setupTestServices() {
	mem := newMemoryDynamo()
	svc = mem
	storySvc = storyapi.NewStoryService(svc, tableName, corsHeaders)
}

var _ storyapi.DynamoClient = (*memoryDynamo)(nil)
