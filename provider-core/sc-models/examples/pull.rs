//! Fetch + verify a model from a registry base URL.
//!
//!   cargo run -p sc-models --example pull -- <base_url> <model_id> <pubkey_b64> <dest_dir>

use sc_models::ModelStore;

#[tokio::main(flavor = "current_thread")]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let args: Vec<String> = std::env::args().collect();
    if args.len() != 5 {
        eprintln!("usage: pull <base_url> <model_id> <pubkey_b64> <dest_dir>");
        std::process::exit(2);
    }
    let (base, model_id, pubkey, dest) = (&args[1], &args[2], &args[3], &args[4]);

    let manifest = ModelStore::fetch_manifest(base, pubkey).await?;
    println!(
        "manifest OK: {} model(s), signature verified",
        manifest.models.len()
    );

    let store = ModelStore::new(dest);
    let resolved = store.ensure(base, model_id, &manifest).await?;
    println!("resolved: {}", resolved.primary_path.display());
    println!("aggregate_sha256: {}", resolved.entry.aggregate_sha256);
    Ok(())
}
