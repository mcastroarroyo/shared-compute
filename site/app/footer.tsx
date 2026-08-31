import Link from "next/link";

const REPO = "https://github.com/mcastroarroyo/shared-compute";

export function Footer() {
  return (
    <footer className="footer">
      <div className="wrap">
        <div className="cols">
          <div>
            <div className="brand">Ayni</div>
            <div style={{ maxWidth: "32ch", marginTop: 6 }}>
              Reciprocity for the age of AI. Share the value, broaden the access,
              govern it together.
            </div>
          </div>
          <div>
            <div className="brand">Take part</div>
            <div><Link href="/share/">Share your compute</Link></div>
            <div><Link href="/rent/">Rent compute</Link></div>
            <div><Link href="/initiatives/">Submit an initiative</Link></div>
            <div><Link href="/waitlist/">Join the waitlist</Link></div>
          </div>
          <div>
            <div className="brand">Learn</div>
            <div><Link href="/manifesto/">Manifesto</Link></div>
            <div><Link href="/technology/">Technology</Link></div>
            <div><Link href="/security/">Security</Link></div>
            <div><Link href="/council/">Council</Link></div>
            <div><a href={`${REPO}/blob/main/WHITEPAPER.md`}>White paper</a></div>
            <div><a href={REPO}>Source (GitHub)</a></div>
            <div><a href={`${REPO}/blob/main/LICENSE`}>License — Apache 2.0</a></div>
          </div>
        </div>
        <div style={{ marginTop: 32, fontSize: "0.85rem" }}>
          © {new Date().getFullYear()} Ayni · operated by Zalesgen LLC · Open source
          under Apache 2.0. No prompt or response content is ever logged.
        </div>
      </div>
    </footer>
  );
}
