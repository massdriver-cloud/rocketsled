package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/aws/aws-lambda-go/events"
)

func TestHandleReturnsJSON(t *testing.T) {
	t.Setenv("GREETING", "howdy")
	req := events.APIGatewayV2HTTPRequest{RawPath: "/hello", QueryStringParameters: map[string]string{"name": "sled"}}
	req.RequestContext.HTTP.Method = "GET"

	res, err := handle(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
	var body response
	if err := json.Unmarshal([]byte(res.Body), &body); err != nil {
		t.Fatal(err)
	}
	if body.Message != "howdy" || body.Path != "/hello" || body.Method != "GET" || body.Query["name"] != "sled" {
		t.Fatalf("unexpected body: %+v", body)
	}
}
