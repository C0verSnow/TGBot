#!/usr/bin/env python3
"""Install root-owned at /usr/local/lib/tgbot-control.py; use an SSH forced command."""
import fcntl
import json
import os
from pathlib import Path
import re
import sqlite3
import subprocess


def control(command, container, database):
    match = re.fullmatch(r"(start|stop) ([0-9]{1,20})", command)
    if not match or not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,127}", container):
        raise ValueError("invalid command or container")
    action, update = match.groups()
    # A separate flock covers both the durable claim and Docker execution.
    # A crash after claiming means no replay: operator must check state and send a NEW command.
    with open(str(database) + ".lock", "a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        with sqlite3.connect(database) as db:
            db.execute("CREATE TABLE IF NOT EXISTS seen (id TEXT PRIMARY KEY)")
            if db.execute("SELECT 1 FROM seen WHERE id = ?", (update,)).fetchone():
                return "duplicate"
            db.execute("INSERT INTO seen VALUES (?)", (update,))
            db.commit()
            try:
                result = subprocess.run(["/usr/bin/docker", action, "--", container],
                                        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                                        timeout=35, check=False)
                return "ok" if result.returncode == 0 else "failed"
            except subprocess.TimeoutExpired:
                return "timeout"
            except OSError:
                return "failed"


def main():
    os.umask(0o077)
    container = os.environ.get("DOCKER_CONTAINER", "")
    database = Path.home() / ".local/state/tgbot/updates.sqlite3"
    database.parent.mkdir(parents=True, exist_ok=True)
    result = control(os.environ.get("SSH_ORIGINAL_COMMAND", ""), container, database)
    print(json.dumps({"result": result}))


if __name__ == "__main__":
    try:
        main()
    except Exception:
        print('{"result":"failed"}')
        raise SystemExit(1)
