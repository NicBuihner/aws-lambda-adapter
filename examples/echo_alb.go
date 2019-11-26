package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/labstack/echo"
	"github.com/pkg/errors"
)

var (
	e *echo.Echo
)

func home(c echo.Context) error {
	return c.String(http.StatusOK, "Hello World!")
}

func init() {
	fmt.Fprint(os.Stderr, "Init...\n")
	e = echo.New()
	e.GET("/", home)
}

func handler(ctx context.Context, adapterRequest awseventadapter.AdapterRequest) (events.ALBTargetGroupResponse, error) {
	adapterResponse, err := adapterRequest.Proxy(ctx, e)
	if err != nil {
		return events.ALBTargetGroupResponse{}, errors.Wrap(err, "Unable to proxy request")
	}
	return adapterResponse.ALBTargetGroupResponse()
}

func main() {
	lambda.Start(handler)
}
