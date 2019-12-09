package awseventadapter

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/aws/aws-lambda-go/events"
	"github.com/pkg/errors"
)

const (
	// CustomHostVariable is the name of the environment variable that contains
	// the custom hostname for the request. If this variable is not set the framework
	// reverts to `DefaultServerAddress`. The value for a custom host should include
	// a protocol: http://my-custom.host.com
	CustomHostVariable = "GO_API_HOST"

	// DefaultServerAddress is prepended to the path of each incoming reuqest
	DefaultServerAddress = "https://aws-serverless-go-api.com"

	// APIGwContextHeader is the custom header key used to store the
	// API Gateway context. To access the Context properties use the
	// GetAPIGatewayContext method of the RequestAccessor object.
	APIGwContextHeader = "X-GoLambdaProxy-ApiGw-Context"

	// APIGwStageVarsHeader is the custom header key used to store the
	// API Gateway stage variables. To access the stage variable values
	// use the GetAPIGatewayStageVars method of the RequestAccessor object.
	APIGwStageVarsHeader = "X-GoLambdaProxy-ApiGw-StageVars"

	contentTypeHeaderKey = "Content-Type"
)

// AdapterRequest is a struct that contains fields required to produce either
// an events.APIGatewayResponse or events.ALBTargetGroupResponse
type AdapterRequest struct {
	Resource                        string              `json:"resource"`
	Path                            string              `json:"path"`
	HTTPMethod                      string              `json:"httpMethod"`
	Headers                         map[string]string   `json:"headers,omitempty"`
	MultiValueHeaders               map[string][]string `json:"multiValueHeaders,omitempty"`
	QueryStringParameters           map[string]string   `json:"queryStringParameters,omitempty"`
	MultiValueQueryStringParameters map[string][]string `json:"multiValueQueryStringParameters,omitempty"`
	PathParameters                  map[string]string   `json:"pathParameters"`
	StageVariables                  map[string]string   `json:"stageVariables"`
	RequestContext                  interface{}         `json:"requestContext"`
	Body                            string              `json:"body"`
	IsBase64Encoded                 bool                `json:"isBase64Encoded,omitempty"`
	stripBasePath                   string
}

// According to the docs, defer in a wrapped handler will fire after the request has
// completed, which allows us to ensure that the request has finished before the .Result()
// is called later. I missed my initial reading in the docs stating that .Result()
// should only be called after the request has finished, assumed that .Result() forced
// the request handling into being synchronous.
// https://golang.org/pkg/net/http/httptest/#ResponseRecorder.Result
// https://golang.org/pkg/net/http/#Handler
func requestDoneHandler(h http.Handler, ch chan struct{}) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(ch)
		h.ServeHTTP(w, r)
	})
}

// Proxy takes the handler from your flavor of framework and processes it into
// an AdapterResponse which can be cast to the required event.Response type
func (ar *AdapterRequest) Proxy(ctx context.Context, handler http.Handler) (*AdapterResponse, error) {
	httpRequest, err := ar.ToRequest()
	if err != nil {
		return nil, errors.Wrap(err, "Unable to convert AdapterRequest to http.Request")
	}
	httpRequest = httpRequest.WithContext(ctx)

	ch := make(chan struct{})
	wh := requestDoneHandler(handler, ch) // Wrap the handler with our done notifier
	w := httptest.NewRecorder()
	wh.ServeHTTP(http.ResponseWriter(w), httpRequest)
	<-ch      // Wait for the request to finish completely
	w.Flush() // Not positive this is necessary, but it's got a Flush() so I'll use a Flush().
	resp := w.Result()

	aresp, err := NewAdapterResponse(resp)
	if err != nil {
		return nil, errors.Wrap(err, "Unable to convert http.Response into AdapterResponse")
	}

	return aresp, nil
}

// ToRequest converts the AdapterRequest object into an http.Request that can
// be fed into the framework's http.ServeHTTP method
func (ar *AdapterRequest) ToRequest() (*http.Request, error) {
	decodedBody := []byte(ar.Body)
	if ar.IsBase64Encoded {
		base64Body, err := base64.StdEncoding.DecodeString(ar.Body)
		if err != nil {
			return nil, err
		}
		decodedBody = base64Body
	}

	path := ar.Path
	if ar.stripBasePath != "" && len(ar.stripBasePath) > 1 {
		if strings.HasPrefix(path, ar.stripBasePath) {
			path = strings.Replace(path, ar.stripBasePath, "", 1)
		}
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	serverAddress := DefaultServerAddress
	if customAddress, ok := os.LookupEnv(CustomHostVariable); ok {
		serverAddress = customAddress
	}
	path = serverAddress + path

	if len(ar.MultiValueQueryStringParameters) > 0 {
		queryString := ""
		for q, l := range ar.MultiValueQueryStringParameters {
			for _, v := range l {
				if queryString != "" {
					queryString += "&"
				}
				queryString += url.QueryEscape(q) + "=" + url.QueryEscape(v)
			}
		}
		ar.Path += "?" + queryString
	} else if len(ar.QueryStringParameters) > 0 {
		// Support `QueryStringParameters` for backward compatibility.
		// https://github.com/awslabs/aws-lambda-go-api-proxy/issues/37
		queryString := ""
		for q := range ar.QueryStringParameters {
			if queryString != "" {
				queryString += "&"
			}
			queryString += url.QueryEscape(q) + "=" + url.QueryEscape(ar.QueryStringParameters[q])
		}
		ar.Path += "?" + queryString
	}

	httpRequest, err := http.NewRequest(
		strings.ToUpper(ar.HTTPMethod),
		ar.Path,
		bytes.NewReader(decodedBody),
	)
	if err != nil {
		fmt.Printf("Could not convert request %s:%s to http.Request\n", ar.HTTPMethod, ar.Path)
		log.Println(err)
		return nil, err
	}

	for h := range ar.Headers {
		httpRequest.Header.Add(h, ar.Headers[h])
	}
	return httpRequest, nil
}

// StripBasePath used to satisfy base path mappings in API Gateway
func (ar *AdapterRequest) StripBasePath(basePath string) string {
	if strings.Trim(basePath, " ") == "" {
		ar.stripBasePath = ""
		return ""
	}

	newBasePath := basePath
	if !strings.HasPrefix(newBasePath, "/") {
		newBasePath = "/" + newBasePath
	}

	if strings.HasSuffix(newBasePath, "/") {
		newBasePath = newBasePath[:len(newBasePath)-1]
	}

	ar.stripBasePath = newBasePath

	return newBasePath
}

// AdapterResponse is a struct that contains fields required to produce either
// an events.APIGatewayResponse or events.ALBTargetGroupResponse
type AdapterResponse struct {
	StatusCode        int                 `json:"statusCode"`
	StatusDescription string              `json:"statusDescription"`
	Headers           map[string]string   `json:"headers"`
	MultiValueHeaders map[string][]string `json:"multiValueHeaders"`
	Body              string              `json:"body"`
	IsBase64Encoded   bool                `json:"isBase64Encoded,omitempty"`
}

// NewAdapterResponse converts an http.Response into an AdapterResponse
func NewAdapterResponse(r *http.Response) (*AdapterResponse, error) {
	defer r.Body.Close()
	rb, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return nil, errors.Wrap(err, "Unable to read response body")
	}

	var output string
	isBase64 := false

	if utf8.Valid(rb) {
		output = string(rb)
	} else {
		output = base64.StdEncoding.EncodeToString(rb)
		isBase64 = true
	}

	return &AdapterResponse{
		StatusCode:        r.StatusCode,
		StatusDescription: "", // Why?
		Headers:           map[string]string{},
		MultiValueHeaders: r.Header,
		Body:              output,
		IsBase64Encoded:   isBase64,
	}, nil
}

// APIGatewayProxyResponse returns an events.APIGatewayProxyResponse from the
// AdapterResponse
func (ar *AdapterResponse) APIGatewayProxyResponse() (events.APIGatewayProxyResponse, error) {
	return events.APIGatewayProxyResponse{
		StatusCode:        ar.StatusCode,
		Headers:           map[string]string{},
		MultiValueHeaders: ar.MultiValueHeaders,
		Body:              ar.Body,
		IsBase64Encoded:   ar.IsBase64Encoded,
	}, nil
}

// ALBTargetGroupResponse returns an events.ALBTargetGroupResponse from the
// AdapterResponse
func (ar *AdapterResponse) ALBTargetGroupResponse() (events.ALBTargetGroupResponse, error) {
	return events.ALBTargetGroupResponse(*ar), nil
}
