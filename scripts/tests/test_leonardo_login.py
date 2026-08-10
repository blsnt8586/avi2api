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
    def __init__(self, responses: list[dict[str, object]]) -> None:
        self.responses = responses
        self.waits: list[int] = []

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


if __name__ == "__main__":
    unittest.main()
