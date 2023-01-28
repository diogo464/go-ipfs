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
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	"github.com/multiformats/go-multiaddr"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric/global"
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
	Protocol string `json:"protocol"`
	// Timestamp of when the stream was opened
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
	// Timestamp of when the connection was opened
	Opened  int64    `json:"opened"`
	Streams []Stream `json:"streams"`
}

func Start(node *core.IpfsNode) error {
	var t telemetry.MeterProvider
	gm := global.MeterProvider()

	if tm, ok := gm.(telemetry.MeterProvider); ok {
		t = tm
	} else {
		t = telemetry.NewNoopMeterProvider()
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

func registerProperties(t telemetry.MeterProvider) error {
	m := t.TelemetryMeter("libp2p.io/telemetry")

	m.Property(
		"process.runtime.os",
		telemetry.PropertyValueString(runtime.GOOS),
		instrument.WithDescription("The operating system this node is running on. Obtained from runtime.GOOS"),
	)

	m.Property(
		"process.runtime.arch",
		telemetry.PropertyValueString(runtime.GOARCH),
		instrument.WithDescription("The architecture this node is running on. Obtained from runtime.GOARCH"),
	)

	m.Property(
		"process.runtime.numcpu",
		telemetry.PropertyValueInteger(int64(runtime.NumCPU())),
		instrument.WithDescription("The number of logical CPUs usable by the current process. Obtained from runtime.NumCPU"),
	)

	m.Property(
		"process.boottime",
		telemetry.PropertyValueInteger(time.Now().Unix()),
		instrument.WithDescription("Boottime of this node in UNIX seconds"),
	)

	return nil
}

func registerNetworkCaptures(t telemetry.MeterProvider, node *core.IpfsNode) error {
	m := t.TelemetryMeter("libp2p.io/network")

	m.PeriodicEvent(
		context.TODO(),
		"libp2p.network.connections",
		time.Minute,
		func(_ context.Context, e telemetry.EventEmitter) error {
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

			e.Emit(connections)
			return nil
		},
		instrument.WithDescription("All current connections and streams of this node."),
	)

	m.PeriodicEvent(
		context.TODO(),
		"libp2p.network.addresses",
		2*time.Minute,
		func(_ context.Context, e telemetry.EventEmitter) error {
			e.Emit(node.PeerHost.Addrs())
			return nil
		},
		instrument.WithDescription("The addresses the node is listening on"),
	)

	return nil
}

func registerStorageMetrics(t telemetry.MeterProvider, node *core.IpfsNode) error {
	var (
		err error

		storageUsed    asyncint64.UpDownCounter
		storageObjects asyncint64.UpDownCounter
		storageTotal   asyncint64.UpDownCounter
	)

	meter := t.Meter("libp2p.io/ipfs/storage")

	if storageUsed, err = meter.AsyncInt64().UpDownCounter(
		"ipfs.storage.used",
		instrument.WithUnit(unit.Bytes),
		instrument.WithDescription("Total number of bytes used by storage"),
	); err != nil {
		return err
	}

	if storageObjects, err = meter.AsyncInt64().UpDownCounter(
		"ipfs.storage.objects",
		instrument.WithUnit(unit.Dimensionless),
		instrument.WithDescription("Total number of objects in storage"),
	); err != nil {
		return err
	}

	if storageTotal, err = meter.AsyncInt64().UpDownCounter(
		"ipfs.storage.total",
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
	if err != nil {
		return err
	}

	return nil
}

func registerNetworkMetrics(t telemetry.MeterProvider, node *core.IpfsNode) error {
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

	m := t.Meter("libp2p.io/network")

	if lowWater, err = m.AsyncInt64().UpDownCounter(
		"libp2p.network.low_water",
		instrument.WithUnit(unit.Dimensionless),
		instrument.WithDescription("Network Low Water number of peers"),
	); err != nil {
		return err
	}

	if highWater, err = m.AsyncInt64().UpDownCounter(
		"libp2p.network.high_water",
		instrument.WithUnit(unit.Dimensionless),
		instrument.WithDescription("Network High Water number of peers"),
	); err != nil {
		return err
	}

	if connections, err = m.AsyncInt64().UpDownCounter(
		"libp2p.network.connections",
		instrument.WithUnit(unit.Dimensionless),
		instrument.WithDescription("Number of connections"),
	); err != nil {
		return err
	}

	if rateIn, err = m.AsyncInt64().UpDownCounter(
		"libp2p.network.rate_in",
		instrument.WithUnit(unit.Bytes),
		instrument.WithDescription("Network in rate in bytes per second"),
	); err != nil {
		return err
	}

	if rateOut, err = m.AsyncInt64().UpDownCounter(
		"libp2p.network.rate_out",
		instrument.WithUnit(unit.Bytes),
		instrument.WithDescription("Network out rate in bytes per second"),
	); err != nil {
		return err
	}

	if totalIn, err = m.AsyncInt64().UpDownCounter(
		"libp2p.network.total_in",
		instrument.WithUnit(unit.Bytes),
		instrument.WithDescription("Network total bytes in"),
	); err != nil {
		return err
	}

	if totalOut, err = m.AsyncInt64().UpDownCounter(
		"libp2p.network.total_out",
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

		for p, s := range node.Reporter.GetBandwidthByProtocol() {
			rateIn.Observe(ctx, int64(s.RateIn), attribute.String("protocol", string(p)))
			rateOut.Observe(ctx, int64(s.RateOut), attribute.String("protocol", string(p)))
			totalIn.Observe(ctx, s.TotalIn, attribute.String("protocol", string(p)))
			totalOut.Observe(ctx, s.TotalOut, attribute.String("protocol", string(p)))
		}
	})

	return nil
}

func registerTraceroute(t telemetry.MeterProvider, node *core.IpfsNode) error {
	m := t.TelemetryMeter("libp2p.io/misc")

	picker := newPeerPicker(node.PeerHost)
	em := m.Event(
		"telemetry.misc.traceroute",
		instrument.WithDescription("Traceroute"),
	)
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
