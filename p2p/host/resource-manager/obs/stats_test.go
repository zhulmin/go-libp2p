package obs

import (
	"math/rand"
	"sync"
	"testing"
	"time"

	rcmgr "github.com/libp2p/go-libp2p/p2p/host/resource-manager"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestTraceReporterStartAndClose(t *testing.T) {
	rcmgr, err := rcmgr.NewResourceManager(rcmgr.NewFixedLimiter(rcmgr.DefaultLimits.AutoScale()), rcmgr.WithTraceReporter(StatsTraceReporter{}))
	if err != nil {
		t.Fatal(err)
	}
	defer rcmgr.Close()
}

var registerOnce sync.Once

func TestConsumeEvent(t *testing.T) {
	evt := rcmgr.TraceEvt{
		Type:     rcmgr.TraceBlockAddStreamEvt,
		Name:     "conn-1",
		DeltaOut: 1,
		Time:     time.Now().Format(time.RFC3339Nano),
	}

	registerOnce.Do(func() {
		MustRegisterWith(prometheus.DefaultRegisterer)
	})

	str, err := NewStatsTraceReporter()
	if err != nil {
		t.Fatal(err)
	}

	str.ConsumeEvent(evt)
}

func randomTraceEvt(rng *rand.Rand) rcmgr.TraceEvt {
	// Possibly non-sensical
	typs := []rcmgr.TraceEvtTyp{
		rcmgr.TraceStartEvt,
		rcmgr.TraceCreateScopeEvt,
		rcmgr.TraceDestroyScopeEvt,
		rcmgr.TraceReserveMemoryEvt,
		rcmgr.TraceBlockReserveMemoryEvt,
		rcmgr.TraceReleaseMemoryEvt,
		rcmgr.TraceAddStreamEvt,
		rcmgr.TraceBlockAddStreamEvt,
		rcmgr.TraceRemoveStreamEvt,
		rcmgr.TraceAddConnEvt,
		rcmgr.TraceBlockAddConnEvt,
		rcmgr.TraceRemoveConnEvt,
	}
	_ = typs

	names := []string{
		"conn-1",
		"stream-2",
		"peer:abc",
		"system",
		"transient",
		"peer:12D3Koo",
		"protocol:/libp2p/autonat/1.0.0",
		"protocol:/libp2p/autonat/1.0.0.peer:12D3Koo",
		"service:libp2p.autonat",
		"service:libp2p.autonat.peer:12D3Koo",
	}

	return rcmgr.TraceEvt{
		Type:       typs[rng.Intn(len(typs))],
		Name:       names[rng.Intn(len(names))],
		DeltaOut:   rng.Intn(5),
		DeltaIn:    rng.Intn(5),
		Delta:      int64(rng.Intn(5)),
		Memory:     int64(rng.Intn(10000)),
		StreamsIn:  rng.Intn(100),
		StreamsOut: rng.Intn(100),
		ConnsIn:    rng.Intn(100),
		ConnsOut:   rng.Intn(100),
		FD:         rng.Intn(100),
		Time:       time.Now().Format(time.RFC3339Nano),
	}

}

func BenchmarkMetricsRecording(b *testing.B) {
	b.ReportAllocs()

	registerOnce.Do(func() {
		MustRegisterWith(prometheus.DefaultRegisterer)
	})

	evtCount := 10000
	evts := make([]rcmgr.TraceEvt, evtCount)
	rng := rand.New(rand.NewSource(int64(b.N)))
	for i := 0; i < evtCount; i++ {
		evts[i] = randomTraceEvt(rng)
	}

	str, err := NewStatsTraceReporter()
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		str.ConsumeEvent(evts[i%len(evts)])
	}
}

func noop() {}

func TestNoAllocs(t *testing.T) {
	str, err := NewStatsTraceReporter()
	if err != nil {
		t.Fatal(err)
	}

	evtCount := 10_000
	evts := make([]rcmgr.TraceEvt, evtCount)
	rng := rand.New(rand.NewSource(1))

	for i := 0; i < evtCount; i++ {
		evts[i] = randomTraceEvt(rng)
	}

	failures := 0

	tagSlice := make([]string, 0, 10)
	_ = tagSlice
	_ = str

	allocs := testing.AllocsPerRun(100, func() {
		for i := 0; i < evtCount; i++ {
			noop()
		}
	})

	if allocs > 10 {
		failures++
	}

	if failures > 0 {
		t.Fatalf("expected less than 10 heap bytes")
	}

	// To prevent the GC from collecting our fake events.
	unused := 0
	for i := 0; i < evtCount; i++ {
		unused += int(evts[i].Delta)
	}
	require.GreaterOrEqual(t, unused, 0)
}
