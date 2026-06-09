package main

import (
	"log"

	"github.com/evdi/agent/pkg/config"
)

func main() {
	cfg := config.Load()
	log.Printf("EVDI Agent starting, WS port=%s, display=%s, video=%dx%d@%dfps",
		cfg.WSPort, cfg.Display, cfg.VideoWidth, cfg.VideoHeight, cfg.VideoFPS)

	// 后续 Task 将在此初始化各组件
	select {}
}
