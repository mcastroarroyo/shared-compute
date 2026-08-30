import type { Metadata } from "next";
import { WaitlistForm } from "../waitlist-form";

export const metadata: Metadata = {
  title: "Join the waitlist — Ayni",
  description:
    "Sign up to share idle compute and get paid, rent private inference, or bring an initiative to the community.",
};

export default function Waitlist() {
  return (
    <section className="wrap" style={{ maxWidth: 640 }}>
      <p className="kicker">Waitlist</p>
      <h1>Take part in Ayni</h1>
      <p className="lead" style={{ maxWidth: "48ch" }}>
        We&rsquo;re onboarding in waves. Tell us how you want to take part and
        we&rsquo;ll open your track and email you — nothing else.
      </p>
      <div style={{ marginTop: 28 }}>
        <WaitlistForm />
      </div>
    </section>
  );
}
