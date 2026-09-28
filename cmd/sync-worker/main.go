package main

import (
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/service"
	"os"
)

func main() { os.Exit(service.Entrypoint(config.Worker, os.Args[1:])) }
