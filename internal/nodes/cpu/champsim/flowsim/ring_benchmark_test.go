package flowsim

import (
	"math/rand"
	"testing"

	"github.com/Readm/flow_sim/internal/components/coherence"
)

// BenchmarkSingleCore 单核性能基准
func BenchmarkSingleCore(b *testing.B) {
	// 单核配置：1 CPU
	config := RingTopologyConfig{
		NumClusters:   1,
		CPUsPerL3:     1,
		L2PerCPU:      1,
		CPUIDStart:    0,
		L2IDStart:     1,
		L3IDStart:     2,
		HAIDStart:     3,
		DRAMIDStart:   4,
	}

	nodeIDs := BuildRingTopologyNodeIDs(config)

	addrConfig := coherence.AddressMappingConfig{
		Granularity: 64,
		Strategy:    coherence.MappingInterleaved,
	}

	tree, err := BuildRingCoherenceTree(config, nodeIDs, addrConfig)
	if err != nil {
		b.Fatalf("构建一致性树失败: %v", err)
	}

	// 创建路由器
	router := coherence.NewCoherenceRouter(0, tree) // CPU 0

	// 准备随机地址（避免测量随机数生成的开销）
	numAddrs := 1000
	addrs := make([]uint64, numAddrs)
	for i := 0; i < numAddrs; i++ {
		addrs[i] = uint64(rand.Intn(1024*1024)) * 64 // 随机地址（64B 对齐）
	}

	b.ResetTimer()

	// 基准测试：模拟 CPU 发送请求并路由
	for i := 0; i < b.N; i++ {
		addr := addrs[i%numAddrs]

		// 1. 查询 Home Node
		_, err := router.GetHomeNode(addr)
		if err != nil {
			b.Fatalf("GetHomeNode 失败: %v", err)
		}

		// 2. 查询下一跳
		_, err = router.RouteForCoherence(addr)
		if err != nil {
			b.Fatalf("RouteForCoherence 失败: %v", err)
		}

		// 3. 判断是否是 Home Node
		_ = router.IsHomeNode(addr)
	}

	// 报告吞吐量
	ops := float64(b.N)
	seconds := b.Elapsed().Seconds()
	throughput := ops / seconds / 1000.0 // KOPS (千次操作/秒)

	b.ReportMetric(throughput, "KOPS")
}

// BenchmarkRing32Core Ring 拓扑 32 核性能基准
func BenchmarkRing32Core(b *testing.B) {
	// 32 核配置
	config := DefaultRingTopologyConfig()
	nodeIDs := BuildRingTopologyNodeIDs(config)

	addrConfig := coherence.AddressMappingConfig{
		Granularity: 64,
		Strategy:    coherence.MappingInterleaved,
	}

	tree, err := BuildRingCoherenceTree(config, nodeIDs, addrConfig)
	if err != nil {
		b.Fatalf("构建一致性树失败: %v", err)
	}

	// 为所有 CPU 创建路由器
	routers := make([]*coherence.CoherenceRouter, len(nodeIDs.CPUIDs))
	for i, cpuID := range nodeIDs.CPUIDs {
		routers[i] = coherence.NewCoherenceRouter(cpuID, tree)
	}

	// 准备随机地址
	numAddrs := 1000
	addrs := make([]uint64, numAddrs)
	for i := 0; i < numAddrs; i++ {
		addrs[i] = uint64(rand.Intn(1024*1024)) * 64
	}

	b.ResetTimer()

	// 基准测试：模拟所有 CPU 并发发送请求
	for i := 0; i < b.N; i++ {
		// 轮询所有 CPU
		cpuIdx := i % len(routers)
		router := routers[cpuIdx]

		addr := addrs[i%numAddrs]

		// 1. 查询 Home Node
		_, err := router.GetHomeNode(addr)
		if err != nil {
			b.Fatalf("GetHomeNode 失败: %v", err)
		}

		// 2. 查询下一跳
		_, err = router.RouteForCoherence(addr)
		if err != nil {
			b.Fatalf("RouteForCoherence 失败: %v", err)
		}

		// 3. 判断是否是 Home Node
		_ = router.IsHomeNode(addr)
	}

	// 报告吞吐量
	ops := float64(b.N)
	seconds := b.Elapsed().Seconds()
	throughput := ops / seconds / 1000.0 // KOPS

	b.ReportMetric(throughput, "KOPS")
}

// BenchmarkRing32Core_Parallel Ring 拓扑 32 核并行性能基准
func BenchmarkRing32Core_Parallel(b *testing.B) {
	// 32 核配置
	config := DefaultRingTopologyConfig()
	nodeIDs := BuildRingTopologyNodeIDs(config)

	addrConfig := coherence.AddressMappingConfig{
		Granularity: 64,
		Strategy:    coherence.MappingInterleaved,
	}

	tree, err := BuildRingCoherenceTree(config, nodeIDs, addrConfig)
	if err != nil {
		b.Fatalf("构建一致性树失败: %v", err)
	}

	// 为所有 CPU 创建路由器
	routers := make([]*coherence.CoherenceRouter, len(nodeIDs.CPUIDs))
	for i, cpuID := range nodeIDs.CPUIDs {
		routers[i] = coherence.NewCoherenceRouter(cpuID, tree)
	}

	// 准备随机地址
	numAddrs := 1000
	addrs := make([]uint64, numAddrs)
	for i := 0; i < numAddrs; i++ {
		addrs[i] = uint64(rand.Intn(1024*1024)) * 64
	}

	b.ResetTimer()

	// 并行基准测试
	b.RunParallel(func(pb *testing.PB) {
		// 每个 goroutine 使用一个 CPU
		cpuIdx := 0
		for pb.Next() {
			router := routers[cpuIdx%len(routers)]
			cpuIdx++

			addr := addrs[cpuIdx%numAddrs]

			// 1. 查询 Home Node
			_, err := router.GetHomeNode(addr)
			if err != nil {
				b.Fatalf("GetHomeNode 失败: %v", err)
			}

			// 2. 查询下一跳
			_, err = router.RouteForCoherence(addr)
			if err != nil {
				b.Fatalf("RouteForCoherence 失败: %v", err)
			}

			// 3. 判断是否是 Home Node
			_ = router.IsHomeNode(addr)
		}
	})

	// 报告吞吐量
	ops := float64(b.N)
	seconds := b.Elapsed().Seconds()
	throughput := ops / seconds / 1000.0 // KOPS

	b.ReportMetric(throughput, "KOPS")
}

// BenchmarkCoherenceTreeLookup 一致性树查找性能
func BenchmarkCoherenceTreeLookup(b *testing.B) {
	config := DefaultRingTopologyConfig()
	nodeIDs := BuildRingTopologyNodeIDs(config)

	addrConfig := coherence.AddressMappingConfig{
		Granularity: 64,
		Strategy:    coherence.MappingInterleaved,
	}

	tree, err := BuildRingCoherenceTree(config, nodeIDs, addrConfig)
	if err != nil {
		b.Fatalf("构建一致性树失败: %v", err)
	}

	// 准备随机地址
	numAddrs := 1000
	addrs := make([]uint64, numAddrs)
	for i := 0; i < numAddrs; i++ {
		addrs[i] = uint64(rand.Intn(1024*1024)) * 64
	}

	b.ResetTimer()

	// 只测量 GetHomeNode 的性能
	for i := 0; i < b.N; i++ {
		addr := addrs[i%numAddrs]
		_, err := tree.GetHomeNode(addr)
		if err != nil {
			b.Fatalf("GetHomeNode 失败: %v", err)
		}
	}

	// 报告吞吐量
	ops := float64(b.N)
	seconds := b.Elapsed().Seconds()
	throughput := ops / seconds / 1000.0 // KOPS

	b.ReportMetric(throughput, "KOPS")
}

// BenchmarkCoherencePathCalculation 一致性路径计算性能
func BenchmarkCoherencePathCalculation(b *testing.B) {
	config := DefaultRingTopologyConfig()
	nodeIDs := BuildRingTopologyNodeIDs(config)

	addrConfig := coherence.AddressMappingConfig{
		Granularity: 64,
		Strategy:    coherence.MappingInterleaved,
	}

	tree, err := BuildRingCoherenceTree(config, nodeIDs, addrConfig)
	if err != nil {
		b.Fatalf("构建一致性树失败: %v", err)
	}

	// 准备随机地址
	numAddrs := 1000
	addrs := make([]uint64, numAddrs)
	for i := 0; i < numAddrs; i++ {
		addrs[i] = uint64(rand.Intn(1024*1024)) * 64
	}

	b.ResetTimer()

	// 只测量 GetCoherencePath 的性能
	for i := 0; i < b.N; i++ {
		cpuID := nodeIDs.CPUIDs[i%len(nodeIDs.CPUIDs)]
		addr := addrs[i%numAddrs]

		_, err := tree.GetCoherencePath(cpuID, addr)
		if err != nil {
			b.Fatalf("GetCoherencePath 失败: %v", err)
		}
	}

	// 报告吞吐量
	ops := float64(b.N)
	seconds := b.Elapsed().Seconds()
	throughput := ops / seconds / 1000.0 // KOPS

	b.ReportMetric(throughput, "KOPS")
}
