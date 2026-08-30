//! Host capability probing and dynamic telemetry sampling.
//!
//! Battery / thermal / network signals are stubbed at Tier 0 on desktop and filled in per
//! platform later (Android reports real battery + thermal in M5).

use sc_protocol::Telemetry;
use sysinfo::System;

pub struct HostInfo {
    pub platform: String,
    pub arch: String,
    pub cpu: String,
    pub ram_mb: u64,
}

/// One-time host description.
pub fn host_info() -> HostInfo {
    let mut sys = System::new();
    sys.refresh_memory();
    sys.refresh_cpu_all();

    let cpu = sys
        .cpus()
        .first()
        .map(|c| c.brand().trim().to_string())
        .filter(|s| !s.is_empty())
        .unwrap_or_else(|| "unknown".to_string());

    HostInfo {
        platform: platform_name().to_string(),
        arch: System::cpu_arch().unwrap_or_else(|| std::env::consts::ARCH.to_string()),
        cpu,
        ram_mb: sys.total_memory() / (1024 * 1024),
    }
}

/// Sample dynamic state for a heartbeat. Caller supplies job counters it owns.
pub fn sample_telemetry(active_jobs: u32, queue_depth: u32) -> Telemetry {
    let mut sys = System::new();
    sys.refresh_memory();
    sys.refresh_cpu_all();

    Telemetry {
        active_jobs,
        queue_depth,
        cpu_load: (sys.global_cpu_usage() as f64 / 100.0).clamp(0.0, 1.0),
        mem_available_mb: sys.available_memory() / (1024 * 1024),
        thermal_state: "nominal".to_string(),
        battery_pct: None,
        charging: None,
        network: Some("unknown".to_string()),
    }
}

const fn platform_name() -> &'static str {
    #[cfg(target_os = "macos")]
    {
        "macos"
    }
    #[cfg(target_os = "linux")]
    {
        "linux"
    }
    #[cfg(target_os = "windows")]
    {
        "windows"
    }
    #[cfg(target_os = "android")]
    {
        "android"
    }
    #[cfg(not(any(
        target_os = "macos",
        target_os = "linux",
        target_os = "windows",
        target_os = "android"
    )))]
    {
        "unknown"
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn host_info_is_populated() {
        let h = host_info();
        assert!(h.ram_mb > 0);
        assert!(!h.arch.is_empty());
        assert_ne!(h.platform, "unknown");
    }

    #[test]
    fn telemetry_in_range() {
        let t = sample_telemetry(1, 0);
        assert!(t.cpu_load >= 0.0 && t.cpu_load <= 1.0);
        assert_eq!(t.active_jobs, 1);
    }
}
