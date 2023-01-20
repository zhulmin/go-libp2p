package rcmgr

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"

	"github.com/pbnjay/memory"
)

type baseLimitConfig struct {
	BaseLimit         BaseLimit
	BaseLimitIncrease BaseLimitIncrease
}

// ScalingLimitConfig is a struct for configuring default limits.
// {}BaseLimit is the limits that Apply for a minimal node (128 MB of memory for libp2p) and 256 file descriptors.
// {}LimitIncrease is the additional limit granted for every additional 1 GB of RAM.
type ScalingLimitConfig struct {
	SystemBaseLimit     BaseLimit
	SystemLimitIncrease BaseLimitIncrease

	TransientBaseLimit     BaseLimit
	TransientLimitIncrease BaseLimitIncrease

	AllowlistedSystemBaseLimit     BaseLimit
	AllowlistedSystemLimitIncrease BaseLimitIncrease

	AllowlistedTransientBaseLimit     BaseLimit
	AllowlistedTransientLimitIncrease BaseLimitIncrease

	ServiceBaseLimit     BaseLimit
	ServiceLimitIncrease BaseLimitIncrease
	ServiceLimits        map[string]baseLimitConfig // use AddServiceLimit to modify

	ServicePeerBaseLimit     BaseLimit
	ServicePeerLimitIncrease BaseLimitIncrease
	ServicePeerLimits        map[string]baseLimitConfig // use AddServicePeerLimit to modify

	ProtocolBaseLimit     BaseLimit
	ProtocolLimitIncrease BaseLimitIncrease
	ProtocolLimits        map[protocol.ID]baseLimitConfig // use AddProtocolLimit to modify

	ProtocolPeerBaseLimit     BaseLimit
	ProtocolPeerLimitIncrease BaseLimitIncrease
	ProtocolPeerLimits        map[protocol.ID]baseLimitConfig // use AddProtocolPeerLimit to modify

	PeerBaseLimit     BaseLimit
	PeerLimitIncrease BaseLimitIncrease
	PeerLimits        map[peer.ID]baseLimitConfig // use AddPeerLimit to modify

	ConnBaseLimit     BaseLimit
	ConnLimitIncrease BaseLimitIncrease

	StreamBaseLimit     BaseLimit
	StreamLimitIncrease BaseLimitIncrease
}

func (cfg *ScalingLimitConfig) AddServiceLimit(svc string, base BaseLimit, inc BaseLimitIncrease) {
	if cfg.ServiceLimits == nil {
		cfg.ServiceLimits = make(map[string]baseLimitConfig)
	}
	cfg.ServiceLimits[svc] = baseLimitConfig{
		BaseLimit:         base,
		BaseLimitIncrease: inc,
	}
}

func (cfg *ScalingLimitConfig) AddProtocolLimit(proto protocol.ID, base BaseLimit, inc BaseLimitIncrease) {
	if cfg.ProtocolLimits == nil {
		cfg.ProtocolLimits = make(map[protocol.ID]baseLimitConfig)
	}
	cfg.ProtocolLimits[proto] = baseLimitConfig{
		BaseLimit:         base,
		BaseLimitIncrease: inc,
	}
}

func (cfg *ScalingLimitConfig) AddPeerLimit(p peer.ID, base BaseLimit, inc BaseLimitIncrease) {
	if cfg.PeerLimits == nil {
		cfg.PeerLimits = make(map[peer.ID]baseLimitConfig)
	}
	cfg.PeerLimits[p] = baseLimitConfig{
		BaseLimit:         base,
		BaseLimitIncrease: inc,
	}
}

func (cfg *ScalingLimitConfig) AddServicePeerLimit(svc string, base BaseLimit, inc BaseLimitIncrease) {
	if cfg.ServicePeerLimits == nil {
		cfg.ServicePeerLimits = make(map[string]baseLimitConfig)
	}
	cfg.ServicePeerLimits[svc] = baseLimitConfig{
		BaseLimit:         base,
		BaseLimitIncrease: inc,
	}
}

func (cfg *ScalingLimitConfig) AddProtocolPeerLimit(proto protocol.ID, base BaseLimit, inc BaseLimitIncrease) {
	if cfg.ProtocolPeerLimits == nil {
		cfg.ProtocolPeerLimits = make(map[protocol.ID]baseLimitConfig)
	}
	cfg.ProtocolPeerLimits[proto] = baseLimitConfig{
		BaseLimit:         base,
		BaseLimitIncrease: inc,
	}
}

type LimitVal int

const (
	// DefaultLimit is the default value for resources. The exact value depends on the context, but will get values from `DefaultLimits`.
	DefaultLimit LimitVal = 0
	// Unlimited is the value for unlimited resources. An arbitrarily high number will also work.
	Unlimited LimitVal = -1
	// BlockAllLimit is the LimitVal for allowing no amount of resources.
	BlockAllLimit LimitVal = -2
)

func (l LimitVal) MarshalJSON() ([]byte, error) {
	if l == Unlimited {
		return json.Marshal("unlimited")
	} else if l == DefaultLimit {
		return json.Marshal("default")
	} else if l == BlockAllLimit {
		return json.Marshal("blockAll")
	}
	return json.Marshal(int(l))
}

func (l *LimitVal) UnmarshalJSON(b []byte) error {
	if string(b) == `"default"` {
		*l = DefaultLimit
		return nil
	} else if string(b) == `"unlimited"` {
		*l = Unlimited
		return nil
	} else if string(b) == `"blockAll"` {
		*l = BlockAllLimit
		return nil
	}

	var val int
	if err := json.Unmarshal(b, &val); err != nil {
		return err
	}
	*l = LimitVal(val)
	return nil
}

func (l LimitVal) Build(defaultVal int) int {
	if l == DefaultLimit {
		return defaultVal
	}
	if l == Unlimited {
		return math.MaxInt
	}
	if l == BlockAllLimit {
		return 0
	}
	return int(l)
}

type LimitVal64 int64

const (
	// Default is the default value for resources.
	DefaultLimit64 LimitVal64 = 0
	// Unlimited is the value for unlimited resources.
	Unlimited64 LimitVal64 = -1
	// BlockAllLimit64 is the LimitVal for allowing no amount of resources.
	BlockAllLimit64 LimitVal64 = -2
)

func (l LimitVal64) MarshalJSON() ([]byte, error) {
	if l == Unlimited64 {
		return json.Marshal("unlimited")
	} else if l == DefaultLimit64 {
		return json.Marshal("default")
	} else if l == BlockAllLimit64 {
		return json.Marshal("blockAll")
	}

	// Convert this to a string because JSON doesn't support 64-bit integers.
	return json.Marshal(strconv.FormatInt(int64(l), 10))
}

func (l *LimitVal64) UnmarshalJSON(b []byte) error {
	if string(b) == `"default"` {
		*l = DefaultLimit64
		return nil
	} else if string(b) == `"unlimited"` {
		*l = Unlimited64
		return nil
	} else if string(b) == `"blockAll"` {
		*l = BlockAllLimit64
		return nil
	}

	var val string
	if err := json.Unmarshal(b, &val); err != nil {
		// Is this an integer? Possible because of backwards compatibility.
		var val int
		if err := json.Unmarshal(b, &val); err != nil {
			return fmt.Errorf("failed to unmarshal limit value: %w", err)
		}

		*l = LimitVal64(val)
		return nil
	}

	i, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return err
	}
	*l = LimitVal64(i)

	return nil
}

func (l LimitVal64) Build(defaultVal int64) int64 {
	if l == DefaultLimit64 {
		return defaultVal
	}
	if l == Unlimited64 {
		return math.MaxInt64
	}
	if l == BlockAllLimit64 {
		return 0
	}
	return int64(l)
}

// ResourceLimits is the type for basic resource limits.
type ResourceLimits struct {
	Streams         LimitVal   `json:",omitempty"`
	StreamsInbound  LimitVal   `json:",omitempty"`
	StreamsOutbound LimitVal   `json:",omitempty"`
	Conns           LimitVal   `json:",omitempty"`
	ConnsInbound    LimitVal   `json:",omitempty"`
	ConnsOutbound   LimitVal   `json:",omitempty"`
	FD              LimitVal   `json:",omitempty"`
	Memory          LimitVal64 `json:",omitempty"`
}

// Apply overwrites all default limits with the values of l2
func (l *ResourceLimits) Apply(l2 *ResourceLimits) {
	if l2 == nil {
		return
	}
	if l.Streams == DefaultLimit {
		l.Streams = l2.Streams
	}
	if l.StreamsInbound == DefaultLimit {
		l.StreamsInbound = l2.StreamsInbound
	}
	if l.StreamsOutbound == DefaultLimit {
		l.StreamsOutbound = l2.StreamsOutbound
	}
	if l.Conns == DefaultLimit {
		l.Conns = l2.Conns
	}
	if l.ConnsInbound == DefaultLimit {
		l.ConnsInbound = l2.ConnsInbound
	}
	if l.ConnsOutbound == DefaultLimit {
		l.ConnsOutbound = l2.ConnsOutbound
	}
	if l.FD == DefaultLimit {
		l.FD = l2.FD
	}
	if l.Memory == DefaultLimit64 {
		l.Memory = l2.Memory
	}
}

func (l *ResourceLimits) Build(defaults BaseLimit) BaseLimit {
	if l == nil {
		return defaults
	}

	out := defaults
	out.Streams = l.Streams.Build(defaults.Streams)
	out.StreamsInbound = l.StreamsInbound.Build(defaults.StreamsInbound)
	out.StreamsOutbound = l.StreamsOutbound.Build(defaults.StreamsOutbound)
	out.Conns = l.Conns.Build(defaults.Conns)
	out.ConnsInbound = l.ConnsInbound.Build(defaults.ConnsInbound)
	out.ConnsOutbound = l.ConnsOutbound.Build(defaults.ConnsOutbound)
	out.FD = l.FD.Build(defaults.FD)
	out.Memory = l.Memory.Build(defaults.Memory)

	return out
}

type PartialLimitConfig struct {
	System    *ResourceLimits `json:",omitempty"`
	Transient *ResourceLimits `json:",omitempty"`

	// Limits that are applied to resources with an allowlisted multiaddr.
	// These will only be used if the normal System & Transient limits are
	// reached.
	AllowlistedSystem    *ResourceLimits `json:",omitempty"`
	AllowlistedTransient *ResourceLimits `json:",omitempty"`

	ServiceDefault *ResourceLimits           `json:",omitempty"`
	Service        map[string]ResourceLimits `json:",omitempty"`

	ServicePeerDefault *ResourceLimits           `json:",omitempty"`
	ServicePeer        map[string]ResourceLimits `json:",omitempty"`

	ProtocolDefault *ResourceLimits                `json:",omitempty"`
	Protocol        map[protocol.ID]ResourceLimits `json:",omitempty"`

	ProtocolPeerDefault *ResourceLimits                `json:",omitempty"`
	ProtocolPeer        map[protocol.ID]ResourceLimits `json:",omitempty"`

	PeerDefault *ResourceLimits            `json:",omitempty"`
	Peer        map[peer.ID]ResourceLimits `json:",omitempty"`

	Conn   *ResourceLimits `json:",omitempty"`
	Stream *ResourceLimits `json:",omitempty"`
}

func resourceLimitsMapFromBaseLimitMapWithDefaults[K comparable](m map[K]BaseLimit, defaultLimits map[K]BaseLimit, fallbackDefault BaseLimit) map[K]ResourceLimits {
	if len(m) == 0 {
		return nil
	}

	out := make(map[K]ResourceLimits, len(m))
	for k, v := range m {
		def := fallbackDefault
		if defaultForKey, ok := defaultLimits[k]; ok {
			def = defaultForKey
		}
		rl := v.ToResourceLimitsWithDefault(def)
		if rl != nil {
			out[k] = *rl
		}
	}
	return out
}

func (cfg *PartialLimitConfig) MarshalJSON() ([]byte, error) {
	// we want to marshal the encoded peer id
	encodedPeerMap := make(map[string]ResourceLimits, len(cfg.Peer))
	for p, v := range cfg.Peer {
		encodedPeerMap[p.String()] = v
	}

	type Alias PartialLimitConfig
	return json.Marshal(&struct {
		*Alias
		Peer map[string]ResourceLimits `json:",omitempty"`
	}{
		Alias: (*Alias)(cfg),
		Peer:  encodedPeerMap,
	})
}

func applyResourceLimitsMap[K comparable](this *map[K]ResourceLimits, other map[K]ResourceLimits, fallbackDefault *ResourceLimits) {
	for k, l := range *this {
		r := fallbackDefault
		if l2, ok := other[k]; ok {
			r = &l2
		}
		l.Apply(r)
		(*this)[k] = l
	}
	if *this == nil && other != nil {
		*this = make(map[K]ResourceLimits)
	}
	for k, l := range other {
		if _, ok := (*this)[k]; !ok {
			(*this)[k] = l
		}
	}
}

func (cfg *PartialLimitConfig) Apply(c PartialLimitConfig) {
	cfg.System.Apply(c.System)
	cfg.Transient.Apply(c.Transient)
	cfg.AllowlistedSystem.Apply(c.AllowlistedSystem)
	cfg.AllowlistedTransient.Apply(c.AllowlistedTransient)
	cfg.ServiceDefault.Apply(c.ServiceDefault)
	cfg.ServicePeerDefault.Apply(c.ServicePeerDefault)
	cfg.ProtocolDefault.Apply(c.ProtocolDefault)
	cfg.ProtocolPeerDefault.Apply(c.ProtocolPeerDefault)
	cfg.PeerDefault.Apply(c.PeerDefault)
	cfg.Conn.Apply(c.Conn)
	cfg.Stream.Apply(c.Stream)

	applyResourceLimitsMap(&cfg.Service, c.Service, cfg.ServiceDefault)
	applyResourceLimitsMap(&cfg.ServicePeer, c.ServicePeer, cfg.ServicePeerDefault)
	applyResourceLimitsMap(&cfg.Protocol, c.Protocol, cfg.ProtocolDefault)
	applyResourceLimitsMap(&cfg.ProtocolPeer, c.ProtocolPeer, cfg.ProtocolPeerDefault)
	applyResourceLimitsMap(&cfg.Peer, c.Peer, cfg.PeerDefault)
}

func (cfg PartialLimitConfig) Build(defaults ConcreteLimitConfig) ConcreteLimitConfig {
	out := defaults

	out.system = cfg.System.Build(defaults.system)
	out.transient = cfg.Transient.Build(defaults.transient)
	out.allowlistedSystem = cfg.AllowlistedSystem.Build(defaults.allowlistedSystem)
	out.allowlistedTransient = cfg.AllowlistedTransient.Build(defaults.allowlistedTransient)
	out.serviceDefault = cfg.ServiceDefault.Build(defaults.serviceDefault)
	out.servicePeerDefault = cfg.ServicePeerDefault.Build(defaults.servicePeerDefault)
	out.protocolDefault = cfg.ProtocolDefault.Build(defaults.protocolDefault)
	out.protocolPeerDefault = cfg.ProtocolPeerDefault.Build(defaults.protocolPeerDefault)
	out.peerDefault = cfg.PeerDefault.Build(defaults.peerDefault)
	out.conn = cfg.Conn.Build(defaults.conn)
	out.stream = cfg.Stream.Build(defaults.stream)

	out.service = buildMapWithDefault(cfg.Service, defaults.service, out.serviceDefault)
	out.servicePeer = buildMapWithDefault(cfg.ServicePeer, defaults.servicePeer, out.servicePeerDefault)
	out.protocol = buildMapWithDefault(cfg.Protocol, defaults.protocol, out.protocolDefault)
	out.protocolPeer = buildMapWithDefault(cfg.ProtocolPeer, defaults.protocolPeer, out.protocolPeerDefault)
	out.peer = buildMapWithDefault(cfg.Peer, defaults.peer, out.peerDefault)

	return out
}

func buildMapWithDefault[K comparable](definedLimits map[K]ResourceLimits, defaults map[K]BaseLimit, fallbackDefault BaseLimit) map[K]BaseLimit {
	if definedLimits == nil && defaults == nil {
		return nil
	}

	out := make(map[K]BaseLimit)
	for k, l := range defaults {
		out[k] = l
	}

	for k, l := range definedLimits {
		if defaultForKey, ok := out[k]; ok {
			out[k] = l.Build(defaultForKey)
		} else {
			out[k] = l.Build(fallbackDefault)
		}
	}

	return out
}

// ConcreteLimitConfig is similar to PartialLimitConfig, but all values are defined.
// There is no unset "default" value. Commonly constructed by calling
// PartialLimitConfig.Build(rcmgr.DefaultLimits.AutoScale())
type ConcreteLimitConfig struct {
	system    BaseLimit
	transient BaseLimit

	// Limits that are applied to resources with an allowlisted multiaddr.
	// These will only be used if the normal System & Transient limits are
	// reached.
	allowlistedSystem    BaseLimit
	allowlistedTransient BaseLimit

	serviceDefault BaseLimit
	service        map[string]BaseLimit

	servicePeerDefault BaseLimit
	servicePeer        map[string]BaseLimit

	protocolDefault BaseLimit
	protocol        map[protocol.ID]BaseLimit

	protocolPeerDefault BaseLimit
	protocolPeer        map[protocol.ID]BaseLimit

	peerDefault BaseLimit
	peer        map[peer.ID]BaseLimit

	conn   BaseLimit
	stream BaseLimit
}

// ToLimitConfigWithDefaults converts a ConcreteLimitConfig to a PartialLimitConfig.
// Uses the defaults config to know what was specifically set and what was left
// as default. Returns a minimal PartialLimitConfig. Build the returned PartialLimitConfig
// with the defaults to get back to the original ConcreteLimitConfig.
func (cfg ConcreteLimitConfig) ToLimitConfigWithDefaults(defaults ConcreteLimitConfig) PartialLimitConfig {
	out := PartialLimitConfig{}

	out.System = cfg.system.ToResourceLimitsWithDefault(defaults.system)
	out.Transient = cfg.transient.ToResourceLimitsWithDefault(defaults.transient)

	out.AllowlistedSystem = cfg.allowlistedSystem.ToResourceLimitsWithDefault(defaults.allowlistedSystem)
	out.AllowlistedTransient = cfg.allowlistedTransient.ToResourceLimitsWithDefault(defaults.allowlistedTransient)

	out.ServiceDefault = cfg.serviceDefault.ToResourceLimitsWithDefault(defaults.serviceDefault)
	out.Service = resourceLimitsMapFromBaseLimitMapWithDefaults(cfg.service, defaults.service, defaults.serviceDefault)

	out.ServicePeerDefault = cfg.servicePeerDefault.ToResourceLimitsWithDefault(defaults.servicePeerDefault)
	out.ServicePeer = resourceLimitsMapFromBaseLimitMapWithDefaults(cfg.servicePeer, defaults.servicePeer, defaults.servicePeerDefault)

	out.ProtocolDefault = cfg.protocolDefault.ToResourceLimitsWithDefault(defaults.protocolDefault)
	out.Protocol = resourceLimitsMapFromBaseLimitMapWithDefaults(cfg.protocol, defaults.protocol, defaults.protocolDefault)

	out.ProtocolPeerDefault = cfg.protocolPeerDefault.ToResourceLimitsWithDefault(defaults.protocolPeerDefault)
	out.ProtocolPeer = resourceLimitsMapFromBaseLimitMapWithDefaults(cfg.protocolPeer, defaults.protocolPeer, defaults.protocolPeerDefault)

	out.PeerDefault = cfg.peerDefault.ToResourceLimitsWithDefault(defaults.peerDefault)
	out.Peer = resourceLimitsMapFromBaseLimitMapWithDefaults(cfg.peer, defaults.peer, defaults.peerDefault)

	out.Conn = cfg.conn.ToResourceLimitsWithDefault(defaults.conn)
	out.Stream = cfg.stream.ToResourceLimitsWithDefault(defaults.stream)

	return out
}

func resourceLimitsMapFromBaseLimitMap[K comparable](baseLimitMap map[K]BaseLimit) map[K]ResourceLimits {
	if baseLimitMap == nil {
		return nil
	}

	out := make(map[K]ResourceLimits)
	for k, l := range baseLimitMap {
		out[k] = *l.ToResourceLimits()
	}

	return out
}

// ToLimitConfig converts a ReifiedLimitConfig to a PartialLimitConfig. The returned
// PartialLimitConfig will have no default values.
func (cfg ConcreteLimitConfig) ToLimitConfig() PartialLimitConfig {
	return PartialLimitConfig{
		System:               cfg.system.ToResourceLimits(),
		Transient:            cfg.transient.ToResourceLimits(),
		AllowlistedSystem:    cfg.allowlistedSystem.ToResourceLimits(),
		AllowlistedTransient: cfg.allowlistedTransient.ToResourceLimits(),
		ServiceDefault:       cfg.serviceDefault.ToResourceLimits(),
		Service:              resourceLimitsMapFromBaseLimitMap(cfg.service),
		ServicePeerDefault:   cfg.servicePeerDefault.ToResourceLimits(),
		ServicePeer:          resourceLimitsMapFromBaseLimitMap(cfg.servicePeer),
		ProtocolDefault:      cfg.protocolDefault.ToResourceLimits(),
		Protocol:             resourceLimitsMapFromBaseLimitMap(cfg.protocol),
		ProtocolPeerDefault:  cfg.protocolPeerDefault.ToResourceLimits(),
		ProtocolPeer:         resourceLimitsMapFromBaseLimitMap(cfg.protocolPeer),
		PeerDefault:          cfg.peerDefault.ToResourceLimits(),
		Peer:                 resourceLimitsMapFromBaseLimitMap(cfg.peer),
		Conn:                 cfg.conn.ToResourceLimits(),
		Stream:               cfg.stream.ToResourceLimits(),
	}
}

// Scale scales up a limit configuration.
// memory is the amount of memory that the stack is allowed to consume,
// for a dedicated node it's recommended to use 1/8 of the installed system memory.
// If memory is smaller than 128 MB, the base configuration will be used.
func (cfg *ScalingLimitConfig) Scale(memory int64, numFD int) ConcreteLimitConfig {
	lc := ConcreteLimitConfig{
		system:               scale(cfg.SystemBaseLimit, cfg.SystemLimitIncrease, memory, numFD),
		transient:            scale(cfg.TransientBaseLimit, cfg.TransientLimitIncrease, memory, numFD),
		allowlistedSystem:    scale(cfg.AllowlistedSystemBaseLimit, cfg.AllowlistedSystemLimitIncrease, memory, numFD),
		allowlistedTransient: scale(cfg.AllowlistedTransientBaseLimit, cfg.AllowlistedTransientLimitIncrease, memory, numFD),
		serviceDefault:       scale(cfg.ServiceBaseLimit, cfg.ServiceLimitIncrease, memory, numFD),
		servicePeerDefault:   scale(cfg.ServicePeerBaseLimit, cfg.ServicePeerLimitIncrease, memory, numFD),
		protocolDefault:      scale(cfg.ProtocolBaseLimit, cfg.ProtocolLimitIncrease, memory, numFD),
		protocolPeerDefault:  scale(cfg.ProtocolPeerBaseLimit, cfg.ProtocolPeerLimitIncrease, memory, numFD),
		peerDefault:          scale(cfg.PeerBaseLimit, cfg.PeerLimitIncrease, memory, numFD),
		conn:                 scale(cfg.ConnBaseLimit, cfg.ConnLimitIncrease, memory, numFD),
		stream:               scale(cfg.StreamBaseLimit, cfg.ConnLimitIncrease, memory, numFD),
	}
	if cfg.ServiceLimits != nil {
		lc.service = make(map[string]BaseLimit)
		for svc, l := range cfg.ServiceLimits {
			lc.service[svc] = scale(l.BaseLimit, l.BaseLimitIncrease, memory, numFD)
		}
	}
	if cfg.ProtocolLimits != nil {
		lc.protocol = make(map[protocol.ID]BaseLimit)
		for proto, l := range cfg.ProtocolLimits {
			lc.protocol[proto] = scale(l.BaseLimit, l.BaseLimitIncrease, memory, numFD)
		}
	}
	if cfg.PeerLimits != nil {
		lc.peer = make(map[peer.ID]BaseLimit)
		for p, l := range cfg.PeerLimits {
			lc.peer[p] = scale(l.BaseLimit, l.BaseLimitIncrease, memory, numFD)
		}
	}
	if cfg.ServicePeerLimits != nil {
		lc.servicePeer = make(map[string]BaseLimit)
		for svc, l := range cfg.ServicePeerLimits {
			lc.servicePeer[svc] = scale(l.BaseLimit, l.BaseLimitIncrease, memory, numFD)
		}
	}
	if cfg.ProtocolPeerLimits != nil {
		lc.protocolPeer = make(map[protocol.ID]BaseLimit)
		for p, l := range cfg.ProtocolPeerLimits {
			lc.protocolPeer[p] = scale(l.BaseLimit, l.BaseLimitIncrease, memory, numFD)
		}
	}
	return lc
}

func (cfg *ScalingLimitConfig) AutoScale() ConcreteLimitConfig {
	return cfg.Scale(
		int64(memory.TotalMemory())/8,
		getNumFDs()/2,
	)
}

func scale(base BaseLimit, inc BaseLimitIncrease, memory int64, numFD int) BaseLimit {
	// mebibytesAvailable represents how many MiBs we're allowed to use. Used to
	// scale the limits. If this is below 128MiB we set it to 0 to just use the
	// base amounts.
	var mebibytesAvailable int
	if memory > 128<<20 {
		mebibytesAvailable = int((memory) >> 20)
	}
	l := BaseLimit{
		StreamsInbound:  base.StreamsInbound + (inc.StreamsInbound*mebibytesAvailable)>>10,
		StreamsOutbound: base.StreamsOutbound + (inc.StreamsOutbound*mebibytesAvailable)>>10,
		Streams:         base.Streams + (inc.Streams*mebibytesAvailable)>>10,
		ConnsInbound:    base.ConnsInbound + (inc.ConnsInbound*mebibytesAvailable)>>10,
		ConnsOutbound:   base.ConnsOutbound + (inc.ConnsOutbound*mebibytesAvailable)>>10,
		Conns:           base.Conns + (inc.Conns*mebibytesAvailable)>>10,
		Memory:          base.Memory + (inc.Memory*int64(mebibytesAvailable))>>10,
		FD:              base.FD,
	}
	if inc.FDFraction > 0 && numFD > 0 {
		l.FD = int(inc.FDFraction * float64(numFD))
		if l.FD < base.FD {
			// Use at least the base amount
			l.FD = base.FD
		}
	}
	return l
}

// DefaultLimits are the limits used by the default limiter constructors.
var DefaultLimits = ScalingLimitConfig{
	SystemBaseLimit: BaseLimit{
		ConnsInbound:    64,
		ConnsOutbound:   128,
		Conns:           128,
		StreamsInbound:  64 * 16,
		StreamsOutbound: 128 * 16,
		Streams:         128 * 16,
		Memory:          128 << 20,
		FD:              256,
	},

	SystemLimitIncrease: BaseLimitIncrease{
		ConnsInbound:    64,
		ConnsOutbound:   128,
		Conns:           128,
		StreamsInbound:  64 * 16,
		StreamsOutbound: 128 * 16,
		Streams:         128 * 16,
		Memory:          1 << 30,
		FDFraction:      1,
	},

	TransientBaseLimit: BaseLimit{
		ConnsInbound:    32,
		ConnsOutbound:   64,
		Conns:           64,
		StreamsInbound:  128,
		StreamsOutbound: 256,
		Streams:         256,
		Memory:          32 << 20,
		FD:              64,
	},

	TransientLimitIncrease: BaseLimitIncrease{
		ConnsInbound:    16,
		ConnsOutbound:   32,
		Conns:           32,
		StreamsInbound:  128,
		StreamsOutbound: 256,
		Streams:         256,
		Memory:          128 << 20,
		FDFraction:      0.25,
	},

	// Setting the allowlisted limits to be the same as the normal limits. The
	// allowlist only activates when you reach your normal system/transient
	// limits. So it's okay if these limits err on the side of being too big,
	// since most of the time you won't even use any of these. Tune these down
	// if you want to manage your resources against an allowlisted endpoint.
	AllowlistedSystemBaseLimit: BaseLimit{
		ConnsInbound:    64,
		ConnsOutbound:   128,
		Conns:           128,
		StreamsInbound:  64 * 16,
		StreamsOutbound: 128 * 16,
		Streams:         128 * 16,
		Memory:          128 << 20,
		FD:              256,
	},

	AllowlistedSystemLimitIncrease: BaseLimitIncrease{
		ConnsInbound:    64,
		ConnsOutbound:   128,
		Conns:           128,
		StreamsInbound:  64 * 16,
		StreamsOutbound: 128 * 16,
		Streams:         128 * 16,
		Memory:          1 << 30,
		FDFraction:      1,
	},

	AllowlistedTransientBaseLimit: BaseLimit{
		ConnsInbound:    32,
		ConnsOutbound:   64,
		Conns:           64,
		StreamsInbound:  128,
		StreamsOutbound: 256,
		Streams:         256,
		Memory:          32 << 20,
		FD:              64,
	},

	AllowlistedTransientLimitIncrease: BaseLimitIncrease{
		ConnsInbound:    16,
		ConnsOutbound:   32,
		Conns:           32,
		StreamsInbound:  128,
		StreamsOutbound: 256,
		Streams:         256,
		Memory:          128 << 20,
		FDFraction:      0.25,
	},

	ServiceBaseLimit: BaseLimit{
		StreamsInbound:  1024,
		StreamsOutbound: 4096,
		Streams:         4096,
		Memory:          64 << 20,
	},

	ServiceLimitIncrease: BaseLimitIncrease{
		StreamsInbound:  512,
		StreamsOutbound: 2048,
		Streams:         2048,
		Memory:          128 << 20,
	},

	ServicePeerBaseLimit: BaseLimit{
		StreamsInbound:  128,
		StreamsOutbound: 256,
		Streams:         256,
		Memory:          16 << 20,
	},

	ServicePeerLimitIncrease: BaseLimitIncrease{
		StreamsInbound:  4,
		StreamsOutbound: 8,
		Streams:         8,
		Memory:          4 << 20,
	},

	ProtocolBaseLimit: BaseLimit{
		StreamsInbound:  512,
		StreamsOutbound: 2048,
		Streams:         2048,
		Memory:          64 << 20,
	},

	ProtocolLimitIncrease: BaseLimitIncrease{
		StreamsInbound:  256,
		StreamsOutbound: 512,
		Streams:         512,
		Memory:          164 << 20,
	},

	ProtocolPeerBaseLimit: BaseLimit{
		StreamsInbound:  64,
		StreamsOutbound: 128,
		Streams:         256,
		Memory:          16 << 20,
	},

	ProtocolPeerLimitIncrease: BaseLimitIncrease{
		StreamsInbound:  4,
		StreamsOutbound: 8,
		Streams:         16,
		Memory:          4,
	},

	PeerBaseLimit: BaseLimit{
		// 8 for now so that it matches the number of concurrent dials we may do
		// in swarm_dial.go. With future smart dialing work we should bring this
		// down
		ConnsInbound:    8,
		ConnsOutbound:   8,
		Conns:           8,
		StreamsInbound:  256,
		StreamsOutbound: 512,
		Streams:         512,
		Memory:          64 << 20,
		FD:              4,
	},

	PeerLimitIncrease: BaseLimitIncrease{
		StreamsInbound:  128,
		StreamsOutbound: 256,
		Streams:         256,
		Memory:          128 << 20,
		FDFraction:      1.0 / 64,
	},

	ConnBaseLimit: BaseLimit{
		ConnsInbound:  1,
		ConnsOutbound: 1,
		Conns:         1,
		FD:            1,
		Memory:        32 << 20,
	},

	StreamBaseLimit: BaseLimit{
		StreamsInbound:  1,
		StreamsOutbound: 1,
		Streams:         1,
		Memory:          16 << 20,
	},
}

var infiniteBaseLimit = BaseLimit{
	Streams:         math.MaxInt,
	StreamsInbound:  math.MaxInt,
	StreamsOutbound: math.MaxInt,
	Conns:           math.MaxInt,
	ConnsInbound:    math.MaxInt,
	ConnsOutbound:   math.MaxInt,
	FD:              math.MaxInt,
	Memory:          math.MaxInt64,
}

// InfiniteLimits are a limiter configuration that uses unlimited limits, thus effectively not limiting anything.
// Keep in mind that the operating system limits the number of file descriptors that an application can use.
var InfiniteLimits = ConcreteLimitConfig{
	system:               infiniteBaseLimit,
	transient:            infiniteBaseLimit,
	allowlistedSystem:    infiniteBaseLimit,
	allowlistedTransient: infiniteBaseLimit,
	serviceDefault:       infiniteBaseLimit,
	servicePeerDefault:   infiniteBaseLimit,
	protocolDefault:      infiniteBaseLimit,
	protocolPeerDefault:  infiniteBaseLimit,
	peerDefault:          infiniteBaseLimit,
	conn:                 infiniteBaseLimit,
	stream:               infiniteBaseLimit,
}
