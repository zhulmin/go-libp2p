package main

import (
	"context"
	"encoding/csv"
	"log"
	"os"
	"runtime"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/struCoder/pidusage"
)

func NewStdoutMetricTracker(ctx context.Context, interval time.Duration) MetricTracker {
	return CollectMetrics(ctx, interval, func(m Metric) {
		log.Printf(
			"[metric] %d stream(s) | %d%% (CPU) | %d byte(s) (HEAP)\n",
			m.IncomingStreams, m.CpuPercentage, m.MemoryHeapBytes,
		)
	})
}

func NewCSVMetricTracker(ctx context.Context, interval time.Duration, filepath string) MetricTracker {
	file, err := os.Create(filepath)
	if err != nil {
		log.Fatalf("create CSV Metrics file: %v", err)
		return nil
	}

	writer := csv.NewWriter(file)

	return CollectMetrics(ctx, interval, func(m Metric) {
		writer.Write([]string{
			strconv.FormatUint(uint64(m.IncomingStreams), 10),
			strconv.FormatUint(uint64(m.CpuPercentage), 10),
			strconv.FormatUint(uint64(m.MemoryHeapBytes), 10),
		})
		writer.Flush()
	})
}

func NewNoopMetricTracker(context.Context, time.Duration) MetricTracker {
	return DummyMetricTracker{}
}

func CollectMetrics(ctx context.Context, interval time.Duration, cb func(Metric)) MetricTracker {
	var collector MetricCollector
	collector.Start(ctx, interval, cb)
	return &collector
}

type (
	// Collects metrics each interval and writes them to a csv file.
	//
	// - Incoming streams are collected manually
	// - CPU / Memory is collected using https://github.com/shirou/gopsutil
	MetricCollector struct {
		started                     bool
		lastIncomingStreamsSnapshot uint32
		currentIncomingStreams      uint32
	}

	// Metric is a single metric collected by the MetricCollector.
	Metric struct {
		IncomingStreams uint32
		CpuPercentage   uint
		MemoryHeapBytes uint64
	}

	MetricTracker interface {
		AddIncomingStream() uint32
	}
)

func (c *MetricCollector) AddIncomingStream() uint32 {
	return atomic.AddUint32(&c.currentIncomingStreams, 1)
}

func (c *MetricCollector) Start(ctx context.Context, interval time.Duration, cb func(Metric)) {
	if c.started {
		panic("MetricCollector already started")
	}
	c.started = true
	pid := os.Getpid()
	cpu := runtime.NumCPU()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(interval):
				cb(c.collect(interval, pid, cpu))
			}
		}
	}()
}

func (c *MetricCollector) collect(interval time.Duration, pid, cpu int) Metric {
	// track current incoming streams
	current := atomic.LoadUint32(&c.currentIncomingStreams)
	incomingStreams := current - c.lastIncomingStreamsSnapshot
	c.lastIncomingStreamsSnapshot = current

	// track CPU usage
	sysInfo, err := pidusage.GetStat(pid)
	if err != nil {
		sysInfo = new(pidusage.SysInfo)
	}
	cpuPercentage := uint(sysInfo.CPU)

	// track Memory usage (percentage + bytes)
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	memUsage := m.HeapAlloc

	// return all metrics
	return Metric{
		IncomingStreams: incomingStreams,
		CpuPercentage:   cpuPercentage,
		MemoryHeapBytes: memUsage,
	}
}

type DummyMetricTracker struct{}

func (DummyMetricTracker) AddIncomingStream() uint32 { return 0 }
