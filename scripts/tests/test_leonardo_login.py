import asyncio
import importlib.util
import types
import unittest
from pathlib import Path


MODULE_PATH = Path(__file__).parents[1] / "leonardo_login.py"
SPEC = importlib.util.spec_from_file_location("leonardo_login", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
login_module = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(login_module)


class FakePage:
    def __init__(
        self,
        responses: list[dict[str, object]],
        url: str = "https://app.leonardo.ai/",
    ) -> None:
        self.responses = responses
        self.waits: list[int] = []
        self.url = url

    async def evaluate(
        self, _expression: str, _argument: object | None = None
    ) -> dict[str, object]:
        return self.responses.pop(0)

    async def wait_for_timeout(self, milliseconds: int) -> None:
        self.waits.append(milliseconds)


class FakeCheckbox:
    def __init__(self, visible: bool, on_click=None) -> None:
        self.visible = visible
        self.on_click = on_click
        self.clicked = False

    async def is_visible(self) -> bool:
        return self.visible

    async def click(self) -> None:
        self.clicked = True
        if self.on_click is not None:
            self.on_click()


class FakeFrame:
    def __init__(self, url: str, checkboxes: list[FakeCheckbox]) -> None:
        self.url = url
        self.checkboxes = checkboxes

    def is_detached(self) -> bool:
        return False

    async def query_selector_all(self, _selector: str) -> list[FakeCheckbox]:
        return self.checkboxes


class FakeLocator:
    def __init__(self, page, kind: str, count: int | None = None) -> None:
        self.page = page
        self.kind = kind
        self.fixed_count = count

    async def count(self) -> int:
        if self.fixed_count is not None:
            return self.fixed_count
        return 1 if self.kind == "response" else 0

    def nth(self, _index: int):
        return self

    async def input_value(self) -> str:
        return "verified" if self.page.completed else ""

    async def is_enabled(self) -> bool:
        return True


class FakeCaptchaPage:
    def __init__(self) -> None:
        self.completed = False
        self.checkbox = FakeCheckbox(True, lambda: setattr(self, "completed", True))
        self.frames = [
            FakeFrame("https://challenges.cloudflare.com/widget", [self.checkbox])
        ]
        self.waits: list[int] = []

    def locator(self, selector: str) -> FakeLocator:
        kind = "response" if "cf-turnstile-response" in selector else "button"
        return FakeLocator(self, kind, count=1)

    async def wait_for_timeout(self, milliseconds: int) -> None:
        self.waits.append(milliseconds)


class BrowserSessionTests(unittest.TestCase):
    def test_empty_turnstile_response_is_not_completed_by_enabled_button(self) -> None:
        page = FakeCaptchaPage()

        self.assertFalse(asyncio.run(login_module.captcha_completed(page)))

    def test_patchright_turnstile_solver_clicks_and_completes(self) -> None:
        page = FakeCaptchaPage()

        asyncio.run(login_module.solve_captcha(page))

        self.assertTrue(page.checkbox.clicked)
        self.assertTrue(page.completed)

    def test_turnstile_checkbox_uses_patchright_frame(self) -> None:
        hidden = FakeCheckbox(False)
        visible = FakeCheckbox(True)
        page = types.SimpleNamespace(
            frames=[
                FakeFrame("https://app.leonardo.ai/", [visible]),
                FakeFrame("https://challenges.cloudflare.com/widget", [hidden, visible]),
            ]
        )

        checkbox = asyncio.run(login_module.find_turnstile_checkbox(page))

        self.assertIs(checkbox, visible)

    def test_checkpoint_is_retried_through_page_context(self) -> None:
        page = FakePage(
            [
                {"status": 429, "verified": False},
                {"status": 200, "verified": True, "access_token": "jwt"},
            ]
        )

        session = asyncio.run(login_module.fetch_browser_session(page))

        self.assertTrue(session["verified"])
        self.assertEqual(page.waits, [2000])

    def test_non_checkpoint_response_is_not_retried(self) -> None:
        page = FakePage([{"status": 200, "verified": False}])

        self.assertFalse(asyncio.run(login_module.authenticated(page)))
        self.assertEqual(page.waits, [])

    def test_login_requires_verified_session_after_navigation(self) -> None:
        page = FakePage(
            [{"status": 200, "verified": False}] * 6,
            url="https://app.leonardo.ai/launch-app",
        )

        with self.assertRaises(login_module.LoginAuthenticationError) as raised:
            asyncio.run(
                login_module.wait_for_authenticated_login(page, 30, [])
            )

        self.assertEqual(raised.exception.code, "authenticated_session_missing")
        self.assertEqual(page.waits, [2000, 2000, 2000, 2000])

    def test_login_reports_auth_endpoint_rejection(self) -> None:
        page = FakePage(
            [{"status": 200, "verified": False}],
            url="https://app.leonardo.ai/auth/login",
        )
        responses = [
            {
                "path": "/api/auth/sign-in/email",
                "status": 200,
                "error_fields": [
                    {"field": "error.message", "value": "Invalid credentials"}
                ],
            }
        ]

        with self.assertRaises(login_module.LoginAuthenticationError) as raised:
            asyncio.run(
                login_module.wait_for_authenticated_login(page, 30, responses)
            )

        self.assertEqual(raised.exception.code, "authentication_rejected")

    def test_success_message_is_not_an_authentication_rejection(self) -> None:
        responses = [
            {
                "path": "/api/auth/sign-in/email",
                "status": 200,
                "success": True,
                "error_fields": [
                    {"field": "message", "value": "Sign in completed"}
                ],
            }
        ]

        self.assertFalse(login_module.response_rejected_authentication(responses))

    def test_canva_sso_lock_is_detected_without_generic_rejection(self) -> None:
        responses = [
            {
                "path": "/api/auth/sign-in/email",
                "status": 401,
                "error_fields": [
                    {"field": "code", "value": "CANVA_SSO_LOCK"},
                    {"field": "message", "value": "Sign in with Canva"},
                ],
            }
        ]

        self.assertTrue(login_module.canva_sso_required(responses))
        self.assertFalse(login_module.response_rejected_authentication(responses))

    def test_sanitized_error_fields_remove_email_and_tokens(self) -> None:
        token = "eyJabcdefghijk.abcdefghijklmnop.abcdefghijklmnop"
        fields = login_module.safe_error_fields(
            {
                "message": f"Login failed for fixture@example.com with {token}",
                "access_token": token,
                "nested": {"reason": "fixture-password was rejected"},
            },
            ("fixture-password",),
        )

        encoded = str(fields)
        self.assertNotIn("fixture@example.com", encoded)
        self.assertNotIn("fixture-password", encoded)
        self.assertNotIn(token, encoded)
        self.assertNotIn("access_token", encoded)
        self.assertIn("[redacted-email]", encoded)
        self.assertIn("[redacted-secret]", encoded)

    def test_form_outline_contains_no_input_values(self) -> None:
        expression = login_module.collect_form_outline.__code__.co_consts

        self.assertNotIn("element.value", str(expression))

    def test_canva_email_locator_supports_username_webauthn(self) -> None:
        constants = login_module.complete_canva_sso.__code__.co_consts

        self.assertTrue(
            any(
                'input[name="username"][autocomplete*="username"]:visible' in value
                for value in constants
                if isinstance(value, str)
            )
        )


if __name__ == "__main__":
    unittest.main()
