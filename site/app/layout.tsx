import "./globals.css";
import type { Metadata } from "next";
import { Nav } from "./nav";
import { Footer } from "./footer";

const title = "Ayni — share the value of AI with the world";
const description =
  "A community that builds ways to share the benefits of AI broadly. Contribute idle compute and get paid, rent private inference, or propose your own initiative.";

export const metadata: Metadata = {
  metadataBase: new URL("https://ayni-ai.com"),
  title,
  description,
  icons: { icon: "/favicon.svg" },
  openGraph: {
    title,
    description,
    url: "https://ayni-ai.com",
    siteName: "Ayni",
    images: ["/ayni-logo.png"],
    type: "website",
  },
  twitter: { card: "summary_large_image", title, description, images: ["/ayni-logo.png"] },
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>
        <Nav />
        <main>{children}</main>
        <Footer />
      </body>
    </html>
  );
}
