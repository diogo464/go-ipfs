package libp2p

import (
	"fmt"

	"github.com/diogo464/telemetry"
	ipfs_config "github.com/ipfs/kubo/config"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/config"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/core/routing"
	"go.opentelemetry.io/otel/metric/global"
)

type HostOption func(id peer.ID, ps peerstore.Peerstore, cfg ipfs_config.Telemetry, options ...libp2p.Option) (host.Host, error)

var DefaultHostOption HostOption = constructPeerHost

// isolates the complex initialization steps
func constructPeerHost(id peer.ID, ps peerstore.Peerstore, cfg ipfs_config.Telemetry, options ...libp2p.Option) (host.Host, error) {
	pkey := ps.PrivKey(id)
	if pkey == nil {
		return nil, fmt.Errorf("missing private key for node ID: %s", id.Pretty())
	}
	options = append([]libp2p.Option{libp2p.Identity(pkey), libp2p.Peerstore(ps)}, options...)

	telemetryConstructor := func(h host.Host) error {
		opts := []telemetry.ServiceOption{
			telemetry.WithServiceMetricsPeriod(cfg.GetMetricsPeriod()),
			telemetry.WithServiceBandwidth(!cfg.BandwidthDisabled),
			telemetry.WithServiceActiveBufferDuration(cfg.GetActiveBufferDuration()),
			telemetry.WithServiceWindowDuration(cfg.GetWindowDuration()),
		}

		if len(cfg.DebugListener) > 0 {
			opts = append(opts, telemetry.WithServiceTcpListener(cfg.DebugListener))
		}

		t, err := telemetry.NewService(h, opts...)

		if err != nil {
			return err
		}

		global.SetMeterProvider(t.MeterProvider())

		return nil
	}

	options = append(options, func(cfg *config.Config) error {
		rctor := cfg.Routing
		if rctor != nil {
			cfg.Routing = func(h host.Host) (routing.PeerRouting, error) {
				if err := telemetryConstructor(h); err != nil {
					return nil, err
				}
				return rctor(h)
			}
		}
		return nil
	})

	h, err := libp2p.New(options...)
	if err != nil {
		return nil, err
	}

	if _, ok := global.MeterProvider().(telemetry.MeterProvider); !ok {
		if err := telemetryConstructor(h); err != nil {
			return nil, err
		}
	}

	return h, nil
}
