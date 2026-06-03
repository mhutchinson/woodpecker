# Woodpecker - The Transparency Log Inspector

![Screenshot of Woodpecker](./demo.gif)

Woodpecker is a command-line tool that launches a UI to inspect logs.

To run it:

```bash
# From a local checkout
go run .
```

```bash
# Without a local checkout:
go run github.com/mhutchinson/woodpecker@main
```

To change the default log that is displayed, the `--origin` flag can be provided:

```bash
# This will show the contents of the Go SumDB by default:
go run github.com/mhutchinson/woodpecker@main --origin "go.sum database tree"
```

## Custom Logs

You can register custom logs using flags:
* `--custom_log_url`: The base URL of the custom log.
* `--custom_log_origin`: The origin of the custom log.
* `--custom_log_vkey`: The verifier key of the custom log.
* `--custom_log_type`: The type of the custom log. Must be one of `tiles`, `serverless`, or `static-ct`.

Example:
```bash
go run github.com/mhutchinson/woodpecker@main \
  --custom_log_url "https://example.com/log/" \
  --custom_log_origin "example-origin" \
  --custom_log_vkey "example-origin+vkey-hash" \
  --custom_log_type "tiles"
```

## Features
- `q` or `<Ctrl-c>` to quit.
- **Left/Right arrows**: Move to previous/next leaf.
- `l`: Show the log selector to switch to a different log.
  - The selector displays log details including the log type and base URL.
  - Press `/` to enter search mode. Search uses a **fuzzy finder** supporting multi-term AND logic (order-independent) matching log origin, type, or URL.
- `g`: Jump to a specific leaf index.
- `w`/`W`: Increment/decrement the number of witness signatures to query.

## Built-in Logs
Woodpecker comes pre-configured with several transparency logs:
* **Go SumDB**: `go.sum database tree` (sumdb)
* **Rekor**: `log2025-1.rekor.sigstore.dev` (tiles)
* **Armored Witness (Prod)**: `transparency.dev/armored-witness/firmware_transparency/prod/1` (serverless)
* **Armored Witness (CI)**: `transparency.dev/armored-witness/firmware_transparency/ci/4` (serverless)
* **Armory Drive**: `Armory Drive Prod 2` (serverless)
* **Coach & Horses 2026 H1**: `coachandhorses2026h1.staging.certificate.transparency.goog` (static-ct)

## Roadmap (flight plan?)

 - [x] Support log switcher to other serverless logs
 - [x] Support getting witnessed checkpoints from distributor
 - [x] Support logs other than serverless
 - [ ] Support generating an offline inclusion proof bundle for the selected leaf including witness sigs
 - [ ] Custom leaf renderer (needed if leaf data is not text-friendly)


