import "./globals.css";
import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "Ayni — live demo",
  description:
    "Get a real AI summary from someone else's device, encrypted end to end, and see what it cost.",
  icons: { icon: "/favicon.svg" },
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>
        <header className="appnav">
          <div className="appnav-inner">
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img src="/ayni-logo.png" alt="Ayni" />
            <span className="spacer" />
            <a className="who" href="https://ayni-ai.com">
              ayni-ai.com
            </a>
          </div>
        </header>
        <main className="wrap section">{children}</main>
      </body>
    </html>
  );
}
