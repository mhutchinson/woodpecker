# Woodpecker - The Transparency Log Inspector

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

Features:
 - `q` or `<Ctrl-c>` to quit
 - Left/Right arrows: previous/next leaf
- `l`: show the log selector to switch to a different log
- `g`: jump to a specific leaf
- `w`/`W`: increment/decrement the number of witness signatures to query

## Roadmap (flight plan?)

 - [x] Support log switcher to other serverless logs
 - [x] Support getting witnessed checkpoints from distributor
 - [x] Support logs other than serverless
 - [ ] Support generating an offline inclusion proof bundle for the selected leaf including witness sigs
 - [ ] Custom leaf renderer (needed if leaf data is not text-friendly)
   - This feature is in progress!

