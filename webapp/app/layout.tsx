import "./globals.css";
import type { Metadata } from "next";
import { AppNav } from "./nav";

export const metadata: Metadata = {
  title: "Ayni",
  description: "Share your computer's idle compute, or run a workload on the network.",
  icons: { icon: "/favicon.svg" },
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>
        <AppNav />
        <main className="wrap section">{children}</main>
      </body>
    </html>
  );
}
