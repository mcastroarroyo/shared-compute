"use client";
import Link from "next/link";
import { usePathname } from "next/navigation";

const links: [string, string, boolean][] = [
  ["/manifesto/", "Manifesto", false],
  ["/council/", "Council", true],
  ["/initiatives/", "Initiatives", false],
  ["/share/", "Share compute", false],
  ["/rent/", "Run a workload", true],
  ["/technology/", "Technology", true],
  ["/security/", "Security", true],
];

export function Nav() {
  const path = usePathname();
  return (
    <header className="nav">
      <div className="nav-inner">
        <Link href="/" className="brand" aria-label="Ayni home">
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img src="/ayni-logo.png" alt="Ayni" width={88} height={36} />
        </Link>
        <nav className="links">
          {links.map(([href, label, hideSm]) => (
            <Link
              key={href}
              href={href}
              className={`${path === href ? "active" : ""} ${hideSm ? "hide-sm" : ""}`}
            >
              {label}
            </Link>
          ))}
          <Link href="/waitlist/" className="btn btn-ghost hide-sm">
            Join the waitlist
          </Link>
          <a href="https://app.ayni-ai.com/" className="btn btn-primary">
            Open the app
          </a>
        </nav>
      </div>
    </header>
  );
}
