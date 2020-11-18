package handler

import (
	"errors"
	"fmt"

	"github.com/go-chassis/go-chassis/v2/core/invocation"
	"github.com/go-chassis/go-chassis/v2/pkg/string"
)

var errViolateBuildIn = errors.New("can not replace build-in handler func")

//ErrDuplicatedHandler means you registered more than 1 handler with same name
var ErrDuplicatedHandler = errors.New("duplicated handler registration")
var buildIn = []string{Loadbalance, Router, TracingConsumer,
	TracingProvider, Transport, FaultInject}

// HandlerFuncMap handler function map
var HandlerFuncMap = make(map[string]func() Handler)

// constant keys for handlers
const (
	//consumer chain
	Transport       = "transport"
	Loadbalance     = "loadbalance"
	TracingConsumer = "tracing-consumer"

	Router             = "router"
	FaultInject        = "fault-inject"
	SkyWalkingConsumer = "skywalking-consumer"

	//provider chain
	RateLimiterProvider = "ratelimiter-provider"
	TracingProvider     = "tracing-provider"
	SkyWalkingProvider  = "skywalking-provider"
)

// init is for to initialize the all handlers at boot time
func init() {
	//register build-in handler,don't need to call RegisterHandlerFunc
	HandlerFuncMap[Transport] = newTransportHandler
	HandlerFuncMap[Loadbalance] = newLBHandler
	HandlerFuncMap[TracingProvider] = newTracingProviderHandler
	HandlerFuncMap[TracingConsumer] = newTracingConsumerHandler
	HandlerFuncMap[Router] = newRouterHandler
	HandlerFuncMap[FaultInject] = newFaultHandler
	HandlerFuncMap[TrafficMarker] = newMarkHandler
}

// Handler interface for handlers
type Handler interface {
	// handle invocation transportation,and tr response
	Handle(*Chain, *invocation.Invocation, invocation.ResponseCallBack)
	Name() string
}

//WriteBackErr write err and callback
func WriteBackErr(err error, status int, cb invocation.ResponseCallBack) {
	r := &invocation.Response{
		Err:    err,
		Status: status,
	}
	cb(r)
}

// RegisterHandler Let developer custom handler
func RegisterHandler(name string, f func() Handler) error {
	if stringutil.StringInSlice(name, buildIn) {
		return errViolateBuildIn
	}
	_, ok := HandlerFuncMap[name]
	if ok {
		return ErrDuplicatedHandler
	}
	HandlerFuncMap[name] = f
	return nil
}

// CreateHandler create a new handler by name your registered
func CreateHandler(name string) (Handler, error) {
	f := HandlerFuncMap[name]
	if f == nil {
		return nil, fmt.Errorf("don't have handler [%s]", name)
	}
	return f(), nil
}
