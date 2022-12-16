package itest

import (
	"context"
	"log"
	"testing"

	rcmgr "github.com/libp2p/go-libp2p/p2p/host/resource-manager"
	"github.com/libp2p/go-libp2p/p2p/host/resource-manager/obs"
)

func BenchmarkMetrics(b *testing.B) {
	// this test checks that we can not exceed the inbound conn limit at system level
	// we specify: 1 conn per peer, 3 conns total, and we try to create 4 conns
	cfg := rcmgr.DefaultLimits.AutoScale()

	b.Run("on", func(b *testing.B) {
		b.ReportAllocs()

		tr, err := obs.NewStatsTraceReporter()
		if err != nil {
			b.Fatal(err)
		}
		echos := createEchos(b, 2, makeRcmgrOption(b, cfg, rcmgr.WithTraceReporter(&tr)))
		defer closeEchos(echos)
		defer closeRcmgrs(echos)

		host1 := echos[0]
		host2 := echos[1]

		for i := 0; i < b.N; i++ {
			stream, err := host1.Host.NewStream(context.Background(), host2.Host.ID(), EchoProtoID)
			if err != nil {
				log.Fatal(err)
			}
			stream.Close()
			stream.Conn().Close()
		}
	})

	b.Run("off", func(b *testing.B) {
		b.ReportAllocs()

		echos := createEchos(b, 2, makeRcmgrOption(b, cfg))
		defer closeEchos(echos)
		defer closeRcmgrs(echos)

		host1 := echos[0]
		host2 := echos[1]

		for i := 0; i < b.N; i++ {
			stream, err := host1.Host.NewStream(context.Background(), host2.Host.ID(), EchoProtoID)
			if err != nil {
				log.Fatal(err)
			}
			stream.Close()
			stream.Conn().Close()
		}
	})
}
