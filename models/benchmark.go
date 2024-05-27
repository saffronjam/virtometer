package models

import "time"

type BenchmarkResult struct {
}

type NodeMetrics struct {
	Name string `json:"name"`
	CPU  struct {
		Cores   []float64 `json:"cores"`
		Average float64   `json:"average"`
	} `json:"cpu"`
	RAM       float64   `json:"ram"`
	Disk      float64   `json:"disk"`
	Timestamp time.Time `json:"timestamp"`
}

type TestDefinition struct {
	Name     string
	Func     func() []TestResult
	RunCount int
	Disabled bool
}

type TestResult struct {
	Name     string            `json:"name"`
	Group    string            `json:"group"`
	Metadata map[string]string `json:"metadata"`

	Timers  map[string]time.Time `json:"timers"`
	Metrics []NodeMetrics        `json:"metrics"`

	Err error `json:"-"`
}
