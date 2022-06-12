package config

import (
	"time"
)

type TelemetryCollector struct {
	Enabled  bool
	Interval time.Duration
}

type TelemetryStream struct {
	Duration       time.Duration
	UpdateInterval time.Duration
}

type Telemetry struct {
	Enabled    bool
	Debug      bool
	Stream     *TelemetryStream
	Collectors map[string]*TelemetryCollector
}

var TelemetryDefault = Telemetry{
	Enabled: true,
	Debug:   false,
	Stream: &TelemetryStream{
		Duration:       time.Minute * 30,
		UpdateInterval: time.Minute * 5,
	},
	Collectors: map[string]*TelemetryCollector{
		"bitswap": {
			Enabled:  true,
			Interval: time.Second * 30,
		},
		"connections": {
			Enabled:  true,
			Interval: time.Minute,
		},
		"kademlia": {
			Enabled:  true,
			Interval: time.Second * 30,
		},
		"kademliaquery": {
			Enabled:  true,
			Interval: time.Second * 5,
		},
		"kademliahandler": {
			Enabled:  true,
			Interval: time.Second * 5,
		},
		"network": {
			Enabled:  true,
			Interval: time.Second * 30,
		},
		"ping": {
			Enabled:  true,
			Interval: time.Second * 5,
		},
		"resources": {
			Enabled:  true,
			Interval: time.Second * 10,
		},
		"routingtable": {
			Enabled:  true,
			Interval: time.Minute,
		},
		"storage": {
			Enabled:  true,
			Interval: time.Minute,
		},
		"traceroute": {
			Enabled:  true,
			Interval: time.Second * 5,
		},
	},
}
