# tvheadend-exporter

Prometheus exporter for [Tvheadend](https://tvheadend.org) 4.3. It shows who is watching what, from where and for how long, plus tuner health, mux/service inventory and DVR status.

It only talks to the Tvheadend HTTP API with read-only GET requests.

## Quick start

Create a Tvheadend user for the exporter (Configuration → Users → Access Entries) with admin rights, so it can read status, inputs and DVR.

Docker:

```sh
echo -n 'your-password' > tvh_password.txt
docker run -d --name tvheadend-exporter -p 9429:9429 \
  -e TVH_URL=http://tvheadend:9981 \
  -e TVH_USERNAME=exporter \
  -e TVH_PASSWORD_FILE=/run/secrets/tvh_password \
  -v "$PWD/tvh_password.txt:/run/secrets/tvh_password:ro" \
  ghcr.io/imaleeexx/tvheadend-exporter:latest
```

Binary:

```sh
go install github.com/imaleeexx/tvheadend-exporter/cmd/tvheadend-exporter@latest
TVH_URL=http://tvheadend:9981 TVH_USERNAME=exporter TVH_PASSWORD=secret tvheadend-exporter
```

Then check `http://localhost:9429/metrics`.

There's also a compose file and a systemd unit in `deploy/`.

## Configuration

Everything can be set by env var or flag (`-h` lists the flags).

| Variable | Default | |
|---|---|---|
| `TVH_URL` | — | Tvheadend base URL (required) |
| `TVH_USERNAME` | — | required |
| `TVH_PASSWORD` / `TVH_PASSWORD_FILE` | — | one of them required; the file wins |
| `TVH_LISTEN` | `:9429` | |
| `TVH_POLL_STATUS` | `10s` | subscriptions, connections, inputs |
| `TVH_POLL_TOPOLOGY` | `60s` | networks, muxes, services, channels, DVR |
| `TVH_TIMEOUT` | `5s` | per request |
| `TVH_SESSION_GRACE` | `60s` | how long Tvheadend can be unreachable before open sessions are closed |
| `TVH_GEOIP_DB` | — | MaxMind GeoLite2/GeoIP2 City `.mmdb` for country/city labels |
| `TVH_LOG_FORMAT` | `json` | `json` or `text` |
| `TVH_LOG_LEVEL` | `info` | |
| `TVH_TLS_INSECURE_SKIP_VERIFY` | `false` | |

## What it exports

- **Viewers**: one series per active subscription (user, channel, client app, IP, profile, service, state), bitrate, bytes and errors; active streams per user and viewers per channel.
- **Sessions**: each subscription is tracked from start to end, so you get watch time, bytes and errors per user/channel/client as counters, a session duration histogram, and started/ended counts.
- **Tuners/inputs**: bitrate, signal, SNR, BER, UNC, continuity/transport errors, current tune.
- **Inventory**: networks, muxes by scan result, services (mapped/unmapped), channels, channel tags.
- **DVR**: entries by status/owner, recording sizes, data errors, active/upcoming recordings, autorec/timerec rules.
- **Server**: `tvheadend_up`, version, capabilities, access entries and connection limits.
- **Exporter itself**: poll duration/errors, API request timing, last successful poll.

Full list with labels: [docs/METRICS.md](docs/METRICS.md).

Example:

```
tvheadend_up 1
tvheadend_subscription_info{channel="La 1 HD",client="Kodi Media Center",country="ES",city="Madrid",id="15268",peer="203.0.113.10",profile="htsp",service="IPTV #2/IPTV/LA 1 HD IPTV/Service01",state="Running",title="203.0.113.10 [ alice | Kodi Media Center ]",user="alice"} 1
tvheadend_user_active_streams{user="alice"} 1
tvheadend_channel_active_viewers{channel="La 1 HD"} 2
tvheadend_session_seconds_total{channel="La 1 HD",client="Kodi Media Center",country="ES",city="Madrid",peer="203.0.113.10",profile="htsp",user="alice"} 3620
tvheadend_input_bitrate_bps{input="IPTV #1",uuid="a83ead2b96370a88f68dffe6382d32d9"} 2.092064e+06
tvheadend_muxes{enabled="true",network="THOTH",scan_result="ok"} 1
tvheadend_dvr_entries{config="Default",creator="bob",owner="admin",status="completedError"} 1
```

When a session ends it also writes a log line, which is handy if you ship logs to Loki:

```json
{"time":"2026-09-26T21:00:20Z","level":"INFO","msg":"session_end","event":"session_end","user":"alice","channel":"La 1 HD","client":"Kodi Media Center","peer":"203.0.113.10","profile":"htsp","country":"ES","city":"Madrid","type":"htsp","start":"2026-09-26T20:00:00Z","end":"2026-09-26T21:00:20Z","duration_s":3620,"bytes_out":1523400000,"bytes_in":1523400000,"errors":6,"reason":"gone"}
```

## Prometheus and Grafana

- Scrape config: `deploy/prometheus/scrape.yml`
- Recording and alerting rules: `deploy/prometheus/rules/` (Tvheadend down, polling failing, input with no bitrate, continuity errors, subscription error spikes, user over connection limit or on several IPs, failed recordings, mux scan failures)
- Dashboards (overview, users, channels, sources, DVR, geo, exporter): `deploy/grafana/dashboards/`, with a provisioning file in `deploy/grafana/provisioning/`

## Notes

- Stream URLs, tokens, passwords and DVR programme titles are never exported. If a service is named after its URL, the URL shows up as `<url>`.
- The session counters keep one series per user/channel/client/IP combination and are never deleted. On a server with lots of changing client IPs, drop `peer`/`city` with `metric_relabel_configs` or leave GeoIP off.
- If the network grid doesn't expose the network class, `tvheadend_network_info` reports `type="unknown"`.

## Building

```sh
make build   # bin/tvheadend-exporter
make test
```

## License

MIT
