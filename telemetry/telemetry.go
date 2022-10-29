package telemetry

import (
	"context"
	"runtime"
	"time"

	"github.com/diogo464/telemetry"
	logging "github.com/ipfs/go-log"
	"github.com/ipfs/kubo/core"
	"github.com/ipfs/kubo/core/corerepo"
	"github.com/ipfs/kubo/telemetry/traceroute"
	"github.com/libp2p/go-libp2p-core/protocol"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	"github.com/multiformats/go-multiaddr"
	"go.opentelemetry.io/otel/metric/instrument"
	"go.opentelemetry.io/otel/metric/instrument/asyncint64"
	"go.opentelemetry.io/otel/metric/unit"
)

var log = logging.Logger("ipfs/telemetry")

type ProtocolStats struct {
	TotalIn  int64   `json:"total_in"`
	TotalOut int64   `json:"total_out"`
	RateIn   float64 `json:"rate_in"`
	RateOut  float64 `json:"rate_out"`
}

type Stream struct {
	Protocol  string `json:"protocol"`
	Opened    int64  `json:"opened"`
	Direction string `json:"direction"`
}

type Traceroute struct {
	Target   peer.ID `json:"target"`
	Provider string  `json:"provider"`
	Output   []byte  `json:"output"`
}

type Connection struct {
	ID      peer.ID             `json:"id"`
	Addr    multiaddr.Multiaddr `json:"addr"`
	Latency int64               `json:"latency"`
	Opened  int64               `json:"opened"`
	Streams []Stream            `json:"streams"`
}

func Start(node *core.IpfsNode) error {
	t := telemetry.GetGlobalTelemetry()
	if t == nil {
		t = telemetry.NewNoOpTelemetry()
	}

	if err := registerProperties(t); err != nil {
		return err
	}
	if err := registerNetworkCaptures(t, node); err != nil {
		return err
	}
	if err := registerStorageMetrics(t, node); err != nil {
		return err
	}
	if err := registerNetworkMetrics(t, node); err != nil {
		return err
	}
	if err := registerTraceroute(t, node); err != nil {
		return err
	}

	return nil
}

func registerProperties(t telemetry.Telemetry) error {
	t.Property(telemetry.PropertyConfig{
		Name:        "libp2p_host_os",
		Description: "The operating system this node is running on. Obtained from runtime.GOOS",
		Value:       telemetry.NewPropertyValueString(runtime.GOOS),
	})

	t.Property(telemetry.PropertyConfig{
		Name:        "libp2p_host_arch",
		Description: "The architecture this node is running on. Obtained from runtime.GOARCH",
		Value:       telemetry.NewPropertyValueString(runtime.GOARCH),
	})

	t.Property(telemetry.PropertyConfig{
		Name:        "libp2p_host_numcpu",
		Description: "The number of logical CPUs usable by the current process. Obtained from runtime.NumCPU",
		Value:       telemetry.NewPropertyValueInteger(int64(runtime.NumCPU())),
	})

	t.Property(telemetry.PropertyConfig{
		Name:        "libp2p_host_boottime",
		Description: "Boottime of this node in UNIX seconds",
		Value:       telemetry.NewPropertyValueInteger(time.Now().Unix()),
	})

	return nil
}

func registerNetworkCaptures(t telemetry.Telemetry, node *core.IpfsNode) error {
	t.Capture(telemetry.CaptureConfig{
		Name:        "libp2p_network_connections",
		Description: "All current connections and streams of this node.",
		Callback: func(context.Context) (interface{}, error) {
			networkConns := node.PeerHost.Network().Conns()
			connections := make([]Connection, 0, len(networkConns))

			for _, conn := range networkConns {
				streams := make([]Stream, 0, len(conn.GetStreams()))
				for _, stream := range conn.GetStreams() {
					streams = append(streams, Stream{
						Protocol:  string(stream.Protocol()),
						Opened:    stream.Stat().Opened.Unix(),
						Direction: stream.Stat().Direction.String(),
					})
				}
				connections = append(connections, Connection{
					ID:      conn.RemotePeer(),
					Addr:    conn.RemoteMultiaddr(),
					Latency: node.PeerHost.Network().Peerstore().LatencyEWMA(conn.RemotePeer()).Microseconds(),
					Opened:  conn.Stat().Opened.Unix(),
					Streams: streams,
				})
			}

			return connections, nil
		},
		Interval: time.Minute,
	})

	t.Capture(telemetry.CaptureConfig{
		Name:        "libp2p_network_stats_by_protocol",
		Description: "Network stats by protocol",
		Callback: func(context.Context) (interface{}, error) {
			rstats := node.Reporter.GetBandwidthByProtocol()
			stats := make(map[protocol.ID]ProtocolStats, len(rstats))
			for k, v := range rstats {
				stats[k] = ProtocolStats(v)
			}
			return stats, nil
		},
		Interval: time.Minute,
	})

	t.Capture(telemetry.CaptureConfig{
		Name:        "libp2p_network_addresses",
		Description: "The addresses this node currently listens on",
		Callback: func(context.Context) (interface{}, error) {
			return node.PeerHost.Addrs(), nil
		},
		Interval: 2 * time.Minute,
	})

	return nil
}

func registerStorageMetrics(t telemetry.Telemetry, node *core.IpfsNode) error {
	var (
		err error

		storageUsed    asyncint64.UpDownCounter
		storageObjects asyncint64.UpDownCounter
		storageTotal   asyncint64.UpDownCounter
	)

	meter := t.Meter("libp2p.io/ipfs/storage")

	if storageUsed, err = meter.AsyncInt64().UpDownCounter(
		"used",
		instrument.WithUnit(unit.Bytes),
		instrument.WithDescription("Total number of bytes used by storage"),
	); err != nil {
		return err
	}

	if storageObjects, err = meter.AsyncInt64().UpDownCounter(
		"objects",
		instrument.WithUnit(unit.Dimensionless),
		instrument.WithDescription("Total number of objects in storage"),
	); err != nil {
		return err
	}

	if storageTotal, err = meter.AsyncInt64().UpDownCounter(
		"total",
		instrument.WithUnit(unit.Bytes),
		instrument.WithDescription("Total number of bytes avaible for storage"),
	); err != nil {
		return err
	}

	err = meter.RegisterCallback([]instrument.Asynchronous{
		storageUsed,
		storageObjects,
		storageTotal,
	}, func(ctx context.Context) {
		stat, err := corerepo.RepoStat(ctx, node)
		if err != nil {
			log.Errorf("corerepo.RepoStat failed", "error", err)
			return
		}

		storageUsed.Observe(ctx, int64(stat.RepoSize))
		storageObjects.Observe(ctx, int64(stat.NumObjects))
		storageTotal.Observe(ctx, int64(stat.StorageMax))
	})

	return nil
}

func registerNetworkMetrics(t telemetry.Telemetry, node *core.IpfsNode) error {
	var (
		err error

		lowWater    asyncint64.UpDownCounter
		highWater   asyncint64.UpDownCounter
		connections asyncint64.UpDownCounter
		rateIn      asyncint64.UpDownCounter
		rateOut     asyncint64.UpDownCounter
		totalIn     asyncint64.Counter
		totalOut    asyncint64.Counter
	)

	m := t.Meter("libp2p.io/ipfs/network")

	if lowWater, err = m.AsyncInt64().UpDownCounter(
		"low_water",
		instrument.WithUnit(unit.Dimensionless),
		instrument.WithDescription("Network Low Water number of peers"),
	); err != nil {
		return err
	}

	if highWater, err = m.AsyncInt64().UpDownCounter(
		"high_water",
		instrument.WithUnit(unit.Dimensionless),
		instrument.WithDescription("Network High Water number of peers"),
	); err != nil {
		return err
	}

	if connections, err = m.AsyncInt64().UpDownCounter(
		"connections",
		instrument.WithUnit(unit.Dimensionless),
		instrument.WithDescription("Number of connections"),
	); err != nil {
		return err
	}

	if rateIn, err = m.AsyncInt64().UpDownCounter(
		"rate_in",
		instrument.WithUnit(unit.Bytes),
		instrument.WithDescription("Network in rate in bytes per second"),
	); err != nil {
		return err
	}

	if rateOut, err = m.AsyncInt64().UpDownCounter(
		"rate_out",
		instrument.WithUnit(unit.Bytes),
		instrument.WithDescription("Network out rate in bytes per second"),
	); err != nil {
		return err
	}

	if totalIn, err = m.AsyncInt64().UpDownCounter(
		"total_in",
		instrument.WithUnit(unit.Bytes),
		instrument.WithDescription("Network total bytes in"),
	); err != nil {
		return err
	}

	if totalOut, err = m.AsyncInt64().UpDownCounter(
		"total_out",
		instrument.WithUnit(unit.Bytes),
		instrument.WithDescription("Network total bytes out"),
	); err != nil {
		return err
	}

	m.RegisterCallback([]instrument.Asynchronous{
		lowWater,
		highWater,
		connections,
		rateIn,
		rateOut,
		totalIn,
		totalOut,
	}, func(ctx context.Context) {
		reporter := node.Reporter
		cmgr := node.PeerHost.ConnManager().(*connmgr.BasicConnMgr)
		info := cmgr.GetInfo()

		lowWater.Observe(ctx, int64(info.LowWater))
		highWater.Observe(ctx, int64(info.HighWater))
		connections.Observe(ctx, int64(info.ConnCount))

		bt := reporter.GetBandwidthTotals()
		rateIn.Observe(ctx, int64(bt.RateIn))
		rateOut.Observe(ctx, int64(bt.RateOut))
		totalIn.Observe(ctx, bt.TotalIn)
		totalOut.Observe(ctx, bt.TotalOut)
	})

	return nil
}

func registerTraceroute(t telemetry.Telemetry, node *core.IpfsNode) error {
	picker := newPeerPicker(node.PeerHost)
	em := t.Event(telemetry.EventConfig{
		Name:        "libp2p_misc_traceroute",
		Description: "Traceroute",
	})

	go func() {
		timeout := time.Second * 15

		for {
			time.Sleep(time.Second * 10)
			if pid, ok := picker.pick(); ok {
				addrinfo := node.PeerHost.Network().Peerstore().PeerInfo(pid)
				addr, err := getFirstPublicAddressFromMultiaddrs(addrinfo.Addrs)
				if err == nil {
					ctx, cancel := context.WithTimeout(context.Background(), timeout)
					result, err := traceroute.Trace(ctx, addr.String())
					cancel()
					if err == nil {
						em.Emit(&Traceroute{
							Target:   pid,
							Provider: result.Provider,
							Output:   result.Output,
						})
					} else if err != traceroute.ErrNoProviderAvailable {
						log.Warn("Traceroute to ", addr, "failed with", err)
					}
				}
			}
		}
	}()

	return nil
}
