package scheduler

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// ConfigFromEnvironment reads scheduler configuration with production-safe defaults.
func ConfigFromEnvironment() (Config, error) {
	config := Config{PollInterval: defaultPollInterval, RetentionMonths: defaultRetentionMonths}
	if rawInterval := os.Getenv("SCHEDULER_POLL_INTERVAL"); rawInterval != "" {
		interval, err := time.ParseDuration(rawInterval)
		if err != nil || interval <= 0 {
			return Config{}, fmt.Errorf("SCHEDULER_POLL_INTERVAL must be a positive duration")
		}
		config.PollInterval = interval
	}
	if rawMonths := os.Getenv("JOB_RUNS_RETENTION_MONTHS"); rawMonths != "" {
		months, err := strconv.Atoi(rawMonths)
		if err != nil || months <= 0 {
			return Config{}, fmt.Errorf("JOB_RUNS_RETENTION_MONTHS must be a positive integer")
		}
		config.RetentionMonths = months
	}
	return config, nil
}
