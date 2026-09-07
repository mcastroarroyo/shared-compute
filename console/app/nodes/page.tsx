"use client";
import { useEffect } from "react";
import { useRouter } from "next/navigation";

// The Nodes page became Devices; keep the old URL working.
export default function NodesRedirect() {
  const router = useRouter();
  useEffect(() => {
    router.replace("/devices/");
  }, [router]);
  return <p className="muted">Moved to Devices…</p>;
}
