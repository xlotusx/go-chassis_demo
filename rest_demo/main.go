package main

import (
	"github.com/go-chassis/go-chassis/v2"
	"github.com/go-mesh/openlogging"
	"go-chassis_demo/presentation/service/hello"
)

// environment: CHASSIS_HOME=./rest_demo

func main() {
	// start all server you register in server/schemas.
	chassis.RegisterSchema("rest", &hello.Presentation{})

	if err := chassis.Init(); err != nil {
		openlogging.Error("Init failed.")
		return
	}
	chassis.Run()
}
