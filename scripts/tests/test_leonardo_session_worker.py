import argparse
import importlib.util
import os
import stat
import sys
import tempfile
import types
import unittest
from pathlib import Path


if "requests" not in sys.modules:
    sys.modules["requests"] = types.SimpleNamespace(Session=object, RequestException=Exception)

MODULE_PATH = Path(__file__).parents[1] / "leonardo_session_worker.py"
SPEC = importlib.util.spec_from_file_location("leonardo_session_worker", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
worker_module = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(worker_module)


class FakeResponse:
    def raise_for_status(self) -> None:
        return None


class FakeClient:
    def __init__(self) -> None:
        self.posts: list[tuple[str, dict[str, object]]] = []

    def post(self, url: str, json: dict[str, object], timeout: int) -> FakeResponse:
        self.posts.append((url, json))
        return FakeResponse()


def worker_args(root: Path) -> argparse.Namespace:
    env_file = root / "worker.env"
    env_file.write_text("LEO_SESSION_SYNC_TOKEN=test-token\n", encoding="utf-8")
    return argparse.Namespace(
        python="python",
        sync_script=root / "sync.py",
        login_script=root / "login.py",
        profile_root=root / "profiles",
        output_root=root / "output",
        base_url="http://127.0.0.1:18080",
        env_file=env_file,
        proxy="http://127.0.0.1:7890",
        channel="chrome",
        concurrency=1,
        worker_group="default",
        idle_poll=0.1,
        heartbeat=60.0,
        solve_captcha=True,
        headless_refresh=True,
        headless_channel="chrome",
        headless_login_timeout=60,
        headless_process_timeout=120,
    )


class SessionWorkerTests(unittest.TestCase):
    def test_cookie_header_conversion_preserves_equals(self) -> None:
        cookies = worker_module.cookie_header_to_patchright(
            "next-auth.session-token=abc==; theme=dark; malformed"
        )
        self.assertEqual(
            cookies,
            [
                {
                    "name": "next-auth.session-token",
                    "value": "abc==",
                    "url": "https://app.leonardo.ai",
                },
                {
                    "name": "theme",
                    "value": "dark",
                    "url": "https://app.leonardo.ai",
                },
            ],
        )

    def test_private_json_uses_owner_only_permissions(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "private" / "cookies.json"
            worker_module.write_private_json(path, [{"name": "token"}])
            if os.name == "posix":
                self.assertEqual(stat.S_IMODE(os.stat(path).st_mode), 0o600)
            else:
                self.assertTrue(path.is_file())

    def test_headless_failure_falls_back_to_headed(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            worker = worker_module.SessionWorker(worker_args(root))
            commands: list[list[str]] = []

            def run_command(_client, _env, command, _job_id, _lease_token):
                commands.append(command)
                return 1 if len(commands) == 1 else 0

            worker.run_command = run_command
            client = FakeClient()
            worker.execute_job(
                client,
                {},
                {
                    "job": {
                        "id": "job-id",
                        "account_id": "account-id",
                        "lease_token": "lease-token",
                    },
                    "browser_profile_key": "profile-key",
                    "proxy_url": "",
                    "cookie_header": "next-auth.session-token=value",
                },
            )

            self.assertEqual(len(commands), 2)
            self.assertIn("--headless", commands[0])
            self.assertIn("--import-cookies", commands[0])
            self.assertNotIn("--solve-captcha", commands[0])
            self.assertEqual(commands[0][commands[0].index("--channel") + 1], "chrome")
            self.assertNotIn("--backend", commands[0])
            self.assertNotIn("--headless", commands[1])
            self.assertIn("--solve-captcha", commands[1])
            self.assertNotIn("--backend", commands[1])
            profile_index = commands[1].index("--profile-dir") + 1
            self.assertEqual(Path(commands[1][profile_index]), root / "profiles" / "profile-key")
            self.assertTrue(client.posts[-1][0].endswith("/complete"))

    def test_login_credentials_use_temporary_headed_profile_and_child_environment(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            worker = worker_module.SessionWorker(worker_args(root))
            calls: list[tuple[list[str], dict[str, str]]] = []

            def run_command(_client, env, command, _job_id, _lease_token):
                calls.append((command, env.copy()))
                return 1 if len(calls) == 1 else 0

            worker.run_command = run_command
            client = FakeClient()
            worker.execute_job(
                client,
                {},
                {
                    "job": {
                        "id": "job-id",
                        "account_id": "account-id",
                        "lease_token": "lease-token",
                    },
                    "browser_profile_key": "profile-key",
                    "cookie_header": "next-auth.session-token=value",
                    "login_email": "fixture@example.com",
                    "login_password": "fixture-password",
                },
            )

            self.assertEqual(len(calls), 2)
            headed_command, headed_env = calls[1]
            profile_index = headed_command.index("--profile-dir") + 1
            self.assertEqual(
                Path(headed_command[profile_index]),
                root / "output" / "job-id" / "headed-profile",
            )
            self.assertEqual(headed_env["LEONARDO_EMAIL"], "fixture@example.com")
            self.assertEqual(headed_env["LEONARDO_PASSWORD"], "fixture-password")
            self.assertNotIn("fixture@example.com", headed_command)
            self.assertNotIn("fixture-password", headed_command)
            self.assertFalse((root / "output" / "job-id").exists())

    def test_headless_success_skips_headed_browser(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            worker = worker_module.SessionWorker(worker_args(root))
            commands: list[list[str]] = []

            def run_command(_client, _env, command, _job_id, _lease_token):
                commands.append(command)
                return 0

            worker.run_command = run_command
            client = FakeClient()
            worker.execute_job(
                client,
                {},
                {
                    "job": {
                        "id": "job-id",
                        "account_id": "account-id",
                        "lease_token": "lease-token",
                    },
                    "browser_profile_key": "profile-key",
                    "cookie_header": "next-auth.session-token=value",
                },
            )

            self.assertEqual(len(commands), 1)
            self.assertIn("--headless", commands[0])
            self.assertTrue(client.posts[-1][0].endswith("/complete"))


if __name__ == "__main__":
    unittest.main()
