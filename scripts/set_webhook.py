"""Configure Telegram using environment values; never print credential-bearing errors."""
import re
import sys
from pathlib import Path
from urllib.parse import urlsplit

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from main import post_json, required


def main():
    url = required("WEBHOOK_URL")
    parsed = urlsplit(url)
    if (parsed.scheme != "https" or not parsed.hostname or parsed.path != "/webhook"
            or parsed.username or parsed.password or parsed.query or parsed.fragment):
        raise ValueError("invalid webhook URL")
    secret = required("TELEGRAM_WEBHOOK_SECRET")
    if not re.fullmatch(r"[A-Za-z0-9_-]{32,256}", secret):
        raise ValueError("invalid webhook secret")
    result = post_json("https://api.telegram.org/bot" + required("TELEGRAM_BOT_TOKEN") + "/setWebhook",
                       {"url": url, "secret_token": secret, "allowed_updates": ["message"],
                        "max_connections": 1, "drop_pending_updates": False})
    if not result or not result.get("ok"):
        raise RuntimeError("registration failed")
    print("Webhook registered successfully.")


if __name__ == "__main__":
    try:
        main()
    except Exception:
        print("Webhook registration failed; check HTTPS URL and Telegram configuration.")
        sys.exit(1)
