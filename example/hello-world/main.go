// hello-world is the smallest useful Rocketsled app: a JSON endpoint packaged as
// a Lambda container image. Rocketsled builds it from this directory, pushes it to
// the environment's registry, and points the aws-lambda-function component at it.
package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

// version is stamped by the Dockerfile from the commit Rocketsled built.
var version = "dev"

type response struct {
	Message string            `json:"message"`
	Path    string            `json:"path"`
	Method  string            `json:"method"`
	Version string            `json:"version"`
	Time    time.Time         `json:"time"`
	Query   map[string]string `json:"query,omitempty"`
}

func handle(_ context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	body, err := json.Marshal(response{
		Message: greeting(),
		Path:    req.RawPath,
		Method:  req.RequestContext.HTTP.Method,
		Version: version,
		Time:    time.Now().UTC(),
		Query:   req.QueryStringParameters,
	})
	if err != nil {
		return events.APIGatewayV2HTTPResponse{StatusCode: http.StatusInternalServerError}, err
	}
	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       string(body),
	}, nil
}

// greeting reads GREETING so the same image can be reconfigured per environment.
func greeting() string {
	if g := os.Getenv("GREETING"); g != "" {
		return g
	}
	return "hello, world"
}

func main() {
	lambda.Start(handle)
}
