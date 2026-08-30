/** @type {import('next').NextConfig} */
const nextConfig = {
  // Fully static — the console only talks to the coordinator from the browser.
  output: "export",
  images: { unoptimized: true },
  trailingSlash: true,
};

export default nextConfig;
