import "./globals.css";
import type { Metadata } from "next";
import { Nav } from "./nav";

export const metadata: Metadata = {
  title: "shared-compute console",
  description: "Operator console for the shared-compute inference network",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>
        <Nav />
        <div className="wrap">{children}</div>
      </body>
    </html>
  );
}
