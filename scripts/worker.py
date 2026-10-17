"""Actions-only worker; SSH private keys never enter Vercel."""
import io
import json
import os
import re
import socket
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from main import required, send_message, whitelist


def inputs():
    action = required("INPUT_ACTION")
    uid, chat, update = (required("INPUT_" + key) for key in ("USER_ID", "CHAT_ID", "UPDATE_ID"))
    if action not in {"start", "stop"} or not all(re.fullmatch(r"[0-9]{1,20}", x) for x in (uid, chat, update)):
        raise ValueError("invalid job")
    if int(uid) not in whitelist() or uid != chat:
        raise ValueError("not allowed")
    return action, chat, update


def execute(action, update_id):
    import paramiko
    host = required("SSH_HOST")
    client = paramiko.SSHClient()
    try:
        # load_host_keys + RejectPolicy: unrecognized AND changed keys are refused.
        with __import__("tempfile").TemporaryDirectory() as temp:
            path = Path(temp) / "known_hosts"
            path.write_text(required("SSH_KNOWN_HOSTS") + "\n")
            client.load_host_keys(str(path))
            client.set_missing_host_key_policy(paramiko.RejectPolicy())
            key = paramiko.Ed25519Key.from_private_key(io.StringIO(required("SSH_PRIVATE_KEY")))
            client.connect(host, port=22, username="deploy", pkey=key,
                           allow_agent=False, look_for_keys=False,
                           timeout=10, auth_timeout=10, banner_timeout=10)
            _, stdout, _ = client.exec_command(f"{action} {update_id}", timeout=50)
            output = stdout.read(4096)
            status = stdout.channel.recv_exit_status()
        if status != 0:
            return "远程执行失败：请检查服务器限制脚本、deploy 权限及 Docker 服务。"
        result = json.loads(output)
        return {
            "ok": "容器启动成功。" if action == "start" else "容器停止成功。",
            "duplicate": "这条指令已经处理过，本次未重复执行。",
            "failed": "Docker 操作失败：请检查容器名称、deploy 权限和 Docker 服务。",
            "timeout": "Docker 操作超时，结果暂不确定，请先查看容器状态。",
        }.get(result.get("result"), "服务器返回了无法识别的结果，请检查配置。")
    except paramiko.BadHostKeyException:
        return "服务器身份不匹配，已拒绝连接。请通过服务器控制台核对主机公钥。"
    except paramiko.AuthenticationException:
        return "SSH 密钥登录失败，请检查 deploy 用户和专用公钥。"
    except (socket.timeout, TimeoutError):
        return "连接或执行超时，结果暂不确定，请检查 Tailscale 和容器状态。"
    except Exception:
        return "连接服务器失败，请检查 Tailscale、主机公钥和 SSH 配置。"
    finally:
        client.close()


def main():
    action, chat, update = inputs()
    if "--notify-failure" in sys.argv:
        message = "任务未正常完成，请在 GitHub Actions 查看运行状态；再次操作前先确认容器状态。"
    else:
        message = execute(action, update)
    send_message(chat, message)


if __name__ == "__main__":
    try:
        main()
    except Exception:
        print("Worker failed; check configuration or Telegram availability.")
        sys.exit(1)
