# ayni (Python)

Client library and MCP server for running batch inference on the Ayni network: one
quote up front, one charge at the quoted price, results never written to disk. Standard
library only.

## Library

```python
from ayni import Ayni

ayni = Ayni(api_key="sc_live_...")
result = ayni.run(prompts=["Classify this log line: ..."], max_price_usd=5.00)
for item in result.items:
    print(item.content)
```

Guide: [docs/WORKLOAD-RUNNERS.md](../../docs/WORKLOAD-RUNNERS.md).

## MCP server

Lets Claude Desktop, Claude Code, Cursor or your own agent check capacity, estimate,
quote, run and collect workloads, and triage security logs, with a price cap on every
spend.

```bash
pip install "git+https://github.com/mcastroarroyo/shared-compute#subdirectory=clients/python"
AYNI_API_KEY=sc_live_... ayni-mcp --selfcheck
```

Guide: [docs/MCP-SERVER.md](../../docs/MCP-SERVER.md).

## Tests

```bash
python3 -m unittest tests/test_mcp.py
```
