package node

import (
	"time"

	"github.com/diogo464/telemetry"
	"github.com/libp2p/go-libp2p/core/host"
	"go.opentelemetry.io/otel/metric/global"
)

type TelemetryProvider = func(h host.Host) (*telemetry.Service, error)

var DefaultTelemetryProvider = func(h host.Host) (*telemetry.Service, error) {
	t, err := telemetry.NewService(h,
		telemetry.WithServiceTcpListener("127.0.0.1:4000"),
		telemetry.WithServiceMetricsPeriod(time.Second*2),
		telemetry.WithServiceDefaultStreamOpts(
			telemetry.WithStreamSegmentLifetime(time.Second*60), telemetry.WithStreamActiveBufferLifetime(time.Second*5),
		))

	global.SetMeterProvider(t)
	telemetry.SetGlobalTelemetry(t)

	return t, err
}
