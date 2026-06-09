package config

import (
	"os"
	"strconv"
)

type Config struct {
	WSPort        string
	VideoWidth    int
	VideoHeight   int
	VideoFPS      int
	Display       string
	WebRTCPortMin uint16
	WebRTCPortMax uint16
}

func Load() *Config {
	return &Config{
		WSPort:        getEnv("AGENT_WS_PORT", "8080"),
		VideoWidth:    getEnvInt("VIDEO_WIDTH", 1920),
		VideoHeight:   getEnvInt("VIDEO_HEIGHT", 1080),
		VideoFPS:      getEnvInt("VIDEO_FPS", 30),
		Display:       getEnv("DISPLAY", ":99"),
		WebRTCPortMin: uint16(getEnvInt("WEBRTC_PORT_MIN", 50000)),
		WebRTCPortMax: uint16(getEnvInt("WEBRTC_PORT_MAX", 50100)),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}
