"""On-demand bounded Upstash maintenance; dry-run by default, no secret output."""
import argparse
from collections import Counter
import json
import os
from pathlib import Path
import re
import urllib.parse
import urllib.request


TARGET_BYTES = 50_000_000
KEY = re.compile(r"^(tgbot:\{[0-9a-f]{64}\}:)(session:([1-9][0-9]*):([1-9][0-9]*)|event:[0-9]+)$")


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None


def request(command):
    url = os.environ.get("UPSTASH_REDIS_REST_URL", "")
    token = os.environ.get("UPSTASH_REDIS_REST_TOKEN", "")
    parsed = urllib.parse.urlsplit(url)
    if parsed.scheme != "https" or not parsed.hostname or parsed.username or parsed.password or parsed.query or parsed.fragment or not token:
        raise ValueError("configuration")
    req = urllib.request.Request(url.rstrip("/"), data=json.dumps(command).encode(), headers={"Authorization": "Bearer " + token, "Content-Type": "application/json"}, method="POST")
    with urllib.request.build_opener(NoRedirect).open(req, timeout=10) as response:
        raw = response.read(1_048_577)
    if len(raw) > 1_048_576:
        raise ValueError("response size")
    value = json.loads(raw)
    if "error" in value or "result" not in value:
        raise ValueError("response")
    return value["result"]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--cursor", default="0", type=lambda s: s if re.fullmatch(r"[0-9]+", s) else parser.error("cursor must be numeric"))
    parser.add_argument("--apply", action="store_true", help="apply safe TTL migration and expired-record deletion")
    args = parser.parse_args()
    # A monitoring failure must not prevent safe classification or affect Bot traffic.
    try:
        info = request(["INFO", "memory"])
        used = int(re.search(r"(?m)^used_memory:([0-9]+)\r?$", info)[1])
        print(json.dumps({"used_memory_bytes": used, "target_bytes": TARGET_BYTES, "near_or_over_target": used >= TARGET_BYTES * 0.9}))
    except Exception:
        print("Memory measurement unavailable; check Upstash console storage metrics.")
    try:
        cursor, keys = request(["SCAN", args.cursor, "MATCH", "tgbot:*", "COUNT", 25])
        # COUNT is a hint, not a limit. Stop without mutation on an oversized page.
        if not isinstance(cursor, str) or not cursor.isdigit() or not isinstance(keys, list) or len(keys) > 1000:
            raise ValueError("scan response")
        script = Path(__file__).with_name("redis_cleanup.lua").read_text(encoding="utf-8")
        counts = Counter()
        for key in keys:
            match = KEY.fullmatch(key)
            if not match:
                counts["retained"] += 1
                continue
            prefix = match[1]
            kind = "session" if match[3] else "event"
            order = prefix + "order:" + match[3] + ":" + match[4] if kind == "session" else prefix + "order:unused"
            action = request(["EVAL", script, 3, key, prefix + "lock", order, kind, "apply" if args.apply else "dry-run"])
            if action not in {"missing", "retained", "protected", "expired-session", "session-ttl", "expired-event", "event-ttl"}:
                raise ValueError("cleanup response")
            counts[action] += 1
        print(json.dumps({"mode": "apply" if args.apply else "dry-run", "counts": counts, "next_cursor": cursor}))
        return 0
    except Exception:
        # Never print exception strings, HTTP bodies, keys, credentials or records.
        print("Maintenance incomplete; retry the same cursor. No protected state is force-deleted.")
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
