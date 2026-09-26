#!/usr/bin/env python3
"""Generate Grafana 11 dashboards for tvheadend-exporter into deploy/grafana/dashboards/."""
import json, os, pathlib

OUT = pathlib.Path(__file__).resolve().parent.parent / "deploy" / "grafana" / "dashboards"
DS = {"type": "prometheus", "uid": "${DS_PROMETHEUS}"}
W = 24  # grid width


def target(expr, legend="", instant=False, fmt="time_series", ref="A"):
    return {"datasource": DS, "expr": expr, "legendFormat": legend, "refId": ref,
            "instant": instant, "range": not instant, "format": fmt}


def panel(kind, title, targets, w, h, unit=None, opts=None, overrides=None, thresholds=None, desc=""):
    p = {"type": kind, "title": title, "description": desc, "datasource": DS, "targets": targets,
         "gridPos": {"w": w, "h": h, "x": 0, "y": 0},
         "fieldConfig": {"defaults": {}, "overrides": overrides or []}, "options": opts or {}}
    if unit:
        p["fieldConfig"]["defaults"]["unit"] = unit
    if thresholds:
        p["fieldConfig"]["defaults"]["thresholds"] = {"mode": "absolute", "steps": thresholds}
    if kind == "timeseries":
        p["fieldConfig"]["defaults"].setdefault("custom", {"lineWidth": 1, "fillOpacity": 10, "showPoints": "never"})
        p["options"] = {"legend": {"displayMode": "list", "placement": "bottom"}, "tooltip": {"mode": "multi", "sort": "desc"}, **(opts or {})}
    if kind == "stat":
        p["options"] = {"reduceOptions": {"calcs": ["lastNotNull"]}, "colorMode": "value", "graphMode": "area", **(opts or {})}
    if kind == "table":
        p["options"] = {"showHeader": True, "sortBy": [], **(opts or {})}
    return p


def stat(title, expr, w=4, h=4, unit=None, thresholds=None, desc="", legend="", text=None):
    opts = {"textMode": text} if text else None
    return panel("stat", title, [target(expr, legend, instant=True)], w, h, unit=unit, opts=opts, thresholds=thresholds, desc=desc)


def ts(title, expr, legend, w=12, h=8, unit=None, stacked=False, desc=""):
    p = panel("timeseries", title, [target(expr, legend)], w, h, unit=unit, desc=desc)
    if stacked:
        p["fieldConfig"]["defaults"]["custom"]["stacking"] = {"mode": "normal"}
        p["fieldConfig"]["defaults"]["custom"]["fillOpacity"] = 40
    return p


def ts_multi(title, series, w=12, h=8, unit=None, stacked=False, desc="", overrides=None):
    """A timeseries panel with more than one query. `series` is a list of
    (expr, legend, refId) tuples — each becomes its own target() with a
    distinct refId so every promised series is actually plotted."""
    targets = [target(expr, legend, ref=ref) for expr, legend, ref in series]
    p = panel("timeseries", title, targets, w, h, unit=unit, desc=desc, overrides=overrides or [])
    if stacked:
        p["fieldConfig"]["defaults"]["custom"]["stacking"] = {"mode": "normal"}
        p["fieldConfig"]["defaults"]["custom"]["fillOpacity"] = 40
    return p


def bar(title, expr, legend, w=12, h=8, unit=None, desc=""):
    return panel("bargauge", title, [target(expr, legend, instant=True)], w, h, unit=unit,
                 opts={"orientation": "horizontal", "displayMode": "gradient", "reduceOptions": {"calcs": ["lastNotNull"]},
                       "showUnfilled": True, "namePlacement": "left", "sizing": "auto"}, desc=desc)


def table(title, expr, w=24, h=10, rename=None, hide=None, units=None, desc=""):
    p = panel("table", title, [target(expr, instant=True, fmt="table")], w, h, desc=desc)
    org = {"excludeByName": {c: True for c in (hide or ["Time", "__name__", "job", "instance"])},
           "renameByName": rename or {}}
    p["transformations"] = [{"id": "organize", "options": org}]
    for col, unit in (units or {}).items():
        p["fieldConfig"]["overrides"].append({"matcher": {"id": "byName", "options": col}, "properties": [{"id": "unit", "value": unit}]})
    return p


def row(title):
    return {"type": "row", "title": title, "collapsed": False, "gridPos": {"w": W, "h": 1, "x": 0, "y": 0}, "panels": []}


def layout(panels):
    """Assign x/y by flowing panels left-to-right, wrapping at 24 columns; rows force a new line."""
    x = y = line_h = 0
    for p in panels:
        w, h = p["gridPos"]["w"], p["gridPos"]["h"]
        if p["type"] == "row" or x + w > W:
            x, y, line_h = 0, y + line_h, 0
        p["gridPos"].update({"x": x, "y": y})
        x += w
        line_h = max(line_h, h)
        if p["type"] == "row":
            x, y, line_h = 0, y + 1, 0
    for i, p in enumerate(panels, 1):
        p["id"] = i
    return panels


def dashboard(uid, title, panels, variables=(), tags=("tvheadend",), refresh="30s", time_from="now-6h"):
    templating = [{"name": "DS_PROMETHEUS", "type": "datasource", "query": "prometheus", "label": "Datasource",
                   "current": {}, "hide": 0}]
    for v in variables:
        templating.append({"name": v["name"], "type": "query", "datasource": DS, "label": v.get("label", v["name"]),
                           "query": {"query": v["query"], "refId": "A"}, "definition": v["query"], "refresh": 2,
                           "includeAll": v.get("all", True), "multi": v.get("multi", True), "allValue": ".*",
                           "current": {"selected": True, "text": "All", "value": "$__all"}, "sort": 1})
    return {"uid": uid, "title": title, "tags": list(tags), "timezone": "browser", "schemaVersion": 39, "version": 1,
            "editable": True, "graphTooltip": 1, "refresh": refresh, "time": {"from": time_from, "to": "now"},
            "templating": {"list": templating}, "panels": layout(panels),
            "__inputs": [{"name": "DS_PROMETHEUS", "label": "Prometheus", "type": "datasource", "pluginId": "prometheus"}],
            "__requires": [{"type": "datasource", "id": "prometheus", "name": "Prometheus", "version": "1.0.0"},
                           {"type": "grafana", "id": "grafana", "name": "Grafana", "version": "11.0.0"}]}


U = 'user=~"$user"'
C = 'channel=~"$channel"'

DASHBOARDS = {
    "overview": dashboard("tvh-overview", "Tvheadend / Overview", [
        stat("Tvheadend", "tvheadend_up", thresholds=[{"color": "red", "value": None}, {"color": "green", "value": 1}]),
        stat("Version", "tvheadend_build_info", w=4, desc="Tvheadend version", legend="{{version}}", text="name"),
        stat("Active streams", "sum(tvheadend_subscriptions_active)"),
        stat("Unique viewers", "count(tvheadend_user_active_streams)"),
        stat("Total out bandwidth", "sum(tvheadend_subscription_bitrate_out_bps)", unit="bps"),
        stat("Inputs in use", "count(tvheadend_input_subscriptions > 0)"),
        ts("Active streams by type", "sum by (type) (tvheadend_subscriptions_active)", "{{type}}", stacked=True),
        ts("Output bandwidth by user", "sum by (user) (tvheadend_subscription_bitrate_out_bps)", "{{user}}", unit="bps", stacked=True),
        bar("Top channels now", "topk(10, tvheadend_channel_active_viewers)", "{{channel}}"),
        bar("Top channels (24h watch time)", "topk(10, tvheadend:channel_watch_seconds:rate24h * 86400)", "{{channel}}", unit="s"),
        row("Sources"),
        ts("Input bitrate", "tvheadend_input_bitrate_bps", "{{input}}", unit="bps"),
        ts("Input continuity errors/s", "rate(tvheadend_input_continuity_errors_total[5m])", "{{input}}"),
        row("DVR"),
        stat("Recording now", "tvheadend_dvr_active_recordings"),
        stat("Upcoming", "tvheadend_dvr_upcoming_recordings"),
        stat("Failed (total)", 'sum(tvheadend_dvr_entries{status="completedError"})', thresholds=[{"color": "green", "value": None}, {"color": "orange", "value": 1}]),
        stat("Recorded bytes", "sum(tvheadend_dvr_recording_bytes)", unit="bytes"),
        panel("alertlist", "Firing alerts", [], 8, 4, opts={"alertName": "Tvheadend", "stateFilter": {"firing": True, "pending": True}}),
    ]),

    "users": dashboard("tvh-users", "Tvheadend / Users", [
        stat("Active streams", f"sum(tvheadend_user_active_streams{{{U}}})"),
        stat("Watch time (24h)", f"sum(tvheadend:user_watch_seconds:increase24h{{{U}}})", unit="s"),
        stat("Data out (24h)", f"sum(tvheadend:user_bytes_out:increase24h{{{U}}})", unit="bytes"),
        stat("Distinct IPs", f'count(count by (peer) (tvheadend_subscription_info{{{U}}}))'),
        stat("Over conn limit", f"count(tvheadend_user_active_streams{{{U}}} > on (user) (tvheadend_user_conn_limit > 0)) or vector(0)",
             thresholds=[{"color": "green", "value": None}, {"color": "red", "value": 1}]),
        stat("Errors/s", f"sum(rate(tvheadend_session_errors_total{{{U}}}[5m]))"),
        ts("Active streams per user", f"sum by (user) (tvheadend_user_active_streams{{{U}}})", "{{user}}", stacked=True),
        ts("Output bandwidth per user", f"sum by (user) (tvheadend_subscription_bitrate_out_bps{{{U}}})", "{{user}}", unit="bps", stacked=True),
        ts("Watch time per day", f"sum by (user) (increase(tvheadend_session_seconds_total{{{U}}}[1d]))", "{{user}}", unit="s"),
        bar("Channels watched (7d)", f"topk(15, sum by (channel) (increase(tvheadend_session_seconds_total{{{U}}}[7d])))", "{{channel}}", unit="s"),
        # Review fix round 1 (reverses N4 for this panel): spec §9 places this panel under the
        # Users dashboard's $user-scoped section, so it must respect $user. Use the raw
        # increase() over the session counter (matching the other $user-scoped panels here)
        # instead of the tvheadend:client_watch_seconds:increase7d recording rule, which
        # aggregates by `client` only and drops the `user` label needed to filter by $user.
        bar("Client apps (7d)", f"sum by (client) (increase(tvheadend_session_seconds_total{{{U}}}[7d]))", "{{client}}", unit="s"),
        bar("Locations (7d)", f'sum by (country, city) (increase(tvheadend_session_seconds_total{{{U}}}[7d]))', "{{country}} {{city}}", unit="s"),
        # D7 (preflight): title promises p50 AND p90 — plot both as distinct targets/refIds.
        ts_multi("Session duration (p50 / p90)", [
            (f"histogram_quantile(0.5, sum by (le) (rate(tvheadend_session_duration_seconds_bucket{{{U}}}[1h])))", "p50", "A"),
            (f"histogram_quantile(0.9, sum by (le) (rate(tvheadend_session_duration_seconds_bucket{{{U}}}[1h])))", "p90", "B"),
        ], unit="s"),
        table("Live sessions",
              f'tvheadend_subscription_info{{{U}}} * on (id) group_left() tvheadend_subscription_bitrate_out_bps{{{U}}}',
              rename={"user": "User", "channel": "Channel", "client": "Client", "peer": "IP", "country": "Country", "city": "City", "profile": "Profile", "Value": "Out bps"},
              hide=["Time", "__name__", "job", "instance", "id", "service", "state", "title"], units={"Out bps": "bps"}),
    ], variables=[{"name": "user", "query": "label_values(tvheadend_session_seconds_total, user)"}]),

    "channels": dashboard("tvh-channels", "Tvheadend / Channels", [
        bar("Most watched — 1h", f"topk(15, tvheadend:channel_watch_seconds:rate1h{{{C}}} * 3600)", "{{channel}}", unit="s", w=8, h=12),
        bar("Most watched — 24h", f"topk(15, tvheadend:channel_watch_seconds:rate24h{{{C}}} * 86400)", "{{channel}}", unit="s", w=8, h=12),
        bar("Most watched — 7d", f"topk(15, tvheadend:channel_watch_seconds:increase7d{{{C}}})", "{{channel}}", unit="s", w=8, h=12),
        ts("Viewers over time", f"tvheadend_channel_active_viewers{{{C}}}", "{{channel}}", stacked=True),
        ts("Unique viewers (24h)", f"count by (channel) (count by (channel, user) (increase(tvheadend_session_seconds_total{{{C}}}[24h]) > 0))", "{{channel}}"),
        ts("Error rate per channel", f"sum by (channel) (rate(tvheadend_session_errors_total{{{C}}}[5m]))", "{{channel}}"),
        ts("Sessions started per hour", f"sum by (channel) (increase(tvheadend_sessions_started_total{{{C}}}[1h]))", "{{channel}}"),
    ], variables=[{"name": "channel", "query": "label_values(tvheadend_session_seconds_total, channel)"}]),

    "sources": dashboard("tvh-sources", "Tvheadend / Sources & Inputs", [
        ts("Input bitrate", "tvheadend_input_bitrate_bps", "{{input}}", unit="bps"),
        ts("Subscribers per input", "tvheadend_input_subscriptions", "{{input}}", stacked=True),
        ts("Continuity errors/s", "rate(tvheadend_input_continuity_errors_total[5m])", "{{input}}"),
        ts("Transport errors/s", "rate(tvheadend_input_transport_errors_total[5m])", "{{input}}"),
        ts("Signal (DVB)", 'tvheadend_input_signal{input!~"IPTV.*"}', "{{input}}"),
        ts("SNR (DVB)", 'tvheadend_input_snr{input!~"IPTV.*"}', "{{input}}"),
        # S3 (preflight): spec §9 asks for "tune history from input_info" — a state timeline
        # showing which stream each input was tuned to over time.
        panel("state-timeline", "Tune history", [target("tvheadend_input_info", "{{input}}: {{stream}}")], 24, 8,
              opts={"mergeValues": True, "showValue": "never", "rowHeight": 0.9, "tooltip": {"mode": "single"}},
              desc="Which stream each input was tuned to over time, derived from input_info."),
        table("Current tune per input", "tvheadend_input_info",
              rename={"input": "Input", "stream": "Stream", "weight": "Weight"}, hide=["Time", "__name__", "job", "instance", "uuid", "Value"]),
        row("Networks"),
        bar("Muxes by scan result", "sum by (network, scan_result) (tvheadend_muxes)", "{{network}} / {{scan_result}}", w=12, h=10),
        bar("Services encrypted vs clear", "sum by (network, encrypted) (tvheadend_services)", "{{network}} / encrypted={{encrypted}}", w=12, h=10),
        # S3 (preflight): spec §9 also asks for services "mapped", not just encrypted/clear.
        bar("Services mapped", "sum by (network, mapped) (tvheadend_services)", "{{network}} / mapped={{mapped}}", w=12, h=10),
        ts("Scan queue length", "tvheadend_network_scan_queue_length", "{{name}}"),
        table("Network inventory", "tvheadend_network_info * on (name) group_left() tvheadend_network_muxes",
              rename={"name": "Network", "type": "Type", "enabled": "Enabled", "Value": "Muxes"}, hide=["Time", "__name__", "job", "instance", "uuid"]),
    ]),

    "dvr": dashboard("tvh-dvr", "Tvheadend / DVR", [
        stat("Recording now", "tvheadend_dvr_active_recordings"),
        stat("Upcoming", "tvheadend_dvr_upcoming_recordings"),
        stat("Completed", 'sum(tvheadend_dvr_entries{status="completed"})'),
        stat("Failed", 'sum(tvheadend_dvr_entries{status="completedError"})', thresholds=[{"color": "green", "value": None}, {"color": "orange", "value": 1}]),
        stat("Storage used", "sum(tvheadend_dvr_recording_bytes)", unit="bytes"),
        stat("Autorec / Timerec", "tvheadend_dvr_autorec_rules + tvheadend_dvr_timerec_rules"),
        ts("Entries by status", "sum by (status) (tvheadend_dvr_entries)", "{{status}}", stacked=True),
        # S2 (preflight): dvr_entries is a gauge — increase() over it misreads deletions as
        # counter resets and can fire/plot spuriously. Use delta() instead.
        # `or vector(0)`: the exporter emits no series for an absent status, so
        # without it the panel shows "No data" on a clean system and misses the
        # first failure (a one-point subquery has no delta).
        ts("Failed recordings (new per day)", 'delta((sum(tvheadend_dvr_entries{status="completedError"}) or vector(0))[1d:1h])', "failed"),
        bar("Recordings by creator", "sum by (creator) (tvheadend_dvr_entries)", "{{creator}}"),
        # S3 (preflight): spec §9 asks for entries "by status/owner" — add owner breakdown.
        bar("Recordings by owner", "sum by (owner) (tvheadend_dvr_entries)", "{{owner}}"),
        bar("Bytes by config", "sum by (config) (tvheadend_dvr_recording_bytes)", "{{config}}", unit="bytes"),
        ts("Data errors", "sum by (status) (tvheadend_dvr_data_errors)", "{{status}}"),
        table("DVR configs", "tvheadend_dvr_config_info", rename={"name": "Config", "storage": "Storage", "profile": "Profile"}, hide=["Time", "__name__", "job", "instance", "uuid", "Value"]),
    ], refresh="1m"),

    "geo": dashboard("tvh-geo", "Tvheadend / Geo", [
        panel("geomap", "Viewers by location (24h)", [target('sum by (country) (increase(tvheadend_session_seconds_total{country!="",country!="private"}[24h]))', instant=True, fmt="table")], 24, 14,
              opts={"view": {"id": "zero", "lat": 40, "lon": 0, "zoom": 3},
                    "layers": [{"type": "markers", "name": "Viewers", "location": {"mode": "lookup", "lookup": "country"},
                                "config": {"style": {"size": {"field": "Value", "min": 4, "max": 20}, "color": {"fixed": "blue"}}}}],
                    "basemap": {"type": "default"}},
              desc="Requires TVH_GEOIP_DB. Markers are placed by ISO country code via Grafana's built-in gazetteer; city-level detail is in the bars below."),
        bar("Watch time by country (7d)", 'sum by (country) (increase(tvheadend_session_seconds_total{country!=""}[7d]))', "{{country}}", unit="s"),
        bar("Watch time by city (7d)", 'topk(20, sum by (country, city) (increase(tvheadend_session_seconds_total{city!=""}[7d])))', "{{city}} ({{country}})", unit="s"),
        table("Users by IP and location", 'count by (user, peer, country, city) (tvheadend_subscription_info)',
              rename={"user": "User", "peer": "IP", "country": "Country", "city": "City", "Value": "Streams"}),
    ], refresh="1m"),

    "exporter": dashboard("tvh-exporter", "Tvheadend / Exporter health", [
        stat("Up", "tvheadend_up", thresholds=[{"color": "red", "value": None}, {"color": "green", "value": 1}]),
        stat("Sessions tracked", "tvheadend_exporter_sessions_tracked"),
        stat("Poll errors (1h)", "sum(increase(tvheadend_exporter_poll_errors_total[1h]))", thresholds=[{"color": "green", "value": None}, {"color": "orange", "value": 1}]),
        stat("Last status success", "time() - tvheadend_exporter_last_success_timestamp_seconds{group=\"status\"}", unit="s",
             thresholds=[{"color": "green", "value": None}, {"color": "red", "value": 60}]),
        stat("Exporter version", "tvheadend_exporter_build_info", w=8, legend="{{version}} ({{commit}})", text="name"),
        ts("Poll duration p90 by group", "histogram_quantile(0.9, sum by (group, le) (rate(tvheadend_exporter_poll_duration_seconds_bucket[5m])))", "{{group}}", unit="s"),
        ts("API latency p90 by endpoint", "histogram_quantile(0.9, sum by (endpoint, le) (rate(tvheadend_exporter_api_request_duration_seconds_bucket[5m])))", "{{endpoint}}", unit="s"),
        ts("API requests/s by code", "sum by (code) (rate(tvheadend_exporter_api_requests_total[5m]))", "{{code}}", stacked=True),
        ts("GeoIP lookups/s", "sum by (result) (rate(tvheadend_exporter_geoip_lookups_total[5m]))", "{{result}}"),
        # D7 (preflight): title promises memory AND goroutines — plot both, with a per-series
        # unit override since goroutines is a plain count, not bytes.
        ts_multi("Memory / goroutines", [
            ("process_resident_memory_bytes", "RSS", "A"),
            ("go_goroutines", "Goroutines", "B"),
        ], unit="bytes", overrides=[{"matcher": {"id": "byFrameRefID", "options": "B"}, "properties": [{"id": "unit", "value": "short"}]}]),
    ]),
}


def main():
    OUT.mkdir(parents=True, exist_ok=True)
    for name, d in DASHBOARDS.items():
        path = OUT / f"{name}.json"
        path.write_text(json.dumps(d, indent=2, sort_keys=False) + "\n")
        print("wrote", os.path.relpath(path))


if __name__ == "__main__":
    main()
