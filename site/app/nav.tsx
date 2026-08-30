"use client";
import Link from "next/link";
import { usePathname } from "next/navigation";

const links: [string, string, boolean][] = [
  ["/manifesto/", "Manifesto", false],
  ["/initiatives/", "Initiatives", false],
  ["/share/", "Share compute", false],
  ["/rent/", "Rent compute", true],
  ["/technology/", "Technology", true],
];

export function Nav() {
  const path = usePathname();
  return (
    <header className="nav">
      <div className="nav-inner">
        <Link href="/" className="brand" aria-label="Ayni home">
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img src="/ayni-logo.png" alt="Ayni" width={92} height={38} />
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
          <Link href="/waitlist/" className="btn btn-primary">
            Join the waitlist
          </Link>
        </nav>
      </div>
    </header>
  );
}
