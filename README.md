# reaperoma

`reaperoma` is a Go tool that automatically cleans up stale, unconfirmed users from Pleroma instances.

It periodically checks local, unconfirmed accounts and deletes accounts that have exceeded the configured registration age.

## Usage

```bash
reaperoma \
  -pleromahost https://example.com \
  -admintoken 'your-admin-token' \
  -cleanAge 1d \
  -checkInterval 1h
```

### Options

| Flag             | Description                                             |
| ---------------- | ------------------------------------------------------- |
| `-pleromahost`   | Pleroma instance URL                                    |
| `-admintoken`    | Pleroma admin token                                     |
| `-cleanAge`      | Maximum age of an unconfirmed account, e.g. `1d`, `12h` |
| `-checkInterval` | How often cleanup runs, e.g. `1h`, `30m`                |

If `-checkInterval` is omitted, cleanup runs once and exits.

## License

See `LICENSE`.
