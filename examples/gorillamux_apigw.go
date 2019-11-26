package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/gorilla/mux"
	"github.com/pkg/errors"
)

var (
	r *mux.Router
)

func init() {
	r = mux.NewRouter()
	r.HandleFunc("/", helloHandler)
}

func helloHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "Hello World!")
}

func handler(ctx context.Context, adapterRequest awseventadapter.AdapterRequest) (events.APIGatewayProxyResponse, error) {
	adapterResponse, err := adapterRequest.Proxy(ctx, r)
	if err != nil {
		return events.APIGatewayProxyResponse{}, errors.Wrap(err, "Unable to get adapter response")
	}
	return adapterResponse.APIGatewayProxyResponse()
}

func main() {
	lambda.Start(handler)
}
