import copy
import io
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import unittest
from unittest.mock import Mock, patch
from urllib.error import HTTPError

import main
from scripts import remote_control, worker, set_webhook


def update(text="/dockerstart", uid=123):
    return {"update_id": 42, "message": {"from": {"id": uid},
            "chat": {"id": uid, "type": "private"}, "text": text}}


class WebhookTests(unittest.TestCase):
    def test_empty_whitelist_denies_everyone(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "ids"
            path.write_text("# 123\n")
            self.assertEqual(main.whitelist(path), set())
            path.write_text("123\n456 # friend\n")
            self.assertEqual(main.whitelist(path), {123, 456})
            path.write_text("@name\n")
            with self.assertRaises(ValueError):
                main.whitelist(path)

    @patch("main.dispatch")
    @patch("main.send_message")
    @patch("main.whitelist", return_value={123})
    def test_commands_and_denial(self, ids, send, dispatch):
        main.process_update(update())
        dispatch.assert_called_once_with("start", 123, 123, 42)
        dispatch.reset_mock()
        main.process_update(update("/dockerstop"))
        dispatch.assert_called_once_with("stop", 123, 123, 42)
        dispatch.reset_mock()
        main.process_update(update(uid=999))
        send.assert_called_with(999, "无权限执行此操作")
        main.process_update(update("/dockerstart; rm -rf /"))
        main.process_update(update("/dockerstop other-container"))
        dispatch.assert_not_called()

    def test_nonprivate_and_malformed_updates(self):
        for value in [None, [], {}, {"update_id": True}, {"update_id": 3, "message": []}]:
            self.assertIsNone(main.parse_update(value))
        message = update()
        message["message"]["chat"]["type"] = "group"
        self.assertIsNone(main.parse_update(message))
        message = update()
        message["edited_message"] = message.pop("message")
        self.assertIsNone(main.parse_update(message))
        message = update()
        message["message"]["chat"]["id"] = 999
        self.assertIsNone(main.parse_update(message))

    @patch.dict(os.environ, {"GITHUB_REPOSITORY": "owner/repo", "GITHUB_REF": "main",
                             "GITHUB_DISPATCH_TOKEN": "secret"})
    @patch("main.post_json")
    def test_dispatch_payload(self, post):
        main.dispatch("stop", 123, 123, 42)
        args = post.call_args.args
        self.assertIn("docker-control.yml/dispatches", args[0])
        self.assertEqual(args[1]["inputs"]["update_id"], "42")
        self.assertEqual(args[1]["ref"], "main")

    def request(self, body, secret="secret", length=None):
        fake = Mock()
        fake.headers = {"X-Telegram-Bot-Api-Secret-Token": secret,
                        "Content-Length": str(len(body) if length is None else length)}
        fake.rfile = io.BytesIO(body)
        with patch.dict(os.environ, {"TELEGRAM_WEBHOOK_SECRET": "secret"}):
            main.handler.do_POST(fake)
        return fake

    @patch("main.process_update")
    def test_http_auth_validation_and_retry(self, process):
        self.assertEqual(self.request(b"{}", "wrong").reply.call_args.args[0], 403)
        process.assert_not_called()
        self.assertEqual(self.request(b"bad").reply.call_args.args[0], 400)
        self.assertEqual(self.request(b"{}", length=70000).reply.call_args.args[0], 413)
        self.assertEqual(self.request(b"{}").reply.call_args.args[0], 200)
        process.side_effect = RuntimeError("private secret")
        with patch("builtins.print") as output:
            self.assertEqual(self.request(b"{}").reply.call_args.args[0], 503)
            self.assertNotIn("private secret", str(output.call_args))

    @patch("main.process_update")
    def test_upstream_http_error_logs_only_status(self, process):
        process.side_effect = HTTPError(
            "https://example.com/private-token", 403, "private reason", {}, None)
        with patch("builtins.print") as output:
            self.assertEqual(self.request(b"{}").reply.call_args.args[0], 503)
        output.assert_called_once_with("Webhook upstream request failed; HTTP status 403.")


class RemoteTests(unittest.TestCase):
    @patch("scripts.remote_control.subprocess.run")
    def test_fixed_argv_and_durable_deduplication(self, run):
        run.return_value.returncode = 0
        with tempfile.TemporaryDirectory() as tmp:
            db = Path(tmp) / "seen.db"
            self.assertEqual(remote_control.control("start 42", "my-container", db), "ok")
            self.assertEqual(remote_control.control("start 42", "my-container", db), "duplicate")
            run.assert_called_once()
            self.assertEqual(run.call_args.args[0], ["/usr/bin/docker", "start", "--", "my-container"])
            self.assertEqual(remote_control.control("stop 43", "my-container", db), "ok")
            self.assertEqual(remote_control.control("start 42", "my-container", db), "duplicate")

    @patch("scripts.remote_control.subprocess.run")
    def test_invalid_commands_never_execute(self, run):
        for cmd, container in [("start 1; id", "valid"), ("restart 1", "valid"),
                               ("stop 1 other", "valid"), ("start 1", "bad;id")]:
            with self.assertRaises(ValueError):
                remote_control.control(cmd, container, Path("unused"))
        run.assert_not_called()

    @patch("scripts.remote_control.subprocess.run")
    def test_failures_and_timeout_are_claimed(self, run):
        with tempfile.TemporaryDirectory() as tmp:
            db = Path(tmp) / "seen.db"
            run.return_value.returncode = 1
            self.assertEqual(remote_control.control("stop 1", "valid", db), "failed")
            run.side_effect = subprocess.TimeoutExpired("docker", 35)
            self.assertEqual(remote_control.control("start 2", "valid", db), "timeout")
            self.assertEqual(remote_control.control("start 2", "valid", db), "duplicate")


class WorkerTests(unittest.TestCase):
    @patch.dict(os.environ, {"INPUT_ACTION": "start", "INPUT_USER_ID": "123",
                             "INPUT_CHAT_ID": "123", "INPUT_UPDATE_ID": "42"})
    @patch("scripts.worker.whitelist", return_value={123})
    def test_worker_rechecks_authorization(self, ids):
        self.assertEqual(worker.inputs(), ("start", "123", "42"))
        with patch.dict(os.environ, {"INPUT_ACTION": "start;id"}):
            with self.assertRaises(ValueError):
                worker.inputs()
        ids.return_value = set()
        with self.assertRaises(ValueError):
            worker.inputs()

    @patch.dict(os.environ, {"SSH_HOST": "100.1.2.3", "SSH_KNOWN_HOSTS": "placeholder",
                             "SSH_PRIVATE_KEY": "placeholder"})
    @patch("paramiko.Ed25519Key.from_private_key")
    @patch("paramiko.SSHClient")
    def test_host_verification_fixed_user_and_command(self, factory, key):
        import paramiko
        client = factory.return_value
        stdout = Mock()
        stdout.read.return_value = b'{"result":"ok"}'
        stdout.channel.recv_exit_status.return_value = 0
        client.exec_command.return_value = (None, stdout, None)
        self.assertEqual(worker.execute("start", "42"), "容器启动成功。")
        self.assertIsInstance(client.set_missing_host_key_policy.call_args.args[0], paramiko.RejectPolicy)
        self.assertEqual(client.connect.call_args.kwargs["username"], "deploy")
        self.assertFalse(client.connect.call_args.kwargs["look_for_keys"])
        client.exec_command.assert_called_once_with("start 42", timeout=50)
        client.close.assert_called_once()
        client.connect.side_effect = paramiko.AuthenticationException("secret")
        self.assertIn("密钥登录失败", worker.execute("stop", "42"))
        client.connect.side_effect = socket.timeout()
        self.assertIn("超时", worker.execute("stop", "42"))
        client.connect.side_effect = paramiko.SSHException("secret")
        self.assertNotIn("secret", worker.execute("stop", "42"))

    @patch.dict(os.environ, {"WEBHOOK_URL": "https://example.com/webhook",
                             "TELEGRAM_WEBHOOK_SECRET": "a" * 64,
                             "TELEGRAM_BOT_TOKEN": "token"})
    @patch("scripts.set_webhook.post_json", return_value={"ok": True})
    def test_set_webhook_uses_secret_and_keeps_pending(self, post):
        set_webhook.main()
        self.assertEqual(post.call_args.args[1]["secret_token"], "a" * 64)
        self.assertFalse(post.call_args.args[1]["drop_pending_updates"])


class MigrationWebhookTests(unittest.TestCase):
    @patch.dict(os.environ, {"WEBHOOK_URL": "https://example.com/webhook-go",
                             "TELEGRAM_WEBHOOK_SECRET": "a" * 64,
                             "TELEGRAM_BOT_TOKEN": "token"})
    @patch("scripts.set_webhook.post_json", return_value={"ok": True})
    def test_go_url_supported_without_dropping_pending(self, post):
        set_webhook.main()
        self.assertEqual(post.call_args.args[1]["url"], "https://example.com/webhook-go")
        self.assertFalse(post.call_args.args[1]["drop_pending_updates"])
