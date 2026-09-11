import type { Metadata } from "next";
import FleetCounter from "../fleet-counter";

export const metadata: Metadata = {
  title: "Network status — Ayni",
  description: "Live Ayni fleet: devices online, devices that have joined, jobs served. Aggregates only.",
};

export default function Status() {
  return (
    <section className="wrap" style={{ maxWidth: 760 }}>
      <p className="kicker">Network status</p>
      <h1>The Ayni fleet, live.</h1>
      <p className="lead">
        Devices online right now, devices that have ever joined, and jobs served. These are aggregates
        from the coordinator; no person, device or job is identifiable from this page.
      </p>
      <FleetCounter />
      <h2>What the numbers mean</h2>
      <ul style={{ lineHeight: 1.8 }}>
        <li><strong>Devices online now</strong>: providers connected to the coordinator at this moment and able to take a job.</li>
        <li><strong>Devices that have joined</strong>: every device that has benchmarked itself at least once, online or not.</li>
        <li><strong>Jobs in the last 24 h</strong>: inference requests served, including the small test jobs every new device receives.</li>
      </ul>
      <p>
        Want your device in the count? <a href="https://app.ayni-ai.com/testers/">Ten minutes on the testers page.</a>
      </p>
    </section>
  );
}
