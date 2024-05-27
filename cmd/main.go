package main

import (
	"log"
	"performance/pkg/app"
	"performance/pkg/benchmark"
	"performance/pkg/environment"
)

func main() {
	app.LoadConfig(nil)

	environments, err := environment.Setup()
	if err != nil {
		log.Fatalln(err.Error())
	}

	_, err = benchmark.Run(environments)
	if err != nil {
		log.Fatalln(err.Error())
	}

	err = environment.Shutdown()
	if err != nil {
		log.Fatalln(err.Error())
	}
}
