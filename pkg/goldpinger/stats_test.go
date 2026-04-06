package goldpinger

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// TestDeletePeerMetrics_CleansResponseTimeHistogram verifies that
// DeletePeerMetrics removes the response-time histogram label set for
// a destroyed peer. This prevents stale pod IPs from lingering in
// /metrics after rolling updates (see #167).
func TestDeletePeerMetrics_CleansResponseTimeHistogram(t *testing.T) {
	origHostname := GoldpingerConfig.Hostname
	GoldpingerConfig.Hostname = "test-instance"
	defer func() { GoldpingerConfig.Hostname = origHostname }()

	hostIP := "10.0.0.1"
	podIP := "10.0.0.2"

	// Simulate a ping observation (call_type="ping")
	goldpingerResponseTimePeersHistogram.WithLabelValues(
		GoldpingerConfig.Hostname, "ping", hostIP, podIP,
	).Observe(0.005)

	if countMetrics(goldpingerResponseTimePeersHistogram) == 0 {
		t.Fatal("response time histogram has no label values before cleanup — test setup is broken")
	}

	DeletePeerMetrics(hostIP, podIP)

	if n := countMetrics(goldpingerResponseTimePeersHistogram); n != 0 {
		t.Errorf("response time histogram still has %d label set(s) after DeletePeerMetrics", n)
	}
}

// TestDeletePeerMetrics_LeavesOtherPeersIntact verifies that pruning
// metrics for one peer does not affect a different peer's label set.
func TestDeletePeerMetrics_LeavesOtherPeersIntact(t *testing.T) {
	origHostname := GoldpingerConfig.Hostname
	GoldpingerConfig.Hostname = "test-instance"
	defer func() { GoldpingerConfig.Hostname = origHostname }()

	// Peer A
	goldpingerResponseTimePeersHistogram.WithLabelValues(
		GoldpingerConfig.Hostname, "ping", "10.0.0.1", "10.0.0.2",
	).Observe(0.005)
	SetPeerLossPct("10.0.0.1", "10.0.0.2", 0)

	// Peer B
	goldpingerResponseTimePeersHistogram.WithLabelValues(
		GoldpingerConfig.Hostname, "ping", "10.0.0.3", "10.0.0.4",
	).Observe(0.010)
	SetPeerLossPct("10.0.0.3", "10.0.0.4", 1.5)

	// Delete peer A only
	DeletePeerMetrics("10.0.0.1", "10.0.0.2")
	DeletePeerUDPMetrics("10.0.0.1", "10.0.0.2")

	// Peer B should survive in both metrics
	if countMetrics(goldpingerResponseTimePeersHistogram) == 0 {
		t.Error("response time histogram lost all label sets — peer B should still exist")
	}
	if countMetrics(goldpingerPeersLossPct) == 0 {
		t.Error("loss pct gauge lost all label sets — peer B should still exist")
	}

	// Clean up peer B so it doesn't leak into other tests
	goldpingerResponseTimePeersHistogram.DeleteLabelValues(
		GoldpingerConfig.Hostname, "ping", "10.0.0.3", "10.0.0.4",
	)
	goldpingerPeersLossPct.DeleteLabelValues(
		GoldpingerConfig.Hostname, "10.0.0.3", "10.0.0.4",
	)
}

// TestDeletePeerUDPMetrics_CleansAllPerPeerMetrics verifies that
// DeletePeerUDPMetrics removes label sets from every per-peer UDP metric.
// If a new per-peer metric is added but not cleaned up in
// DeletePeerUDPMetrics, this test will fail.
func TestDeletePeerUDPMetrics_CleansAllPerPeerMetrics(t *testing.T) {
	// Save and restore hostname since we set it for the test
	origHostname := GoldpingerConfig.Hostname
	origUseHostIP := GoldpingerConfig.UseHostIP
	GoldpingerConfig.Hostname = "test-instance"
	GoldpingerConfig.UseHostIP = false
	defer func() {
		GoldpingerConfig.Hostname = origHostname
		GoldpingerConfig.UseHostIP = origUseHostIP
	}()

	hostIP := "10.0.0.1"
	podIP := "10.0.0.2"

	// Populate all per-peer UDP metrics so they have label values
	SetPeerLossPct(hostIP, podIP, 5.0)
	SetPeerHopCount(hostIP, podIP, 2)
	ObservePeerUDPRtt(hostIP, podIP, 0.001)
	CountUDPDuplicates(hostIP, podIP, 1)
	CountUDPOutOfOrder(hostIP, podIP, 1)
	CountUDPError(podIP) // UseHostIP=false so target=podIP

	// Verify they exist before cleanup
	perPeerCollectors := map[string]prometheus.Collector{
		"goldpinger_peers_loss_pct":         goldpingerPeersLossPct,
		"goldpinger_peers_hop_count":        goldpingerPeersHopCount,
		"goldpinger_peers_udp_rtt_s":        goldpingerPeersUDPRtt,
		"goldpinger_udp_duplicates_total":   goldpingerUDPDuplicatesCounter,
		"goldpinger_udp_out_of_order_total": goldpingerUDPOutOfOrderCounter,
		"goldpinger_udp_errors_total":       goldpingerUDPErrorsCounter,
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
