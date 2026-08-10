import importlib.util
import json
import sys
import tempfile
import types
import unittest
from pathlib import Path


if "requests" not in sys.modules:
    sys.modules["requests"] = types.SimpleNamespace(Session=object)

MODULE_PATH = Path(__file__).parents[1] / "leonardo_sync_session.py"
SPEC = importlib.util.spec_from_file_location("leonardo_sync_session", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
sync_module = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(sync_module)


class SyncSessionTests(unittest.TestCase):
    def test_load_browser_session_includes_refreshed_cookie(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            output_dir = Path(directory)
            (output_dir / "session-token.json").write_text(
                json.dumps({"access_token": "jwt", "verified": True, "status": 200}),
                encoding="utf-8",
            )
            (output_dir / "cookie-header.txt").write_text(
                "next-auth.session-token=fresh-cookie",
                encoding="utf-8",
            )

            session = sync_module.load_browser_session(output_dir)

            self.assertEqual(session["access_token"], "jwt")
            self.assertNotIn("status", session)
            self.assertEqual(
                session["cookie_header"],
                "next-auth.session-token=fresh-cookie",
            )

    def test_import_error_uses_sanitized_api_message(self) -> None:
        class Response:
            ok = False
            status_code = 400

            @staticmethod
            def json() -> dict[str, object]:
                return {"error": {"message": "browser session is incomplete"}}

        response = Response()
        message = f"HTTP {response.status_code}"
        payload = response.json()
        error = payload.get("error") or {}
        detail = str(error.get("message") or error.get("code") or "").strip()
        if detail:
            message = f"{message}: {detail[:300]}"

        self.assertEqual(message, "HTTP 400: browser session is incomplete")


if __name__ == "__main__":
    unittest.main()
