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
        args = ["redis-cli", "--raw", "EVAL", self.script, "3",
                f"{prefix}session:{user}:{user}", f"{prefix}event:{update}",
                f"{prefix}lock", op, str(user), str(user), str(update),
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
