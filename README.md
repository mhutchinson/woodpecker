# Woodpecker - The Transparency Log Inspector

Woodpecker is a command-line tool that launches a UI to inspect logs.

To run it:

```bash
go run .
```

Features:
 - `<Ctrl-c>` to quit
 - Left/Right arrows: previous/next leaf
- `l`: show the log selector to switch to a different log
- `w`/`W`: increment/decrement the number of witness signatures to query

## Roadmap (flight plan?)

 - [x] Support log switcher to other serverless logs
 - [ ] Support logs other than serverless
 - [ ] Support generating an offline inclusion proof bundle for the selected leaf
 - [ ] Support getting witnessed checkpoints from distributor, and include this in offline inclusion proofs
 - [ ] Custom leaf renderer (needed if leaf data is not text-friendly)

