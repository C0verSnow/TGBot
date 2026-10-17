"""Short-lived Telegram webhook. Private networking belongs to the Actions worker."""
import hmac
import json
import os
import re
from http.server import BaseHTTPRequestHandler
from pathlib import Path
from urllib import request

ROOT = Path(__file__).resolve().parent
COMMANDS = {"/dockerstart": "start", "/dockerstop": "stop"}


def required(name):
    value = os.environ.get(name, "").strip()
    if not value:
        raise ValueError("missing configuration")
    return value


def whitelist(path=None):
    ids = set()
    for line in (path or ROOT / "idlist.md").read_text(encoding="utf-8").splitlines():
        value = line.split("#", 1)[0].strip()
        if not value:
            continue
        if not re.fullmatch(r"[1-9][0-9]*", value):
            raise ValueError("invalid whitelist")
        ids.add(int(value))
    return ids


def post_json(url, body, headers=None, timeout=5):
    req = request.Request(url, json.dumps(body).encode(),
                          {"Content-Type": "application/json", **(headers or {})}, method="POST")
    with request.urlopen(req, timeout=timeout) as response:
        data = response.read()
    return json.loads(data) if data else None


def send_message(chat_id, text):
    result = post_json("https://api.telegram.org/bot" + required("TELEGRAM_BOT_TOKEN") + "/sendMessage",
                       {"chat_id": chat_id, "text": text})
    if not result or not result.get("ok"):
        raise RuntimeError("notification failed")


def parse_update(update):
    """Only original private text messages; never edited posts, channels or group commands."""
    if not isinstance(update, dict) or type(update.get("update_id")) is not int:
        return None
    message = update.get("message")
    if not isinstance(message, dict):
        return None
    sender, chat = message.get("from"), message.get("chat")
    if not isinstance(sender, dict) or not isinstance(chat, dict):
        return None
    uid, cid = sender.get("id"), chat.get("id")
    if (type(uid) is not int or uid <= 0 or type(cid) is not int or cid != uid
            or chat.get("type") != "private" or sender.get("is_bot", False)):
        return None
    text = message.get("text", "")
    if not isinstance(text, str):
        return None
    return uid, cid, COMMANDS.get(text.strip()), update["update_id"]


def dispatch(action, uid, chat_id, update_id):
    repo = required("GITHUB_REPOSITORY")
    if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repo):
        raise ValueError("invalid repository")
    return post_json(
        f"https://api.github.com/repos/{repo}/actions/workflows/docker-control.yml/dispatches",
        {"ref": required("GITHUB_REF"), "inputs": {
            "action": action, "user_id": str(uid), "chat_id": str(chat_id),
            "update_id": str(update_id)}},
        {"Authorization": "Bearer " + required("GITHUB_DISPATCH_TOKEN"),
         "Accept": "application/vnd.github+json", "X-GitHub-Api-Version": "2022-11-28"})


def process_update(update):
    parsed = parse_update(update)
    if parsed is None:
        return
    uid, chat_id, action, update_id = parsed
    if uid not in whitelist():
        send_message(chat_id, "无权限执行此操作")
        return
    if action is None:
        send_message(chat_id, "请发送 /dockerstart 或 /dockerstop，不要附带其他内容。")
        return
    dispatch(action, uid, chat_id, update_id)
    # Do not send a second network request here: ACK promptly after durable dispatch.
    # The worker sends the final result. Telegram retries on dispatch/transport failure.


class handler(BaseHTTPRequestHandler):
    def reply(self, code, value):
        data = json.dumps(value).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        self.reply(200, {"ok": True})

    def do_POST(self):
        try:
            secret = required("TELEGRAM_WEBHOOK_SECRET")
        except ValueError:
            self.reply(503, {"ok": False})
            return
        supplied = self.headers.get("X-Telegram-Bot-Api-Secret-Token", "")
        if not hmac.compare_digest(supplied.encode(), secret.encode()):
            self.reply(403, {"ok": False})
            return
        try:
            length = int(self.headers.get("Content-Length", "0"))
            if not 0 < length <= 65536:
                self.reply(413, {"ok": False})
                return
            update = json.loads(self.rfile.read(length))
        except (ValueError, UnicodeError):
            self.reply(400, {"ok": False})
            return
        try:
            process_update(update)
        except Exception:
            # Never log exceptions containing bot URLs, tokens, or remote output.
            print("Webhook processing failed; check service configuration and availability.")
            self.reply(503, {"ok": False})
            return
        self.reply(200, {"ok": True})

    def log_message(self, format, *args):
        pass
