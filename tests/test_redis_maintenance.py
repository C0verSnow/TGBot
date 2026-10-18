"""Maintenance transport tests with fake data; executed by remote CI."""
import contextlib
import io
import json
from unittest import mock
import unittest

from scripts import redis_maintenance as maintenance


class MaintenanceTests(unittest.TestCase):
    def run_main(self, request, args=()):
        output = io.StringIO()
        with mock.patch.object(maintenance, "request", side_effect=request), mock.patch("sys.argv", ["maintenance", *args]), contextlib.redirect_stdout(output):
            status = maintenance.main()
        return status, output.getvalue()

    def test_dry_run_scope_and_counts(self):
        prefix = "tgbot:{" + "a" * 64 + "}:"
        commands = []

        def request(command):
            commands.append(command)
            if command[0] == "INFO":
                return "used_memory:49000000\r\n"
            if command[0] == "SCAN":
                return ["5", [prefix + "session:123:123", prefix + "lock", "other:secret"]]
            self.assertEqual(command[0], "EVAL")
            self.assertEqual(command[-1], "dry-run")
            self.assertEqual(command[3:6], [prefix + "session:123:123", prefix + "lock", prefix + "order:123:123"])
            return "session-ttl"

        status, output = self.run_main(request)
        self.assertEqual(status, 0)
        self.assertEqual(len(commands), 3)
        result = json.loads(output.splitlines()[-1])
        self.assertEqual(result["next_cursor"], "5")
        self.assertEqual(result["counts"], {"session-ttl": 1, "retained": 2})
        self.assertNotIn(prefix, output)

    def test_monitoring_failure_does_not_stop_cleanup(self):
        def request(command):
            if command[0] == "INFO":
                raise ValueError("sensitive-token")
            self.assertEqual(command[1], "42")
            return ["0", []]

        status, output = self.run_main(request, ["--apply", "--cursor", "42"])
        self.assertEqual(status, 0)
        self.assertIn('"mode": "apply"', output)
        self.assertNotIn("sensitive-token", output)

    def test_cleanup_errors_are_redacted_and_retryable(self):
        status, output = self.run_main(lambda _: (_ for _ in ()).throw(ValueError("sensitive-token")))
        self.assertEqual(status, 1)
        self.assertIn("retry the same cursor", output)
        self.assertNotIn("sensitive-token", output)

    def test_oversized_scan_has_no_mutation(self):
        def request(command):
            if command[0] == "INFO":
                return "used_memory:1\r\n"
            self.assertEqual(command[0], "SCAN")
            return ["1", ["anything"] * 1001]

        status, _ = self.run_main(request, ["--apply"])
        self.assertEqual(status, 1)
