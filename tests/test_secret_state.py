"""Real Redis atomicity tests; run only by the remote CI Redis job."""
import concurrent.futures
import json
import os
from pathlib import Path
import re
import subprocess
import unittest


@unittest.skipUnless(os.environ.get("TGBOT_REDIS_TEST"), "remote Redis service required")
class SecretStateTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        source = Path("pkg/bot/secret_state.go").read_text(encoding="utf-8-sig")
        cls.script = re.search(r"const redisSecretScript = `([\s\S]*?)`", source)[1]

    def setUp(self):
        subprocess.run(["redis-cli", "FLUSHDB"], check=True, capture_output=True)

    def call(self, op, update, message=None, user=123, result="safe-result", uncertain=False):
        prefix = "tgbot:{test}:"
        args = ["redis-cli", "--raw", "EVAL", self.script, "4",
                f"{prefix}session:{user}:{user}", f"{prefix}event:{update}",
                f"{prefix}lock", f"{prefix}order:{user}:{user}", op, str(user), str(user), str(update),
                str(message if message is not None else update), result,
                str(uncertain).lower()]
        output = subprocess.run(args, check=True, capture_output=True, text=True).stdout
        return json.loads(output)

    def test_duplicates_and_result_replay(self):
        self.assertEqual(self.call("begin", 10)["state"], "done")
        self.assertEqual(self.call("peek", 11)["state"], "waiting")
        self.assertEqual(self.call("claim", 11)["state"], "claimed")
        self.assertEqual(self.call("claim", 11)["state"], "working")
        self.assertEqual(self.call("finish", 11, result="verified")["state"], "done")
        self.assertEqual(self.call("claim", 11)["result"], "verified")
        self.assertEqual(self.call("claim", 12)["state"], "expired")

    def test_parallel_users_share_target_lock(self):
        for user in (123, 456):
            self.call("begin", user, user=user)
        with concurrent.futures.ThreadPoolExecutor() as pool:
            results = list(pool.map(lambda user: self.call("claim", user+1000, user=user), (123, 456)))
        self.assertCountEqual([r["state"] for r in results], ["claimed", "locked"])

    def test_parallel_duplicate_claims(self):
        self.call("begin", 10)
        with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
            results = list(pool.map(lambda _: self.call("claim", 11), range(8)))
        self.assertEqual(sum(r["state"] == "claimed" for r in results), 1)

    def test_uncertainty_and_crashes_never_unlock(self):
        self.call("begin", 10)
        self.call("claim", 11)
        self.assertEqual(self.call("begin", 12)["state"], "locked")
        self.call("finish", 11, uncertain=True)
        self.assertEqual(self.call("begin", 13)["state"], "locked")

    def test_invalid_input_expiry_and_order(self):
        self.call("begin", 10)
        self.assertEqual(self.call("claim", 9)["state"], "expired")
        self.assertEqual(self.call("invalid", 11)["state"], "done")
        self.assertEqual(self.call("claim", 10)["state"], "done")
        raw = subprocess.run(["redis-cli", "GET", "tgbot:{test}:session:123:123"], check=True, capture_output=True, text=True)
        state = json.loads(raw.stdout)
        state["expires"] = 0
        subprocess.run(["redis-cli", "SET", "tgbot:{test}:session:123:123", json.dumps(state)], check=True, capture_output=True)
        self.assertEqual(self.call("claim", 12)["state"], "expired")
        self.assertEqual(self.call("begin", 9)["state"], "expired")

    def test_large_ids_keep_exact_order(self):
        value = 2**53 + 10
        self.call("begin", value)
        self.assertEqual(self.call("claim", value+1)["state"], "claimed")

    def redis(self, *args):
        return subprocess.run(["redis-cli", "--raw", *map(str, args)], check=True, capture_output=True, text=True).stdout.strip()

    def ttl(self, suffix):
        return int(self.redis("TTL", "tgbot:{test}:" + suffix))

    def test_ttls_and_consumption(self):
        self.call("begin", 10)
        self.assertIn(self.ttl("session:123:123"), range(298, 301))
        self.assertIn(self.ttl("event:10"), range(604798, 604801))
        self.redis("EXPIRE", "tgbot:{test}:session:123:123", 250)
        state = json.loads(self.redis("GET", "tgbot:{test}:session:123:123"))
        state["expires"] -= 50
        self.redis("SET", "tgbot:{test}:session:123:123", json.dumps(state), "EX", 250)
        self.call("invalid", 11)
        self.assertIn(self.ttl("session:123:123"), range(248, 251))
        self.call("claim", 12)
        self.assertEqual(self.ttl("session:123:123"), -2)
        self.assertEqual(self.ttl("event:12"), -1)
        self.assertEqual(self.ttl("lock"), -1)
        self.call("finish", 12)
        self.assertIn(self.ttl("event:12"), range(604798, 604801))
        self.assertEqual(self.ttl("lock"), -2)

    def test_uncertain_done_has_no_ttl(self):
        self.call("begin", 10)
        self.call("claim", 11)
        self.call("finish", 11, uncertain=True)
        self.assertEqual(self.ttl("event:11"), -1)
        self.assertEqual(self.ttl("lock"), -1)

    def test_order_survives_session_and_event_expiry(self):
        self.call("begin", 10)
        self.redis("DEL", "tgbot:{test}:session:123:123", "tgbot:{test}:event:10")
        self.assertEqual(self.call("begin", 9)["state"], "expired")
        self.assertEqual(self.call("begin", 11)["state"], "done")

    def cleanup(self, kind, suffix, apply=True):
        script = Path("scripts/redis_cleanup.lua").read_text(encoding="utf-8")
        prefix = "tgbot:{test}:"
        return self.redis("EVAL", script, 3, prefix + suffix, prefix + "lock", prefix + "order:123:123", kind, "apply" if apply else "dry-run")

    def test_cleanup_migrates_legacy_session_without_losing_order(self):
        self.call("begin", 10)
        self.redis("DEL", "tgbot:{test}:order:123:123")
        state = json.loads(self.redis("GET", "tgbot:{test}:session:123:123"))
        state["expires"] = 1
        self.redis("SET", "tgbot:{test}:session:123:123", json.dumps(state))
        self.assertEqual(self.cleanup("session", "session:123:123", False), "expired-session")
        self.assertEqual(self.ttl("session:123:123"), -1)
        self.assertEqual(self.cleanup("session", "session:123:123"), "expired-session")
        self.assertEqual(self.ttl("session:123:123"), -2)
        self.redis("DEL", "tgbot:{test}:event:10")
        self.assertEqual(self.call("begin", 9)["state"], "expired")
        self.assertEqual(self.cleanup("session", "session:123:123"), "missing")

    def test_cleanup_retains_ambiguous_events_and_protected_state(self):
        for state in ({"state": "working"}, {"state": "done"}, {"state": "done", "completed_at": 1, "uncertain": True}, {"state": "done", "completed_at": "1", "uncertain": False}):
            self.redis("SET", "tgbot:{test}:event:99", json.dumps(state))
            self.assertEqual(self.cleanup("event", "event:99"), "retained")
            self.assertEqual(self.ttl("event:99"), -1)
        self.redis("SET", "tgbot:{test}:event:99", json.dumps({"state": "done", "completed_at": 1, "uncertain": False}))
        self.redis("SET", "tgbot:{test}:lock", "99")
        self.assertEqual(self.cleanup("event", "event:99"), "protected")
        self.redis("DEL", "tgbot:{test}:lock")
        self.assertEqual(self.cleanup("event", "event:99"), "expired-event")
        self.assertEqual(self.ttl("event:99"), -2)

    def test_cleanup_adds_ttl_without_extending_it(self):
        self.call("begin", 10)
        self.redis("PERSIST", "tgbot:{test}:session:123:123")
        self.assertEqual(self.cleanup("session", "session:123:123"), "session-ttl")
        self.assertIn(self.ttl("session:123:123"), range(298, 301))
        self.redis("PERSIST", "tgbot:{test}:event:10")
        self.assertEqual(self.cleanup("event", "event:10"), "event-ttl")
        self.redis("EXPIRE", "tgbot:{test}:event:10", 100)
        self.cleanup("event", "event:10")
        self.assertIn(self.ttl("event:10"), range(98, 101))
