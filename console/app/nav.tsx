"use client";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useState } from "react";
import { hasAdmin } from "@/lib/api";

const links: [string, string][] = [
  ["/", "Overview"],
  ["/devices/", "Devices"],
  ["/users/", "Users"],
  ["/events/", "Events"],
  ["/controls/", "Controls"],
  ["/keys/", "API keys"],
  ["/payouts/", "Payouts"],
  ["/feedback/", "Feedback"],
  ["/playground/", "Playground"],
  ["/settings/", "Settings"],
];

export function Nav() {
  const path = usePathname();
  const [auth, setAuth] = useState<boolean | null>(null);
  useEffect(() => {
    setAuth(hasAdmin());
  }, [path]);
  return (
    <>
      <nav>
        <span className="brand">
          <span className="brand-mark" aria-hidden />
          Ayni Admin
        </span>
        {links.map(([href, label]) => (
          <Link key={href} href={href} className={path === href ? "active" : ""}>
            {label}
          </Link>
        ))}
        <span className="nav-right">
          {auth === false ? (
            <Link href="/settings/" className="pill bad">
              no admin token
            </Link>
          ) : auth ? (
            <span className="pill good">admin</span>
          ) : null}
        </span>
      </nav>
      {auth === false && path !== "/settings/" && (
        <div className="banner">
          This console needs the coordinator admin token. Paste it once in{" "}
          <Link href="/settings/">Settings</Link>; it stays in this browser only.
        </div>
      )}
    </>
  );
}
