package goldpinger

import (
	"encoding/binary"
	"net"
	"testing"
	"time"
)

func startTestEchoListener(t *testing.T) (int, func()) {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := pc.LocalAddr().(*net.UDPAddr).Port

	go func() {
		buf := make([]byte, 1500)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			if n >= udpHeaderSize {
				magic := binary.BigEndian.Uint32(buf[0:4])
				if magic == udpMagic {
					pc.WriteTo(buf[:n], addr)
				}
			}
		}
	}()

	return port, func() { pc.Close() }
}

func TestProbeUDP_NoLoss(t *testing.T) {
	port, cleanup := startTestEchoListener(t)
	defer cleanup()

	result := ProbeUDP("127.0.0.1", port, 10, 64, 2*time.Second)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if result.LossPct != 0 {
		t.Errorf("expected 0%% loss, got %.1f%%", result.LossPct)
	}
	if result.AvgRttMs <= 0 {
		t.Errorf("expected positive RTT, got %.4f ms", result.AvgRttMs)
	}
	t.Logf("avg UDP RTT: %.4f ms", result.AvgRttMs)
}

func TestProbeUDP_FullLoss(t *testing.T) {
	// Use a port with no listener
	result := ProbeUDP("127.0.0.1", 19999, 5, 64, 200*time.Millisecond)
	if result.LossPct != 100 {
		t.Errorf("expected 100%% loss, got %.1f%%", result.LossPct)
	}
}

func TestProbeUDP_PacketFormat(t *testing.T) {
	pkt := make([]byte, 64)
	binary.BigEndian.PutUint32(pkt[0:4], udpMagic)
	binary.BigEndian.PutUint32(pkt[4:8], 42)
	binary.BigEndian.PutUint64(pkt[8:16], uint64(time.Now().UnixNano()))

	magic := binary.BigEndian.Uint32(pkt[0:4])
	if magic != 0x47504E47 {
		t.Errorf("expected magic 0x47504E47, got 0x%X", magic)
	}
	seq := binary.BigEndian.Uint32(pkt[4:8])
	if seq != 42 {
		t.Errorf("expected seq 42, got %d", seq)
	}
}

func TestEstimateHops(t *testing.T) {
	tests := []struct {
		ttl  int
		want int32
	}{
		{64, 0},  // same host, Linux
		{63, 1},  // 1 hop, Linux
		{56, 8},  // 8 hops, Linux
		{128, 0}, // same host, Windows (TTL > 64 → initial=128)
		{127, 1}, // 1 hop, Windows
		{0, 0},   // invalid
	}
	for _, tt := range tests {
		got := estimateHops(tt.ttl)
		if got != tt.want {
			t.Errorf("estimateHops(%d) = %d, want %d", tt.ttl, got, tt.want)
		}
	}
}
