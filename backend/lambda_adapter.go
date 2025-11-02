package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

var (
	lambdaRouter     http.Handler
	lambdaRouterOnce sync.Once
	lambdaRouterMu   sync.RWMutex
)

func runningInLambda() bool {
	_, ok := os.LookupEnv("AWS_LAMBDA_RUNTIME_API")
	return ok
}

func startLambda(router http.Handler) {
	setLambdaRouter(router)
	lambda.Start(func(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
		return serveLambdaRequest(ctx, router, req)
	})
}

func setLambdaRouter(router http.Handler) {
	lambdaRouterMu.Lock()
	lambdaRouter = router
	lambdaRouterMu.Unlock()
}

func getLambdaRouter() http.Handler {
	lambdaRouterMu.RLock()
	r := lambdaRouter
	lambdaRouterMu.RUnlock()
	if r != nil {
		return r
	}
	lambdaRouterOnce.Do(func() {
		lambdaRouterMu.Lock()
		defer lambdaRouterMu.Unlock()
		if lambdaRouter == nil {
			lambdaRouter = newRouter()
		}
	})
	lambdaRouterMu.RLock()
	r = lambdaRouter
	lambdaRouterMu.RUnlock()
	return r
}

func handler(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	return serveLambdaRequest(ctx, getLambdaRouter(), req)
}

func getHandler(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	return serveLambdaRequest(ctx, getLambdaRouter(), req)
}

func serveLambdaRequest(ctx context.Context, router http.Handler, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	if err := ensureLoaded(); err != nil {
		log.Printf("lambda: ensureLoaded error: %v", err)
		return jsonError(http.StatusInternalServerError, "failed to initialize"), nil
	}

	httpReq, err := buildHTTPRequest(ctx, req)
	if err != nil {
		log.Printf("lambda: build request error: %v", err)
		return jsonError(http.StatusBadRequest, "invalid request"), nil
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httpReq)
	res := rec.Result()
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)

	headers := make(map[string]string, len(res.Header))
	for k, values := range res.Header {
		if len(values) == 0 {
			continue
		}
		headers[k] = values[len(values)-1]
	}

	return events.APIGatewayProxyResponse{
		StatusCode:      res.StatusCode,
		Headers:         headers,
		Body:            string(body),
		IsBase64Encoded: false,
	}, nil
}

func buildHTTPRequest(ctx context.Context, req events.APIGatewayProxyRequest) (*http.Request, error) {
	var bodyReader io.Reader
	if req.IsBase64Encoded {
		data, err := base64.StdEncoding.DecodeString(req.Body)
		if err != nil {
			return nil, fmt.Errorf("decode body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	} else {
		bodyReader = strings.NewReader(req.Body)
	}

	path := req.Path
	if path == "" {
		path = "/"
	}

	query := url.Values{}
	for k, values := range req.MultiValueQueryStringParameters {
		for _, v := range values {
			query.Add(k, v)
		}
	}
	for k, v := range req.QueryStringParameters {
		if _, ok := req.MultiValueQueryStringParameters[k]; ok {
			continue
		}
		query.Add(k, v)
	}
	if raw := query.Encode(); raw != "" {
		path = path + "?" + raw
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.HTTPMethod, path, bodyReader)
	if err != nil {
		return nil, err
	}
	for k, v := range req.Headers {
		if v == "" {
			continue
		}
		httpReq.Header.Set(k, v)
	}
	if req.RequestContext.Identity.SourceIP != "" {
		httpReq.RemoteAddr = req.RequestContext.Identity.SourceIP
	}
	return httpReq, nil
}

func jsonError(status int, message string) events.APIGatewayProxyResponse {
	body := fmt.Sprintf(`{"error":"%s"}`, message)
	return events.APIGatewayProxyResponse{
		StatusCode: status,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: body,
	}
}
