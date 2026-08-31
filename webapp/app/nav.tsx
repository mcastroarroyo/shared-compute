"use client";
import { useEffect, useState } from "react";
import Link from "next/link";
import { api } from "./lib/api";

export function AppNav() {
  const [email, setEmail] = useState<string | null>(null);

  useEffect(() => {
    api
      .me()
      .then((m) => setEmail(m.user.email))
      .catch(() => setEmail(null));
  }, []);

  return (
    <header className="appnav">
      <div className="appnav-inner">
        <Link href={email ? "/dashboard/" : "/"}>
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img src="/ayni-logo.png" alt="Ayni" />
        </Link>
        <span className="spacer" />
        {email && (
          <>
            <span className="who">{email}</span>
            <button
              className="btn btn-ghost"
              onClick={async () => {
                await api.logout().catch(() => {});
                location.href = "/";
              }}
            >
              Sign out
            </button>
          </>
        )}
      </div>
    </header>
  );
}
