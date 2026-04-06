package goldpinger

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// TestDeletePeerUDPMetrics_CleansAllPerPeerMetrics verifies that
// DeletePeerUDPMetrics removes label sets from every per-peer UDP metric.
// If a new per-peer metric is added but not cleaned up in
// DeletePeerUDPMetrics, this test will fail.
func TestDeletePeerUDPMetrics_CleansAllPerPeerMetrics(t *testing.T) {
	// Save and restore hostname since we set it for the test
	origHostname := GoldpingerConfig.Hostname
	GoldpingerConfig.Hostname = "test-instance"
	defer func() { GoldpingerConfig.Hostname = origHostname }()

	hostIP := "10.0.0.1"
	podIP := "10.0.0.2"

	// Populate all per-peer UDP metrics so they have label values
	SetPeerLossPct(hostIP, podIP, 5.0)
	SetPeerHopCount(hostIP, podIP, 2)
	ObservePeerUDPRtt(hostIP, podIP, 0.001)
	CountUDPDuplicates(hostIP, podIP, 1)
	CountUDPOutOfOrder(hostIP, podIP, 1)

	// Verify they exist before cleanup
	perPeerCollectors := map[string]prometheus.Collector{
		"goldpinger_peers_loss_pct":         goldpingerPeersLossPct,
		"goldpinger_peers_hop_count":        goldpingerPeersHopCount,
		"goldpinger_peers_udp_rtt_s":        goldpingerPeersUDPRtt,
		"goldpinger_udp_duplicates_total":   goldpingerUDPDuplicatesCounter,
		"goldpinger_udp_out_of_order_total": goldpingerUDPOutOfOrderCounter,
	}

	for name, collector := range perPeerCollectors {
		if countMetrics(collector) == 0 {
			t.Fatalf("metric %s has no label values before cleanup — test setup is broken", name)
		}
	}

	// Run cleanup
	DeletePeerUDPMetrics(hostIP, podIP)

	// Verify all per-peer metrics are cleaned up
	for name, collector := range perPeerCollectors {
		if n := countMetrics(collector); n != 0 {
			t.Errorf("metric %s still has %d label set(s) after DeletePeerUDPMetrics — add it to the cleanup function", name, n)
		}
	}
}

// countMetrics returns the number of metric families (label sets) for a collector.
func countMetrics(c prometheus.Collector) int {
	ch := make(chan prometheus.Metric, 100)
	c.Collect(ch)
	close(ch)
	count := 0
	for m := range ch {
		var d dto.Metric
		if err := m.Write(&d); err == nil {
			count++
		}
	}
	return count
}
