package main

import (
	"context"
	"log"
	"net/http"
	"time"

	ocprom "contrib.go.opencensus.io/exporter/prometheus"
	"github.com/libp2p/go-libp2p"

	"github.com/libp2p/go-libp2p/core/network"
	rcmgr "github.com/libp2p/go-libp2p/p2p/host/resource-manager"
	rcmgrObs "github.com/libp2p/go-libp2p/p2p/host/resource-manager/obs"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opencensus.io/stats/view"
)

func SetupResourceManager() (network.ResourceManager, error) {
	// Hook up the trace reporter metrics. This will expose all opencensus
	// stats via the default prometheus registry. See https://opencensus.io/exporters/supported-exporters/go/prometheus/ for other options.
	view.Register(rcmgrObs.DefaultViews...)
	pe, _ := ocprom.NewExporter(ocprom.Options{
		Registry: prometheus.DefaultRegisterer.(*prometheus.Registry),
	})

	str, err := rcmgrObs.NewStatsTraceReporter()
	if err != nil {
		return nil, err
	}

	// Create a non-global registry.
	reg := prometheus.NewRegistry()

	// Create new metrics and register them using the custom registry.
	rcmgrObs.MustRegisterWith(reg)

	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics2", promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg}))

		mux.Handle("/metrics", pe)
		if err := http.ListenAndServe(":8888", mux); err != nil {
			log.Fatalf("Failed to run Prometheus /metrics endpoint: %v", err)
		}
	}()

	return rcmgr.NewResourceManager(rcmgr.NewFixedLimiter(rcmgr.DefaultLimits.AutoScale()), rcmgr.WithTraceReporter(str))
}

func main() {
	rcmgr, err := SetupResourceManager()
	if err != nil {
		panic(err)
	}
	h, err := libp2p.New(libp2p.ResourceManager(rcmgr))
	if err != nil {
		panic(err)
	}
	defer h.Close()

	log.Printf("Hello World, my hosts ID is %s\n", h.ID())

	h2, err := libp2p.New()
	if err != nil {
		panic(err)
	}
	defer h2.Close()
	h3, err := libp2p.New()
	if err != nil {
		panic(err)
	}
	defer h3.Close()

	// Inbound connection
	h2.Connect(context.Background(), h.Peerstore().PeerInfo(h.ID()))

	// Outbound connection
	h.Connect(context.Background(), h3.Peerstore().PeerInfo(h3.ID()))

	// Wait for ctrl-c.
	time.Sleep(30 * time.Minute)
}
