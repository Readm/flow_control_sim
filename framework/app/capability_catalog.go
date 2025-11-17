package app

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Readm/flow_sim/framework/core"
	"github.com/Readm/flow_sim/framework/hook"
	"github.com/Readm/flow_sim/framework/plugins/capabilities"
	chicap "github.com/Readm/flow_sim/framework/plugins/capabilities/chi"
	policy "github.com/Readm/flow_sim/framework/plugins/policy_manager"
	protochi "github.com/Readm/flow_sim/framework/plugins/protocols/chi"
)

type nodeCapabilityProfile struct {
	role   graphNodeRole
	params map[string]string
}

type capabilityEnv struct {
	broker    *hooks.PluginBroker
	policy    policy.Manager
	txFactory *TxFactory
	txnMgr    *TransactionManager
	packetIDs *PacketIDAllocator
}

func (p nodeCapabilityProfile) paramString(key string) (string, bool) {
	if p.params == nil {
		return "", false
	}
	value, ok := p.params[key]
	return value, ok
}

func (p nodeCapabilityProfile) paramInt(key string, fallback int) int {
	raw, ok := p.paramString(key)
	if !ok {
		return fallback
	}
	if value, err := strconv.Atoi(raw); err == nil {
		return value
	}
	return fallback
}

func (p nodeCapabilityProfile) paramFloat(key string, fallback float64) float64 {
	raw, ok := p.paramString(key)
	if !ok {
		return fallback
	}
	if value, err := strconv.ParseFloat(raw, 64); err == nil {
		return value
	}
	return fallback
}

func attachRequesterCapabilities(rn *RequestNode, profile nodeCapabilityProfile, env capabilityEnv) error {
	if rn == nil {
		return fmt.Errorf("request node is nil")
	}
	if env.broker == nil {
		return fmt.Errorf("request node %d missing plugin broker", rn.ID)
	}
	rn.broker = env.broker
	if env.policy != nil {
		rn.policyMgr = env.policy
	}
	if env.txnMgr != nil {
		rn.txnMgr = env.txnMgr
	}
	if env.packetIDs != nil {
		rn.packetIDs = env.packetIDs
	}
	if capacity := profile.paramInt("cache.capacity", rn.cacheCapacity); capacity > 0 {
		rn.cacheCapacity = capacity
	}

	caps := make([]capabilities.NodeCapability, 0, 4)

	if rn.cacheStore == nil {
		caps = append(caps, capabilities.NewMESICacheCapability(
			fmt.Sprintf("request-cache-%d", rn.ID),
		))
	}

	if env.txFactory != nil {
		txCap := capabilities.NewTransactionCapability(
			fmt.Sprintf("request-txn-factory-%d", rn.ID),
			func(params capabilities.TxRequestParams) (*core.Packet, *core.Transaction, error) {
				packet, txn := env.txFactory.CreateRequest(params)
				if packet == nil {
					return nil, txn, fmt.Errorf("tx factory returned nil packet")
				}
				return packet, txn, nil
			},
		)
		caps = append(caps, txCap)
	} else if rn.txnCreator == nil {
		packetAllocator := func() (int64, error) {
			if env.packetIDs == nil {
				return 0, fmt.Errorf("packet allocator not configured")
			}
			return env.packetIDs.Allocate(), nil
		}
		transactionCreator := func(txType core.CHITransactionType, addr uint64, cycle int) *core.Transaction {
			if env.txnMgr == nil {
				return nil
			}
			return env.txnMgr.CreateTransaction(txType, addr, cycle)
		}
		caps = append(caps, capabilities.NewDefaultTransactionCapability(
			fmt.Sprintf("request-txn-default-%d", rn.ID),
			packetAllocator,
			transactionCreator,
		))
	}

	if env.policy != nil {
		caps = append(caps,
			capabilities.NewRoutingCapability(
				fmt.Sprintf("request-routing-%d", rn.ID),
				env.policy,
			),
			capabilities.NewFlowControlCapability(
				fmt.Sprintf("request-flow-%d", rn.ID),
				env.policy,
			),
		)
	}

	for _, cap := range caps {
		rn.registerCapability(cap)
	}

	if rn.cacheStore != nil && rn.txnCreator != nil && rn.chiRequest == nil {
		smCap, err := protochi.NewMESIMidStateMachine(rn.cacheStore)
		if err != nil {
			return fmt.Errorf("request node %d: init CHI state machine failed: %w", rn.ID, err)
		}
		rn.registerCapability(smCap)
		handler, ok := rn.cacheCapability.(capabilities.RequestCacheHandler)
		if !ok {
			return fmt.Errorf("request node %d: cache capability missing RequestCacheHandler", rn.ID)
		}
		reqCap, err := chicap.NewRequestCapability(chicap.RequestConfig{
			Creator:      rn.txnCreator,
			Cache:        rn.cacheStore,
			StateMachine: smCap,
			CacheHandler: handler,
			PacketAllocator: func() (int64, error) {
				if env.packetIDs == nil {
					return 0, fmt.Errorf("packet allocator not configured")
				}
				return env.packetIDs.Allocate(), nil
			},
		})
		if err != nil {
			return fmt.Errorf("request node %d: init CHI request capability failed: %w", rn.ID, err)
		}
		rn.registerCapability(reqCap)
		rn.chiRequest = reqCap
	}

	if rn.cacheStore != nil && rn.cacheEvictor == nil {
		lruCap := capabilities.NewLRUEvictionCapability(
			fmt.Sprintf("request-cache-lru-%d", rn.ID),
			capabilities.LRUEvictionConfig{
				Capacity:     rn.cacheCapacity,
				RequestCache: rn.cacheStore,
			},
		)
		rn.registerCapability(lruCap)
	}

	return nil
}

func attachHomeCapabilities(hn *HomeNode, profile nodeCapabilityProfile, env capabilityEnv) error {
	if hn == nil {
		return fmt.Errorf("home node is nil")
	}
	if env.broker == nil {
		return fmt.Errorf("home node %d missing plugin broker", hn.ID)
	}
	hn.broker = env.broker
	if env.policy != nil {
		hn.policyMgr = env.policy
	}
	if env.txnMgr != nil {
		hn.txnMgr = env.txnMgr
	}
	if env.packetIDs != nil {
		hn.packetIDs = env.packetIDs
	}
	if capacity := profile.paramInt("cache.capacity", hn.cacheCapacity); capacity > 0 {
		hn.cacheCapacity = capacity
	}

	caps := make([]capabilities.NodeCapability, 0, 4)
	if hn.cacheStore == nil {
		caps = append(caps, capabilities.NewHomeCacheCapability(
			fmt.Sprintf("home-cache-%d", hn.ID),
		))
	}
	if hn.directoryStore == nil {
		caps = append(caps, capabilities.NewDirectoryCapability(
			fmt.Sprintf("home-directory-%d", hn.ID),
		))
	}
	if env.policy != nil {
		caps = append(caps,
			capabilities.NewRoutingCapability(
				fmt.Sprintf("home-routing-%d", hn.ID),
				env.policy,
			),
			capabilities.NewFlowControlCapability(
				fmt.Sprintf("home-flow-%d", hn.ID),
				env.policy,
			),
		)
	}
	for _, cap := range caps {
		hn.registerCapability(cap)
	}

	if hn.cacheStore != nil && hn.cacheEvictor == nil {
		lruCap := capabilities.NewLRUEvictionCapability(
			fmt.Sprintf("home-cache-lru-%d", hn.ID),
			capabilities.LRUEvictionConfig{
				Capacity:  hn.cacheCapacity,
				HomeCache: hn.cacheStore,
			},
		)
		hn.registerCapability(lruCap)
	}
	if hn.cacheStore != nil && hn.directoryStore != nil && hn.chiHome == nil {
		packetAllocator := func() (int64, error) {
			if env.packetIDs == nil {
				return 0, fmt.Errorf("packet allocator not configured")
			}
			return env.packetIDs.Allocate(), nil
		}
		metadataRecorder := func(txnID int64, key, value string) {
			if env.txnMgr != nil {
				env.txnMgr.AddMetadata(txnID, key, value)
			}
		}
		homeCap, err := chicap.NewHomeCapability(chicap.HomeConfig{
			Name:             fmt.Sprintf("chi-home-%d", hn.ID),
			NodeID:           hn.ID,
			Cache:            hn.cacheStore,
			Directory:        hn.directoryStore,
			CacheEvictor:     hn.cacheEvictor,
			PacketAllocator:  packetAllocator,
			Recorder:         packetRecorderAdapter{tm: env.txnMgr},
			MetadataRecorder: metadataRecorder,
			SetFinalTarget:   hn.ensureFinalTargetMetadata,
		})
		if err != nil {
			return fmt.Errorf("home node %d: init CHI home capability failed: %w", hn.ID, err)
		}
		hn.registerCapability(homeCap)
	}
	return nil
}

func attachSlaveCapabilities(sn *SlaveNode, env capabilityEnv) error {
	if sn == nil {
		return fmt.Errorf("slave node is nil")
	}
	if env.broker == nil {
		return fmt.Errorf("slave node %d missing plugin broker", sn.ID)
	}
	sn.broker = env.broker
	if env.txnMgr != nil {
		sn.txnMgr = env.txnMgr
	}

	before := func(ctx *hooks.ProcessContext) error {
		if env.txnMgr == nil || ctx == nil || ctx.Packet == nil || ctx.Packet.TransactionID == 0 {
			return nil
		}
		event := &PacketEvent{
			TransactionID:  ctx.Packet.TransactionID,
			PacketID:       ctx.Packet.ID,
			ParentPacketID: ctx.Packet.ParentPacketID,
			NodeID:         ctx.NodeID,
			EventType:      PacketProcessingStart,
			Cycle:          ctx.Cycle,
			EdgeKey:        nil,
		}
		env.txnMgr.RecordPacketEvent(event)
		return nil
	}
	after := func(ctx *hooks.ProcessContext) error {
		if env.txnMgr == nil || ctx == nil || ctx.Packet == nil || ctx.Packet.TransactionID == 0 {
			return nil
		}
		event := &PacketEvent{
			TransactionID:  ctx.Packet.TransactionID,
			PacketID:       ctx.Packet.ID,
			ParentPacketID: ctx.Packet.ParentPacketID,
			NodeID:         ctx.NodeID,
			EventType:      PacketProcessingEnd,
			Cycle:          ctx.Cycle,
			EdgeKey:        nil,
		}
		env.txnMgr.RecordPacketEvent(event)
		return nil
	}

	hookCap := capabilities.NewHookCapability(
		fmt.Sprintf("slave-processing-%d", sn.ID),
		hooks.PluginCategoryInstrumentation,
		"default slave processing instrumentation",
		hooks.HookBundle{
			BeforeProcess: []hooks.BeforeProcessHook{before},
			AfterProcess:  []hooks.AfterProcessHook{after},
		},
	)
	sn.registerCapability(hookCap)

	if sn.chiSlave == nil {
		slaveCap, err := chicap.NewSlaveCapability(chicap.SlaveConfig{
			Name:           fmt.Sprintf("chi-slave-%d", sn.ID),
			NodeID:         sn.ID,
			Recorder:       slavePacketRecorder{tm: env.txnMgr},
			SetFinalTarget: sn.setFinalTargetMetadata,
		})
		if err != nil {
			return fmt.Errorf("slave node %d: init CHI slave capability failed: %w", sn.ID, err)
		}
		sn.registerCapability(slaveCap)
	}
	return nil
}

func attachRouterCapabilities(rr *RingRouterNode, env capabilityEnv) error {
	if rr == nil {
		return fmt.Errorf("router node is nil")
	}
	if env.broker == nil {
		return fmt.Errorf("router node %d missing plugin broker", rr.ID)
	}
	rr.SetPluginBroker(env.broker)
	if env.txnMgr != nil {
		rr.SetTransactionManager(env.txnMgr)
	}
	if env.packetIDs != nil {
		rr.SetPacketIDAllocator(env.packetIDs)
	}
	if env.policy == nil {
		return nil
	}
	caps := []capabilities.NodeCapability{
		capabilities.NewRoutingCapability(
			fmt.Sprintf("router-routing-%d", rr.ID),
			env.policy,
		),
		capabilities.NewFlowControlCapability(
			fmt.Sprintf("router-flow-%d", rr.ID),
			env.policy,
		),
	}
	for _, cap := range caps {
		rr.RegisterCapability(cap)
	}
	return nil
}

func extractParams(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return nil
	}
	params := make(map[string]string, len(metadata))
	for key, value := range metadata {
		if strings.HasPrefix(key, "param.") {
			params[strings.TrimPrefix(key, "param.")] = value
		}
	}
	if len(params) == 0 {
		return nil
	}
	return params
}
