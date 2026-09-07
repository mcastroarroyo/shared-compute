//! Live integration test: the mobile core must re-register on its own after the
//! coordinator closes the connection (a deploy drain or an operator disconnect).
//!
//! Needs a running coordinator, so it is ignored by default:
//!
//!   SC_TEST_COORDINATOR_URL=ws://127.0.0.1:8080/ws/provider \
//!   SC_TEST_ADMIN_TOKEN=local-admin-token \
//!   cargo test -p sc-mobile --test reconnect -- --ignored --nocapture

use std::io::{Read, Write};
use std::net::TcpStream;
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use sc_mobile::{MobileConfig, Provider, ProviderListener, SpEvent};

#[derive(Default)]
struct Recorder {
    registered: Mutex<Vec<String>>,
    other: Mutex<Vec<String>>,
}

impl ProviderListener for Recorder {
    fn on_event(&self, event: SpEvent) {
        match event {
            SpEvent::Registered {
                provider_id,
                trust_tier,
            } => {
                self.registered.lock().unwrap().push(provider_id.clone());
                eprintln!("event: registered {provider_id} tier={trust_tier}");
            }
            SpEvent::Connecting { url } => {
                self.other.lock().unwrap().push(format!("connecting:{url}"));
                eprintln!("event: connecting {url}");
            }
            SpEvent::Disconnected { reason } => {
                self.other
                    .lock()
                    .unwrap()
                    .push(format!("disconnected:{reason}"));
                eprintln!("event: disconnected {reason}");
            }
            SpEvent::Error { message } => {
                self.other.lock().unwrap().push(format!("error:{message}"));
                eprintln!("event: error {message}");
            }
            _ => {}
        }
    }
}

/// Minimal HTTP/1.1 POST so the test has no HTTP-client dependency.
fn admin_post(http_base: &str, path: &str, token: &str) -> (u16, String) {
    let hostport = http_base.trim_start_matches("http://");
    let mut s = TcpStream::connect(hostport).expect("connect to coordinator");
    s.set_read_timeout(Some(Duration::from_secs(10))).unwrap();
    write!(
        s,
        "POST {path} HTTP/1.1\r\nHost: {hostport}\r\nX-Admin-Token: {token}\r\nContent-Type: application/json\r\nContent-Length: 2\r\nConnection: close\r\n\r\n{{}}"
    )
    .unwrap();
    let mut buf = String::new();
    s.read_to_string(&mut buf).unwrap();
    let status: u16 = buf
        .split_whitespace()
        .nth(1)
        .and_then(|c| c.parse().ok())
        .unwrap_or(0);
    let body = buf.split("\r\n\r\n").nth(1).unwrap_or("").to_string();
    (status, body)
}

fn wait_until(deadline: Duration, mut cond: impl FnMut() -> bool) -> bool {
    let start = Instant::now();
    while start.elapsed() < deadline {
        if cond() {
            return true;
        }
        std::thread::sleep(Duration::from_millis(200));
    }
    cond()
}

#[test]
#[ignore = "needs a live coordinator (SC_TEST_COORDINATOR_URL + SC_TEST_ADMIN_TOKEN)"]
fn reconnects_after_coordinator_disconnect() {
    let Ok(url) = std::env::var("SC_TEST_COORDINATOR_URL") else {
        eprintln!("SC_TEST_COORDINATOR_URL not set; skipping");
        return;
    };
    let admin = std::env::var("SC_TEST_ADMIN_TOKEN").expect("SC_TEST_ADMIN_TOKEN");
    let token =
        std::env::var("SC_TEST_PROVIDER_TOKEN").unwrap_or_else(|_| "dev-provider-token".into());
    let http = url
        .replace("ws://", "http://")
        .replace("wss://", "https://")
        .trim_end_matches("/ws/provider")
        .to_string();
    let data_dir = std::env::temp_dir().join(format!("sc-mobile-reconnect-{}", std::process::id()));
    std::fs::create_dir_all(&data_dir).unwrap();

    let provider = Provider::new();
    let rec = Arc::new(Recorder::default());
    provider
        .start(
            MobileConfig {
                coordinator_url: url,
                registration_token: token,
                model: "qwen2.5-0.5b-instruct-q4_k_m".into(),
                data_dir: data_dir.to_string_lossy().into_owned(),
                backend: "mock".into(),
                manifest_url: None,
                registry_pubkey: None,
                max_context: 4096,
                manifest_verify_key: None,
                require_manifest: false,
            },
            rec.clone(),
            None,
        )
        .expect("start");

    assert!(
        wait_until(Duration::from_secs(20), || rec
            .registered
            .lock()
            .unwrap()
            .len()
            >= 1),
        "never registered the first time: {:?}",
        rec.other.lock().unwrap()
    );
    let first = rec.registered.lock().unwrap()[0].clone();

    // Operator disconnect (same close the deploy drain sends).
    let (status, body) = admin_post(
        &http,
        &format!("/admin/providers/{first}/disconnect"),
        &admin,
    );
    assert_eq!(status, 200, "disconnect failed: {body}");
    let kicked_at = Instant::now();

    assert!(
        wait_until(Duration::from_secs(30), || rec
            .registered
            .lock()
            .unwrap()
            .len()
            >= 2),
        "did not re-register after the coordinator closed the connection; events: {:?}",
        rec.other.lock().unwrap()
    );
    let elapsed = kicked_at.elapsed();
    let ids = rec.registered.lock().unwrap().clone();
    assert_ne!(ids[0], ids[1], "a reconnect mints a new provider id");
    eprintln!(
        "re-registered {:.1}s after the disconnect (ids {} -> {})",
        elapsed.as_secs_f32(),
        &ids[0][..8],
        &ids[1][..8]
    );
    assert!(
        elapsed < Duration::from_secs(15),
        "reconnect took {elapsed:?}, backoff should start at 2 s"
    );

    provider.stop();
    assert!(
        wait_until(Duration::from_secs(5), || !provider.is_running()),
        "stop() must end the reconnect loop"
    );
    let _ = std::fs::remove_dir_all(&data_dir);
}
