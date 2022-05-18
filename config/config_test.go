package config

import (
	"fmt"
	"runtime"
	"testing"
)

func TestGoMaxProcs(t *testing.T) {
	fmt.Println("GOMAXPROCS is:", runtime.GOMAXPROCS(0))
	t.Fail()
}

func TestNilOption(t *testing.T) {
	var cfg Config
	optsRun := 0
	opt := func(c *Config) error {
		optsRun++
		return nil
	}
	if err := cfg.Apply(nil); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Apply(opt, nil, nil, opt, opt, nil); err != nil {
		t.Fatal(err)
	}
	if optsRun != 3 {
		t.Fatalf("expected to have handled 3 options, handled %d", optsRun)
	}
}
