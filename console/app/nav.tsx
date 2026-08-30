"use client";
import Link from "next/link";
import { usePathname } from "next/navigation";

const links = [
  ["/", "Overview"],
  ["/keys/", "API keys"],
  ["/payouts/", "Payouts"],
  ["/playground/", "Playground"],
  ["/settings/", "Settings"],
];

export function Nav() {
  const path = usePathname();
  return (
    <nav>
      <span className="brand">shared-compute</span>
      {links.map(([href, label]) => (
        <Link key={href} href={href} className={path === href ? "active" : ""}>
          {label}
        </Link>
      ))}
    </nav>
  );
}
