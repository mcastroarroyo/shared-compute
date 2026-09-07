import "./globals.css";
import type { Metadata } from "next";
import { Nav } from "./nav";

export const metadata: Metadata = {
  title: "Ayni Admin",
  description: "IT-admin console for the Ayni compute network: fleet, users, events and controls",
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
