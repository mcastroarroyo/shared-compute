// Command devprovider is a DEVELOPMENT-ONLY fake provider. It speaks the
// registration, heartbeat and benchmark frames of the provider protocol so a
// local coordinator can be populated with a fleet of pretend phones and PCs for
// exercising the admin console. It cannot run inference: any job it is handed
// is answered with job_error{code:"dev_provider"}.
//
//	go run ./cmd/devprovider -platform android -count 2 -ram 8192 -decode-tps 18
//	go run ./cmd/devprovider -platform macos   -count 1 -ram 32768 -decode-tps 120
//
// Never point it at a production coordinator: it would register as capacity
// that fails every job.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/protocol"
)

func main() {
	url := flag.String("url", "ws://127.0.0.1:8080/ws/provider", "coordinator provider WebSocket URL")
	token := flag.String("token", "dev-provider-token", "registration token")
	platform := flag.String("platform", "android", "platform to advertise: android|macos|linux|windows|other")
	arch := flag.String("arch", "aarch64", "arch to advertise")
	ram := flag.Int("ram", 8192, "RAM in MB to advertise")
	cores := flag.Int("cores", 8, "CPU cores to report in the benchmark")
	model := flag.String("model", "qwen2.5-0.5b-instruct-q4_k_m", "model id to advertise")
	decode := flag.Float64("decode-tps", 20, "decode tokens/s to report in the benchmark")
	count := flag.Int("count", 1, "how many fake providers to run")
	attested := flag.Bool("attested", false, "advertise hardware-key security capabilities (does NOT raise the trust tier)")
	flag.Parse()

	if strings.Contains(*url, "ayni-ai.com") {
		log.Fatal("refusing to run a fake provider against a production coordinator")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	for i := 0; i < *count; i++ {
		name := fmt.Sprintf("%s-%d", *platform, i+1)
		go runForever(ctx, name, *url, *token, protocol.Capabilities{
			Platform: *platform, Arch: *arch, CPU: "dev-cpu", RAMMB: *ram,
			Backend: "mock", HardwareClass: hardwareClass(*ram), MaxContext: 4096,
			Models:   []string{*model},
			Security: protocol.SecurityCapabilities{HardwareKey: *attested, SecureBoot: *attested},
		}, *decode, *cores)
	}
	<-ctx.Done()
}

func hardwareClass(ramMB int) string {
	switch {
	case ramMB <= 6*1024:
		return "MICRO"
	case ramMB <= 16*1024:
		return "SMALL"
	case ramMB <= 32*1024:
		return "MEDIUM"
	case ramMB <= 64*1024:
		return "LARGE"
	}
	return "XL"
}

func runForever(ctx context.Context, name, url, token string, caps protocol.Capabilities, decode float64, cores int) {
	var pk [32]byte
	if _, err := rand.Read(pk[:]); err != nil {
		log.Fatal(err)
	}
	pk[31] &= 127 // keep it a plausible X25519 point encoding
	pkb64 := base64.StdEncoding.EncodeToString(pk[:])
	for {
		if err := runOnce(ctx, name, url, token, pkb64, caps, decode, cores); err != nil && ctx.Err() == nil {
			log.Printf("[%s] session ended: %v — reconnecting in 3s", name, err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}

func runOnce(ctx context.Context, name, url, token, pk string, caps protocol.Capabilities, decode float64, cores int) error {
	ws, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return err
	}
	defer ws.CloseNow() //nolint:errcheck
	send := func(v any, re string) error {
		raw, err := protocol.Marshal(v, re)
		if err != nil {
			return err
		}
		wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return ws.Write(wctx, websocket.MessageText, raw)
	}
	if err := send(protocol.Register{
		Type: protocol.TypeRegister, Agent: "devprovider/0.1", RegistrationToken: token,
		StaticPK: pk, Capabilities: caps,
	}, ""); err != nil {
		return err
	}
	_, raw, err := ws.Read(ctx)
	if err != nil {
		return err
	}
	frame, err := protocol.Parse(raw)
	if err != nil {
		return err
	}
	var ack protocol.RegisterAck
	if err := frame.As(&ack); err != nil || !ack.OK {
		return fmt.Errorf("register rejected: %s", ack.Error)
	}
	hb := ack.HeartbeatSeconds
	if hb <= 0 {
		hb = 15
	}
	log.Printf("[%s] registered as %s tier=%s heartbeat=%ds", name, ack.ProviderID[:8], ack.TrustTier, hb)

	_ = send(protocol.BenchmarkReport{
		Type: protocol.TypeBenchmarkReport, Model: caps.Models[0], Backend: "mock",
		PrefillTPS: decode * 6, DecodeTPS: decode, SampleMS: 1200,
		SustainedSeconds: 30, SustainedStartTPS: decode, SustainedEndTPS: decode * 0.92,
		MemBandwidthGBps: 18, AvailableRAMMB: uint64(caps.RAMMB * 6 / 10), CPUCores: cores,
		ThermalState: "nominal",
	}, "")

	ticker := time.NewTicker(time.Duration(hb) * time.Second)
	defer ticker.Stop()
	frames := make(chan protocol.Frame, 8)
	errc := make(chan error, 1)
	go func() {
		for {
			_, raw, err := ws.Read(ctx)
			if err != nil {
				errc <- err
				return
			}
			f, err := protocol.Parse(raw)
			if err != nil {
				continue
			}
			frames <- f
		}
	}()
	battery := 80
	charging := true
	for {
		select {
		case <-ctx.Done():
			_ = ws.Close(websocket.StatusNormalClosure, "shutdown")
			return nil
		case err := <-errc:
			return err
		case <-ticker.C:
			t := protocol.Telemetry{CPULoad: 0.12, MemAvailableMB: caps.RAMMB / 2, ThermalState: "nominal", Network: "wifi"}
			if caps.Platform == "android" || caps.Platform == "ios" {
				t.BatteryPct, t.Charging = &battery, &charging
			}
			if err := send(protocol.Heartbeat{Type: protocol.TypeHeartbeat, Telemetry: t}, ""); err != nil {
				return err
			}
		case f := <-frames:
			switch f.Type {
			case protocol.TypeJobRequest:
				var probe struct {
					JobID string `json:"job_id"`
				}
				_ = json.Unmarshal(f.Raw, &probe)
				log.Printf("[%s] declining job %s (dev provider cannot run inference)", name, probe.JobID)
				_ = send(protocol.JobError{Type: protocol.TypeJobError, JobID: probe.JobID,
					Code: "dev_provider", Message: "devprovider cannot run inference"}, f.ID)
			case protocol.TypeHeartbeatAck:
				var a protocol.HeartbeatAck
				if err := f.As(&a); err == nil && a.Directive != "" {
					log.Printf("[%s] directive: %s", name, a.Directive)
				}
			}
		}
	}
}

var _ = os.Exit
