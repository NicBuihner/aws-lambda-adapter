### Currently a work in progress, just a test implementation to determine if the approach works.

This is my attempt reimplement the
[aws-lambda-go-api-proxy](https://github.com/awslabs/aws-lambda-go-api-proxy)
solution to handle both ALBTargetGroupRequest and APIGatewayProxyRequest event
scenarios.

### I figure this approach has three advantages:
* Using a superset JSON struct to receive both event types decouples types from the services being consumed. As long as all the fields are present to cast down into the proper event response type.
* Consuming an http.Handler interface means not having to shim each framework. This does have the disadvantage of requiring the framework expose it's http.Handler interface however, which may or may not be a problem.
* Using an httptest.Recorder from the stdlib to force the http.Handler to run synchronously means not having to deal with http.CloseNotifier deprecation in the future, and will likely be maintained by the core team.  
  * _This is not accurate, the httptest.Recorder does not force the request to be synchronous. I've added a channel in an http.Handler as a wrapper with a defer close() to ensure the request has finished processing._

### Stuff I'm not sure about:
* I feel like Go masters would not like using httptest.Recorder in this way, just a gut feeling. But Google uses the http.RoundTripper for OAuth against the docs, sooo, I'm sure it's fine. I suppose if the httptest module assumes it's not used in production there could also be a performance impact over other methods?
* I don't fully understand the context handling code in the original project. So I pass the original lambda context to the constructed http.Request via .WithContext().

### Gotcha's
* Make sure that your Lambda Target Group has [enabled multivalued headers](https://docs.aws.amazon.com/elasticloadbalancing/latest/application/lambda-functions.html#multi-value-headers).

### Why?
I want to be able to use a muxer from
[gorilla](https://github.com/gorilla/mux),
[echo](https://github.com/labstack/echo), etc with API Gateway and in my ALB
Target Groups. Converts the event into an http.Request, processes it through
the framework's http.Handler and returns the proper event response back to API
Gateway or ALBTargetGroup.

This is an incomplete POC so far, lots of borrowed code from
awslabs/aws-lambda-go-api-proxy to help me get started thinking.
