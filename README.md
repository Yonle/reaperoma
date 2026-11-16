# reaperoma

`reaperoma` is a Go tool that automatically cleans up stale, unconfirmed users from Pleroma instances.

It periodically checks local, unconfirmed accounts and deletes accounts that have exceeded the configured registration age.

## Usage

```bash
reaperoma \
  -login \
  -host https://example.com
```

The first run with `-login` starts the Pleroma OAuth authorization flow. After authorization, the OAuth credentials are saved locally.

Once authenticated, simply run:

```bash
reaperoma \
  -cleanAge 1d \
  -checkInterval 1h
```

The saved state contains the Pleroma instance and OAuth credentials, so `-host` is not required for subsequent runs.

### Options

| Flag              | Description                                                       |
| ----------------- | ----------------------------------------------------------------- |
| `-login`          | Authenticate with a Pleroma instance and save the OAuth state     |
| `-host`           | Pleroma instance URL; required with `-login`                      |
| `-stateFile`      | Path to the saved OAuth state file                                |
| `-cleanAge`       | Maximum age of an unconfirmed account, e.g. `1d`, `12h`, `30m`    |
| `-checkInterval`  | How often cleanup runs, e.g. `1h`, `30m`, `1d`                    |
| `-ignoreApproval` | Ignore/Don't check for administrator approved unconfirmed account |

If `-checkInterval` is omitted, cleanup runs once and exits.

## License

See `LICENSE`.
