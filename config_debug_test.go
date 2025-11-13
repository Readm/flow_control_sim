package main

import "testing"

func TestDumpHistoryConfig(t *testing.T) {
    cfg := GetConfigByName("readonce_mesi_snoop")
    if cfg == nil {
        t.Fatal("config not found")
    }
    sim := NewSimulator(cfg)
    if sim == nil {
        t.Fatal("simulator init failed")
    }
    hc := sim.txnMgr.historyConfig
    t.Logf("cfg.EnablePacketHistory=%v", cfg.EnablePacketHistory)
    t.Logf("historyConfig.EnablePacketHistory=%v", hc.EnablePacketHistory)
    t.Logf("historyConfig.MaxPacketHistorySize=%d", hc.MaxPacketHistorySize)
    t.Logf("historyConfig.MaxTransactionHistory=%d", hc.MaxTransactionHistory)
    t.Logf("historyConfig.HistoryOverflowMode=%q", hc.HistoryOverflowMode)
}
