//! Thin WebSocket client for the provider ↔ coordinator link.
//!
//! Reconnect/backoff is the daemon's responsibility; this crate just establishes one
//! connection and provides frame (de)serialization helpers over the protocol wire format.

use anyhow::{Context, Result};
use futures_util::{SinkExt, StreamExt};
use serde::Serialize;
use tokio::net::TcpStream;
use tokio_tungstenite::{
    connect_async, tungstenite::protocol::Message, MaybeTlsStream, WebSocketStream,
};

pub use tokio_tungstenite::tungstenite::protocol::Message as WsMessage;

pub type WsStream = WebSocketStream<MaybeTlsStream<TcpStream>>;

/// Dial `url` (ws:// or wss://) and complete the WebSocket handshake.
pub async fn connect(url: &str) -> Result<WsStream> {
    let (ws, _resp) = connect_async(url)
        .await
        .with_context(|| format!("connect {url}"))?;
    Ok(ws)
}

/// Serialize a protocol payload struct into a full frame and send it as a text message.
pub async fn send_frame<S, T>(sink: &mut S, payload: &T, re: Option<&str>) -> Result<()>
where
    S: SinkExt<Message> + Unpin,
    <S as futures_util::Sink<Message>>::Error: std::error::Error + Send + Sync + 'static,
    T: Serialize,
{
    let bytes = sc_protocol::to_frame(payload, re).context("encode frame")?;
    sink.send(Message::Text(
        String::from_utf8(bytes).context("frame utf8")?,
    ))
    .await
    .context("send frame")?;
    Ok(())
}

/// Pull the next text frame's bytes, skipping ping/pong/binary. Returns `Ok(None)` on
/// clean close.
pub async fn next_text<S>(stream: &mut S) -> Result<Option<Vec<u8>>>
where
    S: StreamExt<Item = std::result::Result<Message, tokio_tungstenite::tungstenite::Error>>
        + Unpin,
{
    while let Some(msg) = stream.next().await {
        match msg.context("ws read")? {
            Message::Text(t) => return Ok(Some(t.into_bytes())),
            Message::Binary(b) => return Ok(Some(b)),
            Message::Close(_) => return Ok(None),
            Message::Ping(_) | Message::Pong(_) | Message::Frame(_) => continue,
        }
    }
    Ok(None)
}
