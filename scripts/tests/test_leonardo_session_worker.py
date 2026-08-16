import argparse
import importlib.util
import json
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
    def test_complete_cookie_conversion_adapts_chrome_export_fields(self) -> None:
        cookies = worker_module.cookie_json_to_patchright(
            [
                {
                    "name": "session_token",
                    "value": "secret",
                    "domain": ".leonardo.ai",
                    "expirationDate": 1800000000,
                    "hostOnly": False,
                    "httpOnly": True,
                    "sameSite": "no_restriction",
                    "storeId": "0",
                }
            ]
        )
        self.assertEqual(
            cookies,
            [
                {
                    "name": "session_token",
                    "value": "secret",
                    "domain": ".leonardo.ai",
                    "path": "/",
                    "expires": 1800000000,
                    "httpOnly": True,
                    "sameSite": "None",
                }
            ],
        )

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

    def test_complete_cookie_json_preserves_browser_attributes(self) -> None:
        cookies = worker_module.cookie_json_to_patchright(
            [
                {
                    "name": "__Secure-better-auth.session_token",
                    "value": "abc==",
                    "domain": ".leonardo.ai",
                    "path": "/",
                    "httpOnly": True,
                    "secure": True,
                    "sameSite": "Lax",
                    "expires": 1800000000,
                }
            ]
        )
        self.assertEqual(cookies[0]["domain"], ".leonardo.ai")
        self.assertEqual(cookies[0]["expires"], 1800000000)

    def test_private_json_uses_owner_only_permissions(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "private" / "cookies.json"
            worker_module.write_private_json(path, [{"name": "token"}])
            if os.name == "posix":
                self.assertEqual(stat.S_IMODE(os.stat(path).st_mode), 0o600)
            else:
                self.assertTrue(path.is_file())

    def test_failure_diagnostic_is_preserved_and_redacted(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "job-id" / "headed" / "failure-diagnostic.json"
            source.parent.mkdir(parents=True)
            source.write_text(
                json.dumps(
                    {
                        "failure_code": "authentication_rejected",
                        "error_message": "fixture@example.com fixture-password",
                    }
                ),
                encoding="utf-8",
            )

            code = worker_module.preserve_failure_diagnostic(
                source,
                root,
                "job-id",
                "account-id",
                ("fixture@example.com", "fixture-password"),
            )

            self.assertEqual(code, "authentication_rejected")
            preserved = json.loads(
                (root / "diagnostics" / "job-id.json").read_text(encoding="utf-8")
            )
            encoded = json.dumps(preserved)
            self.assertNotIn("fixture@example.com", encoded)
            self.assertNotIn("fixture-password", encoded)
            self.assertEqual(preserved["account_id"], "account-id")

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
            self.assertEqual(
                headed_command[headed_command.index("--cookie-json-source") + 1],
                "browser",
            )
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

    def test_complete_cookie_json_source_is_forwarded_to_sync(self) -> None:
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
                    "job": {"id": "job-id", "account_id": "account-id", "lease_token": "lease-token"},
                    "browser_profile_key": "profile-key",
                    "cookie_json_source": "pending",
                    "cookie_json_fingerprint": "fixture-fingerprint",
                    "cookie_json": [
                        {
                            "name": "__Secure-better-auth.session_token",
                            "value": "value",
                            "domain": ".leonardo.ai",
                        }
                    ],
                },
            )

            self.assertIn("--cookie-json-source", commands[0])
            self.assertEqual(commands[0][commands[0].index("--cookie-json-source") + 1], "pending")
            self.assertEqual(commands[0][commands[0].index("--cookie-json-fingerprint") + 1], "fixture-fingerprint")

    def test_additional_verification_is_reported_as_terminal(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            worker = worker_module.SessionWorker(worker_args(root))

            def run_command(_client, _env, command, _job_id, _lease_token):
                if "--headless" in command:
                    return 1
                output_index = command.index("--output-dir") + 1
                diagnostic = Path(command[output_index]) / "failure-diagnostic.json"
                diagnostic.parent.mkdir(parents=True, exist_ok=True)
                diagnostic.write_text(
                    json.dumps(
                        {"failure_code": "canva_additional_verification_required"}
                    ),
                    encoding="utf-8",
                )
                return 1

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

            self.assertTrue(client.posts[-1][0].endswith("/fail"))
            self.assertIs(client.posts[-1][1]["terminal"], True)
            self.assertIn("manual email verification", client.posts[-1][1]["error"])


if __name__ == "__main__":
    unittest.main()
