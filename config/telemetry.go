package config

import "time"

const (
	DefaultMetricsPeriod        = 20 * time.Second
	DefaultWindowDuration       = 30 * time.Minute
	DefaultActiveBufferDuration = 5 * time.Minute
)

type Telemetry struct {
	Disabled             bool
	BandwidthDisabled    bool
	MetricsPeriod        string
	WindowDuration       string
	ActiveBufferDuration string
	DebugListener        string
}

func (t Telemetry) GetMetricsPeriod() time.Duration {
	return parseDurationOrDefault(t.MetricsPeriod, DefaultMetricsPeriod)
}

func (t Telemetry) GetWindowDuration() time.Duration {
	return parseDurationOrDefault(t.WindowDuration, DefaultWindowDuration)
}

func (t Telemetry) GetActiveBufferDuration() time.Duration {
	return parseDurationOrDefault(t.ActiveBufferDuration, DefaultActiveBufferDuration)
}

func parseDurationOrDefault(d string, def time.Duration) time.Duration {
	if dur, err := time.ParseDuration(d); err == nil {
		return dur
	} else {
		return def
	}
}
